package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	gosync "sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/arqueon/dankmail/core/api/server"
	"github.com/arqueon/dankmail/core/config"
	"github.com/arqueon/dankmail/core/ent"
	"github.com/arqueon/dankmail/core/internal/bus"
	"github.com/arqueon/dankmail/core/internal/contacts"
	"github.com/arqueon/dankmail/core/internal/i18n"
	"github.com/arqueon/dankmail/core/internal/ipc"
	"github.com/arqueon/dankmail/core/internal/notify"
	"github.com/arqueon/dankmail/core/internal/paths"
	"github.com/arqueon/dankmail/core/internal/rules"
	"github.com/arqueon/dankmail/core/internal/settings"
	dsync "github.com/arqueon/dankmail/core/internal/sync"
	"github.com/arqueon/dankmail/core/repo"
)

// daemon bundles every long-lived component. One instance per process.
type daemon struct {
	cfg      *config.Config
	db       *ent.Client
	repo     *repo.Repo
	bus      *bus.Bus
	queue    *dsync.Queue
	registry *registry
	notifier notify.Notifier
	settings *settings.Store

	dnd    atomic.Bool
	reload chan struct{}
	exit   context.CancelFunc
	flows  *flowRegistry
	// contactsReauth flags accounts (by id string) whose token lacks the
	// contacts scopes; the compose UI surfaces the re-consent hint.
	contactsReauth gosync.Map

	// UI lifecycle: the daemon owns the Quickshell process and respawns
	// it on demand (ui.show/ui.toggle) — the window is a view, the
	// daemon is the program.
	rootCtx  context.Context
	shellDir string
	uiMu     gosync.Mutex
	uiProc   *os.Process

	mu     gosync.Mutex
	engine *dsync.Engine
}

// runDaemon assembles and runs everything until SIGINT/SIGTERM or
// system.exit. shellDir, when non-empty, launches the Quickshell UI
// pointed at that config directory.
func runDaemon(shellDir string, hidden bool) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	for _, dir := range []string{paths.DataDir(), paths.ConfigDir()} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	dbPath := cfg.DatabasePath
	if dbPath == "" {
		dbPath = paths.DatabasePath()
	}

	rootCtx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	db, err := repo.OpenFile(rootCtx, dbPath)
	if err != nil {
		return err
	}
	defer db.Close()

	d := &daemon{
		cfg:      cfg,
		db:       db,
		repo:     repo.New(db),
		bus:      bus.New(),
		registry: newRegistry(cfg, db),
		notifier: notify.NewBest(),
		settings: settings.NewStore(paths.SettingsPath()),
		reload:   make(chan struct{}, 1),
		exit:     cancel,
		flows:    newFlowRegistry(),
	}
	d.queue = dsync.NewQueue(db, d.bus, d.currentPolicies)

	var wg gosync.WaitGroup

	// IPC server (fails fast if another daemon owns the socket).
	srv := ipc.NewServer(paths.SocketPath(), d.bus)
	d.registerIPC(srv)
	d.registerAccountIPC(srv)
	ipcErr := make(chan error, 1)
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := srv.Serve(rootCtx); err != nil {
			ipcErr <- err
			cancel()
		}
	}()

	// HTTP read API on an ephemeral localhost port, exported to the UI
	// via DMAIL_API_ADDR.
	if !d.cfg.DisableHTTP {
		httpSrv, ln, err := server.New(cfg.APIAddr, server.Deps{
			Repo: d.repo, Version: Version, DND: d.dnd.Load,
		})
		if err != nil {
			return err
		}
		apiAddr := ln.Addr().String()
		_ = os.Setenv("DMAIL_API_ADDR", apiAddr)
		slog.Info("http api listening", "addr", apiAddr)
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-rootCtx.Done()
			_ = httpSrv.Close()
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = httpSrv.Serve(ln)
		}()
	}

	// Notification bridge: bus events → desktop notifications.
	wg.Add(1)
	go func() {
		defer wg.Done()
		d.notificationLoop(rootCtx)
	}()

	// Retention/cleanup job (spec §4): prunes threads beyond retention
	// (starred/snoozed exempt) and sweeps finished ops.
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = dsync.NewJanitor(db, cfg.RetentionDays).Run(rootCtx)
	}()

	// Contacts for the compose autocomplete: mail-correspondent index
	// every 15 min; Google contacts (People API) daily. Accounts whose
	// token predates the contacts scopes are flagged for re-consent.
	wg.Add(1)
	go func() {
		defer wg.Done()
		d.contactsLoop(rootCtx)
	}()

	// Engine lifecycle with reload support.
	wg.Add(1)
	go func() {
		defer wg.Done()
		d.engineLoop(rootCtx)
	}()

	// Optional UI (respawnable later via ui.show/ui.toggle).
	d.rootCtx = rootCtx
	d.shellDir = shellDir
	if shellDir != "" {
		d.startUI(hidden)
	}

	slog.Info("dankmail daemon running", "version", Version, "db", dbPath, "socket", paths.SocketPath())
	<-rootCtx.Done()
	wg.Wait()
	select {
	case err := <-ipcErr:
		return err
	default:
		return nil
	}
}

// engineLoop (re)builds the provider registry and runs the sync engine,
// restarting it whenever system.reload fires.
func (d *daemon) engineLoop(ctx context.Context) {
	for {
		if err := d.registry.rebuild(ctx); err != nil {
			slog.Error("registry rebuild failed", "err", err)
		}
		scheduler := dsync.NewScheduler(d.db, d.bus, d.queue, true)
		engine := dsync.NewEngine(d.db, d.bus, d.queue, d.registry, scheduler, d.settings)
		d.mu.Lock()
		d.engine = engine
		d.mu.Unlock()

		ectx, cancel := context.WithCancel(ctx)
		done := make(chan struct{})
		go func() {
			defer close(done)
			if err := engine.Run(ectx); err != nil {
				slog.Error("engine stopped", "err", err)
			}
		}()

		select {
		case <-ctx.Done():
			cancel()
			<-done
			return
		case <-d.reload:
			slog.Info("reloading engine")
			cancel()
			<-done
		}
	}
}

// contactsLoop keeps the autocomplete sources fresh.
func (d *daemon) contactsLoop(ctx context.Context) {
	mailTick := time.NewTicker(15 * time.Minute)
	googleTick := time.NewTicker(24 * time.Hour)
	defer mailTick.Stop()
	defer googleTick.Stop()

	sweepGoogle := func() {
		accts, err := d.db.Account.Query().All(ctx)
		if err != nil {
			return
		}
		for _, a := range accts {
			hc, ok := d.registry.Client(a.ID)
			if !ok {
				continue
			}
			err := contacts.FetchGoogle(ctx, d.db, a.ID, hc)
			switch {
			case err == nil:
				d.contactsReauth.Store(a.ID.String(), false)
			case contacts.IsInsufficientScope(err):
				// Token predates the contacts scopes: flag it so the
				// compose UI can suggest a re-consent.
				d.contactsReauth.Store(a.ID.String(), true)
				slog.Info("google contacts need re-consent", "account", a.Email)
			default:
				slog.Debug("google contacts fetch failed", "account", a.Email, "err", err)
			}
		}
	}

	if err := contacts.IndexMail(ctx, d.db); err != nil && ctx.Err() == nil {
		slog.Warn("contact mail index failed", "err", err)
	}
	sweepGoogle()

	for {
		select {
		case <-ctx.Done():
			return
		case <-mailTick.C:
			if err := contacts.IndexMail(ctx, d.db); err != nil && ctx.Err() == nil {
				slog.Warn("contact mail index failed", "err", err)
			}
		case <-googleTick.C:
			sweepGoogle()
		}
	}
}

// googleContactsNeedReauth reports whether any account is flagged.
func (d *daemon) googleContactsNeedReauth() bool {
	needs := false
	d.contactsReauth.Range(func(_, v any) bool {
		if b, _ := v.(bool); b {
			needs = true
			return false
		}
		return true
	})
	return needs
}

// currentPolicies maps live settings onto the chained-action policies
// the queue consults on every Enqueue.
func (d *daemon) currentPolicies() rules.Policies {
	s := d.settings.Get()
	return rules.Policies{
		MarkReadOnPreview: s.MarkReadOnPreview,
		MarkReadOnReply:   s.MarkReadOnReply,
		MarkReadOnTrash:   s.MarkReadOnTrash,
		UnarchiveOnStar:   s.UnarchiveOnStar,
	}
}

// uiAlive reports whether the Quickshell child is still running.
func (d *daemon) uiAlive() bool {
	d.uiMu.Lock()
	defer d.uiMu.Unlock()
	return d.uiProc != nil
}

// startUI spawns Quickshell pointed at the configured shell dir. No-op
// when a UI is already running or the daemon is headless (no shell dir).
func (d *daemon) startUI(hidden bool) {
	d.uiMu.Lock()
	defer d.uiMu.Unlock()
	if d.shellDir == "" || d.uiProc != nil {
		return
	}
	cmd := exec.CommandContext(d.rootCtx, "qs", "-p", d.shellDir)
	cmd.Env = os.Environ()
	if hidden {
		cmd.Env = append(cmd.Env, "DMAIL_START_HIDDEN=1")
	}
	if err := cmd.Start(); err != nil {
		slog.Warn("quickshell not started", "err", err)
		return
	}
	d.uiProc = cmd.Process
	slog.Info("ui started", "pid", cmd.Process.Pid, "hidden", hidden)
	go func() {
		_ = cmd.Wait()
		d.uiMu.Lock()
		d.uiProc = nil
		d.uiMu.Unlock()
		slog.Info("ui exited; daemon keeps running (dmail show relaunches it)")
	}()
}

// ensureUIVisible relaunches the UI if it is gone; used by the
// ui.show/ui.toggle IPC handlers so the window can always come back
// while the daemon lives.
func (d *daemon) ensureUIVisible() bool {
	if d.uiAlive() {
		return false
	}
	d.startUI(false) // start with the window visible
	return true
}

// requestReload asks engineLoop for a registry rebuild + engine restart,
// re-reading the settings file along the way.
func (d *daemon) requestReload() {
	if err := d.settings.Reload(); err != nil {
		slog.Warn("settings reload failed", "err", err)
	}
	select {
	case d.reload <- struct{}{}:
	default:
	}
}

func (d *daemon) currentEngine() *dsync.Engine {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.engine
}

// notificationLoop turns bus events into desktop notifications, honoring
// DND (dropped for now; summary-on-exit lands in anillo 2).
func (d *daemon) notificationLoop(ctx context.Context) {
	id, ch := d.bus.Subscribe(128)
	defer d.bus.Unsubscribe(id)
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-ch:
			if !ok {
				return
			}
			d.handleEvent(ctx, ev)
		}
	}
}

func (d *daemon) handleEvent(ctx context.Context, ev bus.Event) {
	str := func(k string) string { s, _ := ev.Payload[k].(string); return s }
	switch ev.Topic {
	case "message.arrived":
		if d.dnd.Load() {
			return
		}
		if inInbox, ok := ev.Payload["inInbox"].(bool); ok && !inInbox {
			return // MVP: only INBOX notifies (NotifyRules land in anillo 2)
		}
		n := notify.Notification{
			AccountID: str("accountId"),
			ThreadID:  str("threadId"),
			Summary:   str("from"),
			Body:      str("subject") + " — " + str("snippet"),
			Urgency:   notify.UrgencyNormal,
			Actions:   d.notifyActions(),
			OnAction:  func(key string) { d.notificationAction(key, ev.Payload) },
		}
		if _, err := d.notifier.Send(n); err != nil {
			slog.Debug("notification failed", "err", err)
		}
	case "op.failed":
		_, _ = d.notifier.Send(notify.Notification{
			Summary: i18n.T("dankmail: operation failed"),
			Body:    fmt.Sprintf("%s: %s", str("opType"), str("error")),
			Urgency: notify.UrgencyCritical,
		})
	case "account.auth":
		_, _ = d.notifier.Send(notify.Notification{
			Summary: i18n.T("dankmail: account needs re-authentication"),
			Body:    i18n.T("Use the key button in Settings → Accounts, or run: dmail account reauth"),
			Urgency: notify.UrgencyCritical,
		})
	case "snooze.woke":
		if d.dnd.Load() {
			return
		}
		_, _ = d.notifier.Send(notify.Notification{
			Summary: i18n.T("Snoozed thread woke up"),
			Body:    str("subject"),
			Urgency: notify.UrgencyNormal,
		})
	}
}

// notifyActions maps the configured action keys (settings.json →
// notifyActions) to labeled buttons. Order is preserved; note most
// notification servers cap the visible buttons (commonly 3).
func (d *daemon) notifyActions() []notify.Action {
	labels := map[string]string{
		"read":    i18n.T("Mark read"),
		"archive": i18n.T("Archive"),
		"trash":   i18n.T("Trash"),
		"snooze":  i18n.T("Snooze"),
		"open":    i18n.T("Open in web"),
	}
	var out []notify.Action
	for _, key := range d.settings.Get().NotifyActions {
		if label, ok := labels[key]; ok {
			out = append(out, notify.Action{Key: key, Label: label})
		}
	}
	return out
}

// notificationAction routes inline notification buttons into the op
// queue / browser, using a background context: the notification may
// outlive the event that created it. "Trash" is exactly that — never a
// permanent delete (spec: nunca destructivo).
func (d *daemon) notificationAction(key string, payload map[string]any) {
	accountID, _ := payload["accountId"].(string)
	threadID, _ := payload["threadId"].(string)
	if accountID == "" || threadID == "" {
		return
	}
	ctx := context.Background()
	switch key {
	case "read":
		_ = d.enqueueByProviderIDs(ctx, accountID, dsync.OpMarkRead, []string{threadID})
	case "archive":
		_ = d.enqueueByProviderIDs(ctx, accountID, dsync.OpArchive, []string{threadID})
	case "trash":
		_ = d.enqueueByProviderIDs(ctx, accountID, dsync.OpTrash, []string{threadID})
	case "snooze":
		id, err := parseUUID(accountID)
		if err != nil {
			return
		}
		_ = d.queue.Enqueue(ctx, dsync.Op{
			AccountID: id, Type: dsync.OpSnooze, ThreadIDs: []string{threadID},
			Payload: dsync.OpPayload{Snooze: &dsync.SnoozePayload{
				Until:      d.settings.Get().SnoozeUntil(time.Now()),
				MarkUnread: true,
			}},
		})
	case "open":
		d.openThreadInBrowser(accountID, threadID)
	}
}

func (d *daemon) openThreadInBrowser(accountID, threadID string) {
	id, err := parseUUID(accountID)
	if err != nil {
		return
	}
	prov, ok := d.registry.Provider(id)
	if !ok {
		return
	}
	if url, ok := prov.WebLink(threadID, ""); ok {
		_ = exec.Command("xdg-open", url).Start()
	}
}

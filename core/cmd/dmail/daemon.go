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
	policies rules.Policies
	settings *settings.Store

	dnd    atomic.Bool
	reload chan struct{}
	exit   context.CancelFunc
	flows  *flowRegistry

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
		policies: rules.DefaultPolicies(),
		settings: settings.NewStore(paths.SettingsPath()),
		reload:   make(chan struct{}, 1),
		exit:     cancel,
		flows:    newFlowRegistry(),
	}
	d.queue = dsync.NewQueue(db, d.bus, d.policies)

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

	// Engine lifecycle with reload support.
	wg.Add(1)
	go func() {
		defer wg.Done()
		d.engineLoop(rootCtx)
	}()

	// Optional UI.
	if shellDir != "" {
		args := []string{"-p", shellDir}
		cmd := exec.CommandContext(rootCtx, "qs", args...)
		cmd.Env = os.Environ()
		if hidden {
			cmd.Env = append(cmd.Env, "DMAIL_START_HIDDEN=1")
		}
		if err := cmd.Start(); err != nil {
			slog.Warn("quickshell not started", "err", err)
		}
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
		engine := dsync.NewEngine(d.db, d.bus, d.queue, d.registry, scheduler)
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
			Summary: "dankmail: operación fallida",
			Body:    fmt.Sprintf("%s: %s", str("opType"), str("error")),
			Urgency: notify.UrgencyCritical,
		})
	case "account.auth":
		_, _ = d.notifier.Send(notify.Notification{
			Summary: "dankmail: cuenta requiere re-autenticación",
			Body:    "Ejecuta: dmail account reauth",
			Urgency: notify.UrgencyCritical,
		})
	case "snooze.woke":
		if d.dnd.Load() {
			return
		}
		_, _ = d.notifier.Send(notify.Notification{
			Summary: "Pospuesto despertó",
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
		"read":    "Marcar leído",
		"archive": "Archivar",
		"trash":   "Borrar",
		"snooze":  "Posponer",
		"open":    "Abrir en web",
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
// outlive the event that created it. "Borrar" is trash — never a
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
		mins := d.settings.Get().SnoozeMinutes
		_ = d.queue.Enqueue(ctx, dsync.Op{
			AccountID: id, Type: dsync.OpSnooze, ThreadIDs: []string{threadID},
			Payload: dsync.OpPayload{Snooze: &dsync.SnoozePayload{
				Until:      time.Now().Add(time.Duration(mins) * time.Minute),
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

package main

import (
	"context"
	"fmt"
	"net/url"
	"os/exec"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/arqueon/dankmail/core/api/server"
	"github.com/arqueon/dankmail/core/ent/thread"
	"github.com/arqueon/dankmail/core/internal/contacts"
	"github.com/arqueon/dankmail/core/internal/ipc"
	"github.com/arqueon/dankmail/core/internal/provider"
	dsync "github.com/arqueon/dankmail/core/internal/sync"
	"github.com/arqueon/dankmail/core/repo"
)

func parseUUID(s string) (uuid.UUID, error) { return uuid.Parse(s) }

// registerIPC wires every daemon-side method. Mutations flow through the
// queue; reads go through repo.
func (d *daemon) registerIPC(srv *ipc.Server) {
	deps := server.Deps{Repo: d.repo, Version: Version, DND: d.dnd.Load}

	srv.Register("accounts.list", func(ctx context.Context, _ map[string]any) (any, error) {
		return d.repo.Accounts(ctx)
	})

	srv.Register("threads.list", func(ctx context.Context, p map[string]any) (any, error) {
		f := repo.ThreadFilter{}
		f.UnreadOnly, _ = p["unread"].(bool)
		f.Starred, _ = p["starred"].(bool)
		f.InboxOnly, _ = p["inbox"].(bool)
		f.Query, _ = p["query"].(string)
		if f.Query != "" {
			// A search sweeps the whole cache, not just the inbox.
			f.InboxOnly = false
		}
		if limit, ok := p["limit"].(float64); ok {
			f.Limit = int(limit)
		}
		if s, ok := p["account"].(string); ok && s != "" {
			id, err := uuid.Parse(s)
			if err != nil {
				return nil, fmt.Errorf("bad account id")
			}
			f.AccountID = &id
		}
		return d.repo.ListThreads(ctx, f)
	})

	srv.Register("threads.get", func(ctx context.Context, p map[string]any) (any, error) {
		id, err := intParam(p, "id")
		if err != nil {
			return nil, err
		}
		return d.repo.GetThread(ctx, id)
	})

	// threads.previewOpened drives the mark-read-on-preview hook.
	srv.Register("threads.previewOpened", func(ctx context.Context, p map[string]any) (any, error) {
		if !d.settings.Get().MarkReadOnPreview {
			return "ok", nil
		}
		accountID, ptids, err := d.resolveThreads(ctx, p)
		if err != nil {
			return nil, err
		}
		return "ok", d.queue.Enqueue(ctx, dsync.Op{AccountID: accountID, Type: dsync.OpMarkRead, ThreadIDs: ptids})
	})

	for method, opType := range map[string]dsync.OpType{
		"ops.markRead":   dsync.OpMarkRead,
		"ops.markUnread": dsync.OpMarkUnread,
		"ops.star":       dsync.OpStar,
		"ops.unstar":     dsync.OpUnstar,
		"ops.archive":    dsync.OpArchive,
		"ops.unarchive":  dsync.OpUnarchive,
		"ops.trash":      dsync.OpTrash,
	} {
		srv.Register(method, d.simpleOpHandler(opType))
	}

	srv.Register("ops.snooze", func(ctx context.Context, p map[string]any) (any, error) {
		until, err := timeParam(p, "until")
		if err != nil {
			return nil, err
		}
		accountID, ptids, err := d.resolveThreads(ctx, p)
		if err != nil {
			return nil, err
		}
		return "ok", d.queue.Enqueue(ctx, dsync.Op{
			AccountID: accountID, Type: dsync.OpSnooze, ThreadIDs: ptids,
			Payload: dsync.OpPayload{Snooze: &dsync.SnoozePayload{Until: until, MarkUnread: true}},
		})
	})

	// ops.snoozePreset snoozes using the configured preset (settings
	// snoozePreset/snoozeMinutes) — the time math stays server-side so
	// bar widgets don't reimplement it.
	srv.Register("ops.snoozePreset", func(ctx context.Context, p map[string]any) (any, error) {
		accountID, ptids, err := d.resolveThreads(ctx, p)
		if err != nil {
			return nil, err
		}
		return "ok", d.queue.Enqueue(ctx, dsync.Op{
			AccountID: accountID, Type: dsync.OpSnooze, ThreadIDs: ptids,
			Payload: dsync.OpPayload{Snooze: &dsync.SnoozePayload{
				Until:      d.settings.Get().SnoozeUntil(time.Now()),
				MarkUnread: true,
			}},
		})
	})

	srv.Register("ops.reply", func(ctx context.Context, p map[string]any) (any, error) {
		id, err := intParam(p, "id")
		if err != nil {
			return nil, err
		}
		body, _ := p["body"].(string)
		if body == "" {
			return nil, fmt.Errorf("empty reply body")
		}
		replyAll, _ := p["replyAll"].(bool)

		detail, err := d.repo.GetThread(ctx, id)
		if err != nil {
			return nil, err
		}
		if len(detail.Messages) == 0 {
			return nil, fmt.Errorf("thread has no messages to reply to")
		}
		last := detail.Messages[len(detail.Messages)-1]
		accountID, err := uuid.Parse(detail.AccountID)
		if err != nil {
			return nil, err
		}
		return "ok", d.queue.Enqueue(ctx, dsync.Op{
			AccountID: accountID, Type: dsync.OpSendReply,
			ThreadIDs: []string{detail.ProviderThreadID},
			Payload: dsync.OpPayload{Reply: &provider.ReplyDraft{
				InReplyToMessageID: last.ProviderMessageID,
				Body:               body,
				ReplyAll:           replyAll,
			}},
		})
	})

	srv.Register("ops.compose", func(ctx context.Context, p map[string]any) (any, error) {
		accountStr, _ := p["account"].(string)
		accountID, err := uuid.Parse(accountStr)
		if err != nil {
			return nil, fmt.Errorf("bad account id")
		}
		draft := provider.ComposeDraft{}
		draft.Subject, _ = p["subject"].(string)
		draft.Body, _ = p["body"].(string)
		if to, ok := p["to"].([]any); ok {
			for _, t := range to {
				if s, ok := t.(string); ok {
					draft.To = append(draft.To, s)
				}
			}
		}
		if len(draft.To) == 0 {
			return nil, fmt.Errorf("no recipients")
		}
		return "ok", d.queue.Enqueue(ctx, dsync.Op{
			AccountID: accountID, Type: dsync.OpCompose,
			Payload: dsync.OpPayload{Compose: &draft},
		})
	})

	// ops.forward sends the thread's latest message to new recipients as a
	// fresh compose (not threaded): "Fwd:" subject, an optional note, then
	// the quoted original. Plain text only — attachments stay in the
	// webmail, same as reply.
	srv.Register("ops.forward", func(ctx context.Context, p map[string]any) (any, error) {
		id, err := intParam(p, "id")
		if err != nil {
			return nil, err
		}
		var to []string
		if arr, ok := p["to"].([]any); ok {
			for _, t := range arr {
				if s, ok := t.(string); ok && strings.TrimSpace(s) != "" {
					to = append(to, strings.TrimSpace(s))
				}
			}
		}
		if len(to) == 0 {
			return nil, fmt.Errorf("no recipients")
		}
		note, _ := p["body"].(string)

		detail, err := d.repo.GetThread(ctx, id)
		if err != nil {
			return nil, err
		}
		if len(detail.Messages) == 0 {
			return nil, fmt.Errorf("thread has no messages to forward")
		}
		last := detail.Messages[len(detail.Messages)-1]
		accountID, err := uuid.Parse(detail.AccountID)
		if err != nil {
			return nil, err
		}

		subject := detail.Subject
		if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(subject)), "fwd:") {
			subject = "Fwd: " + subject
		}
		var b strings.Builder
		if strings.TrimSpace(note) != "" {
			b.WriteString(note)
			b.WriteString("\n\n")
		}
		b.WriteString("---------- Forwarded message ----------\n")
		b.WriteString("From: " + last.From + "\n")
		b.WriteString("Date: " + last.Date.Format("Mon, 2 Jan 2006 15:04") + "\n")
		b.WriteString("Subject: " + detail.Subject + "\n")
		if len(last.To) > 0 {
			b.WriteString("To: " + strings.Join(last.To, ", ") + "\n")
		}
		b.WriteString("\n")
		b.WriteString(last.BodyText)

		draft := provider.ComposeDraft{To: to, Subject: subject, Body: b.String()}
		return "ok", d.queue.Enqueue(ctx, dsync.Op{
			AccountID: accountID, Type: dsync.OpCompose,
			Payload: dsync.OpPayload{Compose: &draft},
		})
	})

	srv.Register("dnd.on", func(ctx context.Context, _ map[string]any) (any, error) {
		d.dnd.Store(true)
		d.bus.Publish("dnd.changed", map[string]any{"enabled": true})
		return "on", nil
	})
	srv.Register("dnd.off", func(ctx context.Context, _ map[string]any) (any, error) {
		d.dnd.Store(false)
		d.bus.Publish("dnd.changed", map[string]any{"enabled": false})
		return "off", nil
	})
	srv.Register("dnd.status", func(ctx context.Context, _ map[string]any) (any, error) {
		return map[string]bool{"enabled": d.dnd.Load()}, nil
	})

	srv.Register("ui.show", func(ctx context.Context, _ map[string]any) (any, error) {
		if !d.ensureUIVisible() {
			d.bus.Publish("ui.show", nil)
		}
		return "ok", nil
	})
	srv.Register("ui.toggle", func(ctx context.Context, _ map[string]any) (any, error) {
		if !d.ensureUIVisible() {
			d.bus.Publish("ui.toggle", nil)
		}
		return "ok", nil
	})
	// ui.compose shows the window with the compose modal open (bar
	// widgets and scripts: `dmail ipc` or the plugin popout).
	srv.Register("ui.compose", func(ctx context.Context, _ map[string]any) (any, error) {
		d.ensureUIVisible()
		d.bus.Publish("ui.compose", nil)
		return "ok", nil
	})
	// ui.showThread shows the window focused on one thread (local id).
	srv.Register("ui.showThread", func(ctx context.Context, p map[string]any) (any, error) {
		id, err := intParam(p, "id")
		if err != nil {
			return nil, err
		}
		d.ensureUIVisible()
		d.bus.Publish("ui.showThread", map[string]any{"id": id})
		return "ok", nil
	})
	// threads.searchRemote sweeps the FULL mailbox history server-side
	// (providers implementing RemoteSearcher, i.e. Gmail) and ingests the
	// results as cache backfill — old threads become previewable and
	// triageable locally, with notifications suppressed.
	srv.Register("threads.searchRemote", func(ctx context.Context, p map[string]any) (any, error) {
		query, _ := p["query"].(string)
		if strings.TrimSpace(query) == "" {
			return nil, fmt.Errorf("empty query")
		}
		wanted, _ := p["account"].(string)

		accounts, err := d.repo.Accounts(ctx)
		if err != nil {
			return nil, err
		}
		rec := dsync.NewReconciler(d.db, d.bus)
		found := 0
		searched := 0
		for _, a := range accounts {
			if wanted != "" && a.ID != wanted {
				continue
			}
			id, err := parseUUID(a.ID)
			if err != nil {
				continue
			}
			prov, ok := d.registry.Provider(id)
			if !ok {
				continue
			}
			searcher, ok := prov.(provider.RemoteSearcher)
			if !ok {
				continue // provider can't search remotely; local cache only
			}
			searched++
			changes, err := searcher.SearchRemote(ctx, query, 25)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", a.Email, err)
			}
			if err := rec.Apply(ctx, id, changes); err != nil {
				return nil, err
			}
			found += len(changes.Upserted)
		}
		if searched == 0 {
			return nil, fmt.Errorf("no account supports remote search")
		}
		return map[string]any{"ingested": found}, nil
	})

	// ui.openSearch continues a local search in the account's webmail
	// (full history lives there; the local cache only spans retention).
	srv.Register("ui.openSearch", func(ctx context.Context, p map[string]any) (any, error) {
		query, _ := p["query"].(string)
		if strings.TrimSpace(query) == "" {
			return nil, fmt.Errorf("empty query")
		}
		accounts, err := d.repo.Accounts(ctx)
		if err != nil {
			return nil, err
		}
		wanted, _ := p["account"].(string)
		for _, a := range accounts {
			if wanted != "" && a.ID != wanted {
				continue
			}
			if a.Type == "gmail" {
				u := "https://mail.google.com/mail/u/0/?authuser=" + url.QueryEscape(a.Email) + "#search/" + url.PathEscape(query)
				_ = exec.Command("xdg-open", u).Start()
				return "ok", nil
			}
		}
		return nil, fmt.Errorf("no gmail account to search in the webmail")
	})

	srv.Register("ui.openLink", func(ctx context.Context, p map[string]any) (any, error) {
		id, err := intParam(p, "id")
		if err != nil {
			return nil, err
		}
		detail, err := d.repo.GetThread(ctx, id)
		if err != nil {
			return nil, err
		}
		d.openThreadInBrowser(detail.AccountID, detail.ProviderThreadID)
		return "ok", nil
	})

	// contacts.search feeds the compose "To" autocomplete: ranked merge
	// of mail correspondents and Google contacts.
	srv.Register("contacts.search", func(ctx context.Context, p map[string]any) (any, error) {
		query, _ := p["query"].(string)
		var accountID *uuid.UUID
		if s, ok := p["account"].(string); ok && s != "" {
			id, err := uuid.Parse(s)
			if err != nil {
				return nil, fmt.Errorf("bad account id")
			}
			accountID = &id
		}
		suggestions, err := contacts.Search(ctx, d.db, accountID, strings.TrimSpace(query), 8)
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"contacts":    suggestions,
			"needsReauth": d.googleContactsNeedReauth(),
		}, nil
	})

	srv.Register("settings.get", func(ctx context.Context, _ map[string]any) (any, error) {
		return d.settings.Get(), nil
	})
	srv.Register("settings.set", func(ctx context.Context, p map[string]any) (any, error) {
		s := d.settings.Get()
		if raw, ok := p["notifyActions"].([]any); ok {
			s.NotifyActions = nil
			for _, v := range raw {
				if a, ok := v.(string); ok {
					s.NotifyActions = append(s.NotifyActions, a)
				}
			}
		}
		if mins, ok := p["snoozeMinutes"].(float64); ok {
			s.SnoozeMinutes = int(mins)
		}
		if preset, ok := p["snoozePreset"].(string); ok && preset != "" {
			s.SnoozePreset = preset
		}
		for key, dst := range map[string]*bool{
			"markReadOnPreview": &s.MarkReadOnPreview,
			"markReadOnReply":   &s.MarkReadOnReply,
			"markReadOnTrash":   &s.MarkReadOnTrash,
			"unarchiveOnStar":   &s.UnarchiveOnStar,
		} {
			if v, ok := p[key].(bool); ok {
				*dst = v
			}
		}
		if err := d.settings.Update(s); err != nil {
			return nil, err
		}
		d.bus.Publish("settings.changed", nil)
		return d.settings.Get(), nil
	})

	srv.Register("system.status", func(ctx context.Context, _ map[string]any) (any, error) {
		return server.BuildStatus(ctx, deps)
	})
	srv.Register("system.sync", func(ctx context.Context, p map[string]any) (any, error) {
		engine := d.currentEngine()
		if engine == nil {
			return nil, fmt.Errorf("engine not running")
		}
		if s, ok := p["account"].(string); ok && s != "" {
			id, err := uuid.Parse(s)
			if err != nil {
				return nil, fmt.Errorf("bad account id")
			}
			return "ok", engine.SyncAccount(ctx, id)
		}
		accounts, err := d.repo.Accounts(ctx)
		if err != nil {
			return nil, err
		}
		for _, a := range accounts {
			id, err := uuid.Parse(a.ID)
			if err != nil {
				continue
			}
			if err := engine.SyncAccount(ctx, id); err != nil {
				return nil, fmt.Errorf("%s: %w", a.Email, err)
			}
		}
		return "ok", nil
	})
	srv.Register("system.reload", func(ctx context.Context, _ map[string]any) (any, error) {
		d.requestReload()
		return "ok", nil
	})
	srv.Register("system.exit", func(ctx context.Context, _ map[string]any) (any, error) {
		go d.exit()
		return "bye", nil
	})
}

// simpleOpHandler builds a handler for flag/archive/trash ops addressed
// by local thread IDs ({ids: [int]}).
func (d *daemon) simpleOpHandler(opType dsync.OpType) ipc.Handler {
	return func(ctx context.Context, p map[string]any) (any, error) {
		accountID, ptids, err := d.resolveThreads(ctx, p)
		if err != nil {
			return nil, err
		}
		return "ok", d.queue.Enqueue(ctx, dsync.Op{AccountID: accountID, Type: opType, ThreadIDs: ptids})
	}
}

// resolveThreads maps local thread IDs ({id: n} or {ids: [n...]}) to the
// owning account and its provider-native thread IDs. All threads must
// belong to one account (the UI batches per account).
func (d *daemon) resolveThreads(ctx context.Context, p map[string]any) (uuid.UUID, []string, error) {
	var ids []int
	if id, err := intParam(p, "id"); err == nil {
		ids = append(ids, id)
	}
	if raw, ok := p["ids"].([]any); ok {
		for _, v := range raw {
			if f, ok := v.(float64); ok {
				ids = append(ids, int(f))
			}
		}
	}
	if len(ids) == 0 {
		return uuid.Nil, nil, fmt.Errorf("missing thread id(s)")
	}
	rows, err := d.db.Thread.Query().
		Where(thread.IDIn(ids...)).
		WithAccount().
		All(ctx)
	if err != nil {
		return uuid.Nil, nil, err
	}
	if len(rows) == 0 {
		return uuid.Nil, nil, fmt.Errorf("threads not found")
	}
	accountID := rows[0].Edges.Account.ID
	ptids := make([]string, 0, len(rows))
	for _, t := range rows {
		if t.Edges.Account.ID != accountID {
			return uuid.Nil, nil, fmt.Errorf("threads span multiple accounts; batch per account")
		}
		ptids = append(ptids, t.ProviderThreadID)
	}
	return accountID, ptids, nil
}

// enqueueByProviderIDs is the notification-action path: it already has
// provider-native IDs.
func (d *daemon) enqueueByProviderIDs(ctx context.Context, accountID string, opType dsync.OpType, ptids []string) error {
	id, err := uuid.Parse(accountID)
	if err != nil {
		return err
	}
	return d.queue.Enqueue(ctx, dsync.Op{AccountID: id, Type: opType, ThreadIDs: ptids})
}

func intParam(p map[string]any, key string) (int, error) {
	if f, ok := p[key].(float64); ok {
		return int(f), nil
	}
	return 0, fmt.Errorf("missing integer param %q", key)
}

func timeParam(p map[string]any, key string) (time.Time, error) {
	s, ok := p[key].(string)
	if !ok {
		return time.Time{}, fmt.Errorf("missing time param %q", key)
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("bad %q: %w (want RFC3339)", key, err)
	}
	return t, nil
}

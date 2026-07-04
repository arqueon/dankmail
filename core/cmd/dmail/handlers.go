package main

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/arqueon/dankmail/core/api/server"
	"github.com/arqueon/dankmail/core/ent/thread"
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
		if !d.policies.MarkReadOnPreview {
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
		d.bus.Publish("ui.show", nil)
		return "ok", nil
	})
	srv.Register("ui.toggle", func(ctx context.Context, _ map[string]any) (any, error) {
		d.bus.Publish("ui.toggle", nil)
		return "ok", nil
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

package main

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/arqueon/dankmail/core/ent/account"
	"github.com/arqueon/dankmail/core/internal/bus"
	"github.com/arqueon/dankmail/core/internal/rules"
	dsync "github.com/arqueon/dankmail/core/internal/sync"
	"github.com/arqueon/dankmail/core/repo"
)

func TestUnspamSelectionRoutesIdenticalProviderIDsToTheirOwners(t *testing.T) {
	ctx := context.Background()
	db, err := repo.OpenFile(ctx, filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	b := bus.New()
	d := &daemon{db: db, bus: b, queue: dsync.NewQueue(db, b, rules.DefaultPolicies)}
	var ids []any
	for _, typ := range []string{"gmail", "microsoft"} {
		a := db.Account.Create().SetType(account.Type(typ)).SetEmail(typ + "@example.org").SaveX(ctx)
		th := db.Thread.Create().SetAccount(a).SetProviderThreadID("same-native-id").SetSubject("spam").SetLabels([]string{"SPAM"}).SetLastMessageAt(time.Now()).SetInInbox(false).SaveX(ctx)
		ids = append(ids, float64(th.ID))
	}
	// Missing selections are rejected before any optimistic state is changed.
	if err := d.enqueueThreadOp(ctx, map[string]any{"ids": append(append([]any{}, ids...), float64(9999))}, dsync.OpUnspam, dsync.OpPayload{}); err == nil || db.PendingOp.Query().CountX(ctx) != 0 {
		t.Fatal("invalid selection partly applied")
	}
	if err := d.enqueueThreadOp(ctx, map[string]any{"ids": append(ids, ids[0])}, dsync.OpUnspam, dsync.OpPayload{}); err != nil {
		t.Fatal(err)
	}
	ops := db.PendingOp.Query().WithAccount().AllX(ctx)
	if len(ops) != 2 || ops[0].Edges.Account.ID == ops[1].Edges.Account.ID {
		t.Fatal("operations not isolated per account")
	}
	for _, op := range ops {
		if len(op.ProviderThreadIds) != 1 || op.ProviderThreadIds[0] != "same-native-id" {
			t.Fatal("IDs duplicated or misrouted", op.ProviderThreadIds)
		}
	}
	for _, th := range db.Thread.Query().AllX(ctx) {
		if !th.InInbox || len(th.Labels) != 0 {
			t.Fatal("not rescued", th)
		}
	}
}

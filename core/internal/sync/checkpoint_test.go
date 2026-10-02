package sync

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/arqueon/dankmail/core/ent"
	"github.com/arqueon/dankmail/core/ent/thread"
	"github.com/arqueon/dankmail/core/internal/bus"
	"github.com/arqueon/dankmail/core/internal/provider"
	"github.com/google/uuid"
)

type checkpointProvider struct {
	provider.Provider
	sync func(context.Context, string) (provider.Changes, string, error)
}

func (p *checkpointProvider) Sync(ctx context.Context, cursor string) (provider.Changes, string, error) {
	return p.sync(ctx, cursor)
}
func TestCheckpointAtomicAndFullPrunesOnlyAtCompletion(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)
	a := mkAccount(t, db)
	b := bus.New()
	r := NewReconciler(db, b)
	mkThread(t, db, a, threadOpts{id: "old"})
	first := provider.Changes{FullResync: true, Incomplete: true, Upserted: []provider.ThreadDelta{delta("first", nil)}}
	if err := r.ApplySync(ctx, a.ID, first, "checkpoint"); err != nil {
		t.Fatal(err)
	}
	saved := db.Account.GetX(ctx, a.ID)
	if saved.SyncCursor != "checkpoint" || saved.LastSyncAt != nil || db.Thread.Query().CountX(ctx) != 2 {
		t.Fatal("partial batch pruned cache or advanced completion")
	}
	last := provider.Changes{FullResync: true, SnapshotThreadIDs: []string{"first"}, Upserted: []provider.ThreadDelta{delta("last", nil)}}
	if err := r.ApplySync(ctx, a.ID, last, "complete"); err != nil {
		t.Fatal(err)
	}
	if db.Thread.Query().Where(thread.ProviderThreadIDEQ("old")).ExistX(ctx) || db.Thread.Query().CountX(ctx) != 2 || db.Account.GetX(ctx, a.ID).LastSyncAt == nil {
		t.Fatal("final snapshot did not preserve earlier batches/prune stale")
	}
	db.Account.Use(func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(ctx context.Context, m ent.Mutation) (ent.Value, error) {
			return nil, errors.New("simulated checkpoint write failure")
		})
	})
	if err := r.ApplySync(ctx, a.ID, provider.Changes{Incomplete: true, Upserted: []provider.ThreadDelta{delta("rolled-back", nil)}}, "bad"); err == nil {
		t.Fatal("expected write failure")
	}
	if db.Thread.Query().Where(thread.ProviderThreadIDEQ("rolled-back")).ExistX(ctx) || db.Account.GetX(ctx, a.ID).SyncCursor != "complete" {
		t.Fatal("cache and cursor were not atomic")
	}
}
func TestEnginePersistsProgressOnErrorAndNewEngineResumes(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)
	a := mkAccount(t, db)
	b := bus.New()
	quota := errors.New("quota")
	p := &checkpointProvider{sync: func(ctx context.Context, cursor string) (provider.Changes, string, error) {
		return provider.Changes{Incomplete: true, Upserted: []provider.ThreadDelta{delta("one", nil)}}, "resume", quota
	}}
	reg := regMap{a.ID: p}
	e := NewEngine(db, b, nil, reg, nil, nil)
	if err := e.SyncAccount(ctx, a.ID); !errors.Is(err, quota) {
		t.Fatal(err)
	}
	saved := db.Account.GetX(ctx, a.ID)
	if saved.SyncCursor != "resume" || saved.LastSyncAt != nil || saved.LastError == "" || db.Thread.Query().CountX(ctx) != 1 {
		t.Fatal("progress discarded")
	}
	p.sync = func(ctx context.Context, cursor string) (provider.Changes, string, error) {
		if cursor != "resume" {
			t.Fatal("did not resume", cursor)
		}
		return provider.Changes{}, "done", nil
	}
	e = NewEngine(db, b, nil, reg, nil, nil)
	if err := e.SyncAccount(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	saved = db.Account.GetX(ctx, a.ID)
	if saved.LastSyncAt == nil || saved.LastError != "" || saved.SyncCursor != "done" {
		t.Fatal("completion not recorded")
	}
}
func TestSyncAllHealthyAccountDoesNotWaitForBlockedProvider(t *testing.T) {
	db := testDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*3)
	defer cancel()
	a := mkAccount(t, db)
	other := db.Account.Create().SetType("microsoft").SetEmail("other@example.org").SaveX(ctx)
	healthy := make(chan struct{})
	slow := &checkpointProvider{sync: func(ctx context.Context, _ string) (provider.Changes, string, error) {
		select {
		case <-healthy:
			return provider.Changes{}, "", nil
		case <-ctx.Done():
			return provider.Changes{}, "", ctx.Err()
		}
	}}
	fast := &checkpointProvider{sync: func(context.Context, string) (provider.Changes, string, error) {
		close(healthy)
		return provider.Changes{}, "done", nil
	}}
	e := NewEngine(db, bus.New(), nil, regMap{uuid.UUID(a.ID): slow, other.ID: fast}, nil, nil)
	if err := e.SyncAll(ctx, false); err != nil {
		t.Fatal(err)
	}
}

func TestExistingMessagesAcquireSentFlagWithoutDuplicating(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)
	a := mkAccount(t, db)
	r := NewReconciler(db, bus.New())
	d := delta("sent", nil)
	d.Messages = []provider.MessageDelta{{MessageID: "m", Date: 50, BodyText: "existing"}}
	if err := r.Apply(ctx, a.ID, provider.Changes{Backfill: true, Upserted: []provider.ThreadDelta{d}}); err != nil {
		t.Fatal(err)
	}
	d.Messages[0].IsSent = true
	if err := r.Apply(ctx, a.ID, provider.Changes{Backfill: true, Upserted: []provider.ThreadDelta{d}}); err != nil {
		t.Fatal(err)
	}
	if db.Message.Query().CountX(ctx) != 1 || !db.Message.Query().OnlyX(ctx).IsSent {
		t.Fatal("legacy sent mail not upgraded")
	}
}

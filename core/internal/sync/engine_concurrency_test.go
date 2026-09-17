package sync

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/arqueon/dankmail/core/internal/bus"
	"github.com/arqueon/dankmail/core/internal/provider"
)

type gatedSyncProvider struct {
	*fakeProvider
	entered chan string
	release chan struct{}
}

func (p *gatedSyncProvider) Sync(ctx context.Context, cursor string) (provider.Changes, string, error) {
	select {
	case p.entered <- cursor:
	case <-ctx.Done():
		return provider.Changes{}, "", ctx.Err()
	}
	select {
	case <-p.release:
	case <-ctx.Done():
		return provider.Changes{}, "", ctx.Err()
	}
	return provider.Changes{}, "updated", nil
}

func TestConcurrentSyncAndFullResetAreSerialized(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)
	acct := mkAccount(t, db)
	if _, err := db.Account.UpdateOne(acct).SetSyncCursor("original").Save(ctx); err != nil {
		t.Fatal(err)
	}
	p := &gatedSyncProvider{fakeProvider: newFakeProvider(acct.ID.String()), entered: make(chan string, 2), release: make(chan struct{})}
	e := NewEngine(db, bus.New(), nil, regMap{acct.ID: p}, nil, nil)
	done := make(chan error, 1)
	go func() { done <- e.SyncAccount(ctx, acct.ID) }()
	select {
	case cursor := <-p.entered:
		if cursor != "original" {
			t.Fatal(cursor)
		}
	case <-time.After(time.Second):
		t.Fatal("sync did not start")
	}
	// A full-sync request waiting behind an active sync must remain cancellable
	// and must not reset the persisted cursor before acquiring the lock.
	waiting, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel()
	if err := e.FullSyncAccount(waiting, acct.ID); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("wait error %v", err)
	}
	stored, err := db.Account.Get(ctx, acct.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.SyncCursor != "original" {
		t.Fatalf("cursor reset while busy: %q", stored.SyncCursor)
	}
	close(p.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := e.SyncAccount(ctx, acct.ID); err != nil {
		t.Fatal(err)
	}
	if cursor := <-p.entered; cursor != "updated" {
		t.Fatalf("stale cursor %q", cursor)
	}
	if err := e.FullSyncAccount(ctx, acct.ID); err != nil {
		t.Fatal(err)
	}
	if cursor := <-p.entered; cursor != "" {
		t.Fatalf("full cursor %q", cursor)
	}
}

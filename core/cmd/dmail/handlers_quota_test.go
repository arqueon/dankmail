package main

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/arqueon/dankmail/core/internal/bus"
	"github.com/arqueon/dankmail/core/internal/provider"
	"github.com/arqueon/dankmail/core/repo"
)

type searchProvider struct {
	provider.Provider
	calls int
	err   error
}

func (p *searchProvider) SearchRemote(context.Context, string, int) (provider.Changes, error) {
	p.calls++
	return provider.Changes{Backfill: true}, p.err
}

func TestRemoteSearchContinuesAfterAccountDeferral(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	db, err := repo.OpenFile(ctx, filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	d := &daemon{db: db, repo: repo.New(db), bus: bus.New(), registry: newRegistry(nil, db)}
	for _, email := range []string{"one@example.com", "two@example.com"} {
		if _, err := db.Account.Create().SetType("gmail").SetEmail(email).Save(ctx); err != nil {
			t.Fatal(err)
		}
	}
	// Use the handler's actual account order, so the failed one is first.
	accounts, err := d.repo.Accounts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	failure := errors.New("Gmail retry deferred for 1h")
	blocked, healthy := &searchProvider{err: failure}, &searchProvider{}
	for i, p := range []*searchProvider{blocked, healthy} {
		id, err := parseUUID(accounts[i].ID)
		if err != nil {
			t.Fatal(err)
		}
		d.registry.providers[id] = p
	}
	_, err = d.searchRemote(ctx, map[string]any{"query": "test"})
	if !errors.Is(err, failure) || blocked.calls != 1 || healthy.calls != 1 {
		t.Fatalf("failed account prevented next search: blocked=%d healthy=%d err=%v", blocked.calls, healthy.calls, err)
	}
	blocked.err = nil
	if _, err := d.searchRemote(ctx, map[string]any{"query": "test"}); err != nil {
		t.Fatalf("healthy search failed: %v", err)
	}
}

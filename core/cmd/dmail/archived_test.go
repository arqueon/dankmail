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

type archiveProvider struct {
	provider.Provider
	tokens []string
	err    error
	next   string
}

func (p *archiveProvider) ArchivedPage(_ context.Context, token string) (provider.Changes, string, error) {
	p.tokens = append(p.tokens, token)
	return provider.Changes{Upserted: []provider.ThreadDelta{{ThreadID: "old", LastMessage: time.Now().AddDate(0, 0, -50).Unix(), InInbox: false, Starred: false, MessageCount: 1}}}, p.next, p.err
}
func TestArchivedPagesAdvanceAccountsIndependently(t *testing.T) {
	ctx := context.Background()
	db, err := repo.OpenFile(ctx, filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	d := &daemon{db: db, repo: repo.New(db), bus: bus.New(), registry: newRegistry(nil, db)}
	a := db.Account.Create().SetType("gmail").SetEmail("one@example.org").SaveX(ctx)
	b := db.Account.Create().SetType("gmail").SetEmail("two@example.org").SaveX(ctx)
	one, two := &archiveProvider{next: "page2"}, &archiveProvider{err: errors.New("quota")}
	d.registry.providers[a.ID] = one
	d.registry.providers[b.ID] = two
	raw, err := d.fetchArchived(ctx, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	r := raw.(map[string]any)
	tokens := r["next"].(map[string]string)
	if tokens[a.ID.String()] != "page2" || tokens[b.ID.String()] != "" || len(tokens) != 2 || r["ingested"] != 1 || r["warning"] == "" {
		t.Fatalf("result=%v", r)
	}
	rows, err := d.repo.ListThreads(ctx, repo.ThreadFilter{ArchivedOnly: true})
	if err != nil || len(rows) != 1 || rows[0].Starred {
		t.Fatalf("rows=%v err=%v", rows, err)
	}
	one.next = ""
	two.err = nil
	raw, err = d.fetchArchived(ctx, map[string]any{"next": map[string]any{a.ID.String(): "page2", b.ID.String(): ""}})
	if err != nil {
		t.Fatal(err)
	}
	if len(raw.(map[string]any)["next"].(map[string]string)) != 0 || one.tokens[1] != "page2" || two.tokens[1] != "" {
		t.Fatal(raw)
	}
	_, err = d.fetchArchived(ctx, map[string]any{"next": map[string]any{}})
	if err != nil || len(one.tokens) != 2 || len(two.tokens) != 2 {
		t.Fatal("completed accounts fetched again")
	}
	if _, err = d.fetchArchived(ctx, map[string]any{"account": "invalid"}); err == nil {
		t.Fatal("invalid account accepted")
	}
}

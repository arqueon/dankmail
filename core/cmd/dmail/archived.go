package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/arqueon/dankmail/core/internal/provider"
	dsync "github.com/arqueon/dankmail/core/internal/sync"
)

// Each account advances independently: failed pages retain their token for a
// retry, while successful accounts are not fetched again on the next page.
func (d *daemon) fetchArchived(ctx context.Context, p map[string]any) (any, error) {
	return d.fetchMailboxHistory(ctx, p, false)
}

func (d *daemon) fetchSent(ctx context.Context, p map[string]any) (any, error) {
	return d.fetchMailboxHistory(ctx, p, true)
}

func (d *daemon) fetchMailboxHistory(ctx context.Context, p map[string]any, sent bool) (any, error) {
	wanted, _ := p["account"].(string)
	if wanted != "" {
		if _, err := parseUUID(wanted); err != nil {
			return nil, fmt.Errorf("bad account id")
		}
	}
	tokens, continuing := p["next"].(map[string]any)
	accounts, err := d.repo.Accounts(ctx)
	if err != nil {
		return nil, err
	}
	next := map[string]string{}
	warnings := []string{}
	ingested, supported := 0, 0
	rec := dsync.NewReconciler(d.db, d.bus)
	for _, a := range accounts {
		if wanted != "" && a.ID != wanted {
			continue
		}
		token := ""
		if continuing {
			raw, ok := tokens[a.ID]
			if !ok {
				continue
			}
			token, ok = raw.(string)
			if !ok {
				return nil, fmt.Errorf("bad archive page token")
			}
		}
		id, _ := parseUUID(a.ID)
		prov, ok := d.registry.Provider(id)
		if !ok {
			next[a.ID] = token
			warnings = append(warnings, "Account provider unavailable")
			continue
		}
		var page func(context.Context, string) (provider.Changes, string, error)
		if sent {
			if pager, ok := prov.(provider.SentPager); ok {
				page = pager.SentPage
			}
		} else {
			if pager, ok := prov.(provider.ArchivedPager); ok {
				page = pager.ArchivedPage
			}
		}
		if page == nil {
			continue
		}
		supported++
		changes, cursor, err := page(ctx, token)
		if len(changes.Upserted) > 0 {
			changes.Backfill = true
			changes.FullResync = false
			if applyErr := rec.Apply(ctx, id, changes); applyErr != nil {
				next[a.ID] = token
				warnings = append(warnings, applyErr.Error())
				continue
			}
			ingested += len(changes.Upserted)
		}
		if err != nil {
			if cursor == "" {
				cursor = token
			}
			next[a.ID] = cursor
			warnings = append(warnings, err.Error())
			continue
		}
		if cursor != "" {
			next[a.ID] = cursor
		}
	}
	return map[string]any{"next": next, "ingested": ingested, "supported": supported, "warning": strings.Join(warnings, "; ")}, nil
}

package sync

import (
	"context"
	"errors"
	"testing"

	"github.com/arqueon/dankmail/core/ent/account"
	"github.com/arqueon/dankmail/core/errdefs"
	"github.com/arqueon/dankmail/core/internal/bus"
)

func TestSyncRetriesTransientAuthTransportFailure(t *testing.T) {
	for _, kind := range []errdefs.Kind{errdefs.KindNetwork, errdefs.KindRateLimit, errdefs.KindAuth} {
		t.Run(kind.String(), func(t *testing.T) {
			ctx := context.Background()
			db := testDB(t)
			acct := mkAccount(t, db)
			prov := newFakeProvider(acct.ID.String())
			prov.fail("Sync", errdefs.Wrap(kind, errors.New("token endpoint failed")))
			engine := NewEngine(db, bus.New(), nil, regMap{acct.ID: prov}, nil, nil)
			if err := engine.SyncAccount(ctx, acct.ID); err == nil {
				t.Fatal("missing sync error")
			}
			got, err := db.Account.Get(ctx, acct.ID)
			if err != nil {
				t.Fatal(err)
			}
			if kind == errdefs.KindAuth {
				if got.Status != account.StatusAuthError || !got.NeedsReauth {
					t.Fatal("revoked token did not request consent")
				}
				return
			}
			if got.Status != account.StatusActive || got.NeedsReauth {
				t.Fatal("transient failure requested consent")
			}
			prov.fail("Sync", nil)
			if err := engine.SyncAccount(ctx, acct.ID); err != nil {
				t.Fatal(err)
			}
			got, err = db.Account.Get(ctx, acct.ID)
			if err != nil {
				t.Fatal(err)
			}
			if got.LastSyncAt == nil || got.LastError != "" {
				t.Fatal("next poll did not recover")
			}
		})
	}
}

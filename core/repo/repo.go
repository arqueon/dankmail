// Package repo wraps the Ent client behind domain-shaped queries and
// mutations, and owns database opening/migration. SQLite runs in WAL
// mode with foreign keys on; the driver is CGO-free (modernc.org/sqlite).
package repo

import (
	"context"
	"database/sql"
	"fmt"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"

	"github.com/arqueon/dankmail/core/ent"

	_ "modernc.org/sqlite"
)

// OpenFile opens (creating if needed) the SQLite database at path and
// runs schema migration.
func OpenFile(ctx context.Context, path string) (*ent.Client, error) {
	dsn := fmt.Sprintf("file:%s?cache=shared&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(ON)", path)
	return open(ctx, dsn)
}

// OpenMemory opens an in-memory database (tests).
func OpenMemory(ctx context.Context) (*ent.Client, error) {
	return open(ctx, "file:dankmail?mode=memory&cache=shared&_pragma=foreign_keys(ON)")
}

func open(ctx context.Context, dsn string) (*ent.Client, error) {
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	drv := entsql.OpenDB(dialect.SQLite, db)
	client := ent.NewClient(ent.Driver(drv))
	if err := client.Schema.Create(ctx); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return client, nil
}

// Repo is the domain-facing handle. Query/mutation methods are organized
// per domain in sibling files (threads_queries.go, ops_mutations.go, ...)
// as they land in Anillo 1.
type Repo struct {
	client *ent.Client
}

func New(client *ent.Client) *Repo { return &Repo{client: client} }

// WithTx runs fn inside a transaction, rolling back on error or panic.
func (r *Repo) WithTx(ctx context.Context, fn func(tx *ent.Tx) error) error {
	tx, err := r.client.Tx(ctx)
	if err != nil {
		return err
	}
	defer func() {
		if v := recover(); v != nil {
			_ = tx.Rollback()
			panic(v)
		}
	}()
	if err := fn(tx); err != nil {
		if rerr := tx.Rollback(); rerr != nil {
			err = fmt.Errorf("%w (rollback: %v)", err, rerr)
		}
		return err
	}
	return tx.Commit()
}

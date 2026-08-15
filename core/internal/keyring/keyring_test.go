package keyring

import (
	"context"
	"database/sql"
	"fmt"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/google/uuid"

	"github.com/arqueon/dankmail/core/ent"
	entaccount "github.com/arqueon/dankmail/core/ent/account"

	_ "modernc.org/sqlite"
)

func testDB(t *testing.T) *ent.Client {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared&_pragma=foreign_keys(ON)", uuid.NewString())
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxIdleConns(1)
	client := ent.NewClient(ent.Driver(entsql.OpenDB(dialect.SQLite, db)))
	if err := client.Schema.Create(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func TestFallbackSecretStore(t *testing.T) {
	db := testDB(t)
	SetFallbackDB(db)
	t.Cleanup(func() { SetFallbackDB(nil) })

	ctx := context.Background()
	acct, err := db.Account.Create().
		SetType(entaccount.TypeGmail).
		SetEmail("test@example.com").
		Save(ctx)
	if err != nil {
		t.Fatal(err)
	}

	id := acct.ID.String()

	if err := setFallback(id, KeyOAuthToken, "token-json-data"); err != nil {
		t.Fatalf("setFallback failed: %v", err)
	}

	val, err := getFallback(id, KeyOAuthToken)
	if err != nil {
		t.Fatalf("getFallback failed: %v", err)
	}
	if val != "token-json-data" {
		t.Fatalf("got %q, want %q", val, "token-json-data")
	}

	// Update existing secret
	if err := setFallback(id, KeyOAuthToken, "updated-token-data"); err != nil {
		t.Fatalf("update setFallback failed: %v", err)
	}
	val, err = getFallback(id, KeyOAuthToken)
	if err != nil || val != "updated-token-data" {
		t.Fatalf("got %q, want %q (err: %v)", val, "updated-token-data", err)
	}

	// Delete secret
	deleteFallback(id, KeyOAuthToken)
	if _, err := getFallback(id, KeyOAuthToken); err == nil {
		t.Fatal("expected error after deleteFallback, got nil")
	}
}

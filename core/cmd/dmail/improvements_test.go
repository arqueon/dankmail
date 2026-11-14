package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	zkeyring "github.com/zalando/go-keyring"

	"github.com/arqueon/dux-mail/core/config"
	"github.com/arqueon/dux-mail/core/ent/account"
	"github.com/arqueon/dux-mail/core/internal/bus"
	"github.com/arqueon/dux-mail/core/internal/oauth"
	"github.com/arqueon/dux-mail/core/internal/provider"
	"github.com/arqueon/dux-mail/core/internal/rules"
	dsync "github.com/arqueon/dux-mail/core/internal/sync"
	"github.com/arqueon/dux-mail/core/repo"
)

type fakeAttProvider struct {
	provider.Provider
	data map[string][]byte
}

func (f *fakeAttProvider) FetchAttachment(_ context.Context, messageID, attachmentID, filename string) ([]byte, error) {
	return f.data[messageID+"|"+attachmentID+"|"+filename], nil
}

func TestSnoozedFilterAndUnsnooze(t *testing.T) {
	ctx := context.Background()
	db, err := repo.OpenFile(ctx, filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	b := bus.New()
	r := repo.New(db)
	d := &daemon{db: db, repo: r, bus: b, queue: dsync.NewQueue(db, b, rules.DefaultPolicies)}

	acct := db.Account.Create().SetType(account.TypeGmail).SetEmail("u@example.org").SaveX(ctx)
	later := time.Now().Add(4 * time.Hour).UTC().Truncate(time.Second)
	sooner := time.Now().Add(1 * time.Hour).UTC().Truncate(time.Second)

	tLater := db.Thread.Create().SetAccount(acct).SetProviderThreadID("th-later").SetSubject("Later").
		SetLastMessageAt(time.Now()).SetInInbox(false).SetSnoozedUntil(later).SaveX(ctx)
	tSooner := db.Thread.Create().SetAccount(acct).SetProviderThreadID("th-sooner").SetSubject("Sooner").
		SetLastMessageAt(time.Now()).SetInInbox(false).SetSnoozedUntil(sooner).SaveX(ctx)
	_ = db.Thread.Create().SetAccount(acct).SetProviderThreadID("th-inbox").SetSubject("Inbox").
		SetLastMessageAt(time.Now()).SetInInbox(true).SaveX(ctx)

	snoozed, err := r.ListThreads(ctx, repo.ThreadFilter{SnoozedOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(snoozed) != 2 || snoozed[0].ID != tSooner.ID || snoozed[1].ID != tLater.ID {
		t.Fatalf("expected snoozed threads ordered by wake time, got %+v", snoozed)
	}

	if err := d.enqueueThreadOp(ctx, map[string]any{"id": float64(tSooner.ID)}, dsync.OpSnoozeWake, dsync.OpPayload{}); err != nil {
		t.Fatal(err)
	}
	woken := db.Thread.GetX(ctx, tSooner.ID)
	if !woken.InInbox || woken.SnoozedUntil != nil {
		t.Fatalf("expected thread woken into inbox with cleared SnoozedUntil, got %+v", woken)
	}
}

func TestAttachmentCacheAndSaveAs(t *testing.T) {
	ctx := context.Background()
	db, err := repo.OpenFile(ctx, filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	r := repo.New(db)
	reg := newRegistry(&config.Config{}, db)
	acct := db.Account.Create().SetType(account.TypeGmail).SetEmail("u@example.org").SaveX(ctx)
	reg.providers[acct.ID] = &fakeAttProvider{
		data: map[string][]byte{"m-1|att-1|report.pdf": []byte("%PDF-1.7 test payload")},
	}
	d := &daemon{db: db, repo: r, registry: reg}

	th := db.Thread.Create().SetAccount(acct).SetProviderThreadID("th-1").SetSubject("Doc").SetLastMessageAt(time.Now()).SaveX(ctx)
	db.Message.Create().SetThread(th).SetProviderMessageID("m-1").SetFrom("a@b.org").SetDate(time.Now()).
		SetAttachments([]provider.AttachmentMeta{{Filename: "report.pdf", MimeType: "application/pdf", Size: 21, AttachmentID: "att-1"}}).
		SaveX(ctx)

	cachedPath, cleanName, err := d.ensureAttachmentCached(ctx, map[string]any{
		"threadId":     float64(th.ID),
		"messageId":    "m-1",
		"attachmentId": "att-1",
		"filename":     "../report.pdf",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(filepath.Dir(cachedPath))
	if cleanName != "report.pdf" {
		t.Fatalf("expected sanitized filename report.pdf, got %q", cleanName)
	}
	st, err := os.Stat(cachedPath)
	if err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("expected 0600 cached file, got %v (err=%v)", st, err)
	}

	destDir := t.TempDir()
	out1 := uniqueDestPath(destDir, cleanName)
	if err := os.WriteFile(out1, []byte("existing"), 0o600); err != nil {
		t.Fatal(err)
	}
	out2 := uniqueDestPath(destDir, cleanName)
	if filepath.Base(out2) != "report (1).pdf" {
		t.Fatalf("expected collision-safe filename report (1).pdf, got %q", filepath.Base(out2))
	}
}

func TestSetupGuideReflectsSavedDefaultClient(t *testing.T) {
	zkeyring.MockInit()
	t.Setenv("DMAIL_OAUTH_CLIENTS_PATH", filepath.Join(t.TempDir(), "oauth-clients.json"))
	if err := oauth.SaveDefaultClientCreds("google", oauth.ClientCreds{ClientID: "g-cid", ClientSecret: "g-sec"}); err != nil {
		t.Fatal(err)
	}
	c, err := oauth.ResolveGoogleClient(oauth.ClientCreds{}, "", "", uuid.Nil.String())
	if err != nil || c.ClientID != "g-cid" {
		t.Fatalf("expected g-cid, got %+v (err=%v)", c, err)
	}
}

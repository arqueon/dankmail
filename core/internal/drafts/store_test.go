package drafts

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/arqueon/dankmail/core/internal/provider"
	"github.com/arqueon/dankmail/core/models"
)

func TestStoreSaveListAndMergeRemote(t *testing.T) {
	dir := t.TempDir()
	attachPath := filepath.Join(dir, "sample.txt")
	if err := os.WriteFile(attachPath, []byte("hello attachment"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := NewStore(filepath.Join(dir, "drafts.json"))

	saved, err := store.Save(models.DraftView{
		AccountID: "acct-1",
		To:        []string{"ada@example.org"},
		Subject:   "Borrador inicial",
		Body:      "Contenido de prueba",
		Attachments: []models.AttachmentView{
			{Path: attachPath},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if saved.ID == "" || len(saved.Attachments) != 1 || saved.Attachments[0].Filename != "sample.txt" || saved.Attachments[0].Size != 16 {
		t.Fatalf("unexpected saved draft: %+v", saved)
	}

	reloaded := NewStore(filepath.Join(dir, "drafts.json"))
	list := reloaded.List("acct-1", "inicial")
	if len(list) != 1 || list[0].ID != saved.ID {
		t.Fatalf("expected 1 persisted draft, got %+v", list)
	}

	reloaded.MergeRemote("acct-1", []provider.DraftItem{
		{
			ProviderDraftID: "gd-1",
			To:              []string{"bob@example.org"},
			Subject:         "Remote draft",
			Body:            "Body from server",
			UpdatedAt:       1700000500,
		},
	})
	if got := len(reloaded.List("acct-1", "")); got != 2 {
		t.Fatalf("expected 2 drafts after remote merge, got %d", got)
	}
}

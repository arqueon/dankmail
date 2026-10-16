package drafts

import (
	"encoding/json"
	"mime"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/arqueon/dankmail/core/internal/provider"
	"github.com/arqueon/dankmail/core/models"
)

// Store persists drafts locally on disk and merges remote provider drafts.
type Store struct {
	path  string
	mu    sync.Mutex
	items map[string]models.DraftView
}

func NewStore(path string) *Store {
	s := &Store{
		path:  path,
		items: map[string]models.DraftView{},
	}
	_ = s.load()
	return s
}

func (s *Store) load() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.path == "" {
		return nil
	}
	raw, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var list []models.DraftView
	if err := json.Unmarshal(raw, &list); err != nil {
		return err
	}
	s.items = make(map[string]models.DraftView, len(list))
	for _, d := range list {
		if d.ID != "" {
			s.items[d.ID] = d
		}
	}
	return nil
}

func (s *Store) persistLocked() error {
	if s.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	list := make([]models.DraftView, 0, len(s.items))
	for _, d := range s.items {
		list = append(list, d)
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].UpdatedAt.After(list[j].UpdatedAt)
	})
	raw, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func makeSnippet(body string) string {
	clean := strings.Join(strings.Fields(body), " ")
	if len(clean) > 140 {
		return clean[:140] + "…"
	}
	return clean
}

// NormalizeAttachments resolves file metadata (filename, size, mimeType) for
// attachments backed by local file paths.
func NormalizeAttachments(in []models.AttachmentView) []models.AttachmentView {
	if len(in) == 0 {
		return nil
	}
	out := make([]models.AttachmentView, 0, len(in))
	for _, a := range in {
		cleanPath := strings.TrimPrefix(strings.TrimSpace(a.Path), "file://")
		if cleanPath != "" {
			a.Path = cleanPath
			if a.Filename == "" {
				a.Filename = filepath.Base(cleanPath)
			}
			if st, err := os.Stat(cleanPath); err == nil && !st.IsDir() {
				a.Size = st.Size()
			}
		}
		if a.MimeType == "" && a.Filename != "" {
			a.MimeType = mime.TypeByExtension(strings.ToLower(filepath.Ext(a.Filename)))
		}
		if a.MimeType == "" {
			a.MimeType = "application/octet-stream"
		}
		if a.Filename != "" || a.Path != "" {
			out = append(out, a)
		}
	}
	return out
}

func (s *Store) Save(d models.DraftView) (models.DraftView, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if d.ID == "" {
		d.ID = uuid.NewString()
	}
	if prev, ok := s.items[d.ID]; ok {
		if d.ProviderDraftID == "" {
			d.ProviderDraftID = prev.ProviderDraftID
		}
		if d.ProviderThreadID == "" {
			d.ProviderThreadID = prev.ProviderThreadID
		}
		if d.InReplyTo == "" {
			d.InReplyTo = prev.InReplyTo
		}
		if d.ThreadID == 0 {
			d.ThreadID = prev.ThreadID
		}
	}
	d.Attachments = NormalizeAttachments(d.Attachments)
	d.Snippet = makeSnippet(d.Body)
	if d.UpdatedAt.IsZero() {
		d.UpdatedAt = time.Now().UTC()
	}
	s.items[d.ID] = d
	return d, s.persistLocked()
}

func (s *Store) Get(id string) (models.DraftView, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.items[id]
	return d, ok
}

func (s *Store) Delete(id string) (models.DraftView, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.items[id]
	if !ok {
		return models.DraftView{}, false, nil
	}
	delete(s.items, id)
	return d, true, s.persistLocked()
}

func (s *Store) List(accountID, query string) []models.DraftView {
	s.mu.Lock()
	defer s.mu.Unlock()
	q := strings.ToLower(strings.TrimSpace(query))
	out := make([]models.DraftView, 0, len(s.items))
	for _, d := range s.items {
		if accountID != "" && d.AccountID != accountID {
			continue
		}
		if q != "" {
			hay := strings.ToLower(d.Subject + " " + d.Body + " " + strings.Join(d.To, " ") + " " + strings.Join(d.Cc, " "))
			if !strings.Contains(hay, q) {
				continue
			}
		}
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	return out
}

// MergeRemote reconciles remote drafts for one account into the local store.
func (s *Store) MergeRemote(accountID string, remote []provider.DraftItem) {
	s.mu.Lock()
	defer s.mu.Unlock()

	byProviderID := map[string]string{}
	for id, d := range s.items {
		if d.AccountID == accountID && d.ProviderDraftID != "" {
			byProviderID[d.ProviderDraftID] = id
		}
	}
	seenRemote := map[string]bool{}
	for _, r := range remote {
		if r.ProviderDraftID == "" {
			continue
		}
		seenRemote[r.ProviderDraftID] = true
		localID, exists := byProviderID[r.ProviderDraftID]
		if !exists {
			localID = uuid.NewString()
		}
		prev := s.items[localID]
		updated := time.Now().UTC()
		if r.UpdatedAt > 0 {
			updated = time.Unix(r.UpdatedAt, 0).UTC()
		}
		if exists && prev.UpdatedAt.After(updated) {
			updated = prev.UpdatedAt
		}
		atts := prev.Attachments
		if len(atts) == 0 && len(r.Attachments) > 0 {
			for _, a := range r.Attachments {
				atts = append(atts, models.AttachmentView{
					Filename: a.Filename,
					MimeType: a.MimeType,
					Size:     a.Size,
					Path:     a.Path,
				})
			}
		}
		body := r.Body
		if exists && prev.Body != "" && r.Body == "" {
			body = prev.Body
		}
		snippet := r.Snippet
		if snippet == "" {
			snippet = makeSnippet(body)
		}
		s.items[localID] = models.DraftView{
			ID:                localID,
			AccountID:         accountID,
			ProviderDraftID:   r.ProviderDraftID,
			ProviderMessageID: r.ProviderMessageID,
			ProviderThreadID:  r.ProviderThreadID,
			ThreadID:          prev.ThreadID,
			InReplyTo:         r.InReplyTo,
			To:                r.To,
			Cc:                r.Cc,
			Bcc:               r.Bcc,
			Subject:           r.Subject,
			Body:              body,
			Snippet:           snippet,
			UpdatedAt:         updated,
			Attachments:       atts,
		}
	}
	// Remove remote-backed drafts for this account that disappeared on the server.
	for providerDraftID, localID := range byProviderID {
		if !seenRemote[providerDraftID] {
			delete(s.items, localID)
		}
	}
	_ = s.persistLocked()
}

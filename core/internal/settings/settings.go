// Package settings holds the daemon's user preferences, persisted as
// JSON under the XDG config dir and reloadable at runtime (IPC
// settings.set applies immediately; system.reload re-reads the file).
package settings

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// Known notification action keys, in the order they may appear as
// buttons. Note: many notification servers cap the visible buttons
// (commonly 3); extras are silently dropped by the server.
var ValidNotifyActions = []string{"read", "archive", "trash", "snooze", "open"}

type Settings struct {
	// NotifyActions are the inline buttons on new-mail notifications.
	NotifyActions []string `json:"notifyActions"`
	// SnoozeMinutes is the duration used by the notification's snooze
	// button (the UI popup has its own picker).
	SnoozeMinutes int `json:"snoozeMinutes"`
}

func Defaults() Settings {
	return Settings{
		NotifyActions: []string{"read", "archive", "open"},
		SnoozeMinutes: 60,
	}
}

// Validate normalizes s, rejecting unknown action keys.
func (s *Settings) Validate() error {
	seen := map[string]bool{}
	out := make([]string, 0, len(s.NotifyActions))
	for _, a := range s.NotifyActions {
		ok := false
		for _, v := range ValidNotifyActions {
			if a == v {
				ok = true
				break
			}
		}
		if !ok {
			return fmt.Errorf("unknown notify action %q (valid: %v)", a, ValidNotifyActions)
		}
		if !seen[a] {
			seen[a] = true
			out = append(out, a)
		}
	}
	s.NotifyActions = out
	if s.SnoozeMinutes <= 0 {
		s.SnoozeMinutes = Defaults().SnoozeMinutes
	}
	return nil
}

// Store is the concurrency-safe holder the daemon reads on every
// notification.
type Store struct {
	path string

	mu  sync.RWMutex
	cur Settings
}

func NewStore(path string) *Store {
	st := &Store{path: path, cur: Defaults()}
	_ = st.Reload() // missing file is fine: defaults apply
	return st
}

func (st *Store) Get() Settings {
	st.mu.RLock()
	defer st.mu.RUnlock()
	s := st.cur
	s.NotifyActions = append([]string(nil), s.NotifyActions...)
	return s
}

// Reload re-reads the file; a missing file resets to defaults.
func (st *Store) Reload() error {
	s := Defaults()
	raw, err := os.ReadFile(st.path)
	switch {
	case os.IsNotExist(err):
	case err != nil:
		return err
	default:
		if err := json.Unmarshal(raw, &s); err != nil {
			return fmt.Errorf("settings %s: %w", st.path, err)
		}
	}
	if err := s.Validate(); err != nil {
		return err
	}
	st.mu.Lock()
	st.cur = s
	st.mu.Unlock()
	return nil
}

// Update validates, applies, and persists s.
func (st *Store) Update(s Settings) error {
	if err := s.Validate(); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(st.path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(st.path, append(raw, '\n'), 0o644); err != nil {
		return err
	}
	st.mu.Lock()
	st.cur = s
	st.mu.Unlock()
	return nil
}

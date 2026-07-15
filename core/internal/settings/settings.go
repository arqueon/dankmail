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
	"time"
)

// Known notification action keys, in their canonical render order — the
// same left-to-right order the per-thread action bar uses in the app
// (archive, trash, read, snooze, open). Validate() normalizes any stored
// set back to this order so notifications always match the UI, regardless
// of the order the user toggled them. Note: many notification servers cap
// the visible buttons (commonly 3); extras are silently dropped.
var ValidNotifyActions = []string{"archive", "trash", "read", "snooze", "open"}

// MinPollSeconds floors the mail poll cadence. The UI only offers >= 30s
// presets; this is the hard floor a hand-edited config can reach so a
// tight interval can never hammer a provider.
const MinPollSeconds = 10

type Settings struct {
	// NotifyActions are the inline buttons on new-mail notifications.
	NotifyActions []string `json:"notifyActions"`
	// SnoozePreset decides what the notification's snooze button means
	// (the UI popup has its own picker). One of ValidSnoozePresets;
	// "minutes" uses SnoozeMinutes.
	SnoozePreset string `json:"snoozePreset"`
	// SnoozeMinutes is the duration for the "minutes" preset.
	SnoozeMinutes int `json:"snoozeMinutes"`

	// Chained-action policies (spec §5): expand one user action into
	// companion ops before enqueueing. Applied live by the queue.
	MarkReadOnPreview bool `json:"markReadOnPreview"`
	MarkReadOnReply   bool `json:"markReadOnReply"`
	MarkReadOnTrash   bool `json:"markReadOnTrash"`
	UnarchiveOnStar   bool `json:"unarchiveOnStar"`

	// PollSeconds is how often each account is polled for new mail. Zero
	// falls back to the built-in default; Validate floors it at
	// MinPollSeconds. A per-account Config["pollSeconds"] override still
	// wins over this global value.
	PollSeconds int `json:"pollSeconds"`
}

func Defaults() Settings {
	return Settings{
		NotifyActions:     []string{"archive", "read", "open"},
		SnoozePreset:      "hour",
		SnoozeMinutes:     60,
		MarkReadOnPreview: true,
		MarkReadOnReply:   true,
		MarkReadOnTrash:   true,
		UnarchiveOnStar:   false,
		PollSeconds:       60,
	}
}

// ValidSnoozePresets, Gmail-flavored: laterweek = +2 days 09:00,
// weekend = next Saturday 09:00, nextweek = next Monday 09:00.
var ValidSnoozePresets = []string{"hour", "evening", "tomorrow", "laterweek", "weekend", "nextweek", "minutes"}

// SnoozeUntil resolves the configured preset to a concrete wake time.
func (s Settings) SnoozeUntil(now time.Time) time.Time {
	at := func(d time.Time, hour int) time.Time {
		return time.Date(d.Year(), d.Month(), d.Day(), hour, 0, 0, 0, d.Location())
	}
	daysUntil := func(target time.Weekday) int {
		diff := (int(target) - int(now.Weekday()) + 7) % 7
		if diff == 0 {
			diff = 7
		}
		return diff
	}
	switch s.SnoozePreset {
	case "evening":
		t := at(now, 18)
		if !t.After(now) {
			t = t.AddDate(0, 0, 1)
		}
		return t
	case "tomorrow":
		return at(now.AddDate(0, 0, 1), 9)
	case "laterweek":
		return at(now.AddDate(0, 0, 2), 9)
	case "weekend":
		return at(now.AddDate(0, 0, daysUntil(time.Saturday)), 9)
	case "nextweek":
		return at(now.AddDate(0, 0, daysUntil(time.Monday)), 9)
	case "minutes":
		return now.Add(time.Duration(s.SnoozeMinutes) * time.Minute)
	default: // "hour"
		return now.Add(time.Hour)
	}
}

// Validate normalizes s, rejecting unknown action keys.
func (s *Settings) Validate() error {
	seen := map[string]bool{}
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
		seen[a] = true
	}
	// Rebuild in canonical order (dedup + app order) so the notification
	// buttons always mirror the in-app action bar, not the toggle order.
	out := make([]string, 0, len(seen))
	for _, v := range ValidNotifyActions {
		if seen[v] {
			out = append(out, v)
		}
	}
	s.NotifyActions = out
	if s.PollSeconds <= 0 {
		s.PollSeconds = Defaults().PollSeconds
	}
	if s.PollSeconds < MinPollSeconds {
		s.PollSeconds = MinPollSeconds
	}
	if s.SnoozeMinutes <= 0 {
		s.SnoozeMinutes = Defaults().SnoozeMinutes
	}
	if s.SnoozePreset == "" {
		s.SnoozePreset = Defaults().SnoozePreset
	}
	validPreset := false
	for _, v := range ValidSnoozePresets {
		if s.SnoozePreset == v {
			validPreset = true
			break
		}
	}
	if !validPreset {
		return fmt.Errorf("unknown snooze preset %q (valid: %v)", s.SnoozePreset, ValidSnoozePresets)
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

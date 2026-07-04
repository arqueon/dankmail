package settings

import (
	"testing"
	"time"
)

func TestSnoozeUntilPresets(t *testing.T) {
	// Saturday 2026-07-04 10:00 local.
	now := time.Date(2026, 7, 4, 10, 0, 0, 0, time.Local)
	cases := []struct {
		preset string
		mins   int
		want   time.Time
	}{
		{"hour", 60, now.Add(time.Hour)},
		{"evening", 60, time.Date(2026, 7, 4, 18, 0, 0, 0, time.Local)},
		{"tomorrow", 60, time.Date(2026, 7, 5, 9, 0, 0, 0, time.Local)},
		{"laterweek", 60, time.Date(2026, 7, 6, 9, 0, 0, 0, time.Local)},
		// From Saturday, "weekend" = NEXT Saturday, "nextweek" = Monday.
		{"weekend", 60, time.Date(2026, 7, 11, 9, 0, 0, 0, time.Local)},
		{"nextweek", 60, time.Date(2026, 7, 6, 9, 0, 0, 0, time.Local)},
		{"minutes", 25, now.Add(25 * time.Minute)},
	}
	for _, c := range cases {
		s := Settings{SnoozePreset: c.preset, SnoozeMinutes: c.mins}
		if got := s.SnoozeUntil(now); !got.Equal(c.want) {
			t.Errorf("%s → %v, want %v", c.preset, got, c.want)
		}
	}

	// Evening already past → tomorrow evening.
	late := time.Date(2026, 7, 4, 20, 0, 0, 0, time.Local)
	s := Settings{SnoozePreset: "evening"}
	if got := s.SnoozeUntil(late); !got.Equal(time.Date(2026, 7, 5, 18, 0, 0, 0, time.Local)) {
		t.Errorf("evening past → %v", got)
	}
}

func TestValidateRejectsUnknownPreset(t *testing.T) {
	s := Settings{SnoozePreset: "mañana-quizás"}
	if err := s.Validate(); err == nil {
		t.Error("want error for unknown preset")
	}
	s = Settings{}
	if err := s.Validate(); err != nil || s.SnoozePreset != "hour" {
		t.Errorf("empty preset should default to hour, got %q err %v", s.SnoozePreset, err)
	}
}

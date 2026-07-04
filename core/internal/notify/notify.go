// Package notify sends desktop notifications over D-Bus
// (org.freedesktop.Notifications), with inline actions when the
// notification server advertises them, falling back to notify-send when
// the bus is unavailable. NotifyRules (per account + label) decide sound,
// urgency, and whether a message notifies at all; DND windows accumulate
// notifications and emit a summary when DND ends.
package notify

// Urgency levels map 1:1 to the freedesktop hint (0/1/2).
type Urgency byte

const (
	UrgencyLow Urgency = iota
	UrgencyNormal
	UrgencyCritical
)

// Action is an inline notification button. The daemon routes activation
// back into the op queue (archive/mark-read/snooze) or xdg-open (web).
type Action struct {
	Key   string // "archive", "read", "open", "snooze"
	Label string // localized
}

// Notification is one message-arrival popup.
type Notification struct {
	AccountID string
	ThreadID  string
	Summary   string // sender
	Body      string // subject — snippet
	Urgency   Urgency
	Sound     string // empty = server default, "none" = mute
	Actions   []Action
	// OnAction receives the Action.Key the user clicked (D-Bus backend
	// only; the notify-send fallback cannot deliver actions).
	OnAction func(actionKey string)
}

// Notifier is the daemon-facing interface; the D-Bus implementation and
// the notify-send fallback both satisfy it. Kept as an interface so the
// DND/calendar integration (dcal IPC, anillo 3) can wrap it.
type Notifier interface {
	Send(n Notification) (id uint32, err error)
	Close(id uint32) error
}

// NewBest returns the D-Bus notifier when the session bus is reachable,
// else the notify-send fallback.
func NewBest() Notifier {
	if n, err := NewDBus(); err == nil {
		return n
	}
	return execFallback{}
}

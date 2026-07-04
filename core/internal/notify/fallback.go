package notify

import "os/exec"

// execFallback shells out to notify-send. No actions, no close, no IDs —
// strictly a last resort when the session bus is unreachable.
type execFallback struct{}

func (execFallback) Send(n Notification) (uint32, error) {
	urgency := map[Urgency]string{
		UrgencyLow: "low", UrgencyNormal: "normal", UrgencyCritical: "critical",
	}[n.Urgency]
	err := exec.Command("notify-send",
		"-a", appName, "-i", appIcon, "-u", urgency,
		n.Summary, n.Body,
	).Run()
	return 0, err
}

func (execFallback) Close(uint32) error { return nil }

// Package ipc implements the local unix-socket control protocol used by
// the CLI, the QML UI, and external automation (niri/DMS keybinds).
// Wire format: newline-delimited JSON, request/response with integer IDs,
// mirroring dankcalendar's protocol.
package ipc

// Capabilities is sent by the server immediately after a client connects.
type Capabilities struct {
	APIVersion int      `json:"apiVersion"`
	Features   []string `json:"features"`
}

// APIVersion of the current protocol.
const APIVersion = 1

// Feature groups exposed over IPC.
var Features = []string{
	"accounts", "threads", "ops", "dnd", "subscribe", "ui", "system",
}

// Request is a client → daemon call.
type Request struct {
	ID     int            `json:"id"`
	Method string         `json:"method"`
	Params map[string]any `json:"params,omitempty"`
}

// Response is the daemon's reply to a Request with the same ID.
type Response[T any] struct {
	ID     int    `json:"id"`
	Result *T     `json:"result,omitempty"`
	Error  string `json:"error,omitempty"`
}

// Event is an unsolicited daemon → client push on a "subscribe" stream
// (sync updates, unread-count changes, op failures, snooze wakes).
type Event struct {
	Topic   string         `json:"topic"` // e.g. "sync.updated", "unread.changed"
	Payload map[string]any `json:"payload,omitempty"`
}

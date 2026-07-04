// Package bus is the in-process pub/sub the daemon components use to
// decouple state changes from their consumers: the sync engine and the
// op queue publish; the IPC subscription stream, the notifier layer, and
// the unread-counter cache subscribe.
package bus

import "sync"

// Event topics used across the daemon:
//
//	threads.changed   — local cache mutated (sync or optimistic op)
//	unread.changed    — unread counters may have moved
//	op.failed         — a PendingOp exhausted retries or hit a permanent error
//	account.auth      — an account needs re-authentication
//	snooze.woke       — a snoozed thread returned to the inbox
//	message.arrived   — a genuinely new message landed (notification source)
type Event struct {
	Topic   string
	Payload map[string]any
}

type Bus struct {
	mu   sync.Mutex
	subs map[int]chan Event
	next int
}

func New() *Bus { return &Bus{subs: map[int]chan Event{}} }

// Publish delivers ev to all subscribers without blocking: a subscriber
// whose buffer is full misses the event (subscribers treat events as
// "something changed" hints and re-query, so a drop is harmless).
func (b *Bus) Publish(topic string, payload map[string]any) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, ch := range b.subs {
		select {
		case ch <- Event{Topic: topic, Payload: payload}:
		default:
		}
	}
}

// Subscribe registers a buffered subscription. Callers must Unsubscribe
// when done; the channel is closed then.
func (b *Bus) Subscribe(buffer int) (int, <-chan Event) {
	if buffer < 1 {
		buffer = 16
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	id := b.next
	b.next++
	ch := make(chan Event, buffer)
	b.subs[id] = ch
	return id, ch
}

func (b *Bus) Unsubscribe(id int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if ch, ok := b.subs[id]; ok {
		delete(b.subs, id)
		close(ch)
	}
}

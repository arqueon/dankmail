package ipc

import (
	"context"
	"net"
)

// Handler processes one IPC method call.
type Handler func(ctx context.Context, params map[string]any) (any, error)

// Server owns the unix socket and the method registry.
//
// Method namespaces (Anillo 1 minimum):
//
//	accounts.list | accounts.add | accounts.remove
//	threads.list  | threads.get
//	ops.markRead  | ops.markUnread | ops.star | ops.unstar
//	ops.archive   | ops.trash | ops.snooze | ops.reply | ops.compose
//	dnd.on | dnd.off | dnd.status
//	ui.show | ui.toggle | ui.openLink
//	system.status | system.sync | system.exit
//	subscribe
type Server struct {
	socketPath string
	handlers   map[string]Handler
	ln         net.Listener
}

func NewServer(socketPath string) *Server {
	return &Server{socketPath: socketPath, handlers: map[string]Handler{}}
}

// Register adds a method handler. Panics on duplicate registration —
// wiring happens once at daemon startup.
func (s *Server) Register(method string, h Handler) {
	if _, dup := s.handlers[method]; dup {
		panic("ipc: duplicate method " + method)
	}
	s.handlers[method] = h
}

// Serve listens on the unix socket until ctx is cancelled.
// TODO(anillo1): accept loop, handshake (Capabilities), line-JSON codec,
// subscription fan-out via an event bus.
func (s *Server) Serve(ctx context.Context) error {
	panic("ipc: Serve not implemented yet (anillo 1)")
}

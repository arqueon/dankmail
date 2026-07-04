package ipc

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"sync"

	"github.com/arqueon/dankmail/core/internal/bus"
)

// Handler processes one IPC method call.
type Handler func(ctx context.Context, params map[string]any) (any, error)

// ErrAlreadyRunning: the socket is alive — another daemon owns it.
var ErrAlreadyRunning = errors.New("ipc: daemon already running on socket")

// Server owns the unix socket and the method registry.
//
// Method namespaces (Anillo 1):
//
//	accounts.list
//	threads.list | threads.get | threads.previewOpened
//	ops.markRead | ops.markUnread | ops.star | ops.unstar
//	ops.archive  | ops.trash | ops.snooze | ops.reply | ops.compose
//	dnd.on | dnd.off | dnd.status
//	ui.show | ui.toggle | ui.openLink
//	system.status | system.sync | system.reload | system.exit
//	subscribe
type Server struct {
	socketPath string
	handlers   map[string]Handler
	bus        *bus.Bus
}

func NewServer(socketPath string, b *bus.Bus) *Server {
	return &Server{socketPath: socketPath, handlers: map[string]Handler{}, bus: b}
}

// Register adds a method handler. Panics on duplicate registration —
// wiring happens once at daemon startup.
func (s *Server) Register(method string, h Handler) {
	if _, dup := s.handlers[method]; dup {
		panic("ipc: duplicate method " + method)
	}
	s.handlers[method] = h
}

// Serve listens on the unix socket until ctx is cancelled. A stale
// socket file is removed; a live one aborts with ErrAlreadyRunning.
func (s *Server) Serve(ctx context.Context) error {
	if conn, err := net.Dial("unix", s.socketPath); err == nil {
		_ = conn.Close()
		return ErrAlreadyRunning
	}
	_ = os.Remove(s.socketPath)

	ln, err := net.Listen("unix", s.socketPath)
	if err != nil {
		return err
	}
	go func() {
		<-ctx.Done()
		_ = ln.Close()
		_ = os.Remove(s.socketPath)
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		go s.handleConn(ctx, conn)
	}
}

func (s *Server) handleConn(ctx context.Context, conn net.Conn) {
	defer conn.Close()
	// The encoder is shared by concurrent request goroutines; writes are
	// serialized through this mutex.
	var wmu sync.Mutex
	enc := json.NewEncoder(conn)
	write := func(v any) error {
		wmu.Lock()
		defer wmu.Unlock()
		return enc.Encode(v)
	}
	if err := write(Capabilities{APIVersion: APIVersion, Features: Features}); err != nil {
		return
	}

	sc := bufio.NewScanner(conn)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := append([]byte(nil), sc.Bytes()...)
		if len(line) == 0 {
			continue
		}
		var req Request
		if err := json.Unmarshal(line, &req); err != nil {
			_ = write(Response[any]{ID: req.ID, Error: "bad request: " + err.Error()})
			continue
		}
		if req.Method == "subscribe" {
			_ = write(Response[string]{ID: req.ID, Result: ptr("subscribed")})
			s.streamEvents(ctx, write)
			return
		}
		h, ok := s.handlers[req.Method]
		if !ok {
			_ = write(Response[any]{ID: req.ID, Error: "unknown method: " + req.Method})
			continue
		}
		// Dispatch on a goroutine so long-running methods (OAuth
		// complete waits minutes for consent) don't block the
		// connection. Clients match responses by id, so out-of-order
		// delivery is part of the protocol.
		go func(req Request) {
			result, err := h(ctx, req.Params)
			if err != nil {
				_ = write(Response[any]{ID: req.ID, Error: err.Error()})
				return
			}
			_ = write(Response[any]{ID: req.ID, Result: &result})
		}(req)
	}
}

// streamEvents turns the connection into a one-way event feed until the
// client disconnects or the daemon stops.
func (s *Server) streamEvents(ctx context.Context, write func(any) error) {
	id, ch := s.bus.Subscribe(64)
	defer s.bus.Unsubscribe(id)
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-ch:
			if !ok {
				return
			}
			if err := write(Event{Topic: ev.Topic, Payload: ev.Payload}); err != nil {
				return
			}
		}
	}
}

func ptr[T any](v T) *T { return &v }

// String renders diagnostics for logs (no payloads — they may hold
// snippets of mail content).
func (s *Server) String() string {
	return fmt.Sprintf("ipc.Server(%s, %d methods)", s.socketPath, len(s.handlers))
}

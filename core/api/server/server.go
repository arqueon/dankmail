// Package server exposes the localhost-only HTTP API (HUMA + chi) that
// backs the QML UI. Reads (thread lists, previews, counters) go over
// HTTP; mutations go over IPC so that CLI, UI, and automation share one
// code path. The daemon binds an ephemeral port and passes the address
// to Quickshell via environment.
package server

import (
	"net"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
)

// New builds the router and binds addr (use port 0 for ephemeral).
// The returned listener's address is what the UI must be told.
func New(addr, version string) (*http.Server, net.Listener, error) {
	r := chi.NewRouter()
	api := humachi.New(r, huma.DefaultConfig("dankmail", version))

	registerRoutes(api)

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, nil, err
	}
	return &http.Server{Handler: r}, ln, nil
}

// registerRoutes wires the read-side endpoints.
// Anillo-1 surface:
//
//	GET /accounts            — accounts with status + unread counts
//	GET /threads?unread=&account=&limit=  — unified triage list
//	GET /threads/{id}        — thread with messages (plain-text bodies)
//	GET /status              — daemon status (queue depth, last sync)
func registerRoutes(api huma.API) {
	// TODO(anillo1): handlers backed by repo queries.
	_ = api
}

// Package models will hold the wire-level DTOs shared by the HTTP API
// and the IPC surface (thread summaries, account status, op results),
// kept separate from ent entities so storage can evolve without breaking
// the UI contract. Populated in Anillo 1 together with the API handlers.
package models

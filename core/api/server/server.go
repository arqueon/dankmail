// Package server exposes the localhost-only HTTP API (HUMA + chi) that
// backs the QML UI. Reads (thread lists, previews, counters) go over
// HTTP; mutations go over IPC so that CLI, UI, and automation share one
// code path. The daemon binds an ephemeral port and passes the address
// to Quickshell via environment.
package server

import (
	"context"
	"net"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/arqueon/dankmail/core/models"
	"github.com/arqueon/dankmail/core/repo"
)

// Deps are the read-side dependencies the routes need.
type Deps struct {
	Repo    *repo.Repo
	Version string
	// DND reports the current do-not-disturb state for /status.
	DND func() bool
}

// New builds the router and binds addr (use port 0 for ephemeral).
// The returned listener's address is what the UI must be told.
func New(addr string, deps Deps) (*http.Server, net.Listener, error) {
	r := chi.NewRouter()
	api := humachi.New(r, huma.DefaultConfig("dankmail", deps.Version))

	registerRoutes(api, deps)

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, nil, err
	}
	return &http.Server{Handler: r}, ln, nil
}

type accountsOutput struct {
	Body []models.AccountView
}

type threadsInput struct {
	Unread  bool   `query:"unread" doc:"only unread threads"`
	Starred bool   `query:"starred" doc:"only starred threads"`
	Inbox   bool   `query:"inbox" doc:"only threads currently in the inbox"`
	Account string `query:"account" doc:"restrict to one account UUID"`
	Limit   int    `query:"limit" doc:"max rows (default 100)"`
}

type threadsOutput struct {
	Body []models.ThreadSummary
}

type threadInput struct {
	ID int `path:"id"`
}

type threadOutput struct {
	Body models.ThreadDetail
}

type statusOutput struct {
	Body models.DaemonStatus
}

func registerRoutes(api huma.API, deps Deps) {
	huma.Get(api, "/accounts", func(ctx context.Context, _ *struct{}) (*accountsOutput, error) {
		accounts, err := deps.Repo.Accounts(ctx)
		if err != nil {
			return nil, err
		}
		return &accountsOutput{Body: accounts}, nil
	})

	huma.Get(api, "/threads", func(ctx context.Context, in *threadsInput) (*threadsOutput, error) {
		f := repo.ThreadFilter{
			UnreadOnly: in.Unread,
			Starred:    in.Starred,
			InboxOnly:  in.Inbox,
			Limit:      in.Limit,
		}
		if in.Account != "" {
			id, err := uuid.Parse(in.Account)
			if err != nil {
				return nil, huma.Error422UnprocessableEntity("bad account id")
			}
			f.AccountID = &id
		}
		threads, err := deps.Repo.ListThreads(ctx, f)
		if err != nil {
			return nil, err
		}
		return &threadsOutput{Body: threads}, nil
	})

	huma.Get(api, "/threads/{id}", func(ctx context.Context, in *threadInput) (*threadOutput, error) {
		d, err := deps.Repo.GetThread(ctx, in.ID)
		if err != nil {
			return nil, huma.Error404NotFound("thread not found")
		}
		return &threadOutput{Body: *d}, nil
	})

	huma.Get(api, "/status", func(ctx context.Context, _ *struct{}) (*statusOutput, error) {
		st, err := BuildStatus(ctx, deps)
		if err != nil {
			return nil, err
		}
		return &statusOutput{Body: *st}, nil
	})
}

// BuildStatus assembles the daemon status snapshot (shared with the IPC
// system.status method).
func BuildStatus(ctx context.Context, deps Deps) (*models.DaemonStatus, error) {
	accounts, err := deps.Repo.Accounts(ctx)
	if err != nil {
		return nil, err
	}
	queue, err := deps.Repo.QueueStats(ctx)
	if err != nil {
		return nil, err
	}
	unread := 0
	for _, a := range accounts {
		unread += a.Unread
	}
	dnd := false
	if deps.DND != nil {
		dnd = deps.DND()
	}
	return &models.DaemonStatus{
		Version:  deps.Version,
		Accounts: accounts,
		Unread:   unread,
		Queue:    queue,
		DND:      dnd,
	}, nil
}

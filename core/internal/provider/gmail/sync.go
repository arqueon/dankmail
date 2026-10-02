package gmail

import (
	"context"
	"encoding/json"
	"sort"
	"strconv"
	"strings"

	"github.com/arqueon/dankmail/core/internal/provider"
)

const syncCheckpointPrefix = "gmail-sync-v1:"
const syncBatchRequests = 25

// Only IDs and pagination state live in the cursor. Bodies are committed to
// the normal cache with the cursor, never duplicated in account metadata.
type syncCheckpoint struct {
	Account string   `json:"account"`
	Full    bool     `json:"full"`
	History uint64   `json:"history"`
	Start   uint64   `json:"start,omitempty"`
	Phase   string   `json:"phase"`
	Labels  []string `json:"labels,omitempty"`
	Label   int      `json:"label,omitempty"`
	Page    string   `json:"page,omitempty"`
	Pending []string `json:"pending,omitempty"`
	Done    []string `json:"done,omitempty"`
	Seen    []string `json:"seen,omitempty"`
}

func (p *Provider) Sync(ctx context.Context, cursor string) (provider.Changes, string, error) {
	ctx = withRetryBudget(ctx)
	var state syncCheckpoint
	if strings.HasPrefix(cursor, syncCheckpointPrefix) {
		err := json.Unmarshal([]byte(strings.TrimPrefix(cursor, syncCheckpointPrefix)), &state)
		if err == nil && state.Account == p.accountID && state.History > 0 &&
			((state.Full && state.Phase == "full" && state.Label >= 0 && state.Label <= len(p.labels) && strings.Join(state.Labels, "\x00") == strings.Join(p.labels, "\x00")) ||
				(!state.Full && state.Start > 0 && (state.Phase == "history" || state.Phase == "threads"))) {
			return p.resumeSync(ctx, state)
		}
		// Unknown/corrupt continuation: rebuild a safe complete snapshot.
		return p.startFullSync(ctx)
	}
	if strings.HasPrefix(cursor, cursorV2Prefix) {
		start, err := strconv.ParseUint(strings.TrimPrefix(cursor, cursorV2Prefix), 10, 64)
		if err == nil && start > 0 {
			return p.resumeSync(ctx, syncCheckpoint{Account: p.accountID, Start: start, History: start, Phase: "history"})
		}
	}
	return p.startFullSync(ctx)
}

func (p *Provider) startFullSync(ctx context.Context) (provider.Changes, string, error) {
	_, history, err := p.api.GetProfile(ctx)
	if err != nil {
		return provider.Changes{}, "", classify(err)
	}
	return p.resumeSync(ctx, syncCheckpoint{Account: p.accountID, Full: true, History: history, Phase: "full", Labels: p.labels})
}

func (p *Provider) resumeSync(ctx context.Context, state syncCheckpoint) (provider.Changes, string, error) {
	changes := provider.Changes{FullResync: state.Full}
	checkpoint := func(err error) (provider.Changes, string, error) {
		changes.Incomplete = true
		raw, _ := json.Marshal(state)
		return changes, syncCheckpointPrefix + string(raw), classify(err)
	}
	done := make(map[string]bool, len(state.Done))
	for _, id := range state.Done {
		done[id] = true
	}
	affected := make(map[string]bool, len(state.Pending))
	for _, id := range state.Pending {
		affected[id] = true
	}
	requests := 0
	for {
		if err := ctx.Err(); err != nil {
			return checkpoint(err)
		}
		if requests >= syncBatchRequests {
			return checkpoint(nil)
		}
		if state.Phase == "history" {
			resp, err := p.api.ListHistory(ctx, state.Start, state.Page)
			requests++
			if err != nil {
				if isNotFound(err) {
					return p.startFullSync(ctx)
				}
				return checkpoint(err)
			}
			state.History = max(state.History, resp.HistoryId)
			for _, h := range resp.History {
				state.History = max(state.History, h.Id)
				collectThreadIDs(affected, h)
			}
			state.Pending = state.Pending[:0]
			for id := range affected {
				state.Pending = append(state.Pending, id)
			}
			sort.Strings(state.Pending)
			state.Page = resp.NextPageToken
			if state.Page == "" {
				state.Phase = "threads"
			}
			continue
		}
		if len(state.Pending) == 0 {
			if state.Full && state.Label < len(p.labels) {
				ids, next, err := p.api.ListThreads(ctx, []string{p.labels[state.Label]}, state.Page)
				requests++
				if err != nil {
					return checkpoint(err)
				}
				state.Pending = ids
				state.Page = next
				if next == "" {
					state.Label++
				}
				continue
			}
			changes.SnapshotThreadIDs = state.Seen
			return changes, cursorV2Prefix + strconv.FormatUint(state.History, 10), nil
		}
		id := state.Pending[0]
		if done[id] {
			state.Pending = state.Pending[1:]
			continue
		}
		t, err := p.api.GetThread(ctx, id)
		requests++
		if err != nil && !isNotFound(err) {
			return checkpoint(err)
		}
		state.Pending = state.Pending[1:]
		if state.Full {
			done[id] = true
			state.Done = append(state.Done, id)
		}
		if err != nil {
			changes.RemovedThreadIDs = append(changes.RemovedThreadIDs, id)
			continue
		}
		if delta := p.threadDelta(t); delta.MessageCount > 0 {
			changes.Upserted = append(changes.Upserted, delta)
			if state.Full {
				state.Seen = append(state.Seen, id)
			}
		}
	}
}

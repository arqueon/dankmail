package microsoft

import (
	"context"
	"encoding/json"
	"sort"
	"strings"

	"github.com/arqueon/dankmail/core/internal/provider"
)

const checkpointPrefix = "graph-sync-v1:"
const syncBatchRequests = 25

type syncCheckpoint struct {
	Account string      `json:"account"`
	Full    bool        `json:"full"`
	State   cursorState `json:"state"`
	Folder  int         `json:"folder"`
	Link    string      `json:"link,omitempty"`
	Phase   string      `json:"phase"`
	Pending []string    `json:"pending,omitempty"`
	Removed []string    `json:"removed,omitempty"`
	Seen    []string    `json:"seen,omitempty"`
}

func (p *Provider) Sync(ctx context.Context, cursor string) (provider.Changes, string, error) {
	state := syncCheckpoint{Account: p.accountID, Full: cursor == "", State: cursorState{}, Phase: "delta"}
	if strings.HasPrefix(cursor, checkpointPrefix) {
		err := json.Unmarshal([]byte(strings.TrimPrefix(cursor, checkpointPrefix)), &state)
		if err != nil || state.Account != p.accountID || state.State == nil || state.Folder < 0 || state.Folder > len(monitoredFolders) ||
			(state.Phase != "delta" && state.Phase != "removed" && state.Phase != "threads") {
			state = syncCheckpoint{Account: p.accountID, Full: true, State: cursorState{}, Phase: "delta"}
		}
	} else if cursor != "" {
		if err := json.Unmarshal([]byte(cursor), &state.State); err != nil || state.State == nil {
			state.Full = true
			state.State = cursorState{}
		}
		for _, folder := range monitoredFolders {
			if state.State[folder] == "" {
				state.Full = true
				state.State = cursorState{}
				break
			}
		}
	}
	return p.resumeSync(ctx, state)
}

func (p *Provider) resumeSync(ctx context.Context, state syncCheckpoint) (provider.Changes, string, error) {
	changes := provider.Changes{FullResync: state.Full}
	checkpoint := func(err error) (provider.Changes, string, error) {
		changes.Incomplete = true
		raw, _ := json.Marshal(state)
		if err != nil {
			err = classify(err)
		}
		return changes, checkpointPrefix + string(raw), err
	}
	affected := map[string]bool{}
	for _, id := range state.Pending {
		affected[id] = true
	}
	addConversation := func(id string) {
		if id != "" && !affected[id] {
			affected[id] = true
			state.Pending = append(state.Pending, id)
		}
	}
	requests := 0
	for {
		if err := ctx.Err(); err != nil {
			return checkpoint(err)
		}
		if requests >= syncBatchRequests {
			return checkpoint(nil)
		}
		switch state.Phase {
		case "delta":
			if state.Folder == len(monitoredFolders) {
				state.Phase = "removed"
				continue
			}
			folder := monitoredFolders[state.Folder]
			link := state.Link
			if link == "" && !state.Full {
				link = state.State[folder]
			}
			page, err := p.api.DeltaMessages(ctx, folder, link)
			requests++
			if err != nil {
				if isGone(err) && link != "" {
					// Checkpoint a reset; do not recurse forever on a broken API.
					state = syncCheckpoint{Account: p.accountID, Full: true, State: cursorState{}, Phase: "delta"}
					changes = provider.Changes{FullResync: true}
					return checkpoint(nil)
				}
				return checkpoint(err)
			}
			for _, m := range page.Messages {
				if m.Removed {
					state.Removed = append(state.Removed, m.ID)
				} else {
					addConversation(m.ConversationID)
				}
			}
			state.Link = page.NextLink
			if page.DeltaLink != "" || page.NextLink == "" {
				state.State[folder] = page.DeltaLink
				state.Folder++
				state.Link = ""
			}
		case "removed":
			if len(state.Removed) == 0 {
				sort.Strings(state.Pending)
				state.Phase = "threads"
				continue
			}
			m, err := p.api.GetMessage(ctx, state.Removed[0])
			requests++
			if err != nil && !isNotFound(err) {
				return checkpoint(err)
			}
			state.Removed = state.Removed[1:]
			if err == nil {
				addConversation(m.ConversationID)
			}
		case "threads":
			if len(state.Pending) == 0 {
				changes.SnapshotThreadIDs = state.Seen
				raw, _ := json.Marshal(state.State)
				return changes, string(raw), nil
			}
			id := state.Pending[0]
			messages, err := p.api.ListConversation(ctx, id)
			requests++
			if err != nil && !isNotFound(err) {
				return checkpoint(err)
			}
			if err != nil || len(messages) == 0 {
				changes.RemovedThreadIDs = append(changes.RemovedThreadIDs, id)
			} else {
				delta, err := p.threadDelta(ctx, id, messages)
				if err != nil {
					return checkpoint(err)
				}
				if delta.MessageCount > 0 {
					changes.Upserted = append(changes.Upserted, delta)
					if state.Full {
						state.Seen = append(state.Seen, id)
					}
				}
			}
			state.Pending = state.Pending[1:]
		}
	}
}

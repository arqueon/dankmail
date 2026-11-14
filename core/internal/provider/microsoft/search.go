package microsoft

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/arqueon/dux-mail/core/internal/provider"
)

type searchMessagesAPI interface {
	SearchMessages(ctx context.Context, query, link string, top int) ([]string, string, error)
}

func escapeGraphSearchQuery(query string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`)
	return `"` + r.Replace(strings.TrimSpace(query)) + `"`
}

// SearchMessages searches across the entire mailbox via Microsoft Graph $search,
// returning deduplicated conversation IDs and an optional pagination link.
func (c *Client) SearchMessages(ctx context.Context, query, link string, top int) ([]string, string, error) {
	if top <= 0 || top > 50 {
		top = 50
	}
	u := link
	if u == "" {
		u = fmt.Sprintf("%s/me/messages?$search=%s&$select=id,conversationId&$top=%d",
			graphBase,
			url.QueryEscape(escapeGraphSearchQuery(query)),
			top,
		)
	}
	var doc struct {
		Value []wireMessage `json:"value"`
		Next  string        `json:"@odata.nextLink"`
	}
	if err := c.do(ctx, http.MethodGet, u, nil, &doc); err != nil {
		return nil, "", err
	}
	ids := make([]string, 0, len(doc.Value))
	seen := make(map[string]bool, len(doc.Value))
	for _, m := range doc.Value {
		if m.ConversationID != "" && !seen[m.ConversationID] {
			seen[m.ConversationID] = true
			ids = append(ids, m.ConversationID)
		}
	}
	return ids, doc.Next, nil
}

// SearchRemote implements provider.RemoteSearcher over Microsoft Graph $search.
// Results are marked Backfill=true so historical hits never trigger notifications.
func (p *Provider) SearchRemote(ctx context.Context, query string, limit int) (provider.Changes, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return provider.Changes{Backfill: true}, nil
	}
	api, ok := p.api.(searchMessagesAPI)
	if !ok {
		return provider.Changes{}, fmt.Errorf("microsoft: remote search unavailable")
	}
	if limit <= 0 {
		limit = 50
	}

	changes := provider.Changes{Backfill: true}
	seenConv := map[string]bool{}
	link := ""
	for len(changes.Upserted) < limit {
		top := min(50, limit-len(changes.Upserted))
		convIDs, next, err := api.SearchMessages(ctx, query, link, top)
		if err != nil {
			return provider.Changes{}, classify(err)
		}
		for _, convID := range convIDs {
			if len(changes.Upserted) >= limit {
				break
			}
			if seenConv[convID] {
				continue
			}
			seenConv[convID] = true
			msgs, err := p.api.ListConversation(ctx, convID)
			if err != nil {
				if isNotFound(err) {
					continue
				}
				return provider.Changes{}, classify(err)
			}
			d, err := p.threadDelta(ctx, convID, msgs)
			if err != nil {
				return provider.Changes{}, err
			}
			if d.MessageCount > 0 {
				changes.Upserted = append(changes.Upserted, d)
			}
		}
		if next == "" {
			break
		}
		link = next
	}
	return changes, nil
}

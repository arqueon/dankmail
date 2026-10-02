package microsoft

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/arqueon/dankmail/core/internal/provider"
)

type sentFolderAPI interface {
	ListSentMessages(context.Context, string) ([]string, string, error)
}

// ListSentMessages reads only conversation IDs, newest outgoing mail first.
// Bodies are fetched one conversation at a time so interruptions can resume.
func (c *Client) ListSentMessages(ctx context.Context, link string) ([]string, string, error) {
	u := link
	if u == "" {
		u = graphBase + "/me/mailFolders/sentitems/messages?$select=id,conversationId&$orderby=" + url.QueryEscape("sentDateTime desc") + "&$top=25"
	}
	var doc struct {
		Value []wireMessage `json:"value"`
		Next  string        `json:"@odata.nextLink"`
	}
	if err := c.do(ctx, http.MethodGet, u, nil, &doc); err != nil {
		return nil, "", err
	}
	ids := []string{}
	seen := map[string]bool{}
	for _, m := range doc.Value {
		if m.ConversationID != "" && !seen[m.ConversationID] {
			ids = append(ids, m.ConversationID)
			seen[m.ConversationID] = true
		}
	}
	return ids, doc.Next, nil
}

type sentCursor struct {
	Account   string   `json:"account"`
	Next      string   `json:"next,omitempty"`
	Remaining []string `json:"remaining,omitempty"`
}

func encodeSentCursor(c sentCursor) string {
	if c.Next == "" && len(c.Remaining) == 0 {
		return ""
	}
	raw, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(raw)
}
func (p *Provider) SentPage(ctx context.Context, pageToken string) (provider.Changes, string, error) {
	api, ok := p.api.(sentFolderAPI)
	if !ok {
		return provider.Changes{}, pageToken, fmt.Errorf("sent history unavailable")
	}
	cursor := sentCursor{Account: p.accountID}
	if pageToken != "" {
		raw, err := base64.RawURLEncoding.DecodeString(pageToken)
		if err != nil {
			return provider.Changes{}, pageToken, fmt.Errorf("invalid sent cursor")
		}
		if err = json.Unmarshal(raw, &cursor); err != nil || cursor.Account != p.accountID {
			return provider.Changes{}, pageToken, fmt.Errorf("invalid sent cursor")
		}
	}
	ids, next := cursor.Remaining, cursor.Next
	if len(ids) == 0 {
		var err error
		ids, next, err = api.ListSentMessages(ctx, cursor.Next)
		if err != nil {
			return provider.Changes{}, pageToken, classify(err)
		}
	}
	changes := provider.Changes{Backfill: true}
	for i, id := range ids {
		messages, err := p.api.ListConversation(ctx, id)
		if isNotFound(err) {
			continue
		}
		var d provider.ThreadDelta
		if err == nil {
			d, err = p.threadDelta(ctx, id, messages)
		}
		if err != nil {
			return changes, encodeSentCursor(sentCursor{Account: p.accountID, Next: next, Remaining: ids[i:]}), classify(err)
		}
		if d.MessageCount > 0 {
			changes.Upserted = append(changes.Upserted, d)
		}
	}
	return changes, encodeSentCursor(sentCursor{Account: p.accountID, Next: next}), nil
}

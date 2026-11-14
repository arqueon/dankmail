package imap

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	goimap "github.com/emersion/go-imap/v2"

	"github.com/arqueon/dux-mail/core/internal/provider"
)

type searchBackend interface {
	SearchMessages(ctx context.Context, mailboxes []string, criteria *goimap.SearchCriteria, limit int) ([]rawMessage, error)
}

// SearchRemote implements provider.RemoteSearcher across the account's Inbox,
// Archive, and Sent folders using server-side IMAP UID SEARCH.
func (p *Provider) SearchRemote(ctx context.Context, query string, limit int) (provider.Changes, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return provider.Changes{Backfill: true}, nil
	}
	sb, ok := p.api.(searchBackend)
	if !ok {
		return provider.Changes{}, fmt.Errorf("imap: remote search unavailable")
	}
	if limit <= 0 {
		limit = 50
	}

	folders, err := p.getFolders(ctx)
	if err != nil {
		return provider.Changes{}, err
	}
	var boxes []string
	seenBox := map[string]bool{}
	for _, b := range []string{folders.Inbox, folders.Archive, folders.Sent} {
		b = strings.TrimSpace(b)
		if b == "" {
			continue
		}
		key := strings.ToLower(b)
		if !seenBox[key] {
			seenBox[key] = true
			boxes = append(boxes, b)
		}
	}

	criteria := parseIMAPSearchCriteria(query)
	msgs, err := sb.SearchMessages(ctx, boxes, criteria, limit)
	if err != nil {
		return provider.Changes{}, classify(err)
	}

	threads := p.groupThreads(msgs, folders)
	if len(threads) > limit {
		threads = threads[:limit]
	}
	return provider.Changes{
		Backfill: true,
		Upserted: threads,
	}, nil
}

func (b *liveBackend) SearchMessages(ctx context.Context, mailboxes []string, criteria *goimap.SearchCriteria, limit int) ([]rawMessage, error) {
	cli, err := b.dialIMAP(ctx)
	if err != nil {
		return nil, err
	}
	defer cli.Close()

	perBoxCap := max(25, limit*2)
	var all []rawMessage
	for _, mailbox := range mailboxes {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		sel, err := cli.Select(mailbox, &goimap.SelectOptions{ReadOnly: true}).Wait()
		if err != nil || sel.NumMessages == 0 {
			continue
		}
		data, err := cli.UIDSearch(criteria, nil).Wait()
		if err != nil {
			continue
		}
		uids := data.AllUIDs()
		if len(uids) == 0 {
			continue
		}
		sort.Slice(uids, func(i, j int) bool { return uids[i] > uids[j] })
		if len(uids) > perBoxCap {
			uids = uids[:perBoxCap]
		}
		var uidSet goimap.UIDSet
		for _, u := range uids {
			uidSet.AddNum(u)
		}
		msgs, err := fetchMessages(cli, mailbox, sel.UIDValidity, uidSet)
		if err != nil {
			continue
		}
		all = append(all, msgs...)
	}
	return all, nil
}

// parseIMAPSearchCriteria translates common search operators (from:, to:,
// cc:, subject:, since:YYYY-MM-DD, before:YYYY-MM-DD, is:unread, is:starred)
// and quoted/free-text terms into go-imap SearchCriteria.
func parseIMAPSearchCriteria(query string) *goimap.SearchCriteria {
	c := &goimap.SearchCriteria{}
	for _, tok := range splitSearchTokens(query) {
		lower := strings.ToLower(tok)
		switch {
		case strings.HasPrefix(lower, "from:") && len(tok) > 5:
			c.Header = append(c.Header, goimap.SearchCriteriaHeaderField{Key: "From", Value: stripQuotes(tok[5:])})
		case strings.HasPrefix(lower, "to:") && len(tok) > 3:
			c.Header = append(c.Header, goimap.SearchCriteriaHeaderField{Key: "To", Value: stripQuotes(tok[3:])})
		case strings.HasPrefix(lower, "cc:") && len(tok) > 3:
			c.Header = append(c.Header, goimap.SearchCriteriaHeaderField{Key: "Cc", Value: stripQuotes(tok[3:])})
		case strings.HasPrefix(lower, "subject:") && len(tok) > 8:
			c.Header = append(c.Header, goimap.SearchCriteriaHeaderField{Key: "Subject", Value: stripQuotes(tok[8:])})
		case strings.HasPrefix(lower, "since:") && len(tok) > 6:
			if t, err := time.Parse("2006-01-02", stripQuotes(tok[6:])); err == nil {
				c.Since = t
			}
		case strings.HasPrefix(lower, "before:") && len(tok) > 7:
			if t, err := time.Parse("2006-01-02", stripQuotes(tok[7:])); err == nil {
				c.Before = t
			}
		case lower == "is:unread":
			c.NotFlag = append(c.NotFlag, goimap.FlagSeen)
		case lower == "is:starred" || lower == "is:flagged":
			c.Flag = append(c.Flag, goimap.FlagFlagged)
		default:
			if clean := stripQuotes(tok); clean != "" {
				c.Text = append(c.Text, clean)
			}
		}
	}
	return c
}

func stripQuotes(s string) string {
	return strings.Trim(strings.TrimSpace(s), `"`)
}

func splitSearchTokens(q string) []string {
	var out []string
	var cur strings.Builder
	inQuote := false
	for i := 0; i < len(q); i++ {
		ch := q[i]
		if ch == '"' {
			inQuote = !inQuote
			cur.WriteByte(ch)
			continue
		}
		if !inQuote && (ch == ' ' || ch == '\t' || ch == '\n') {
			if cur.Len() > 0 {
				out = append(out, cur.String())
				cur.Reset()
			}
			continue
		}
		cur.WriteByte(ch)
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

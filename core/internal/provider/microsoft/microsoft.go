package microsoft

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"
	gosync "sync"

	"github.com/arqueon/dankmail/core/errdefs"
	"github.com/arqueon/dankmail/core/internal/mailmime"
	"github.com/arqueon/dankmail/core/internal/provider"
)

const defaultBodyCap = 32 * 1024

// Options tunes a Microsoft provider instance.
type Options struct {
	// BodyCapBytes truncates plain-text bodies; <=0 means the 32 KiB default.
	BodyCapBytes int
}

// Provider implements provider.Provider for one Outlook/Microsoft 365
// account. Stateless between calls except the lazily resolved folder-ID
// map; safe for concurrent use.
type Provider struct {
	accountID string
	email     string
	api       graphAPI
	bodyCap   int

	folderOnce gosync.Once
	folderIDs  map[string]string
	folderErr  error
}

// New builds a Provider for accountID/email over the given API seam.
func New(accountID, email string, api graphAPI, opts Options) *Provider {
	bodyCap := opts.BodyCapBytes
	if bodyCap <= 0 {
		bodyCap = defaultBodyCap
	}
	return &Provider{accountID: accountID, email: email, api: api, bodyCap: bodyCap}
}

func (p *Provider) ID() string { return p.accountID }

func (p *Provider) Capabilities() provider.Capability {
	return provider.CapModifyFlags | provider.CapArchive | provider.CapTrash |
		provider.CapSendReply | provider.CapCompose | provider.CapDeepLink |
		provider.CapHistorySync
}

// monitoredFolders are the folders whose deltas drive sync: the inbox
// plus junk (spam review view; never notifies because InInbox=false).
var monitoredFolders = []string{folderInbox, folderJunk}

// cursorState is the persisted sync cursor: one deltaLink per monitored
// folder, JSON-encoded into account.sync_cursor.
type cursorState map[string]string

func (p *Provider) folders(ctx context.Context) (map[string]string, error) {
	p.folderOnce.Do(func() {
		p.folderIDs, p.folderErr = p.api.FolderIDs(ctx)
	})
	return p.folderIDs, p.folderErr
}

// Sync runs a message delta per monitored folder, groups the affected
// conversations, and rebuilds each one whole (ListConversation) so the
// resulting ThreadDelta has the same complete-thread shape Gmail
// produces — the reconciler needs no provider-specific logic.
func (p *Provider) Sync(ctx context.Context, cursor string) (provider.Changes, string, error) {
	state := cursorState{}
	full := cursor == ""
	if !full {
		if err := json.Unmarshal([]byte(cursor), &state); err != nil {
			// Unreadable cursor (e.g. account migrated): full resync.
			full = true
			state = cursorState{}
		}
	}

	changes := provider.Changes{FullResync: full}
	affected := map[string]bool{}
	removed := []string{} // message IDs from @removed entries

	for _, folder := range monitoredFolders {
		link := ""
		if !full {
			link = state[folder]
		}
		for {
			page, err := p.api.DeltaMessages(ctx, folder, link)
			if err != nil {
				if isGone(err) {
					// Delta token expired (410 SyncStateNotFound):
					// restart from scratch, like Gmail's history 404.
					return p.Sync(ctx, "")
				}
				return provider.Changes{}, "", classify(err)
			}
			for _, m := range page.Messages {
				if m.Removed {
					removed = append(removed, m.ID)
					continue
				}
				if m.ConversationID != "" {
					affected[m.ConversationID] = true
				}
			}
			if page.DeltaLink != "" {
				state[folder] = page.DeltaLink
				break
			}
			if page.NextLink == "" {
				break
			}
			link = page.NextLink
		}
	}

	// @removed entries carry only the message ID. If the message still
	// exists (it merely left the folder), rebuild its conversation; if
	// it is truly gone, a later full resync reconciles the remainder.
	for _, id := range removed {
		m, err := p.api.GetMessage(ctx, id)
		if err != nil {
			if isNotFound(err) {
				continue
			}
			return provider.Changes{}, "", classify(err)
		}
		if m.ConversationID != "" {
			affected[m.ConversationID] = true
		}
	}

	convIDs := make([]string, 0, len(affected))
	for id := range affected {
		convIDs = append(convIDs, id)
	}
	sort.Strings(convIDs)

	for _, convID := range convIDs {
		msgs, err := p.api.ListConversation(ctx, convID)
		if err != nil {
			if isNotFound(err) {
				changes.RemovedThreadIDs = append(changes.RemovedThreadIDs, convID)
				continue
			}
			return provider.Changes{}, "", classify(err)
		}
		if len(msgs) == 0 {
			changes.RemovedThreadIDs = append(changes.RemovedThreadIDs, convID)
			continue
		}
		delta, err := p.threadDelta(ctx, convID, msgs)
		if err != nil {
			return provider.Changes{}, "", err
		}
		changes.Upserted = append(changes.Upserted, delta)
	}

	raw, err := json.Marshal(state)
	if err != nil {
		return provider.Changes{}, "", errdefs.Wrap(errdefs.KindPermanent, err)
	}
	return changes, string(raw), nil
}

// threadDelta folds a conversation's messages into the provider-neutral
// thread shape (mirror of gmail.threadDelta).
func (p *Provider) threadDelta(ctx context.Context, convID string, msgs []*graphMessage) (provider.ThreadDelta, error) {
	ids, err := p.folders(ctx)
	if err != nil {
		return provider.ThreadDelta{}, classify(err)
	}
	d := provider.ThreadDelta{
		ThreadID:     convID,
		MessageCount: len(msgs),
	}
	fromSeen := map[string]bool{}
	labelSeen := map[string]bool{}
	var newest *graphMessage
	for _, m := range msgs {
		if !m.IsRead {
			d.Unread = true
		}
		if m.Flagged {
			d.Starred = true
		}
		switch m.ParentFolderID {
		case ids[folderInbox]:
			d.InInbox = true
		case ids[folderJunk]:
			if !labelSeen["SPAM"] {
				labelSeen["SPAM"] = true
				d.Labels = append(d.Labels, "SPAM")
			}
		case ids[folderTrash]:
			if !labelSeen["TRASH"] {
				labelSeen["TRASH"] = true
				d.Labels = append(d.Labels, "TRASH")
			}
		}
		if m.From != "" && !fromSeen[m.From] {
			fromSeen[m.From] = true
			d.Participants = append(d.Participants, m.From)
		}
		if newest == nil || m.ReceivedAt >= newest.ReceivedAt {
			newest = m
		}
		d.Messages = append(d.Messages, p.messageDelta(m))
	}
	if newest != nil {
		d.Subject = newest.Subject
		d.Snippet = newest.BodyPreview
		d.LastMessage = newest.ReceivedAt
	}
	return d, nil
}

func (p *Provider) messageDelta(m *graphMessage) provider.MessageDelta {
	d := provider.MessageDelta{
		MessageID:       m.ID,
		RFC822MessageID: strings.Trim(strings.TrimSpace(m.InternetMessageID), "<>"),
		From:            m.From,
		To:              m.To,
		Cc:              m.Cc,
		Date:            m.ReceivedAt,
		Snippet:         m.BodyPreview,
		BodyText:        truncateAtRune(m.BodyText, p.bodyCap),
		Attachments:     m.Attachments,
	}
	replyHeaders := map[string]string{}
	if m.Subject != "" {
		replyHeaders[provider.HeaderSubject] = m.Subject
	}
	for header, key := range map[string]string{
		"References": provider.HeaderReferences,
		"Reply-To":   provider.HeaderReplyTo,
	} {
		if v := m.Headers[header]; v != "" {
			replyHeaders[key] = v
		}
	}
	if len(replyHeaders) > 0 {
		d.ReplyHeaders = replyHeaders
	}
	return d
}

// ModifyFlags maps provider flags onto per-message properties: unread →
// isRead (inverted polarity), starred → flag.flagStatus. Graph has no
// thread-level flags, so the change applies to every message of each
// conversation (same semantics Gmail's thread modify has).
func (p *Provider) ModifyFlags(ctx context.Context, threadIDs []string, add, remove []provider.Flag) error {
	patch := map[string]any{}
	for _, f := range add {
		switch f {
		case provider.FlagUnread:
			patch["isRead"] = false
		case provider.FlagStarred:
			patch["flag"] = map[string]any{"flagStatus": "flagged"}
		default:
			return errdefs.Wrap(errdefs.KindPermanent, fmt.Errorf("microsoft: unsupported flag %q", f))
		}
	}
	for _, f := range remove {
		switch f {
		case provider.FlagUnread:
			patch["isRead"] = true
		case provider.FlagStarred:
			patch["flag"] = map[string]any{"flagStatus": "notFlagged"}
		default:
			return errdefs.Wrap(errdefs.KindPermanent, fmt.Errorf("microsoft: unsupported flag %q", f))
		}
	}
	if len(patch) == 0 {
		return nil
	}
	return p.eachMessage(ctx, threadIDs, func(m *graphMessage) error {
		return p.api.PatchMessage(ctx, m.ID, patch)
	})
}

// Archive moves every inbox message of the threads to the archive folder.
func (p *Provider) Archive(ctx context.Context, threadIDs []string) error {
	return p.moveFromTo(ctx, threadIDs, folderInbox, folderArchive)
}

// Unarchive moves the threads' archived messages back to the inbox
// (snooze wake-up and the star→inbox hook).
func (p *Provider) Unarchive(ctx context.Context, threadIDs []string) error {
	return p.moveFromTo(ctx, threadIDs, folderArchive, folderInbox)
}

// Trash moves every message of the threads (wherever it lives) to
// Deleted Items. Never a permanent delete.
func (p *Provider) Trash(ctx context.Context, threadIDs []string) error {
	ids, err := p.folders(ctx)
	if err != nil {
		return classify(err)
	}
	return p.eachMessage(ctx, threadIDs, func(m *graphMessage) error {
		if m.ParentFolderID == ids[folderTrash] {
			return nil
		}
		return p.api.MoveMessage(ctx, m.ID, folderTrash)
	})
}

// moveFromTo moves the threads' messages sitting in fromFolder to
// toFolder; messages elsewhere are untouched (idempotent re-runs).
func (p *Provider) moveFromTo(ctx context.Context, threadIDs []string, fromFolder, toFolder string) error {
	ids, err := p.folders(ctx)
	if err != nil {
		return classify(err)
	}
	return p.eachMessage(ctx, threadIDs, func(m *graphMessage) error {
		if m.ParentFolderID != ids[fromFolder] {
			return nil
		}
		return p.api.MoveMessage(ctx, m.ID, toFolder)
	})
}

func (p *Provider) eachMessage(ctx context.Context, threadIDs []string, fn func(*graphMessage) error) error {
	for _, convID := range threadIDs {
		msgs, err := p.api.ListConversation(ctx, convID)
		if err != nil {
			return classify(err)
		}
		for _, m := range msgs {
			if err := fn(m); err != nil {
				return classify(err)
			}
		}
	}
	return nil
}

// SendReply builds a threaded plain-text MIME reply from the original
// message headers (mailmime, shared with Gmail) and submits it whole.
func (p *Provider) SendReply(ctx context.Context, threadID string, r provider.ReplyDraft) error {
	orig, err := p.api.GetMessage(ctx, r.InReplyToMessageID)
	if err != nil {
		return classify(err)
	}
	raw, err := mailmime.BuildReply(p.messageDelta(orig), p.email, r)
	if err != nil {
		return errdefs.Wrap(errdefs.KindPermanent, err)
	}
	if err := p.api.SendMail(ctx, raw); err != nil {
		return classify(err)
	}
	return nil
}

// Compose sends a new plain-text message.
func (p *Provider) Compose(ctx context.Context, m provider.ComposeDraft) error {
	raw, err := mailmime.BuildCompose(p.email, m)
	if err != nil {
		return errdefs.Wrap(errdefs.KindPermanent, err)
	}
	if err := p.api.SendMail(ctx, raw); err != nil {
		return classify(err)
	}
	return nil
}

// WebLink deep-links into the Outlook web app. The documented OWA
// pattern accepts a message ID; without one, fall back to the mailbox
// (still ok=true — better than nothing for a notifier).
func (p *Provider) WebLink(threadID, messageID string) (string, bool) {
	if messageID != "" {
		return "https://outlook.live.com/owa/?ItemID=" + url.QueryEscape(messageID) +
			"&exvsurl=1&viewmodel=ReadMessageItem", true
	}
	return "https://outlook.live.com/mail/", true
}

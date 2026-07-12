// Package gmail implements provider.Provider over the Gmail REST API
// (users.threads/messages/history). Sync is cursor-based on historyId;
// mutations are thread-scoped label edits; sending goes through
// mailmime + users.messages.send. All errors returned by Provider
// methods are wrapped with errdefs kinds.
package gmail

import (
	"context"
	"encoding/base64"
	"fmt"
	htmltomarkdown "github.com/JohannesKaufmann/html-to-markdown/v2"
	"net/mail"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/arqueon/dankmail/core/errdefs"
	"github.com/arqueon/dankmail/core/internal/mailmime"
	"github.com/arqueon/dankmail/core/internal/provider"
	gmailv1 "google.golang.org/api/gmail/v1"
)

// defaultBodyCap is the plain-text body truncation limit when Options
// does not set one (spec: 32 KiB).
const defaultBodyCap = 32 * 1024

// Gmail system label IDs the provider maps to flags or filters out of
// ThreadDelta.Labels.
const (
	labelUnread  = "UNREAD"
	labelStarred = "STARRED"
	labelInbox   = "INBOX"
	labelTrash   = "TRASH"
	labelSpam    = "SPAM"
)

// Options tunes a Gmail provider instance.
type Options struct {
	// MonitoredLabels are extra label IDs synced besides INBOX and SPAM
	// (which are always monitored — SPAM so the spam folder can be
	// reviewed and bulk-read from the triage window; it never notifies
	// because its threads carry InInbox=false).
	MonitoredLabels []string
	// BodyCapBytes truncates plain-text bodies; <=0 means the 32 KiB default.
	BodyCapBytes int
}

// Provider implements provider.Provider for one Gmail account. It is
// stateless after construction and safe for concurrent use.
type Provider struct {
	accountID string
	email     string
	api       gmailAPI
	labels    []string // INBOX + monitored labels, deduped, in order
	bodyCap   int
}

// New builds a Provider for accountID/email over the given API seam.
func New(accountID, email string, api gmailAPI, opts Options) *Provider {
	labels := []string{labelInbox, labelSpam}
	seen := map[string]bool{labelInbox: true, labelSpam: true}
	for _, l := range opts.MonitoredLabels {
		if l == "" || seen[l] {
			continue
		}
		seen[l] = true
		labels = append(labels, l)
	}
	bodyCap := opts.BodyCapBytes
	if bodyCap <= 0 {
		bodyCap = defaultBodyCap
	}
	return &Provider{
		accountID: accountID,
		email:     email,
		api:       api,
		labels:    labels,
		bodyCap:   bodyCap,
	}
}

// ID returns the local account row ID this instance serves.
func (p *Provider) ID() string { return p.accountID }

// Capabilities returns the static Gmail feature set. CapPush arrives with
// Pub/Sub in a later ring.
func (p *Provider) Capabilities() provider.Capability {
	return provider.CapModifyFlags |
		provider.CapArchive |
		provider.CapTrash |
		provider.CapSendReply |
		provider.CapCompose |
		provider.CapDeepLink |
		provider.CapHistorySync |
		provider.CapUnspam
}

// Sync returns remote changes since cursor and the new cursor. An empty
// or unparseable cursor, and an expired one (history 404), trigger the
// full-resync path.
func (p *Provider) Sync(ctx context.Context, cursor string) (provider.Changes, string, error) {
	if cursor == "" {
		return p.fullSync(ctx)
	}
	start, err := strconv.ParseUint(cursor, 10, 64)
	if err != nil {
		// A corrupt cursor is equivalent to an expired one.
		return p.fullSync(ctx)
	}
	return p.incrementalSync(ctx, start)
}

// fullSync captures the profile historyId FIRST (so changes racing the
// listing are replayed by the next incremental sync), then lists every
// monitored label and fetches each thread once.
func (p *Provider) fullSync(ctx context.Context) (provider.Changes, string, error) {
	_, historyID, err := p.api.GetProfile(ctx)
	if err != nil {
		return provider.Changes{}, "", classify(err)
	}

	changes := provider.Changes{FullResync: true}
	seen := map[string]bool{}
	for _, label := range p.labels {
		pageToken := ""
		for {
			ids, next, err := p.api.ListThreads(ctx, []string{label}, pageToken)
			if err != nil {
				return provider.Changes{}, "", classify(err)
			}
			for _, id := range ids {
				if seen[id] {
					continue
				}
				seen[id] = true
				t, err := p.api.GetThread(ctx, id)
				if err != nil {
					if isNotFound(err) {
						continue // raced away between list and get
					}
					return provider.Changes{}, "", classify(err)
				}
				changes.Upserted = append(changes.Upserted, p.threadDelta(t))
			}
			if next == "" {
				break
			}
			pageToken = next
		}
	}
	return changes, strconv.FormatUint(historyID, 10), nil
}

// incrementalSync replays history since start. Affected threads are
// re-fetched whole; a 404 on the fetch means the thread is gone and only
// then does it land in RemovedThreadIDs — archived or trashed threads
// come back as upserts with InInbox=false so local snooze/retention
// logic can decide what to do.
func (p *Provider) incrementalSync(ctx context.Context, start uint64) (provider.Changes, string, error) {
	maxHistory := start
	affected := map[string]bool{}
	pageToken := ""
	for {
		resp, err := p.api.ListHistory(ctx, start, pageToken)
		if err != nil {
			if isNotFound(err) {
				// Cursor expired: Gmail only keeps recent history.
				return p.fullSync(ctx)
			}
			return provider.Changes{}, "", classify(err)
		}
		if resp.HistoryId > maxHistory {
			maxHistory = resp.HistoryId
		}
		for _, h := range resp.History {
			if h.Id > maxHistory {
				maxHistory = h.Id
			}
			collectThreadIDs(affected, h)
		}
		if resp.NextPageToken == "" {
			break
		}
		pageToken = resp.NextPageToken
	}

	ids := make([]string, 0, len(affected))
	for id := range affected {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	var changes provider.Changes
	for _, id := range ids {
		t, err := p.api.GetThread(ctx, id)
		if err != nil {
			if isNotFound(err) {
				changes.RemovedThreadIDs = append(changes.RemovedThreadIDs, id)
				continue
			}
			return provider.Changes{}, "", classify(err)
		}
		changes.Upserted = append(changes.Upserted, p.threadDelta(t))
	}
	return changes, strconv.FormatUint(maxHistory, 10), nil
}

func collectThreadIDs(dst map[string]bool, h *gmailv1.History) {
	add := func(m *gmailv1.Message) {
		if m != nil && m.ThreadId != "" {
			dst[m.ThreadId] = true
		}
	}
	for _, x := range h.MessagesAdded {
		add(x.Message)
	}
	for _, x := range h.MessagesDeleted {
		add(x.Message)
	}
	for _, x := range h.LabelsAdded {
		add(x.Message)
	}
	for _, x := range h.LabelsRemoved {
		add(x.Message)
	}
}

// SearchRemote implements provider.RemoteSearcher: a server-side sweep
// of the FULL mailbox history via Gmail's search syntax. Results come
// back as Backfill deltas (upserted into the cache, never notified).
func (p *Provider) SearchRemote(ctx context.Context, query string, limit int) (provider.Changes, error) {
	if limit <= 0 {
		limit = 100
	}
	changes := provider.Changes{Backfill: true}
	pageToken := ""
	for len(changes.Upserted) < limit {
		ids, next, err := p.api.SearchThreads(ctx, query, pageToken)
		if err != nil {
			return provider.Changes{}, classify(err)
		}
		for _, id := range ids {
			if len(changes.Upserted) >= limit {
				break
			}
			t, err := p.api.GetThread(ctx, id)
			if err != nil {
				if isNotFound(err) {
					continue
				}
				return provider.Changes{}, classify(err)
			}
			changes.Upserted = append(changes.Upserted, p.threadDelta(t))
		}
		if next == "" {
			break
		}
		pageToken = next
	}
	return changes, nil
}

// ModifyFlags maps provider flags to Gmail labels (unread→UNREAD,
// starred→STARRED) and applies them per thread.
func (p *Provider) ModifyFlags(ctx context.Context, threadIDs []string, add, remove []provider.Flag) error {
	addLabels, err := flagsToLabels(add)
	if err != nil {
		return err
	}
	removeLabels, err := flagsToLabels(remove)
	if err != nil {
		return err
	}
	if len(addLabels) == 0 && len(removeLabels) == 0 {
		return nil
	}
	return p.modifyAll(ctx, threadIDs, addLabels, removeLabels)
}

// Archive removes the threads from the inbox (remove INBOX).
func (p *Provider) Archive(ctx context.Context, threadIDs []string) error {
	return p.modifyAll(ctx, threadIDs, nil, []string{labelInbox})
}

// Unarchive puts the threads back in the inbox (add INBOX).
func (p *Provider) Unarchive(ctx context.Context, threadIDs []string) error {
	return p.modifyAll(ctx, threadIDs, []string{labelInbox}, nil)
}

// Trash moves the threads to the Gmail trash (add TRASH, remove INBOX —
// matching what Gmail's own threads.trash does). Never a permanent delete.
func (p *Provider) Trash(ctx context.Context, threadIDs []string) error {
	return p.modifyAll(ctx, threadIDs, []string{labelTrash}, []string{labelInbox})
}

// Unspam rescues the threads from the spam folder ("not spam"): remove
// SPAM, back to the inbox.
func (p *Provider) Unspam(ctx context.Context, threadIDs []string) error {
	return p.modifyAll(ctx, threadIDs, []string{labelInbox}, []string{labelSpam})
}

func (p *Provider) modifyAll(ctx context.Context, threadIDs []string, add, remove []string) error {
	for _, id := range threadIDs {
		if err := p.api.ModifyThread(ctx, id, add, remove); err != nil {
			return classify(err)
		}
	}
	return nil
}

func flagsToLabels(flags []provider.Flag) ([]string, error) {
	labels := make([]string, 0, len(flags))
	for _, f := range flags {
		switch f {
		case provider.FlagUnread:
			labels = append(labels, labelUnread)
		case provider.FlagStarred:
			labels = append(labels, labelStarred)
		default:
			return nil, errdefs.Wrap(errdefs.KindPermanent,
				fmt.Errorf("gmail: unsupported flag %q", f))
		}
	}
	return labels, nil
}

// SendReply fetches the original message headers, builds a threaded
// plain-text MIME reply, and sends it on the given thread.
func (p *Provider) SendReply(ctx context.Context, threadID string, r provider.ReplyDraft) error {
	origMsg, err := p.api.GetMessageMetadata(ctx, r.InReplyToMessageID)
	if err != nil {
		return classify(err)
	}
	raw, err := mailmime.BuildReply(p.messageDelta(origMsg), p.email, r)
	if err != nil {
		return errdefs.Wrap(errdefs.KindPermanent, err)
	}
	if err := p.api.SendMessage(ctx, threadID, raw); err != nil {
		return classify(err)
	}
	return nil
}

// Compose sends a new plain-text message (no thread association).
func (p *Provider) Compose(ctx context.Context, m provider.ComposeDraft) error {
	raw, err := mailmime.BuildCompose(p.email, m)
	if err != nil {
		return errdefs.Wrap(errdefs.KindPermanent, err)
	}
	if err := p.api.SendMessage(ctx, "", raw); err != nil {
		return classify(err)
	}
	return nil
}

// WebLink returns a Gmail webmail deep link. With a thread ID it targets
// the thread; with only an RFC 822 Message-ID it falls back to an
// rfc822msgid: search. The authuser query pins the right account in
// multi-login browsers.
func (p *Provider) WebLink(threadID, messageID string) (string, bool) {
	authuser := url.QueryEscape(p.email)
	if threadID != "" {
		return fmt.Sprintf("https://mail.google.com/mail/u/0/?authuser=%s#all/%s",
			authuser, threadID), true
	}
	if id := strings.Trim(strings.TrimSpace(messageID), "<>"); id != "" {
		return fmt.Sprintf("https://mail.google.com/mail/u/0/?authuser=%s#search/rfc822msgid:%s",
			authuser, url.QueryEscape(id)), true
	}
	return "", false
}

// threadDelta maps a full Gmail thread to the provider-neutral delta.
func (p *Provider) threadDelta(t *gmailv1.Thread) provider.ThreadDelta {
	d := provider.ThreadDelta{
		ThreadID:     t.Id,
		MessageCount: len(t.Messages),
	}
	labelSeen := map[string]bool{}
	fromSeen := map[string]bool{}
	var newest *gmailv1.Message
	for _, m := range t.Messages {
		for _, l := range m.LabelIds {
			switch l {
			case labelUnread:
				d.Unread = true
			case labelStarred:
				d.Starred = true
			case labelInbox:
				d.InInbox = true
			}
			if !isSystemLabel(l) && !labelSeen[l] {
				labelSeen[l] = true
				d.Labels = append(d.Labels, l)
			}
		}
		if from := headerValue(m.Payload, "From"); from != "" && !fromSeen[from] {
			fromSeen[from] = true
			d.Participants = append(d.Participants, from)
		}
		if newest == nil || m.InternalDate >= newest.InternalDate {
			newest = m
		}
		d.Messages = append(d.Messages, p.messageDelta(m))
	}
	if newest != nil {
		d.Subject = headerValue(newest.Payload, "Subject")
		d.Snippet = newest.Snippet
		d.LastMessage = newest.InternalDate / 1000 // ms → unix seconds
	}
	return d
}

// messageDelta maps one Gmail message (full or metadata format) to the
// provider-neutral delta. With metadata format the body is empty.
func (p *Provider) messageDelta(m *gmailv1.Message) provider.MessageDelta {
	d := provider.MessageDelta{
		MessageID: m.Id,
		Snippet:   m.Snippet,
		Date:      m.InternalDate / 1000, // ms → unix seconds
		From:      headerValue(m.Payload, "From"),
		To:        splitAddrList(headerValue(m.Payload, "To")),
		Cc:        splitAddrList(headerValue(m.Payload, "Cc")),
	}
	if id := headerValue(m.Payload, "Message-ID"); id != "" {
		d.RFC822MessageID = strings.Trim(strings.TrimSpace(id), "<>")
	}
	replyHeaders := map[string]string{}
	for header, key := range map[string]string{
		"Subject":    provider.HeaderSubject,
		"References": provider.HeaderReferences,
		"Reply-To":   provider.HeaderReplyTo,
	} {
		if v := headerValue(m.Payload, header); v != "" {
			replyHeaders[key] = v
		}
	}
	if len(replyHeaders) > 0 {
		d.ReplyHeaders = replyHeaders
	}
	body := firstPlainText(m.Payload)
	if body == "" {
		// HTML-only message: distill the first text/html part to plain
		// markdown-ish text (never rendered — spec §1).
		body = htmlToText(firstHTML(m.Payload))
	}
	d.BodyText = truncateAtRune(body, p.bodyCap)
	d.Attachments = collectAttachments(m.Payload, nil)
	return d
}

// collectAttachments walks the payload tree gathering metadata for every
// named part (regular and inline attachments). Content is never fetched.
func collectAttachments(part *gmailv1.MessagePart, acc []provider.AttachmentMeta) []provider.AttachmentMeta {
	if part == nil {
		return acc
	}
	if part.Filename != "" {
		size := int64(0)
		if part.Body != nil {
			size = part.Body.Size
		}
		acc = append(acc, provider.AttachmentMeta{
			Filename: part.Filename,
			MimeType: part.MimeType,
			Size:     size,
		})
	}
	for _, child := range part.Parts {
		acc = collectAttachments(child, acc)
	}
	return acc
}

// isSystemLabel filters Gmail system labels out of ThreadDelta.Labels;
// only user labels (and provider basics like TRASH/SPAM, which retention
// logic may care about) pass through.
func isSystemLabel(l string) bool {
	switch l {
	case labelUnread, labelStarred, labelInbox, "SENT", "DRAFT", "IMPORTANT", "CHAT":
		return true
	}
	return strings.HasPrefix(l, "CATEGORY_")
}

// headerValue returns the first header with the given name
// (case-insensitive) from a message payload.
func headerValue(part *gmailv1.MessagePart, name string) string {
	if part == nil {
		return ""
	}
	for _, h := range part.Headers {
		if strings.EqualFold(h.Name, name) {
			return h.Value
		}
	}
	return ""
}

// splitAddrList parses a comma-separated address header into display
// strings; unparseable input degrades to a naive comma split.
func splitAddrList(v string) []string {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	addrs, err := mail.ParseAddressList(v)
	if err != nil {
		var out []string
		for _, part := range strings.Split(v, ",") {
			if part = strings.TrimSpace(part); part != "" {
				out = append(out, part)
			}
		}
		return out
	}
	out := make([]string, 0, len(addrs))
	for _, a := range addrs {
		out = append(out, a.String())
	}
	return out
}

// firstPlainText walks the payload tree depth-first and returns the
// decoded content of the first text/plain part.
func firstPlainText(part *gmailv1.MessagePart) string {
	if part == nil {
		return ""
	}
	if strings.HasPrefix(strings.ToLower(part.MimeType), "text/plain") &&
		part.Body != nil && part.Body.Data != "" {
		return decodeBase64URL(part.Body.Data)
	}
	for _, child := range part.Parts {
		if s := firstPlainText(child); s != "" {
			return s
		}
	}
	return ""
}

// firstHTML walks the payload tree depth-first and returns the decoded
// content of the first text/html part.
func firstHTML(part *gmailv1.MessagePart) string {
	if part == nil {
		return ""
	}
	if strings.HasPrefix(strings.ToLower(part.MimeType), "text/html") &&
		part.Body != nil && part.Body.Data != "" {
		return decodeBase64URL(part.Body.Data)
	}
	for _, child := range part.Parts {
		if s := firstHTML(child); s != "" {
			return s
		}
	}
	return ""
}

// htmlToText distills HTML to plain markdown-ish text. Errors degrade to
// an empty body (the snippet still shows in the UI).
func htmlToText(html string) string {
	if html == "" {
		return ""
	}
	text, err := htmltomarkdown.ConvertString(html)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(text)
}

// decodeBase64URL decodes Gmail body data, which is base64url with or
// without padding depending on the producer.
func decodeBase64URL(data string) string {
	if b, err := base64.URLEncoding.DecodeString(data); err == nil {
		return string(b)
	}
	if b, err := base64.RawURLEncoding.DecodeString(data); err == nil {
		return string(b)
	}
	return ""
}

// truncateAtRune cuts s to at most capBytes bytes without splitting a
// UTF-8 rune.
func truncateAtRune(s string, capBytes int) string {
	if capBytes <= 0 || len(s) <= capBytes {
		return s
	}
	cut := capBytes
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}

// Package imap implements provider.Provider over generic IMAP4rev1/rev2
// (github.com/emersion/go-imap/v2) and SMTP submission (net/smtp), covering
// iCloud Mail, Fastmail, Yahoo Mail, Proton Mail Bridge (127.0.0.1), and
// self-hosted or institutional servers.
package imap

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	stdmail "net/mail"
	"net/smtp"
	"sort"
	"strconv"
	"strings"
	gosync "sync"
	"time"

	goimap "github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/emersion/go-message/charset"
	gomail "github.com/emersion/go-message/mail"

	"github.com/arqueon/dankmail/core/errdefs"
	"github.com/arqueon/dankmail/core/internal/accounts"
	"github.com/arqueon/dankmail/core/internal/mailmime"
	"github.com/arqueon/dankmail/core/internal/mailtext"
	"github.com/arqueon/dankmail/core/internal/provider"
)

const (
	defaultBodyCap = 32 * 1024
	maxSyncWindow  = 150
	pageSize       = 25
)

// Options configures an IMAP provider instance.
type Options struct {
	Config       accounts.IMAPConfig
	Password     string
	SMTPPassword string
	BodyCapBytes int
}

// rawMessage is one fetched IMAP message used to assemble threads.
type rawMessage struct {
	Mailbox      string
	UIDValidity  uint32
	UID          uint32
	Flags        []goimap.Flag
	InternalDate time.Time
	Raw          []byte
}

// backend abstracts live IMAP/SMTP network calls so unit tests exercise
// threading, MIME/HTML distillation, attachment extraction, and mutations
// deterministically without sockets.
type backend interface {
	ResolveFolders(ctx context.Context) (specialFolders, error)
	FetchRecent(ctx context.Context, mailbox string, limit int) (uidValidity uint32, uidNext uint32, msgs []rawMessage, err error)
	FetchPage(ctx context.Context, mailbox string, beforeSeq int, count int) (msgs []rawMessage, nextOffset int, err error)
	FetchRawByID(ctx context.Context, mailbox string, uid uint32) ([]byte, error)
	ModifyFlagsByThread(ctx context.Context, mailboxes []string, threadIDs []string, add, remove []goimap.Flag) error
	MoveByThread(ctx context.Context, fromMailboxes []string, destMailbox string, createDest bool, threadIDs []string) error
	SendSMTP(ctx context.Context, from string, recipients []string, raw []byte) error
	AppendSent(ctx context.Context, sentMailbox string, raw []byte) error
}

type specialFolders struct {
	Inbox   string
	Archive string
	Trash   string
	Junk    string
	Sent    string
}

// Provider implements provider.Provider, provider.AttachmentFetcher,
// provider.ArchivedPager, and provider.SentPager for an IMAP/SMTP account.
type Provider struct {
	accountID  string
	email      string
	webmailURL string
	hasSMTP    bool
	autoSent   bool
	bodyCap    int
	api        backend

	mu      gosync.Mutex
	folders *specialFolders
}

// New constructs a live IMAP/SMTP Provider.
func New(accountID, email string, opts Options) *Provider {
	bodyCap := opts.BodyCapBytes
	if bodyCap <= 0 {
		bodyCap = defaultBodyCap
	}
	smtpPass := opts.SMTPPassword
	if smtpPass == "" {
		smtpPass = opts.Password
	}
	b := &liveBackend{
		cfg:      opts.Config,
		email:    email,
		password: opts.Password,
		smtpPass: smtpPass,
	}
	return newWithBackend(accountID, email, opts.Config, bodyCap, b)
}

func newWithBackend(accountID, email string, cfg accounts.IMAPConfig, bodyCap int, b backend) *Provider {
	if bodyCap <= 0 {
		bodyCap = defaultBodyCap
	}
	hostLower := strings.ToLower(cfg.Host)
	autoSent := accounts.IsLoopbackHost(cfg.Host) ||
		strings.Contains(hostLower, "gmail.") ||
		strings.Contains(hostLower, "office365.") ||
		strings.Contains(hostLower, "outlook.")
	return &Provider{
		accountID:  accountID,
		email:      strings.ToLower(strings.TrimSpace(email)),
		webmailURL: cfg.WebmailURL,
		hasSMTP:    strings.TrimSpace(cfg.SMTPHost) != "",
		autoSent:   autoSent,
		bodyCap:    bodyCap,
		api:        b,
	}
}

func (p *Provider) ID() string { return p.accountID }

func (p *Provider) Capabilities() provider.Capability {
	caps := provider.CapModifyFlags | provider.CapArchive | provider.CapTrash |
		provider.CapUnspam | provider.CapSpam
	if p.hasSMTP {
		caps |= provider.CapSendReply | provider.CapCompose
	}
	return caps
}

func (p *Provider) WebLink(_, _ string) (string, bool) {
	if p.webmailURL == "" {
		return "", false
	}
	return p.webmailURL, true
}

func (p *Provider) getFolders(ctx context.Context) (specialFolders, error) {
	p.mu.Lock()
	if p.folders != nil {
		f := *p.folders
		p.mu.Unlock()
		return f, nil
	}
	p.mu.Unlock()

	f, err := p.api.ResolveFolders(ctx)
	if err != nil {
		return specialFolders{}, classify(err)
	}
	if f.Inbox == "" {
		f.Inbox = "INBOX"
	}
	if f.Archive == "" {
		f.Archive = "Archive"
	}
	p.mu.Lock()
	p.folders = &f
	p.mu.Unlock()
	return f, nil
}

type syncCursor struct {
	InboxUIDValidity uint32 `json:"inboxUidValidity"`
	InboxUIDNext     uint32 `json:"inboxUidNext"`
}

func (p *Provider) Sync(ctx context.Context, cursor string) (provider.Changes, string, error) {
	folders, err := p.getFolders(ctx)
	if err != nil {
		return provider.Changes{}, cursor, err
	}

	uidVal, uidNext, inboxMsgs, err := p.api.FetchRecent(ctx, folders.Inbox, maxSyncWindow)
	if err != nil {
		return provider.Changes{}, cursor, classify(err)
	}

	var allMsgs []rawMessage
	allMsgs = append(allMsgs, inboxMsgs...)

	if folders.Junk != "" && !strings.EqualFold(folders.Junk, folders.Inbox) {
		if _, _, junkMsgs, jerr := p.api.FetchRecent(ctx, folders.Junk, maxSyncWindow/2); jerr == nil {
			allMsgs = append(allMsgs, junkMsgs...)
		}
	}
	if folders.Sent != "" && !strings.EqualFold(folders.Sent, folders.Inbox) {
		if _, _, sentMsgs, serr := p.api.FetchRecent(ctx, folders.Sent, maxSyncWindow/2); serr == nil {
			allMsgs = append(allMsgs, sentMsgs...)
		}
	}

	upserted := p.groupThreads(allMsgs, folders)
	nextCurRaw, _ := json.Marshal(syncCursor{
		InboxUIDValidity: uidVal,
		InboxUIDNext:     uidNext,
	})
	return provider.Changes{
		FullResync: true,
		Upserted:   upserted,
	}, string(nextCurRaw), nil
}

func (p *Provider) ArchivedPage(ctx context.Context, pageToken string) (provider.Changes, string, error) {
	folders, err := p.getFolders(ctx)
	if err != nil {
		return provider.Changes{}, pageToken, err
	}
	if folders.Archive == "" {
		return provider.Changes{Backfill: true}, "", nil
	}
	return p.folderPage(ctx, folders.Archive, folders, pageToken)
}

func (p *Provider) SentPage(ctx context.Context, pageToken string) (provider.Changes, string, error) {
	folders, err := p.getFolders(ctx)
	if err != nil {
		return provider.Changes{}, pageToken, err
	}
	if folders.Sent == "" {
		return provider.Changes{Backfill: true}, "", nil
	}
	return p.folderPage(ctx, folders.Sent, folders, pageToken)
}

func (p *Provider) folderPage(ctx context.Context, mailbox string, folders specialFolders, pageToken string) (provider.Changes, string, error) {
	offset := 0
	if pageToken != "" {
		if v, err := strconv.Atoi(pageToken); err == nil && v >= 0 {
			offset = v
		}
	}
	msgs, nextOffset, err := p.api.FetchPage(ctx, mailbox, offset, pageSize)
	if err != nil {
		return provider.Changes{}, pageToken, classify(err)
	}
	nextToken := ""
	if nextOffset > 0 {
		nextToken = strconv.Itoa(nextOffset)
	}
	return provider.Changes{
		Backfill: true,
		Upserted: p.groupThreads(msgs, folders),
	}, nextToken, nil
}

func (p *Provider) ModifyFlags(ctx context.Context, threadIDs []string, add, remove []provider.Flag) error {
	folders, err := p.getFolders(ctx)
	if err != nil {
		return err
	}
	var imapAdd, imapRemove []goimap.Flag
	for _, f := range add {
		switch f {
		case provider.FlagUnread:
			imapRemove = append(imapRemove, goimap.FlagSeen)
		case provider.FlagStarred:
			imapAdd = append(imapAdd, goimap.FlagFlagged)
		}
	}
	for _, f := range remove {
		switch f {
		case provider.FlagUnread:
			imapAdd = append(imapAdd, goimap.FlagSeen)
		case provider.FlagStarred:
			imapRemove = append(imapRemove, goimap.FlagFlagged)
		}
	}
	boxes := nonEmptyUnique(folders.Inbox, folders.Archive, folders.Junk, folders.Sent)
	return classify(p.api.ModifyFlagsByThread(ctx, boxes, threadIDs, imapAdd, imapRemove))
}

func (p *Provider) Archive(ctx context.Context, threadIDs []string) error {
	folders, err := p.getFolders(ctx)
	if err != nil {
		return err
	}
	return classify(p.api.MoveByThread(ctx, []string{folders.Inbox}, folders.Archive, true, threadIDs))
}

func (p *Provider) Unarchive(ctx context.Context, threadIDs []string) error {
	folders, err := p.getFolders(ctx)
	if err != nil {
		return err
	}
	sources := nonEmptyUnique(folders.Archive, folders.Trash)
	return classify(p.api.MoveByThread(ctx, sources, folders.Inbox, false, threadIDs))
}

func (p *Provider) Trash(ctx context.Context, threadIDs []string) error {
	folders, err := p.getFolders(ctx)
	if err != nil {
		return err
	}
	dest := folders.Trash
	if dest == "" {
		dest = "Trash"
	}
	sources := nonEmptyUnique(folders.Inbox, folders.Archive, folders.Junk)
	return classify(p.api.MoveByThread(ctx, sources, dest, true, threadIDs))
}

func (p *Provider) Spam(ctx context.Context, threadIDs []string) error {
	folders, err := p.getFolders(ctx)
	if err != nil {
		return err
	}
	dest := folders.Junk
	if dest == "" {
		dest = "Junk"
	}
	sources := nonEmptyUnique(folders.Inbox, folders.Archive)
	return classify(p.api.MoveByThread(ctx, sources, dest, true, threadIDs))
}

func (p *Provider) Unspam(ctx context.Context, threadIDs []string) error {
	folders, err := p.getFolders(ctx)
	if err != nil {
		return err
	}
	if folders.Junk == "" {
		return nil
	}
	return classify(p.api.MoveByThread(ctx, []string{folders.Junk}, folders.Inbox, false, threadIDs))
}

func (p *Provider) SendReply(ctx context.Context, _ string, r provider.ReplyDraft) error {
	if !p.hasSMTP {
		return errdefs.Wrap(errdefs.KindPermanent, errors.New("imap: SMTP host is not configured for this account"))
	}
	mailbox, uid, err := decodeMessageID(r.InReplyToMessageID)
	if err != nil {
		return errdefs.Wrap(errdefs.KindPermanent, err)
	}
	rawOrig, err := p.api.FetchRawByID(ctx, mailbox, uid)
	if err != nil {
		return classify(err)
	}
	parsedOrig := p.parseMessage(rawMessage{Mailbox: mailbox, UID: uid, Raw: rawOrig}, specialFolders{})
	rawReply, err := mailmime.BuildReply(parsedOrig.delta, p.email, r)
	if err != nil {
		return errdefs.Wrap(errdefs.KindPermanent, err)
	}
	rcpts := extractRecipients(rawReply)
	if err := p.api.SendSMTP(ctx, p.email, rcpts, rawReply); err != nil {
		return classify(err)
	}
	if !p.autoSent {
		if folders, ferr := p.getFolders(ctx); ferr == nil && folders.Sent != "" {
			_ = p.api.AppendSent(ctx, folders.Sent, rawReply)
		}
	}
	return nil
}

func (p *Provider) Compose(ctx context.Context, m provider.ComposeDraft) error {
	if !p.hasSMTP {
		return errdefs.Wrap(errdefs.KindPermanent, errors.New("imap: SMTP host is not configured for this account"))
	}
	rawMsg, err := mailmime.BuildCompose(p.email, m)
	if err != nil {
		return errdefs.Wrap(errdefs.KindPermanent, err)
	}
	var rcpts []string
	for _, list := range [][]string{m.To, m.Cc, m.Bcc} {
		for _, r := range list {
			if addr := bareEmail(r); addr != "" {
				rcpts = append(rcpts, addr)
			}
		}
	}
	if len(rcpts) == 0 {
		return errdefs.Wrap(errdefs.KindPermanent, mailmime.ErrNoRecipients)
	}
	if err := p.api.SendSMTP(ctx, p.email, rcpts, rawMsg); err != nil {
		return classify(err)
	}
	if !p.autoSent {
		if folders, ferr := p.getFolders(ctx); ferr == nil && folders.Sent != "" {
			_ = p.api.AppendSent(ctx, folders.Sent, rawMsg)
		}
	}
	return nil
}

// FetchAttachment downloads raw bytes of an attachment from the referenced IMAP message.
func (p *Provider) FetchAttachment(ctx context.Context, messageID, attachmentID, filename string) ([]byte, error) {
	mailbox, uid, err := decodeMessageID(messageID)
	if err != nil {
		return nil, errdefs.Wrap(errdefs.KindPermanent, err)
	}
	raw, err := p.api.FetchRawByID(ctx, mailbox, uid)
	if err != nil {
		return nil, classify(err)
	}
	data, err := extractAttachmentBytes(raw, attachmentID, filename)
	if err != nil {
		return nil, errdefs.Wrap(errdefs.KindPermanent, err)
	}
	return data, nil
}

// --- MIME parsing & RFC 5322 threading ---------------------------------

type parsedItem struct {
	threadKey string
	subject   string
	inInbox   bool
	inJunk    bool
	inTrash   bool
	unread    bool
	starred   bool
	delta     provider.MessageDelta
}

func (p *Provider) groupThreads(msgs []rawMessage, folders specialFolders) []provider.ThreadDelta {
	type bucket struct {
		id    string
		items []parsedItem
	}
	byKey := map[string]*bucket{}
	var order []string

	for _, rm := range msgs {
		if hasFlag(rm.Flags, goimap.FlagDraft) || hasFlag(rm.Flags, goimap.FlagDeleted) {
			continue
		}
		item := p.parseMessage(rm, folders)
		b, ok := byKey[item.threadKey]
		if !ok {
			b = &bucket{id: item.threadKey}
			byKey[item.threadKey] = b
			order = append(order, item.threadKey)
		}
		b.items = append(b.items, item)
	}

	out := make([]provider.ThreadDelta, 0, len(order))
	for _, k := range order {
		b := byKey[k]
		sort.Slice(b.items, func(i, j int) bool {
			return b.items[i].delta.Date < b.items[j].delta.Date
		})
		seenMsg := map[string]bool{}
		var deltas []provider.MessageDelta
		var participants []string
		seenPart := map[string]bool{}
		var unread, starred, inInbox, allJunk, allTrash bool
		allJunk = true
		allTrash = true
		subject := ""
		snippet := ""
		var lastDate int64

		for _, it := range b.items {
			dedupeKey := it.delta.RFC822MessageID
			if dedupeKey == "" {
				dedupeKey = it.delta.MessageID
			}
			if seenMsg[dedupeKey] {
				if it.inInbox {
					inInbox = true
				}
				if it.unread {
					unread = true
				}
				if it.starred {
					starred = true
				}
				continue
			}
			seenMsg[dedupeKey] = true

			if it.inInbox {
				inInbox = true
			}
			if !it.inJunk {
				allJunk = false
			}
			if !it.inTrash {
				allTrash = false
			}
			if it.unread && !it.delta.IsSent {
				unread = true
			}
			if it.starred {
				starred = true
			}
			if it.subject != "" && subject == "" {
				subject = it.subject
			}
			if it.delta.Date >= lastDate {
				lastDate = it.delta.Date
				if it.delta.Snippet != "" {
					snippet = it.delta.Snippet
				}
				if it.subject != "" {
					subject = it.subject
				}
			}
			if it.delta.From != "" {
				key := strings.ToLower(it.delta.From)
				if !seenPart[key] {
					seenPart[key] = true
					participants = append(participants, it.delta.From)
				}
			}
			deltas = append(deltas, it.delta)
		}
		if len(deltas) == 0 {
			continue
		}
		var labels []string
		if allJunk && !inInbox {
			labels = append(labels, "SPAM")
		}
		if allTrash && !inInbox {
			labels = append(labels, "TRASH")
		}
		out = append(out, provider.ThreadDelta{
			ThreadID:     b.id,
			Subject:      subject,
			Snippet:      snippet,
			LastMessage:  lastDate,
			Participants: participants,
			Unread:       unread,
			Starred:      starred,
			InInbox:      inInbox,
			Labels:       labels,
			MessageCount: len(deltas),
			Messages:     deltas,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].LastMessage > out[j].LastMessage })
	return out
}

func (p *Provider) parseMessage(rm rawMessage, folders specialFolders) parsedItem {
	msgID := encodeMessageID(rm.Mailbox, rm.UID)
	dateSec := rm.InternalDate.Unix()
	if rm.InternalDate.IsZero() {
		dateSec = time.Now().Unix()
	}

	mr, err := gomail.CreateReader(bytes.NewReader(rm.Raw))
	var (
		subject   string
		from      string
		to, cc    []string
		rfc822ID  string
		refs      string
		inReplyTo string
		replyTo   string
		plainBody string
		htmlBody  string
		atts      []provider.AttachmentMeta
	)

	if err == nil {
		subject, _ = mr.Header.Subject()
		if d, derr := mr.Header.Date(); derr == nil && !d.IsZero() {
			dateSec = d.Unix()
		}
		if addrs, aerr := mr.Header.AddressList("From"); aerr == nil && len(addrs) > 0 {
			from = formatMailAddr(addrs[0])
		} else if rawFrom := mr.Header.Get("From"); rawFrom != "" {
			from = decodeRFC2047(rawFrom)
		}
		to = headerAddrStrings(&mr.Header, "To")
		cc = headerAddrStrings(&mr.Header, "Cc")
		if addrs, aerr := mr.Header.AddressList("Reply-To"); aerr == nil && len(addrs) > 0 {
			replyTo = formatMailAddr(addrs[0])
		}
		rfc822ID = cleanMsgID(mr.Header.Get("Message-Id"))
		refs = strings.TrimSpace(mr.Header.Get("References"))
		inReplyTo = cleanMsgID(mr.Header.Get("In-Reply-To"))

		attIdx := 0
		for {
			part, perr := mr.NextPart()
			if perr != nil {
				break
			}
			switch h := part.Header.(type) {
			case *gomail.InlineHeader:
				ct, _, _ := h.ContentType()
				ct = strings.ToLower(ct)
				data, _ := io.ReadAll(io.LimitReader(part.Body, int64(p.bodyCap*4)))
				if strings.HasPrefix(ct, "text/html") {
					if htmlBody == "" {
						htmlBody = string(data)
					}
				} else if strings.HasPrefix(ct, "text/plain") || ct == "" {
					if plainBody == "" {
						plainBody = string(data)
					}
				}
			case *gomail.AttachmentHeader:
				fname, _ := h.Filename()
				if fname == "" {
					fname = fmt.Sprintf("attachment-%d", attIdx+1)
				}
				ct, _, _ := h.ContentType()
				if ct == "" {
					ct = "application/octet-stream"
				}
				n, _ := io.Copy(io.Discard, io.LimitReader(part.Body, 128<<20))
				atts = append(atts, provider.AttachmentMeta{
					AttachmentID: strconv.Itoa(attIdx),
					Filename:     fname,
					MimeType:     ct,
					Size:         n,
				})
				attIdx++
			}
		}
	} else {
		plainBody = string(rm.Raw)
	}

	body := mailtext.TruncateAtRune(mailtext.ExtractPreferredBody(htmlBody, plainBody), p.bodyCap)
	snippet := makeSnippet(body)
	threadKey := computeThreadKey(rfc822ID, inReplyTo, refs, subject, msgID)

	isSent := strings.EqualFold(rm.Mailbox, folders.Sent) || (p.email != "" && bareEmail(from) == p.email)
	replyHeaders := map[string]string{}
	if subject != "" {
		replyHeaders[provider.HeaderSubject] = subject
	}
	if refs != "" {
		replyHeaders[provider.HeaderReferences] = refs
	}
	if replyTo != "" {
		replyHeaders[provider.HeaderReplyTo] = replyTo
	}

	return parsedItem{
		threadKey: threadKey,
		subject:   subject,
		inInbox:   strings.EqualFold(rm.Mailbox, folders.Inbox) || (folders.Inbox == "" && strings.EqualFold(rm.Mailbox, "INBOX")),
		inJunk:    folders.Junk != "" && strings.EqualFold(rm.Mailbox, folders.Junk),
		inTrash:   folders.Trash != "" && strings.EqualFold(rm.Mailbox, folders.Trash),
		unread:    !hasFlag(rm.Flags, goimap.FlagSeen),
		starred:   hasFlag(rm.Flags, goimap.FlagFlagged),
		delta: provider.MessageDelta{
			MessageID:       msgID,
			RFC822MessageID: rfc822ID,
			IsSent:          isSent,
			From:            from,
			To:              to,
			Cc:              cc,
			Date:            dateSec,
			Snippet:         snippet,
			BodyText:        body,
			Attachments:     atts,
			ReplyHeaders:    replyHeaders,
		},
	}
}

func extractAttachmentBytes(raw []byte, attachmentID, filename string) ([]byte, error) {
	mr, err := gomail.CreateReader(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	wantIdx := -1
	if attachmentID != "" {
		if n, convErr := strconv.Atoi(attachmentID); convErr == nil {
			wantIdx = n
		}
	}
	attIdx := 0
	for {
		part, perr := mr.NextPart()
		if perr != nil {
			break
		}
		h, ok := part.Header.(*gomail.AttachmentHeader)
		if !ok {
			continue
		}
		fname, _ := h.Filename()
		if (wantIdx >= 0 && attIdx == wantIdx) || (filename != "" && fname == filename) {
			return io.ReadAll(io.LimitReader(part.Body, 128<<20))
		}
		attIdx++
	}
	return nil, fmt.Errorf("imap: attachment %q not found", filename)
}

func computeThreadKey(rfc822ID, inReplyTo, refs, subject, fallback string) string {
	if refs != "" {
		fields := strings.Fields(refs)
		if len(fields) > 0 {
			if root := cleanMsgID(fields[0]); root != "" {
				return "thr:" + root
			}
		}
	}
	if inReplyTo != "" {
		return "thr:" + inReplyTo
	}
	if rfc822ID != "" {
		return "thr:" + rfc822ID
	}
	normSubj := normalizeSubject(subject)
	if normSubj != "" {
		return "subj:" + normSubj
	}
	return "msg:" + fallback
}

func normalizeSubject(s string) string {
	s = strings.TrimSpace(s)
	for {
		lower := strings.ToLower(s)
		switch {
		case strings.HasPrefix(lower, "re:"):
			s = strings.TrimSpace(s[3:])
		case strings.HasPrefix(lower, "fwd:"):
			s = strings.TrimSpace(s[4:])
		case strings.HasPrefix(lower, "fw:"):
			s = strings.TrimSpace(s[3:])
		default:
			return strings.ToLower(s)
		}
	}
}

func encodeMessageID(mailbox string, uid uint32) string {
	return base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf("%s\n%d", mailbox, uid)))
}

func decodeMessageID(encoded string) (string, uint32, error) {
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return "", 0, fmt.Errorf("invalid imap message id")
	}
	idx := strings.LastIndexByte(string(raw), '\n')
	if idx <= 0 {
		return "", 0, fmt.Errorf("invalid imap message id format")
	}
	uid, err := strconv.ParseUint(string(raw[idx+1:]), 10, 32)
	if err != nil {
		return "", 0, fmt.Errorf("invalid imap uid")
	}
	return string(raw[:idx]), uint32(uid), nil
}

// --- Live IMAP / SMTP backend -----------------------------------------

type liveBackend struct {
	cfg      accounts.IMAPConfig
	email    string
	password string
	smtpPass string
}

func (b *liveBackend) dialIMAP(ctx context.Context) (*imapclient.Client, error) {
	addr := fmt.Sprintf("%s:%d", b.cfg.Host, b.cfg.Port)
	opts := &imapclient.Options{
		TLSConfig: &tls.Config{
			ServerName:         b.cfg.Host,
			MinVersion:         tls.VersionTLS12,
			InsecureSkipVerify: accounts.IsLoopbackHost(b.cfg.Host),
		},
		WordDecoder: &mime.WordDecoder{CharsetReader: charset.Reader},
		Dialer:      &net.Dialer{Timeout: 20 * time.Second},
	}

	type res struct {
		cli *imapclient.Client
		err error
	}
	ch := make(chan res, 1)
	go func() {
		var (
			cli *imapclient.Client
			err error
		)
		if b.cfg.Security == "starttls" {
			cli, err = imapclient.DialStartTLS(addr, opts)
		} else {
			cli, err = imapclient.DialTLS(addr, opts)
		}
		ch <- res{cli, err}
	}()

	var cli *imapclient.Client
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case r := <-ch:
		if r.err != nil {
			return nil, r.err
		}
		cli = r.cli
	}

	user := b.cfg.Username
	if user == "" {
		user = b.email
	}
	if err := cli.Login(user, b.password).Wait(); err != nil {
		_ = cli.Close()
		return nil, errdefs.Wrap(errdefs.KindAuth, err)
	}
	return cli, nil
}

func (b *liveBackend) ResolveFolders(ctx context.Context) (specialFolders, error) {
	cli, err := b.dialIMAP(ctx)
	if err != nil {
		return specialFolders{}, err
	}
	defer cli.Close()

	boxes, err := cli.List("", "*", &goimap.ListOptions{ReturnSubscribed: false}).Collect()
	if err != nil {
		return specialFolders{}, err
	}
	out := specialFolders{Inbox: "INBOX"}
	for _, mb := range boxes {
		for _, attr := range mb.Attrs {
			switch attr {
			case goimap.MailboxAttrArchive, goimap.MailboxAttrAll:
				if out.Archive == "" {
					out.Archive = mb.Mailbox
				}
			case goimap.MailboxAttrTrash:
				out.Trash = mb.Mailbox
			case goimap.MailboxAttrJunk:
				out.Junk = mb.Mailbox
			case goimap.MailboxAttrSent:
				out.Sent = mb.Mailbox
			}
		}
	}
	for _, mb := range boxes {
		lower := strings.ToLower(mb.Mailbox)
		switch {
		case strings.EqualFold(mb.Mailbox, "INBOX"):
			out.Inbox = mb.Mailbox
		case out.Archive == "" && (lower == "archive" || lower == "archives" || lower == "all mail"):
			out.Archive = mb.Mailbox
		case out.Trash == "" && (lower == "trash" || lower == "deleted messages" || lower == "deleted items" || lower == "bin"):
			out.Trash = mb.Mailbox
		case out.Junk == "" && (lower == "junk" || lower == "spam" || lower == "junk e-mail"):
			out.Junk = mb.Mailbox
		case out.Sent == "" && (lower == "sent" || lower == "sent messages" || lower == "sent items"):
			out.Sent = mb.Mailbox
		}
	}
	return out, nil
}

func (b *liveBackend) FetchRecent(ctx context.Context, mailbox string, limit int) (uint32, uint32, []rawMessage, error) {
	cli, err := b.dialIMAP(ctx)
	if err != nil {
		return 0, 0, nil, err
	}
	defer cli.Close()

	sel, err := cli.Select(mailbox, &goimap.SelectOptions{ReadOnly: true}).Wait()
	if err != nil {
		return 0, 0, nil, err
	}
	if sel.NumMessages == 0 {
		return sel.UIDValidity, uint32(sel.UIDNext), nil, nil
	}
	start := uint32(1)
	if int(sel.NumMessages) > limit {
		start = sel.NumMessages - uint32(limit) + 1
	}
	var seq goimap.SeqSet
	seq.AddRange(start, sel.NumMessages)
	msgs, err := fetchMessages(cli, mailbox, sel.UIDValidity, seq)
	return sel.UIDValidity, uint32(sel.UIDNext), msgs, err
}

func (b *liveBackend) FetchPage(ctx context.Context, mailbox string, offset int, count int) ([]rawMessage, int, error) {
	cli, err := b.dialIMAP(ctx)
	if err != nil {
		return nil, 0, err
	}
	defer cli.Close()

	sel, err := cli.Select(mailbox, &goimap.SelectOptions{ReadOnly: true}).Wait()
	if err != nil {
		return nil, 0, nil
	}
	total := int(sel.NumMessages)
	end := total - offset
	if end <= 0 {
		return nil, 0, nil
	}
	start := end - count + 1
	nextOffset := offset + count
	if start < 1 {
		start = 1
		nextOffset = 0
	}
	var seq goimap.SeqSet
	seq.AddRange(uint32(start), uint32(end))
	msgs, err := fetchMessages(cli, mailbox, sel.UIDValidity, seq)
	return msgs, nextOffset, err
}

func fetchMessages(cli *imapclient.Client, mailbox string, uidVal uint32, numSet goimap.NumSet) ([]rawMessage, error) {
	bodySec := &goimap.FetchItemBodySection{Peek: true}
	bufs, err := cli.Fetch(numSet, &goimap.FetchOptions{
		UID:          true,
		Flags:        true,
		InternalDate: true,
		BodySection:  []*goimap.FetchItemBodySection{bodySec},
	}).Collect()
	if err != nil {
		return nil, err
	}
	out := make([]rawMessage, 0, len(bufs))
	for _, buf := range bufs {
		raw := buf.FindBodySection(bodySec)
		out = append(out, rawMessage{
			Mailbox:      mailbox,
			UIDValidity:  uidVal,
			UID:          uint32(buf.UID),
			Flags:        buf.Flags,
			InternalDate: buf.InternalDate,
			Raw:          raw,
		})
	}
	return out, nil
}

func (b *liveBackend) FetchRawByID(ctx context.Context, mailbox string, uid uint32) ([]byte, error) {
	cli, err := b.dialIMAP(ctx)
	if err != nil {
		return nil, err
	}
	defer cli.Close()

	if _, err := cli.Select(mailbox, &goimap.SelectOptions{ReadOnly: true}).Wait(); err != nil {
		return nil, err
	}
	var uids goimap.UIDSet
	uids.AddNum(goimap.UID(uid))
	bodySec := &goimap.FetchItemBodySection{Peek: true}
	bufs, err := cli.Fetch(uids, &goimap.FetchOptions{
		UID:         true,
		BodySection: []*goimap.FetchItemBodySection{bodySec},
	}).Collect()
	if err != nil {
		return nil, err
	}
	if len(bufs) == 0 {
		return nil, fmt.Errorf("imap: message UID %d not found in %s", uid, mailbox)
	}
	return bufs[0].FindBodySection(bodySec), nil
}

func (b *liveBackend) ModifyFlagsByThread(ctx context.Context, mailboxes []string, threadIDs []string, add, remove []goimap.Flag) error {
	cli, err := b.dialIMAP(ctx)
	if err != nil {
		return err
	}
	defer cli.Close()

	want := toSet(threadIDs)
	for _, box := range mailboxes {
		uids, err := findMatchingUIDs(cli, box, want, false)
		if err != nil || len(uids) == 0 {
			continue
		}
		var uidSet goimap.UIDSet
		for _, u := range uids {
			uidSet.AddNum(u)
		}
		if len(add) > 0 {
			if err := cli.Store(uidSet, &goimap.StoreFlags{Op: goimap.StoreFlagsAdd, Silent: true, Flags: add}, nil).Close(); err != nil {
				return err
			}
		}
		if len(remove) > 0 {
			if err := cli.Store(uidSet, &goimap.StoreFlags{Op: goimap.StoreFlagsDel, Silent: true, Flags: remove}, nil).Close(); err != nil {
				return err
			}
		}
	}
	return nil
}

func (b *liveBackend) MoveByThread(ctx context.Context, fromMailboxes []string, destMailbox string, createDest bool, threadIDs []string) error {
	cli, err := b.dialIMAP(ctx)
	if err != nil {
		return err
	}
	defer cli.Close()

	if createDest && destMailbox != "" {
		_ = cli.Create(destMailbox, nil).Wait()
	}

	want := toSet(threadIDs)
	for _, box := range fromMailboxes {
		if strings.EqualFold(box, destMailbox) {
			continue
		}
		uids, err := findMatchingUIDs(cli, box, want, false)
		if err != nil || len(uids) == 0 {
			continue
		}
		var uidSet goimap.UIDSet
		for _, u := range uids {
			uidSet.AddNum(u)
		}
		if cli.Caps().Has(goimap.CapMove) {
			if _, err := cli.Move(uidSet, destMailbox).Wait(); err != nil {
				return err
			}
			continue
		}
		if _, err := cli.Copy(uidSet, destMailbox).Wait(); err != nil {
			return err
		}
		if err := cli.Store(uidSet, &goimap.StoreFlags{
			Op:     goimap.StoreFlagsAdd,
			Silent: true,
			Flags:  []goimap.Flag{goimap.FlagDeleted},
		}, nil).Close(); err != nil {
			return err
		}
		_ = cli.UIDExpunge(uidSet).Close()
	}
	return nil
}

func findMatchingUIDs(cli *imapclient.Client, mailbox string, want map[string]bool, readOnly bool) ([]goimap.UID, error) {
	sel, err := cli.Select(mailbox, &goimap.SelectOptions{ReadOnly: readOnly}).Wait()
	if err != nil || sel.NumMessages == 0 {
		return nil, err
	}
	var seq goimap.SeqSet
	seq.AddRange(1, sel.NumMessages)
	hdrSec := &goimap.FetchItemBodySection{
		Specifier: goimap.PartSpecifierHeader,
		HeaderFields: []string{
			"Message-ID", "In-Reply-To", "References", "Subject",
		},
		Peek: true,
	}
	bufs, err := cli.Fetch(seq, &goimap.FetchOptions{
		UID:         true,
		BodySection: []*goimap.FetchItemBodySection{hdrSec},
	}).Collect()
	if err != nil {
		return nil, err
	}
	var matched []goimap.UID
	for _, buf := range bufs {
		rawHdr := buf.FindBodySection(hdrSec)
		mr, merr := gomail.CreateReader(bytes.NewReader(append(rawHdr, '\r', '\n')))
		var rfcID, inReply, refs, subj string
		if merr == nil {
			rfcID = cleanMsgID(mr.Header.Get("Message-Id"))
			inReply = cleanMsgID(mr.Header.Get("In-Reply-To"))
			refs = strings.TrimSpace(mr.Header.Get("References"))
			subj, _ = mr.Header.Subject()
		}
		key := computeThreadKey(rfcID, inReply, refs, subj, encodeMessageID(mailbox, uint32(buf.UID)))
		if want[key] {
			matched = append(matched, buf.UID)
		}
	}
	return matched, nil
}

func (b *liveBackend) SendSMTP(ctx context.Context, from string, recipients []string, raw []byte) error {
	host := strings.TrimSpace(b.cfg.SMTPHost)
	port := b.cfg.SMTPPort
	if port == 0 {
		port = 587
	}
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	tlsCfg := &tls.Config{
		ServerName:         host,
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: accounts.IsLoopbackHost(host),
	}

	dialer := &net.Dialer{Timeout: 20 * time.Second}
	var (
		conn net.Conn
		err  error
	)
	if port == 465 {
		tlsDialer := &tls.Dialer{NetDialer: dialer, Config: tlsCfg}
		conn, err = tlsDialer.DialContext(ctx, "tcp", addr)
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return err
	}
	defer conn.Close()

	cli, err := smtp.NewClient(conn, host)
	if err != nil {
		return err
	}
	defer cli.Close()

	if port != 465 {
		if ok, _ := cli.Extension("STARTTLS"); ok {
			if err := cli.StartTLS(tlsCfg); err != nil {
				return err
			}
		}
	}
	user := b.cfg.Username
	if user == "" {
		user = b.email
	}
	if ok, _ := cli.Extension("AUTH"); ok && b.smtpPass != "" {
		if err := cli.Auth(smtp.PlainAuth("", user, b.smtpPass, host)); err != nil {
			return errdefs.Wrap(errdefs.KindAuth, err)
		}
	}
	if err := cli.Mail(bareEmail(from)); err != nil {
		return err
	}
	for _, r := range recipients {
		if err := cli.Rcpt(bareEmail(r)); err != nil {
			return err
		}
	}
	w, err := cli.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(raw); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return cli.Quit()
}

func (b *liveBackend) AppendSent(ctx context.Context, sentMailbox string, raw []byte) error {
	cli, err := b.dialIMAP(ctx)
	if err != nil {
		return err
	}
	defer cli.Close()

	cmd := cli.Append(sentMailbox, int64(len(raw)), &goimap.AppendOptions{
		Flags: []goimap.Flag{goimap.FlagSeen},
		Time:  time.Now(),
	})
	if _, err := cmd.Write(raw); err != nil {
		return err
	}
	if err := cmd.Close(); err != nil {
		return err
	}
	_, err = cmd.Wait()
	return err
}

// --- helpers ----------------------------------------------------------

func classify(err error) error {
	if err == nil {
		return nil
	}
	if errdefs.KindOf(err) != errdefs.KindUnknown {
		return err
	}
	var imapErr *goimap.Error
	if errors.As(err, &imapErr) {
		if imapErr.Code == goimap.ResponseCodeAuthenticationFailed || imapErr.Code == goimap.ResponseCodeAuthorizationFailed {
			return errdefs.Wrap(errdefs.KindAuth, err)
		}
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "authenticationfailed") || strings.Contains(msg, "invalid credentials") || strings.Contains(msg, "login failed") {
		return errdefs.Wrap(errdefs.KindAuth, err)
	}
	return errdefs.Wrap(errdefs.KindNetwork, err)
}

func hasFlag(flags []goimap.Flag, want goimap.Flag) bool {
	for _, f := range flags {
		if strings.EqualFold(string(f), string(want)) {
			return true
		}
	}
	return false
}

func cleanMsgID(raw string) string {
	return strings.Trim(strings.TrimSpace(raw), "<>")
}

func formatMailAddr(a *gomail.Address) string {
	if a == nil {
		return ""
	}
	if a.Name != "" && !strings.EqualFold(a.Name, a.Address) {
		return fmt.Sprintf("%s <%s>", a.Name, a.Address)
	}
	return a.Address
}

func headerAddrStrings(h *gomail.Header, key string) []string {
	addrs, err := h.AddressList(key)
	if err != nil {
		if raw := strings.TrimSpace(h.Get(key)); raw != "" {
			return []string{decodeRFC2047(raw)}
		}
		return nil
	}
	out := make([]string, 0, len(addrs))
	for _, a := range addrs {
		out = append(out, formatMailAddr(a))
	}
	return out
}

func decodeRFC2047(s string) string {
	dec := mime.WordDecoder{CharsetReader: charset.Reader}
	if d, err := dec.DecodeHeader(s); err == nil {
		return d
	}
	return s
}

func makeSnippet(body string) string {
	s := strings.Join(strings.Fields(body), " ")
	if len(s) > 180 {
		return mailtext.TruncateAtRune(s, 180)
	}
	return s
}

func bareEmail(raw string) string {
	if a, err := stdmail.ParseAddress(raw); err == nil {
		return strings.ToLower(a.Address)
	}
	return strings.ToLower(strings.Trim(strings.TrimSpace(raw), "<>"))
}

func extractRecipients(rawMIME []byte) []string {
	msg, err := stdmail.ReadMessage(bytes.NewReader(rawMIME))
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, key := range []string{"To", "Cc", "Bcc"} {
		addrs, err := msg.Header.AddressList(key)
		if err != nil {
			continue
		}
		for _, a := range addrs {
			addr := strings.ToLower(a.Address)
			if addr != "" && !seen[addr] {
				seen[addr] = true
				out = append(out, addr)
			}
		}
	}
	return out
}

func nonEmptyUnique(items ...string) []string {
	seen := map[string]bool{}
	var out []string
	for _, it := range items {
		it = strings.TrimSpace(it)
		key := strings.ToLower(it)
		if it == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, it)
	}
	return out
}

func toSet(items []string) map[string]bool {
	m := make(map[string]bool, len(items))
	for _, it := range items {
		m[it] = true
	}
	return m
}

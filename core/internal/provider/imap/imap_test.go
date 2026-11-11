package imap

import (
	"context"
	"strings"
	"testing"
	"time"

	goimap "github.com/emersion/go-imap/v2"

	"github.com/arqueon/dankmail/core/internal/accounts"
	"github.com/arqueon/dankmail/core/internal/provider"
)

type fakeBackend struct {
	folders  specialFolders
	boxes    map[string][]rawMessage
	sentSMTP [][]byte
	movedTo  string
	addedFlg []goimap.Flag
}

func (f *fakeBackend) ResolveFolders(_ context.Context) (specialFolders, error) {
	return f.folders, nil
}

func (f *fakeBackend) FetchRecent(_ context.Context, mailbox string, _ int) (uint32, uint32, []rawMessage, error) {
	return 10, 42, f.boxes[mailbox], nil
}

func (f *fakeBackend) FetchPage(_ context.Context, mailbox string, offset int, _ int) ([]rawMessage, int, error) {
	if offset > 0 {
		return nil, 0, nil
	}
	return f.boxes[mailbox], 0, nil
}

func (f *fakeBackend) FetchRawByID(_ context.Context, mailbox string, uid uint32) ([]byte, error) {
	for _, m := range f.boxes[mailbox] {
		if m.UID == uid {
			return m.Raw, nil
		}
	}
	return nil, nil
}

func (f *fakeBackend) ModifyFlagsByThread(_ context.Context, _ []string, _ []string, add, _ []goimap.Flag) error {
	f.addedFlg = append(f.addedFlg, add...)
	return nil
}

func (f *fakeBackend) MoveByThread(_ context.Context, _ []string, dest string, _ bool, _ []string) error {
	f.movedTo = dest
	return nil
}

func (f *fakeBackend) SendSMTP(_ context.Context, _ string, _ []string, raw []byte) error {
	f.sentSMTP = append(f.sentSMTP, raw)
	return nil
}

func (f *fakeBackend) AppendSent(_ context.Context, _ string, _ []byte) error { return nil }

func TestIMAPSyncThreadingHTMLAndAttachmentFetch(t *testing.T) {
	raw1 := strings.Join([]string{
		"From: Alice <alice@example.org>",
		"To: User <user@example.org>",
		"Subject: Quarterly Report",
		"Message-ID: <root-1@example.org>",
		"Date: Mon, 11 Nov 2026 09:00:00 +0000",
		"MIME-Version: 1.0",
		"Content-Type: multipart/mixed; boundary=\"mix\"",
		"",
		"--mix",
		"Content-Type: text/html; charset=utf-8",
		"",
		"<p>See the <a href=\"https://example.org/q4\">Q4 dashboard</a>.</p>",
		"--mix",
		"Content-Type: application/pdf; name=\"q4.pdf\"",
		"Content-Disposition: attachment; filename=\"q4.pdf\"",
		"Content-Transfer-Encoding: base64",
		"",
		"JVBERi0xLjQK",
		"--mix--",
		"",
	}, "\r\n")

	raw2 := strings.Join([]string{
		"From: User <user@example.org>",
		"To: Alice <alice@example.org>",
		"Subject: Re: Quarterly Report",
		"Message-ID: <reply-2@example.org>",
		"In-Reply-To: <root-1@example.org>",
		"References: <root-1@example.org>",
		"Date: Mon, 11 Nov 2026 09:10:00 +0000",
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=utf-8",
		"",
		"Looks great, thanks!",
		"",
	}, "\r\n")

	fb := &fakeBackend{
		folders: specialFolders{Inbox: "INBOX", Archive: "Archive", Trash: "Trash", Junk: "Junk", Sent: "Sent"},
		boxes: map[string][]rawMessage{
			"INBOX": {
				{Mailbox: "INBOX", UID: 101, InternalDate: time.Unix(1700000000, 0), Raw: []byte(raw1)},
			},
			"Sent": {
				{Mailbox: "Sent", UID: 202, Flags: []goimap.Flag{goimap.FlagSeen}, InternalDate: time.Unix(1700000600, 0), Raw: []byte(raw2)},
			},
		},
	}

	p := newWithBackend("acct-imap", "user@example.org", accounts.IMAPConfig{
		Host: "imap.example.org", Port: 993, Security: "tls",
		SMTPHost: "smtp.example.org", SMTPPort: 587,
	}, 32768, fb)

	changes, _, err := p.Sync(context.Background(), "")
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if len(changes.Upserted) != 1 {
		t.Fatalf("got %d threads, want 1 merged thread", len(changes.Upserted))
	}
	th := changes.Upserted[0]
	if th.MessageCount != 2 || !th.InInbox || !th.Unread {
		t.Fatalf("unexpected thread state: %+v", th)
	}
	if !strings.Contains(th.Messages[0].BodyText, "[Q4 dashboard](https://example.org/q4)") {
		t.Fatalf("expected distilled HTML link, got %q", th.Messages[0].BodyText)
	}
	if len(th.Messages[0].Attachments) != 1 || th.Messages[0].Attachments[0].Filename != "q4.pdf" {
		t.Fatalf("expected q4.pdf attachment, got %+v", th.Messages[0].Attachments)
	}

	data, err := p.FetchAttachment(context.Background(), th.Messages[0].MessageID, th.Messages[0].Attachments[0].AttachmentID, "q4.pdf")
	if err != nil {
		t.Fatalf("FetchAttachment: %v", err)
	}
	if string(data) != "%PDF-1.4\n" {
		t.Fatalf("unexpected attachment payload: %q", string(data))
	}

	if err := p.Archive(context.Background(), []string{th.ThreadID}); err != nil || fb.movedTo != "Archive" {
		t.Fatalf("Archive failed: err=%v movedTo=%q", err, fb.movedTo)
	}
	if err := p.Compose(context.Background(), provider.ComposeDraft{
		To: []string{"alice@example.org"}, Subject: "Hello", Body: "Test",
	}); err != nil || len(fb.sentSMTP) != 1 {
		t.Fatalf("Compose failed: err=%v sent=%d", err, len(fb.sentSMTP))
	}
}

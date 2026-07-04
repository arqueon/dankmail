package gmail

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/arqueon/dankmail/core/errdefs"
	"github.com/arqueon/dankmail/core/internal/provider"
	"golang.org/x/oauth2"
	gmailv1 "google.golang.org/api/gmail/v1"
	"google.golang.org/api/googleapi"
)

// ---- fake gmailAPI ---------------------------------------------------

type listPage struct {
	ids  []string
	next string
}

type modifyCall struct {
	threadID string
	add      []string
	remove   []string
}

type sentCall struct {
	threadID string
	raw      []byte
}

type fakeAPI struct {
	calls []string

	profileEmail string
	profileHist  uint64
	profileErr   error

	listPages map[string][]listPage // key: single label ID
	listErr   error

	threads   map[string]*gmailv1.Thread
	threadErr map[string]error

	histPages []*gmailv1.ListHistoryResponse
	histErr   error

	msgMeta map[string]*gmailv1.Message

	modifies  []modifyCall
	modifyErr error

	sent    []sentCall
	sendErr error
}

func (f *fakeAPI) record(name string) { f.calls = append(f.calls, name) }

func pageIndex(t *testing.T, token string) int {
	t.Helper()
	if token == "" {
		return 0
	}
	i, err := strconv.Atoi(token)
	if err != nil {
		t.Fatalf("bad page token %q", token)
	}
	return i
}

func (f *fakeAPI) ListThreads(_ context.Context, labelIDs []string, pageToken string) ([]string, string, error) {
	f.record("ListThreads")
	if f.listErr != nil {
		return nil, "", f.listErr
	}
	if len(labelIDs) != 1 {
		return nil, "", errors.New("fake: expected exactly one label per list call")
	}
	pages := f.listPages[labelIDs[0]]
	idx := 0
	if pageToken != "" {
		idx, _ = strconv.Atoi(pageToken)
	}
	if idx >= len(pages) {
		return nil, "", nil
	}
	return pages[idx].ids, pages[idx].next, nil
}

func (f *fakeAPI) GetThread(_ context.Context, id string) (*gmailv1.Thread, error) {
	f.record("GetThread")
	if err, ok := f.threadErr[id]; ok {
		return nil, err
	}
	t, ok := f.threads[id]
	if !ok {
		return nil, &googleapi.Error{Code: 404, Message: "thread not found"}
	}
	return t, nil
}

func (f *fakeAPI) GetMessageMetadata(_ context.Context, id string) (*gmailv1.Message, error) {
	f.record("GetMessageMetadata")
	m, ok := f.msgMeta[id]
	if !ok {
		return nil, &googleapi.Error{Code: 404, Message: "message not found"}
	}
	return m, nil
}

func (f *fakeAPI) ListHistory(_ context.Context, _ uint64, pageToken string) (*gmailv1.ListHistoryResponse, error) {
	f.record("ListHistory")
	if f.histErr != nil {
		return nil, f.histErr
	}
	idx := 0
	if pageToken != "" {
		idx, _ = strconv.Atoi(pageToken)
	}
	if idx >= len(f.histPages) {
		return &gmailv1.ListHistoryResponse{}, nil
	}
	return f.histPages[idx], nil
}

func (f *fakeAPI) ModifyThread(_ context.Context, threadID string, add, remove []string) error {
	f.record("ModifyThread")
	if f.modifyErr != nil {
		return f.modifyErr
	}
	f.modifies = append(f.modifies, modifyCall{threadID: threadID, add: add, remove: remove})
	return nil
}

func (f *fakeAPI) SendMessage(_ context.Context, threadID string, raw []byte) error {
	f.record("SendMessage")
	if f.sendErr != nil {
		return f.sendErr
	}
	f.sent = append(f.sent, sentCall{threadID: threadID, raw: raw})
	return nil
}

func (f *fakeAPI) GetProfile(_ context.Context) (string, uint64, error) {
	f.record("GetProfile")
	if f.profileErr != nil {
		return "", 0, f.profileErr
	}
	return f.profileEmail, f.profileHist, nil
}

// ---- fixtures --------------------------------------------------------

func hdrs(pairs ...string) []*gmailv1.MessagePartHeader {
	var out []*gmailv1.MessagePartHeader
	for i := 0; i < len(pairs); i += 2 {
		out = append(out, &gmailv1.MessagePartHeader{Name: pairs[i], Value: pairs[i+1]})
	}
	return out
}

func b64(s string) string    { return base64.URLEncoding.EncodeToString([]byte(s)) }
func b64raw(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }

func fixtureThreads() map[string]*gmailv1.Thread {
	return map[string]*gmailv1.Thread{
		"t1": {
			Id: "t1",
			Messages: []*gmailv1.Message{
				{
					Id: "m1", ThreadId: "t1", InternalDate: 1700000000000,
					LabelIds: []string{"UNREAD", "INBOX", "CATEGORY_PERSONAL", "Label_7", "IMPORTANT"},
					Snippet:  "first",
					Payload: &gmailv1.MessagePart{
						MimeType: "text/plain",
						Headers: hdrs(
							"From", "Ada <ada@x.example>",
							"To", "Bob <bob@x.example>",
							"Subject", "Hello",
							"Message-ID", "<m1@x.example>",
							"Reply-To", "RT <rt@x.example>",
							"References", "<r0@x.example>",
						),
						Body: &gmailv1.MessagePartBody{Data: b64("hola mundo")},
					},
				},
				{
					Id: "m2", ThreadId: "t1", InternalDate: 1700000100000,
					LabelIds: []string{"INBOX", "STARRED", "SENT"},
					Snippet:  "latest",
					Payload: &gmailv1.MessagePart{
						MimeType: "multipart/alternative",
						Headers: hdrs(
							"From", "Bob <bob@x.example>",
							"Subject", "Re: Hello",
						),
						Parts: []*gmailv1.MessagePart{
							{MimeType: "text/html", Body: &gmailv1.MessagePartBody{Data: b64("<b>nope</b>")}},
							{MimeType: "text/plain", Body: &gmailv1.MessagePartBody{Data: b64raw("plain wins")}},
						},
					},
				},
			},
		},
		"t2": {
			Id: "t2",
			Messages: []*gmailv1.Message{
				{
					Id: "m3", ThreadId: "t2", InternalDate: 1700000200000,
					LabelIds: []string{"INBOX", "Label_7"},
					Snippet:  "dos",
					Payload: &gmailv1.MessagePart{
						MimeType: "text/plain",
						Headers:  hdrs("From", "Eve <eve@x.example>", "Subject", "Second"),
						Body:     &gmailv1.MessagePartBody{Data: b64("segundo")},
					},
				},
			},
		},
		"t3": {
			Id: "t3",
			Messages: []*gmailv1.Message{
				{
					Id: "m4", ThreadId: "t3", InternalDate: 1700000300000,
					LabelIds: []string{"Label_7"},
					Snippet:  "tres",
					Payload: &gmailv1.MessagePart{
						MimeType: "text/plain",
						Headers:  hdrs("From", "Eve <eve@x.example>", "Subject", "Third"),
						Body:     &gmailv1.MessagePartBody{Data: b64("tercero")},
					},
				},
			},
		},
	}
}

func newTestProvider(f *fakeAPI, opts Options) *Provider {
	return New("acct-1", "ada@example.org", f, opts)
}

// ---- Sync: initial ---------------------------------------------------

func TestInitialSync(t *testing.T) {
	f := &fakeAPI{
		profileEmail: "ada@example.org",
		profileHist:  5000,
		listPages: map[string][]listPage{
			"INBOX":   {{ids: []string{"t1"}, next: "1"}, {ids: []string{"t2"}}},
			"Label_7": {{ids: []string{"t2", "t3"}}},
		},
		threads: fixtureThreads(),
	}
	p := newTestProvider(f, Options{MonitoredLabels: []string{"Label_7"}})

	changes, cursor, err := p.Sync(context.Background(), "")
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if !changes.FullResync {
		t.Error("FullResync = false, want true")
	}
	if cursor != "5000" {
		t.Errorf("cursor = %q, want %q", cursor, "5000")
	}
	if f.calls[0] != "GetProfile" {
		t.Errorf("first call = %q, want GetProfile before listing", f.calls[0])
	}

	if got := len(changes.Upserted); got != 3 {
		t.Fatalf("len(Upserted) = %d, want 3", got)
	}
	gets := 0
	for _, c := range f.calls {
		if c == "GetThread" {
			gets++
		}
	}
	if gets != 3 {
		t.Errorf("GetThread called %d times, want 3 (t2 deduped across labels)", gets)
	}

	d := changes.Upserted[0]
	if d.ThreadID != "t1" {
		t.Fatalf("Upserted[0].ThreadID = %q, want t1", d.ThreadID)
	}
	if !d.Unread || !d.Starred || !d.InInbox {
		t.Errorf("flags = unread:%v starred:%v inbox:%v, want all true", d.Unread, d.Starred, d.InInbox)
	}
	if !reflect.DeepEqual(d.Labels, []string{"Label_7"}) {
		t.Errorf("Labels = %v, want [Label_7] (system labels filtered)", d.Labels)
	}
	if d.Subject != "Re: Hello" || d.Snippet != "latest" {
		t.Errorf("Subject/Snippet = %q/%q, want from newest message", d.Subject, d.Snippet)
	}
	if d.LastMessage != 1700000100 {
		t.Errorf("LastMessage = %d, want 1700000100 (ms→s)", d.LastMessage)
	}
	if want := []string{"Ada <ada@x.example>", "Bob <bob@x.example>"}; !reflect.DeepEqual(d.Participants, want) {
		t.Errorf("Participants = %v, want %v", d.Participants, want)
	}
	if d.MessageCount != 2 || len(d.Messages) != 2 {
		t.Errorf("MessageCount/Messages = %d/%d, want 2/2", d.MessageCount, len(d.Messages))
	}

	m1 := d.Messages[0]
	if m1.RFC822MessageID != "m1@x.example" {
		t.Errorf("RFC822MessageID = %q, want m1@x.example (angle brackets stripped)", m1.RFC822MessageID)
	}
	if m1.BodyText != "hola mundo" {
		t.Errorf("BodyText = %q, want %q", m1.BodyText, "hola mundo")
	}
	if m1.Date != 1700000000 {
		t.Errorf("Date = %d, want 1700000000", m1.Date)
	}
	if got := m1.ReplyHeaders[provider.HeaderSubject]; got != "Hello" {
		t.Errorf("ReplyHeaders[Subject] = %q, want Hello", got)
	}
	if got := m1.ReplyHeaders[provider.HeaderReferences]; got != "<r0@x.example>" {
		t.Errorf("ReplyHeaders[References] = %q, want <r0@x.example>", got)
	}
	if got := m1.ReplyHeaders[provider.HeaderReplyTo]; got != "RT <rt@x.example>" {
		t.Errorf("ReplyHeaders[Reply-To] = %q, want RT <rt@x.example>", got)
	}
	// mail.Address.String() canonicalizes display names with quotes.
	if want := []string{`"Bob" <bob@x.example>`}; !reflect.DeepEqual(m1.To, want) {
		t.Errorf("To = %v, want %v", m1.To, want)
	}

	if got := d.Messages[1].BodyText; got != "plain wins" {
		t.Errorf("multipart BodyText = %q, want text/plain part", got)
	}

	if d3 := changes.Upserted[2]; d3.ThreadID != "t3" || d3.InInbox {
		t.Errorf("t3 = %+v, want InInbox=false", d3)
	}
	if len(changes.RemovedThreadIDs) != 0 {
		t.Errorf("RemovedThreadIDs = %v, want empty", changes.RemovedThreadIDs)
	}
}

func TestInitialSyncBodyTruncation(t *testing.T) {
	tests := []struct {
		name string
		body string
		cap  int
		want string
	}{
		{"under cap", "hola", 8, "hola"},
		{"exact cap", "hola", 4, "hola"},
		{"cut at rune boundary", "aaé", 3, "aa"}, // é is 2 bytes; cap lands mid-rune
		{"multibyte kept when whole", "aé", 3, "aé"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeAPI{
				profileHist: 1,
				listPages:   map[string][]listPage{"INBOX": {{ids: []string{"t"}}}},
				threads: map[string]*gmailv1.Thread{
					"t": {Id: "t", Messages: []*gmailv1.Message{{
						Id: "m", ThreadId: "t", InternalDate: 1000,
						Payload: &gmailv1.MessagePart{
							MimeType: "text/plain",
							Body:     &gmailv1.MessagePartBody{Data: b64(tc.body)},
						},
					}}},
				},
			}
			p := newTestProvider(f, Options{BodyCapBytes: tc.cap})
			changes, _, err := p.Sync(context.Background(), "")
			if err != nil {
				t.Fatalf("Sync: %v", err)
			}
			if got := changes.Upserted[0].Messages[0].BodyText; got != tc.want {
				t.Errorf("BodyText = %q, want %q", got, tc.want)
			}
		})
	}
}

// ---- Sync: incremental -----------------------------------------------

func TestIncrementalSync(t *testing.T) {
	f := &fakeAPI{
		threads: fixtureThreads(),
		histPages: []*gmailv1.ListHistoryResponse{
			{
				HistoryId:     6000,
				NextPageToken: "1",
				History: []*gmailv1.History{
					{Id: 5001, MessagesAdded: []*gmailv1.HistoryMessageAdded{
						{Message: &gmailv1.Message{Id: "mA", ThreadId: "t1"}},
					}},
				},
			},
			{
				HistoryId: 6000,
				History: []*gmailv1.History{
					{Id: 5007, LabelsRemoved: []*gmailv1.HistoryLabelRemoved{
						{Message: &gmailv1.Message{Id: "m3", ThreadId: "t2"}},
					}},
					{Id: 5009, MessagesDeleted: []*gmailv1.HistoryMessageDeleted{
						{Message: &gmailv1.Message{Id: "mZ", ThreadId: "tGone"}},
					}},
				},
			},
		},
	}
	p := newTestProvider(f, Options{})

	changes, cursor, err := p.Sync(context.Background(), "5000")
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if changes.FullResync {
		t.Error("FullResync = true, want false")
	}
	if cursor != "6000" {
		t.Errorf("cursor = %q, want 6000 (max historyId seen)", cursor)
	}
	var got []string
	for _, d := range changes.Upserted {
		got = append(got, d.ThreadID)
	}
	if want := []string{"t1", "t2"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Upserted threads = %v, want %v", got, want)
	}
	if want := []string{"tGone"}; !reflect.DeepEqual(changes.RemovedThreadIDs, want) {
		t.Errorf("RemovedThreadIDs = %v, want %v (removal only on 404)", changes.RemovedThreadIDs, want)
	}
	// GetProfile must not be called on the incremental path.
	for _, c := range f.calls {
		if c == "GetProfile" {
			t.Error("GetProfile called during incremental sync")
		}
	}
}

func TestIncrementalSyncNoChanges(t *testing.T) {
	f := &fakeAPI{
		histPages: []*gmailv1.ListHistoryResponse{{HistoryId: 5500}},
	}
	p := newTestProvider(f, Options{})
	changes, cursor, err := p.Sync(context.Background(), "5000")
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if len(changes.Upserted) != 0 || len(changes.RemovedThreadIDs) != 0 || changes.FullResync {
		t.Errorf("changes = %+v, want empty", changes)
	}
	if cursor != "5500" {
		t.Errorf("cursor = %q, want 5500", cursor)
	}
}

func TestIncrementalSyncHistoryExpired(t *testing.T) {
	f := &fakeAPI{
		profileEmail: "ada@example.org",
		profileHist:  7777,
		histErr:      &googleapi.Error{Code: 404, Message: "history expired"},
		listPages:    map[string][]listPage{"INBOX": {{ids: []string{"t2"}}}},
		threads:      fixtureThreads(),
	}
	p := newTestProvider(f, Options{})

	changes, cursor, err := p.Sync(context.Background(), "42")
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if !changes.FullResync {
		t.Error("FullResync = false, want true after history 404")
	}
	if cursor != "7777" {
		t.Errorf("cursor = %q, want profile historyId 7777", cursor)
	}
	if len(changes.Upserted) != 1 || changes.Upserted[0].ThreadID != "t2" {
		t.Errorf("Upserted = %+v, want [t2]", changes.Upserted)
	}
}

// ---- error classification --------------------------------------------

func TestClassify(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want errdefs.Kind
	}{
		{"nil stays nil", nil, errdefs.KindUnknown},
		{"401 → auth", &googleapi.Error{Code: 401}, errdefs.KindAuth},
		{"429 → rate limit", &googleapi.Error{Code: 429}, errdefs.KindRateLimit},
		{
			"403 rateLimitExceeded → rate limit",
			&googleapi.Error{Code: 403, Errors: []googleapi.ErrorItem{{Reason: "rateLimitExceeded"}}},
			errdefs.KindRateLimit,
		},
		{
			"403 userRateLimitExceeded → rate limit",
			&googleapi.Error{Code: 403, Errors: []googleapi.ErrorItem{{Reason: "userRateLimitExceeded"}}},
			errdefs.KindRateLimit,
		},
		{
			"403 other reason → permanent",
			&googleapi.Error{Code: 403, Errors: []googleapi.ErrorItem{{Reason: "domainPolicy"}}},
			errdefs.KindPermanent,
		},
		{"404 → permanent", &googleapi.Error{Code: 404}, errdefs.KindPermanent},
		{"400 → permanent", &googleapi.Error{Code: 400}, errdefs.KindPermanent},
		{"500 → network", &googleapi.Error{Code: 500}, errdefs.KindNetwork},
		{"503 → network", &googleapi.Error{Code: 503}, errdefs.KindNetwork},
		{
			"oauth2 invalid_grant → auth",
			&oauth2.RetrieveError{ErrorCode: "invalid_grant"},
			errdefs.KindAuth,
		},
		{"net error → network", &net.DNSError{IsTimeout: true}, errdefs.KindNetwork},
		{"unclassified → unknown", errors.New("weird"), errdefs.KindUnknown},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := classify(tc.err)
			if tc.err == nil {
				if got != nil {
					t.Fatalf("classify(nil) = %v, want nil", got)
				}
				return
			}
			if kind := errdefs.KindOf(got); kind != tc.want {
				t.Errorf("kind = %v, want %v", kind, tc.want)
			}
			if !errors.Is(got, tc.err) {
				t.Error("classified error does not wrap the original")
			}
		})
	}
}

// ---- flag / label operations -----------------------------------------

func TestModifyFlagsArchiveTrash(t *testing.T) {
	tests := []struct {
		name string
		run  func(p *Provider, ctx context.Context) error
		want []modifyCall
	}{
		{
			"modify flags",
			func(p *Provider, ctx context.Context) error {
				return p.ModifyFlags(ctx, []string{"t1", "t2"},
					[]provider.Flag{provider.FlagUnread}, []provider.Flag{provider.FlagStarred})
			},
			[]modifyCall{
				{threadID: "t1", add: []string{"UNREAD"}, remove: []string{"STARRED"}},
				{threadID: "t2", add: []string{"UNREAD"}, remove: []string{"STARRED"}},
			},
		},
		{
			"archive removes INBOX",
			func(p *Provider, ctx context.Context) error { return p.Archive(ctx, []string{"t1"}) },
			[]modifyCall{{threadID: "t1", remove: []string{"INBOX"}}},
		},
		{
			"unarchive adds INBOX",
			func(p *Provider, ctx context.Context) error { return p.Unarchive(ctx, []string{"t1"}) },
			[]modifyCall{{threadID: "t1", add: []string{"INBOX"}}},
		},
		{
			"trash adds TRASH and leaves the inbox",
			func(p *Provider, ctx context.Context) error { return p.Trash(ctx, []string{"t1"}) },
			[]modifyCall{{threadID: "t1", add: []string{"TRASH"}, remove: []string{"INBOX"}}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeAPI{}
			p := newTestProvider(f, Options{})
			if err := tc.run(p, context.Background()); err != nil {
				t.Fatalf("op: %v", err)
			}
			if !reflect.DeepEqual(f.modifies, tc.want) {
				t.Errorf("modify calls = %+v, want %+v", f.modifies, tc.want)
			}
		})
	}
}

func TestModifyFlagsUnknownFlag(t *testing.T) {
	f := &fakeAPI{}
	p := newTestProvider(f, Options{})
	err := p.ModifyFlags(context.Background(), []string{"t1"}, []provider.Flag{"bogus"}, nil)
	if errdefs.KindOf(err) != errdefs.KindPermanent {
		t.Errorf("kind = %v, want permanent", errdefs.KindOf(err))
	}
	if len(f.modifies) != 0 {
		t.Error("ModifyThread called despite invalid flag")
	}
}

// ---- send paths --------------------------------------------------------

func TestSendReply(t *testing.T) {
	f := &fakeAPI{
		msgMeta: map[string]*gmailv1.Message{
			"mOrig": {
				Id: "mOrig", ThreadId: "t9",
				Payload: &gmailv1.MessagePart{
					Headers: hdrs(
						"From", "Carol <carol@x.example>",
						"To", "Ada <ada@example.org>, Dave <dave@x.example>",
						"Subject", "Topic",
						"Message-ID", "<orig@x.example>",
						"References", "<r1@x.example> <r2@x.example>",
					),
				},
			},
		},
	}
	p := newTestProvider(f, Options{})

	err := p.SendReply(context.Background(), "t9", provider.ReplyDraft{
		InReplyToMessageID: "mOrig",
		Body:               "hola",
		ReplyAll:           true,
	})
	if err != nil {
		t.Fatalf("SendReply: %v", err)
	}
	if len(f.sent) != 1 {
		t.Fatalf("sent %d messages, want 1", len(f.sent))
	}
	if f.sent[0].threadID != "t9" {
		t.Errorf("threadID = %q, want t9", f.sent[0].threadID)
	}

	msg, err := mail.ReadMessage(strings.NewReader(string(f.sent[0].raw)))
	if err != nil {
		t.Fatalf("parse raw MIME: %v", err)
	}
	if got := msg.Header.Get("In-Reply-To"); got != "<orig@x.example>" {
		t.Errorf("In-Reply-To = %q, want <orig@x.example>", got)
	}
	if got := msg.Header.Get("References"); got != "<r1@x.example> <r2@x.example> <orig@x.example>" {
		t.Errorf("References = %q, want chain extended with original id", got)
	}
	if got := msg.Header.Get("Subject"); got != "Re: Topic" {
		t.Errorf("Subject = %q, want Re: Topic", got)
	}
	toList, err := msg.Header.AddressList("To")
	if err != nil {
		t.Fatalf("parse To: %v", err)
	}
	var to []string
	for _, a := range toList {
		to = append(to, a.Address)
	}
	if want := []string{"carol@x.example", "dave@x.example"}; !reflect.DeepEqual(to, want) {
		t.Errorf("To = %v, want %v (sender first, self excluded)", to, want)
	}
	body, err := io.ReadAll(quotedprintable.NewReader(msg.Body))
	if err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if string(body) != "hola" {
		t.Errorf("body = %q, want %q", body, "hola")
	}
}

func TestSendReplyOriginalGone(t *testing.T) {
	f := &fakeAPI{}
	p := newTestProvider(f, Options{})
	err := p.SendReply(context.Background(), "t9", provider.ReplyDraft{InReplyToMessageID: "nope"})
	if errdefs.KindOf(err) != errdefs.KindPermanent {
		t.Errorf("kind = %v, want permanent (404 on original)", errdefs.KindOf(err))
	}
	if len(f.sent) != 0 {
		t.Error("SendMessage called despite missing original")
	}
}

func TestCompose(t *testing.T) {
	f := &fakeAPI{}
	p := newTestProvider(f, Options{})
	err := p.Compose(context.Background(), provider.ComposeDraft{
		To:      []string{"eve@x.example"},
		Subject: "Nuevo",
		Body:    "cuerpo",
	})
	if err != nil {
		t.Fatalf("Compose: %v", err)
	}
	if len(f.sent) != 1 {
		t.Fatalf("sent %d messages, want 1", len(f.sent))
	}
	if f.sent[0].threadID != "" {
		t.Errorf("threadID = %q, want empty for compose", f.sent[0].threadID)
	}
	msg, err := mail.ReadMessage(strings.NewReader(string(f.sent[0].raw)))
	if err != nil {
		t.Fatalf("parse raw MIME: %v", err)
	}
	if got := msg.Header.Get("Subject"); got != "Nuevo" {
		t.Errorf("Subject = %q, want Nuevo", got)
	}
	if got := msg.Header.Get("To"); got != "<eve@x.example>" {
		t.Errorf("To = %q, want <eve@x.example>", got)
	}
}

// ---- misc ---------------------------------------------------------------

func TestWebLink(t *testing.T) {
	p := newTestProvider(&fakeAPI{}, Options{})
	tests := []struct {
		name      string
		threadID  string
		messageID string
		wantURL   string
		wantOK    bool
	}{
		{
			"thread link",
			"t123", "",
			"https://mail.google.com/mail/u/0/?authuser=ada%40example.org#all/t123",
			true,
		},
		{
			"thread wins over message id",
			"t123", "<abc@x.example>",
			"https://mail.google.com/mail/u/0/?authuser=ada%40example.org#all/t123",
			true,
		},
		{
			"rfc822 msgid fallback",
			"", "<abc@x.example>",
			"https://mail.google.com/mail/u/0/?authuser=ada%40example.org#search/rfc822msgid:abc%40x.example",
			true,
		},
		{"nothing to link", "", "", "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			url, ok := p.WebLink(tc.threadID, tc.messageID)
			if ok != tc.wantOK || url != tc.wantURL {
				t.Errorf("WebLink(%q, %q) = %q, %v; want %q, %v",
					tc.threadID, tc.messageID, url, ok, tc.wantURL, tc.wantOK)
			}
		})
	}
}

func TestCapabilitiesAndID(t *testing.T) {
	p := newTestProvider(&fakeAPI{}, Options{})
	if p.ID() != "acct-1" {
		t.Errorf("ID = %q, want acct-1", p.ID())
	}
	want := provider.CapModifyFlags | provider.CapArchive | provider.CapTrash |
		provider.CapSendReply | provider.CapCompose | provider.CapDeepLink |
		provider.CapHistorySync
	if got := p.Capabilities(); got != want {
		t.Errorf("Capabilities = %b, want %b", got, want)
	}
	if p.Capabilities().Has(provider.CapPush) {
		t.Error("CapPush must not be set yet")
	}
}

// Compile-time check: *Provider satisfies provider.Provider.
var _ provider.Provider = (*Provider)(nil)

// ---- html fallback + attachment metadata -------------------------------

func TestMessageDeltaHTMLFallbackAndAttachments(t *testing.T) {
	p := newTestProvider(&fakeAPI{}, Options{})
	m := &gmailv1.Message{
		Id:           "m1",
		InternalDate: 1_700_000_000_000,
		Payload: &gmailv1.MessagePart{
			MimeType: "multipart/mixed",
			Headers: []*gmailv1.MessagePartHeader{
				{Name: "From", Value: "Ada <ada@example.com>"},
			},
			Parts: []*gmailv1.MessagePart{
				{
					MimeType: "text/html",
					Body:     &gmailv1.MessagePartBody{Data: b64("<p>Hola <b>mundo</b></p>")},
				},
				{
					MimeType: "application/pdf",
					Filename: "informe.pdf",
					Body:     &gmailv1.MessagePartBody{AttachmentId: "a1", Size: 12345},
				},
				{
					MimeType: "image/png",
					Filename: "logo.png", // inline signature image: metadata only
					Body:     &gmailv1.MessagePartBody{AttachmentId: "a2", Size: 999},
				},
			},
		},
	}
	d := p.messageDelta(m)

	if !strings.Contains(d.BodyText, "Hola") || !strings.Contains(d.BodyText, "mundo") {
		t.Errorf("html fallback body = %q, want distilled text", d.BodyText)
	}
	if strings.Contains(d.BodyText, "<p>") {
		t.Errorf("body still contains HTML tags: %q", d.BodyText)
	}
	if len(d.Attachments) != 2 {
		t.Fatalf("attachments = %d, want 2", len(d.Attachments))
	}
	if d.Attachments[0].Filename != "informe.pdf" || d.Attachments[0].Size != 12345 || d.Attachments[0].MimeType != "application/pdf" {
		t.Errorf("attachment[0] = %+v", d.Attachments[0])
	}
}

func TestMessageDeltaPlainTextPreferredOverHTML(t *testing.T) {
	p := newTestProvider(&fakeAPI{}, Options{})
	m := &gmailv1.Message{
		Id: "m2",
		Payload: &gmailv1.MessagePart{
			MimeType: "multipart/alternative",
			Parts: []*gmailv1.MessagePart{
				{MimeType: "text/plain", Body: &gmailv1.MessagePartBody{Data: b64("texto plano")}},
				{MimeType: "text/html", Body: &gmailv1.MessagePartBody{Data: b64("<p>html</p>")}},
			},
		},
	}
	if d := p.messageDelta(m); d.BodyText != "texto plano" {
		t.Errorf("body = %q, want the text/plain part untouched", d.BodyText)
	}
}

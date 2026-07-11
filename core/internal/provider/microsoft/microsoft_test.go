package microsoft

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/arqueon/dankmail/core/internal/provider"
)

// fakeAPI implements graphAPI over in-memory fixtures.
type fakeAPI struct {
	email   string
	folders map[string]string
	// deltas: folder → pages served in order per round; a new round
	// (link=="") restarts from the front.
	deltas map[string][]deltaPage
	convs  map[string][]*graphMessage
	// gone makes DeltaMessages fail once with 410 when a deltaLink is used.
	gone bool

	patches []map[string]any
	moves   []string // "id→dest"
	sent    [][]byte
}

func (f *fakeAPI) GetProfile(ctx context.Context) (string, error) { return f.email, nil }

func (f *fakeAPI) DeltaMessages(ctx context.Context, folder, link string) (deltaPage, error) {
	if link != "" && f.gone {
		f.gone = false
		return deltaPage{}, &graphError{Status: 410, Code: "SyncStateNotFound"}
	}
	pages := f.deltas[folder]
	if len(pages) == 0 {
		return deltaPage{DeltaLink: "delta-" + folder}, nil
	}
	return pages[0], nil
}

func (f *fakeAPI) GetMessage(ctx context.Context, id string) (*graphMessage, error) {
	for _, msgs := range f.convs {
		for _, m := range msgs {
			if m.ID == id {
				return m, nil
			}
		}
	}
	return nil, &graphError{Status: 404}
}

func (f *fakeAPI) ListConversation(ctx context.Context, convID string) ([]*graphMessage, error) {
	return f.convs[convID], nil
}

func (f *fakeAPI) PatchMessage(ctx context.Context, id string, body map[string]any) error {
	p := map[string]any{"id": id}
	for k, v := range body {
		p[k] = v
	}
	f.patches = append(f.patches, p)
	return nil
}

func (f *fakeAPI) MoveMessage(ctx context.Context, id, dest string) error {
	f.moves = append(f.moves, id+"→"+dest)
	return nil
}

func (f *fakeAPI) SendMail(ctx context.Context, mime []byte) error {
	f.sent = append(f.sent, mime)
	return nil
}

func (f *fakeAPI) FolderIDs(ctx context.Context) (map[string]string, error) {
	return f.folders, nil
}

var testFolders = map[string]string{
	folderInbox: "fid-inbox", folderJunk: "fid-junk",
	folderArchive: "fid-archive", folderTrash: "fid-trash",
}

func msg(id, conv, folder string, read, flagged bool, at int64) *graphMessage {
	return &graphMessage{
		ID: id, ConversationID: conv, ParentFolderID: folder,
		IsRead: read, Flagged: flagged, ReceivedAt: at,
		Subject: "s-" + id, BodyPreview: "p-" + id, From: "Ada <ada@example.org>",
		InternetMessageID: "<" + id + "@x>", BodyText: "body " + id,
	}
}

func newTestProvider(f *fakeAPI) *Provider {
	return New("acct-1", "me@outlook.com", f, Options{})
}

func TestFullSyncGroupsConversations(t *testing.T) {
	f := &fakeAPI{
		email: "me@outlook.com", folders: testFolders,
		deltas: map[string][]deltaPage{
			folderInbox: {{
				Messages:  []*graphMessage{{ID: "m1", ConversationID: "c1"}, {ID: "m2", ConversationID: "c1"}},
				DeltaLink: "delta-inbox",
			}},
			folderJunk: {{
				Messages:  []*graphMessage{{ID: "m3", ConversationID: "c2"}},
				DeltaLink: "delta-junk",
			}},
		},
		convs: map[string][]*graphMessage{
			"c1": {msg("m1", "c1", "fid-inbox", false, false, 100), msg("m2", "c1", "fid-inbox", true, true, 200)},
			"c2": {msg("m3", "c2", "fid-junk", false, false, 300)},
		},
	}
	p := newTestProvider(f)
	changes, cursor, err := p.Sync(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if !changes.FullResync || len(changes.Upserted) != 2 {
		t.Fatalf("want full resync with 2 threads, got full=%v n=%d", changes.FullResync, len(changes.Upserted))
	}
	byID := map[string]provider.ThreadDelta{}
	for _, d := range changes.Upserted {
		byID[d.ThreadID] = d
	}
	c1 := byID["c1"]
	if !c1.InInbox || !c1.Unread || !c1.Starred || c1.MessageCount != 2 {
		t.Errorf("c1 = %+v; want inbox unread starred 2 msgs", c1)
	}
	if c1.Subject != "s-m2" || c1.LastMessage != 200 {
		t.Errorf("c1 newest fields wrong: subject=%q last=%d", c1.Subject, c1.LastMessage)
	}
	c2 := byID["c2"]
	if c2.InInbox || len(c2.Labels) != 1 || c2.Labels[0] != "SPAM" {
		t.Errorf("c2 = %+v; want junk thread labeled SPAM outside inbox", c2)
	}
	var state cursorState
	if err := json.Unmarshal([]byte(cursor), &state); err != nil {
		t.Fatal(err)
	}
	if state[folderInbox] != "delta-inbox" || state[folderJunk] != "delta-junk" {
		t.Errorf("cursor = %v; want per-folder delta links", state)
	}
}

func TestIncrementalGoneRestartsFull(t *testing.T) {
	f := &fakeAPI{
		email: "me@outlook.com", folders: testFolders, gone: true,
		convs: map[string][]*graphMessage{},
	}
	p := newTestProvider(f)
	cursor, _ := json.Marshal(cursorState{folderInbox: "delta-inbox", folderJunk: "delta-junk"})
	changes, _, err := p.Sync(context.Background(), string(cursor))
	if err != nil {
		t.Fatal(err)
	}
	if !changes.FullResync {
		t.Errorf("410 delta token must trigger a full resync")
	}
}

func TestRemovedMessageRebuildsConversation(t *testing.T) {
	f := &fakeAPI{
		email: "me@outlook.com", folders: testFolders,
		deltas: map[string][]deltaPage{
			folderInbox: {{
				Messages:  []*graphMessage{{ID: "m1", Removed: true}},
				DeltaLink: "delta-inbox",
			}},
		},
		convs: map[string][]*graphMessage{
			"c1": {msg("m1", "c1", "fid-archive", true, false, 100)},
		},
	}
	p := newTestProvider(f)
	changes, _, err := p.Sync(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(changes.Upserted) != 1 || changes.Upserted[0].InInbox {
		t.Fatalf("moved-out message must rebuild its conversation outside the inbox: %+v", changes.Upserted)
	}
}

func TestModifyFlagsPolarity(t *testing.T) {
	f := &fakeAPI{
		email: "me@outlook.com", folders: testFolders,
		convs: map[string][]*graphMessage{
			"c1": {msg("m1", "c1", "fid-inbox", false, false, 100)},
		},
	}
	p := newTestProvider(f)
	// Mark read = REMOVE the unread flag → isRead=true.
	if err := p.ModifyFlags(context.Background(), []string{"c1"}, nil, []provider.Flag{provider.FlagUnread}); err != nil {
		t.Fatal(err)
	}
	if len(f.patches) != 1 || f.patches[0]["isRead"] != true {
		t.Fatalf("mark-read patch = %v; want isRead=true", f.patches)
	}
	f.patches = nil
	// Star = ADD starred → flagStatus flagged.
	if err := p.ModifyFlags(context.Background(), []string{"c1"}, []provider.Flag{provider.FlagStarred}, nil); err != nil {
		t.Fatal(err)
	}
	flag, _ := f.patches[0]["flag"].(map[string]any)
	if flag == nil || flag["flagStatus"] != "flagged" {
		t.Fatalf("star patch = %v; want flagStatus=flagged", f.patches)
	}
}

func TestArchiveTrashMoves(t *testing.T) {
	f := &fakeAPI{
		email: "me@outlook.com", folders: testFolders,
		convs: map[string][]*graphMessage{
			"c1": {
				msg("m1", "c1", "fid-inbox", true, false, 100),
				msg("m2", "c1", "fid-archive", true, false, 200),
				msg("m3", "c1", "fid-trash", true, false, 300),
			},
		},
	}
	p := newTestProvider(f)
	if err := p.Archive(context.Background(), []string{"c1"}); err != nil {
		t.Fatal(err)
	}
	if len(f.moves) != 1 || f.moves[0] != "m1→archive" {
		t.Fatalf("archive moves = %v; want only the inbox message", f.moves)
	}
	f.moves = nil
	if err := p.Trash(context.Background(), []string{"c1"}); err != nil {
		t.Fatal(err)
	}
	// m3 already sits in the trash — idempotence means it is skipped.
	if len(f.moves) != 2 || f.moves[0] != "m1→deleteditems" || f.moves[1] != "m2→deleteditems" {
		t.Fatalf("trash moves = %v; want m1 and m2 only", f.moves)
	}
}

func TestSendReplyThreadsMIME(t *testing.T) {
	orig := msg("m1", "c1", "fid-inbox", true, false, 100)
	orig.Headers = map[string]string{"References": "<r1@x>"}
	f := &fakeAPI{
		email: "me@outlook.com", folders: testFolders,
		convs: map[string][]*graphMessage{"c1": {orig}},
	}
	p := newTestProvider(f)
	err := p.SendReply(context.Background(), "c1", provider.ReplyDraft{
		InReplyToMessageID: "m1", Body: "hola",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(f.sent) != 1 {
		t.Fatalf("want 1 sent message, got %d", len(f.sent))
	}
	mime := string(f.sent[0])
	for _, want := range []string{"In-Reply-To: <m1@x>", "References: <r1@x>", "Subject: Re: s-m1"} {
		if !strings.Contains(mime, want) {
			t.Errorf("MIME missing %q:\n%s", want, mime)
		}
	}
}

package mailmime

import (
	"io"
	"mime"
	"mime/quotedprintable"
	"net/mail"
	"strings"
	"testing"

	"github.com/arqueon/dankmail/core/internal/provider"
)

const self = "Rubén <ruben@example.org>"

func origMsg() provider.MessageDelta {
	return provider.MessageDelta{
		MessageID:       "gm1",
		RFC822MessageID: "orig-123@mail.example.com",
		From:            "Ada Lovelace <ada@example.com>",
		To:              []string{"Rubén <ruben@example.org>", "Carlos <carlos@example.net>"},
		Cc:              []string{"Dana <dana@example.io>", "ruben@example.org"},
		ReplyHeaders: map[string]string{
			provider.HeaderSubject:    "Números de Bernoulli",
			provider.HeaderReferences: "<root-1@mail.example.com>",
		},
	}
}

func parse(t *testing.T, raw []byte) (*mail.Message, string) {
	t.Helper()
	msg, err := mail.ReadMessage(strings.NewReader(string(raw)))
	if err != nil {
		t.Fatalf("unparseable message: %v\n%s", err, raw)
	}
	body, err := io.ReadAll(quotedprintable.NewReader(msg.Body))
	if err != nil {
		t.Fatalf("bad quoted-printable body: %v", err)
	}
	return msg, string(body)
}

func decodedSubject(t *testing.T, msg *mail.Message) string {
	t.Helper()
	s, err := new(mime.WordDecoder).DecodeHeader(msg.Header.Get("Subject"))
	if err != nil {
		t.Fatalf("undecodable subject: %v", err)
	}
	return s
}

func addressEmails(t *testing.T, msg *mail.Message, key string) []string {
	t.Helper()
	if msg.Header.Get(key) == "" {
		return nil
	}
	addrs, err := msg.Header.AddressList(key)
	if err != nil {
		t.Fatalf("bad %s header: %v", key, err)
	}
	out := make([]string, len(addrs))
	for i, a := range addrs {
		out[i] = strings.ToLower(a.Address)
	}
	return out
}

func TestBuildReplyThreadingHeaders(t *testing.T) {
	raw, err := BuildReply(origMsg(), self, provider.ReplyDraft{Body: "hola"})
	if err != nil {
		t.Fatal(err)
	}
	msg, _ := parse(t, raw)

	if got := msg.Header.Get("In-Reply-To"); got != "<orig-123@mail.example.com>" {
		t.Errorf("In-Reply-To = %q", got)
	}
	wantRefs := "<root-1@mail.example.com> <orig-123@mail.example.com>"
	if got := msg.Header.Get("References"); got != wantRefs {
		t.Errorf("References = %q, want %q", got, wantRefs)
	}
}

func TestBuildReplyRefsWithoutChain(t *testing.T) {
	orig := origMsg()
	delete(orig.ReplyHeaders, provider.HeaderReferences)
	raw, err := BuildReply(orig, self, provider.ReplyDraft{Body: "x"})
	if err != nil {
		t.Fatal(err)
	}
	msg, _ := parse(t, raw)
	if got := msg.Header.Get("References"); got != "<orig-123@mail.example.com>" {
		t.Errorf("References = %q", got)
	}
}

func TestBuildReplySubjectRePrefix(t *testing.T) {
	cases := map[string]string{
		"Números de Bernoulli": "Re: Números de Bernoulli",
		"Re: ya con prefijo":   "Re: ya con prefijo",
		"RE: mayúsculas":       "RE: mayúsculas",
		"re: minúsculas":       "re: minúsculas",
		// Leading whitespace: no double prefix; the parser trims the header.
		"  Re: espacios delante": "Re: espacios delante",
	}
	for in, want := range cases {
		orig := origMsg()
		orig.ReplyHeaders[provider.HeaderSubject] = in
		raw, err := BuildReply(orig, self, provider.ReplyDraft{Body: "x"})
		if err != nil {
			t.Fatal(err)
		}
		msg, _ := parse(t, raw)
		if got := decodedSubject(t, msg); got != want {
			t.Errorf("subject %q → %q, want %q", in, got, want)
		}
	}
}

func TestBuildReplySimpleGoesToSenderOnly(t *testing.T) {
	raw, err := BuildReply(origMsg(), self, provider.ReplyDraft{Body: "x"})
	if err != nil {
		t.Fatal(err)
	}
	msg, _ := parse(t, raw)
	to := addressEmails(t, msg, "To")
	if len(to) != 1 || to[0] != "ada@example.com" {
		t.Errorf("To = %v, want only ada@example.com", to)
	}
	if cc := addressEmails(t, msg, "Cc"); len(cc) != 0 {
		t.Errorf("Cc = %v, want empty", cc)
	}
}

func TestBuildReplyAllExcludesSelfAndDedupes(t *testing.T) {
	raw, err := BuildReply(origMsg(), self, provider.ReplyDraft{Body: "x", ReplyAll: true})
	if err != nil {
		t.Fatal(err)
	}
	msg, _ := parse(t, raw)

	to := addressEmails(t, msg, "To")
	want := []string{"ada@example.com", "carlos@example.net"}
	if strings.Join(to, ",") != strings.Join(want, ",") {
		t.Errorf("To = %v, want %v", to, want)
	}
	cc := addressEmails(t, msg, "Cc")
	if len(cc) != 1 || cc[0] != "dana@example.io" {
		t.Errorf("Cc = %v, want only dana@example.io (self excluded)", cc)
	}
}

func TestBuildReplyPrefersReplyTo(t *testing.T) {
	orig := origMsg()
	orig.ReplyHeaders[provider.HeaderReplyTo] = "Lista <lista@example.com>"
	raw, err := BuildReply(orig, self, provider.ReplyDraft{Body: "x"})
	if err != nil {
		t.Fatal(err)
	}
	msg, _ := parse(t, raw)
	to := addressEmails(t, msg, "To")
	if len(to) != 1 || to[0] != "lista@example.com" {
		t.Errorf("To = %v, want lista@example.com", to)
	}
}

func TestBuildReplyToOwnMessageUsesOriginalTo(t *testing.T) {
	orig := origMsg()
	orig.From = self // replying to a message I sent
	raw, err := BuildReply(orig, self, provider.ReplyDraft{Body: "x"})
	if err != nil {
		t.Fatal(err)
	}
	msg, _ := parse(t, raw)
	to := addressEmails(t, msg, "To")
	if len(to) != 1 || to[0] != "carlos@example.net" {
		t.Errorf("To = %v, want carlos@example.net (original To minus self)", to)
	}
}

func TestBuildReplyUTF8SubjectAndBody(t *testing.T) {
	orig := origMsg()
	orig.ReplyHeaders[provider.HeaderSubject] = "¿Café mañana? — señales"
	body := "Sí — nos vemos a las 9:30.\nÁéíóú çñ 中文."
	raw, err := BuildReply(orig, self, provider.ReplyDraft{Body: body})
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(raw), "\r\n") {
		for _, r := range line {
			if r > 127 {
				t.Fatalf("raw message contains unencoded non-ASCII line: %q", line)
			}
		}
	}
	msg, gotBody := parse(t, raw)
	if got := decodedSubject(t, msg); got != "Re: ¿Café mañana? — señales" {
		t.Errorf("subject = %q", got)
	}
	if got := strings.ReplaceAll(gotBody, "\r\n", "\n"); got != body {
		t.Errorf("body roundtrip = %q, want %q", got, body)
	}
}

func TestBuildReplyNoMessageIDOmitsThreading(t *testing.T) {
	orig := origMsg()
	orig.RFC822MessageID = ""
	raw, err := BuildReply(orig, self, provider.ReplyDraft{Body: "x"})
	if err != nil {
		t.Fatal(err)
	}
	msg, _ := parse(t, raw)
	if msg.Header.Get("In-Reply-To") != "" || msg.Header.Get("References") != "" {
		t.Error("threading headers must be absent without a Message-ID")
	}
}

func TestBuildCompose(t *testing.T) {
	raw, err := BuildCompose(self, provider.ComposeDraft{
		To:      []string{"ada@example.com", "Ada <ada@example.com>", "carlos@example.net"},
		Subject: "Prueba",
		Body:    "hola",
	})
	if err != nil {
		t.Fatal(err)
	}
	msg, body := parse(t, raw)
	to := addressEmails(t, msg, "To")
	if len(to) != 2 {
		t.Errorf("To = %v, want deduped 2 entries", to)
	}
	if body != "hola" {
		t.Errorf("body = %q", body)
	}
	if got := msg.Header.Get("Content-Type"); !strings.Contains(got, "text/plain") {
		t.Errorf("Content-Type = %q", got)
	}
}

func TestBuildComposeNoRecipients(t *testing.T) {
	if _, err := BuildCompose(self, provider.ComposeDraft{Subject: "x", Body: "y"}); err != ErrNoRecipients {
		t.Errorf("err = %v, want ErrNoRecipients", err)
	}
}

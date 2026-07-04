// Package mailmime builds the plain-text MIME messages dankmail sends:
// threaded replies and minimal compositions. It is shared by the Gmail
// provider (raw message for users.messages.send) and the future IMAP/SMTP
// provider. Pure functions, no I/O.
package mailmime

import (
	"bytes"
	"errors"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"net/mail"
	"strings"

	"github.com/arqueon/dankmail/core/internal/provider"
)

var (
	ErrNoRecipients = errors.New("mailmime: no recipients")
)

// BuildReply constructs a plain-text reply to orig, sent as from.
// Threading: In-Reply-To is orig's Message-ID and References extends
// orig's References chain. Subject gets a single "Re: " prefix
// (idempotent, case-insensitive). Recipient rules:
//
//   - reply: Reply-To of the original if present, else its From;
//   - reply-all: additionally the original To (as To) and Cc (as Cc);
//   - the sender's own address is always excluded, and if the reply
//     target IS the sender (replying to your own message), the original
//     To list is used instead.
//
// The body is sent as-is (no quoting), UTF-8, quoted-printable.
func BuildReply(orig provider.MessageDelta, from string, d provider.ReplyDraft) ([]byte, error) {
	self := addrEmail(from)

	target := orig.ReplyHeaders[provider.HeaderReplyTo]
	if strings.TrimSpace(target) == "" {
		target = orig.From
	}

	var to []string
	if addrEmail(target) == self && len(orig.To) > 0 {
		to = excludeSelf(orig.To, self)
	} else {
		to = []string{target}
	}
	var cc []string
	if d.ReplyAll {
		to = append(to, excludeSelf(orig.To, self)...)
		cc = excludeSelf(orig.Cc, self)
	}
	to = dedupe(to)
	cc = dedupe(excludeAll(cc, to))
	if len(to) == 0 {
		// Degenerate self-conversation (a note to yourself): after
		// self-exclusion nobody is left — address the reply target
		// anyway rather than failing.
		if strings.TrimSpace(target) != "" {
			to = []string{target}
		} else {
			return nil, ErrNoRecipients
		}
	}

	subject := orig.ReplyHeaders[provider.HeaderSubject]
	if !hasRePrefix(subject) {
		subject = "Re: " + subject
	}

	var h headerList
	h.add("From", formatAddr(from))
	h.add("To", formatAddrList(to))
	if len(cc) > 0 {
		h.add("Cc", formatAddrList(cc))
	}
	h.add("Subject", encodeHeader(subject))
	if id := ensureAngle(orig.RFC822MessageID); id != "" {
		h.add("In-Reply-To", id)
		refs := strings.TrimSpace(orig.ReplyHeaders[provider.HeaderReferences])
		if refs != "" {
			refs += " " + id
		} else {
			refs = id
		}
		h.add("References", refs)
	}
	return assemble(h, d.Body)
}

// BuildCompose constructs a minimal new plain-text message.
func BuildCompose(from string, d provider.ComposeDraft) ([]byte, error) {
	to := dedupe(d.To)
	if len(to) == 0 {
		return nil, ErrNoRecipients
	}
	var h headerList
	h.add("From", formatAddr(from))
	h.add("To", formatAddrList(to))
	h.add("Subject", encodeHeader(d.Subject))
	return assemble(h, d.Body)
}

type headerList struct{ kv [][2]string }

func (h *headerList) add(k, v string) { h.kv = append(h.kv, [2]string{k, v}) }

func assemble(h headerList, body string) ([]byte, error) {
	var buf bytes.Buffer
	for _, kv := range h.kv {
		fmt.Fprintf(&buf, "%s: %s\r\n", kv[0], kv[1])
	}
	buf.WriteString("MIME-Version: 1.0\r\n")
	buf.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	buf.WriteString("Content-Transfer-Encoding: quoted-printable\r\n")
	buf.WriteString("\r\n")

	w := quotedprintable.NewWriter(&buf)
	if _, err := w.Write([]byte(normalizeCRLF(body))); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// hasRePrefix reports whether s already starts with a re: marker.
func hasRePrefix(s string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(s)), "re:")
}

// ensureAngle wraps a Message-ID in <> if present and not already wrapped.
func ensureAngle(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return ""
	}
	if !strings.HasPrefix(id, "<") {
		id = "<" + id + ">"
	}
	return id
}

// formatAddr renders one address in RFC 5322 form, encoding the display
// name if needed. Unparseable input passes through untouched.
func formatAddr(raw string) string {
	a, err := mail.ParseAddress(raw)
	if err != nil {
		return raw
	}
	return a.String()
}

func formatAddrList(raws []string) string {
	out := make([]string, 0, len(raws))
	for _, r := range raws {
		out = append(out, formatAddr(r))
	}
	return strings.Join(out, ", ")
}

// addrEmail extracts the lowercase bare email for comparisons; falls back
// to the trimmed lowercase input.
func addrEmail(raw string) string {
	a, err := mail.ParseAddress(raw)
	if err != nil {
		return strings.ToLower(strings.TrimSpace(raw))
	}
	return strings.ToLower(a.Address)
}

func excludeSelf(list []string, self string) []string {
	out := make([]string, 0, len(list))
	for _, r := range list {
		if addrEmail(r) != self {
			out = append(out, r)
		}
	}
	return out
}

func excludeAll(list, taken []string) []string {
	seen := map[string]bool{}
	for _, t := range taken {
		seen[addrEmail(t)] = true
	}
	out := make([]string, 0, len(list))
	for _, r := range list {
		if !seen[addrEmail(r)] {
			out = append(out, r)
		}
	}
	return out
}

func dedupe(list []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(list))
	for _, r := range list {
		e := addrEmail(r)
		if e == "" || seen[e] {
			continue
		}
		seen[e] = true
		out = append(out, r)
	}
	return out
}

func encodeHeader(s string) string {
	return mime.QEncoding.Encode("utf-8", s)
}

func normalizeCRLF(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.ReplaceAll(s, "\n", "\r\n")
}

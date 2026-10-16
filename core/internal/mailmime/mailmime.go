// Package mailmime builds the plain-text MIME messages dankmail sends:
// threaded replies and minimal compositions. It is shared by the Gmail
// provider (raw message for users.messages.send) and the future IMAP/SMTP
// provider. Pure functions, no I/O.
package mailmime

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"html"
	"mime"
	"mime/quotedprintable"
	"net/http"
	"net/mail"
	"os"
	"path/filepath"
	"regexp"
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
	return assembleWithAttachments(h, d.Body, d.Attachments)
}

// BuildCompose constructs a new MIME message (with optional Cc, Bcc, HTML links, and attachments).
func BuildCompose(from string, d provider.ComposeDraft) ([]byte, error) {
	to := dedupe(d.To)
	cc := dedupe(excludeAll(d.Cc, to))
	bcc := dedupe(excludeAll(d.Bcc, append(append([]string{}, to...), cc...)))
	if len(to) == 0 && len(cc) == 0 && len(bcc) == 0 {
		return nil, ErrNoRecipients
	}
	var h headerList
	h.add("From", formatAddr(from))
	if len(to) > 0 {
		h.add("To", formatAddrList(to))
	}
	if len(cc) > 0 {
		h.add("Cc", formatAddrList(cc))
	}
	if len(bcc) > 0 {
		h.add("Bcc", formatAddrList(bcc))
	}
	h.add("Subject", encodeHeader(d.Subject))
	return assembleWithAttachments(h, d.Body, d.Attachments)
}

// BuildDraft constructs a MIME message for a draft (recipients may be empty).
func BuildDraft(from string, d provider.DraftSave) ([]byte, error) {
	to := dedupe(d.To)
	cc := dedupe(excludeAll(d.Cc, to))
	bcc := dedupe(excludeAll(d.Bcc, append(append([]string{}, to...), cc...)))
	var h headerList
	h.add("From", formatAddr(from))
	if len(to) > 0 {
		h.add("To", formatAddrList(to))
	}
	if len(cc) > 0 {
		h.add("Cc", formatAddrList(cc))
	}
	if len(bcc) > 0 {
		h.add("Bcc", formatAddrList(bcc))
	}
	if d.Subject != "" {
		h.add("Subject", encodeHeader(d.Subject))
	}
	if id := ensureAngle(d.InReplyTo); id != "" {
		h.add("In-Reply-To", id)
		h.add("References", id)
	}
	return assembleWithAttachments(h, d.Body, d.Attachments)
}

type headerList struct{ kv [][2]string }

func (h *headerList) add(k, v string) { h.kv = append(h.kv, [2]string{k, v}) }

var (
	reMdLink   = regexp.MustCompile(`\[([^\]]+)\]\((https?://[^\s()]+|mailto:[^\s()]+)\)`)
	reBold     = regexp.MustCompile(`\*\*([^*\n]+)\*\*`)
	reCodeSpan = regexp.MustCompile("`([^`\n]+)`")
)

func hasRichFormatting(body string) bool {
	return reMdLink.MatchString(body) || reBold.MatchString(body) || reCodeSpan.MatchString(body)
}

func renderSimpleHTML(body string) string {
	escaped := html.EscapeString(strings.ReplaceAll(body, "\r\n", "\n"))
	escaped = reMdLink.ReplaceAllString(escaped, `<a href="$2">$1</a>`)
	escaped = reBold.ReplaceAllString(escaped, `<strong>$1</strong>`)
	escaped = reCodeSpan.ReplaceAllString(escaped, `<code>$1</code>`)
	paragraphs := strings.Split(escaped, "\n\n")
	for i, p := range paragraphs {
		paragraphs[i] = "<p>" + strings.ReplaceAll(p, "\n", "<br>\r\n") + "</p>"
	}
	return "<!DOCTYPE html><html><body>" + strings.Join(paragraphs, "\r\n") + "</body></html>"
}

func stripMarkdownLinksForPlain(body string) string {
	return reMdLink.ReplaceAllStringFunc(body, func(m string) string {
		sub := reMdLink.FindStringSubmatch(m)
		if len(sub) != 3 {
			return m
		}
		if sub[1] == sub[2] || "http://"+sub[1] == sub[2] || "https://"+sub[1] == sub[2] {
			return sub[2]
		}
		return sub[1] + " (" + sub[2] + ")"
	})
}

func writeTextPart(buf *bytes.Buffer, contentType, text string) error {
	fmt.Fprintf(buf, "Content-Type: %s; charset=UTF-8\r\n", contentType)
	buf.WriteString("Content-Transfer-Encoding: quoted-printable\r\n\r\n")
	w := quotedprintable.NewWriter(buf)
	if _, err := w.Write([]byte(normalizeCRLF(text))); err != nil {
		return err
	}
	return w.Close()
}

func writeBodyEntity(buf *bytes.Buffer, body string) error {
	if !hasRichFormatting(body) {
		return writeTextPart(buf, "text/plain", body)
	}
	altBoundary := "dankmail-alt-boundary-9f8e7d6c"
	fmt.Fprintf(buf, "Content-Type: multipart/alternative; boundary=%q\r\n\r\n", altBoundary)
	fmt.Fprintf(buf, "--%s\r\n", altBoundary)
	if err := writeTextPart(buf, "text/plain", stripMarkdownLinksForPlain(body)); err != nil {
		return err
	}
	fmt.Fprintf(buf, "\r\n--%s\r\n", altBoundary)
	if err := writeTextPart(buf, "text/html", renderSimpleHTML(body)); err != nil {
		return err
	}
	fmt.Fprintf(buf, "\r\n--%s--\r\n", altBoundary)
	return nil
}

func resolveAttachment(a provider.AttachmentFile) (filename, mimeType string, data []byte, err error) {
	data = a.Data
	if len(data) == 0 && a.Path != "" {
		cleanPath := strings.TrimPrefix(a.Path, "file://")
		data, err = os.ReadFile(cleanPath)
		if err != nil {
			return "", "", nil, fmt.Errorf("read attachment %s: %w", filepath.Base(cleanPath), err)
		}
		if a.Filename == "" {
			a.Filename = filepath.Base(cleanPath)
		}
	}
	filename = a.Filename
	if filename == "" {
		filename = "attachment.bin"
	}
	mimeType = a.MimeType
	if mimeType == "" {
		mimeType = mime.TypeByExtension(strings.ToLower(filepath.Ext(filename)))
	}
	if mimeType == "" {
		if len(data) > 0 {
			mimeType = http.DetectContentType(data)
		} else {
			mimeType = "application/octet-stream"
		}
	}
	return filename, mimeType, data, nil
}

func assembleWithAttachments(h headerList, body string, attachments []provider.AttachmentFile) ([]byte, error) {
	if len(attachments) == 0 && !hasRichFormatting(body) {
		return assemble(h, body)
	}
	var buf bytes.Buffer
	for _, kv := range h.kv {
		fmt.Fprintf(&buf, "%s: %s\r\n", kv[0], kv[1])
	}
	buf.WriteString("MIME-Version: 1.0\r\n")
	if len(attachments) == 0 {
		if err := writeBodyEntity(&buf, body); err != nil {
			return nil, err
		}
		return buf.Bytes(), nil
	}
	mixBoundary := "dankmail-mix-boundary-1a2b3c4d"
	fmt.Fprintf(&buf, "Content-Type: multipart/mixed; boundary=%q\r\n\r\n", mixBoundary)
	fmt.Fprintf(&buf, "--%s\r\n", mixBoundary)
	if err := writeBodyEntity(&buf, body); err != nil {
		return nil, err
	}
	for _, att := range attachments {
		filename, mimeType, data, err := resolveAttachment(att)
		if err != nil {
			return nil, err
		}
		fmt.Fprintf(&buf, "\r\n--%s\r\n", mixBoundary)
		encName := encodeHeader(filename)
		fmt.Fprintf(&buf, "Content-Type: %s; name=%q\r\n", mimeType, encName)
		fmt.Fprintf(&buf, "Content-Disposition: attachment; filename=%q\r\n", encName)
		buf.WriteString("Content-Transfer-Encoding: base64\r\n\r\n")
		b64 := base64.StdEncoding.EncodeToString(data)
		for i := 0; i < len(b64); i += 76 {
			end := i + 76
			if end > len(b64) {
				end = len(b64)
			}
			buf.WriteString(b64[i:end])
			buf.WriteString("\r\n")
		}
	}
	fmt.Fprintf(&buf, "--%s--\r\n", mixBoundary)
	return buf.Bytes(), nil
}

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

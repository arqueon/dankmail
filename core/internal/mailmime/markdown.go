package mailmime

import (
	"fmt"
	"html"
	"regexp"
	"strconv"
	"strings"
)

var (
	reHeadingLine   = regexp.MustCompile(`^\s*(#{1,6})\s+(.+?)\s*$`)
	reHRLine        = regexp.MustCompile(`^\s*(?:-{3,}|\*{3,}|_{3,})\s*$`)
	reFenceLine     = regexp.MustCompile("(?m)^\\s*```")
	reQuoteLine     = regexp.MustCompile(`(?m)^\s*>\s*\S`)
	reListLine      = regexp.MustCompile(`^(\s*)(?:([-+*])|(\d+)[.)])\s+(.+)$`)
	reListDetect    = regexp.MustCompile(`(?m)^\s*(?:[-+*]|\d+[.)])\s+\S`)
	reTableSepLine  = regexp.MustCompile(`^\s*\|?\s*:?-{3,}:?\s*(\|\s*:?-{3,}:?\s*)+\|?\s*$`)
	reMdLink        = regexp.MustCompile(`\[([^\]]+)\]\((https?://[^\s()]+|mailto:[^\s()]+)\)`)
	reCodeSpan      = regexp.MustCompile("`([^`\n]+)`")
	reBoldItalic    = regexp.MustCompile(`\*\*\*([^*\n]+)\*\*\*`)
	reBoldStar      = regexp.MustCompile(`\*\*([^*\n]+)\*\*`)
	reBoldUnder     = regexp.MustCompile(`(^|[\s(])__([^_\n]+)__`)
	reStrike        = regexp.MustCompile(`~~([^~\n]+)~~`)
	reItalicStar    = regexp.MustCompile(`(^|[\s(«"'])\*([^\s*][^*\n]*?)\*`)
	reItalicUnder   = regexp.MustCompile(`(^|[\s(«"'])_([^\s_][^_\n]*?)_`)
	reHeadingDetect = regexp.MustCompile(`(?m)^\s*#{1,6}\s+\S`)
	reHRDetect      = regexp.MustCompile(`(?m)^\s*(?:-{3,}|\*{3,}|_{3,})\s*$`)

	// Outgoing HTML sanitization patterns (strip active scripts/frames/handlers).
	reDangerousBlock = regexp.MustCompile(`(?is)<\s*(script|iframe|object|embed|applet|form)\b[^>]*>.*?<\s*/\s*(script|iframe|object|embed|applet|form)\s*>`)
	reDangerousTag   = regexp.MustCompile(`(?is)<\s*/?\s*(script|iframe|object|embed|applet|form|meta|base)\b[^>]*>`)
	reEventAttr      = regexp.MustCompile(`(?i)\s+on[a-z0-9_]+\s*=\s*(?:"[^"]*"|'[^']*'|[^\s>]+)`)
	reUnsafeHref     = regexp.MustCompile(`(?i)\b(href|src)\s*=\s*(?:"\s*(?:javascript|vbscript|data\s*:\s*text/html)[^"]*"|'\s*(?:javascript|vbscript|data\s*:\s*text/html)[^']*'|(?:\s*(?:javascript|vbscript|data\s*:\s*text/html)[^\s>]*))`)
)

// HasMarkdownFormatting reports whether body uses Markdown constructs that
// benefit from a multipart/alternative HTML rendering.
func HasMarkdownFormatting(body string) bool {
	if strings.TrimSpace(body) == "" {
		return false
	}
	s := strings.ReplaceAll(body, "\r\n", "\n")
	if reMdLink.MatchString(s) ||
		reBoldStar.MatchString(s) ||
		reBoldUnder.MatchString(s) ||
		reStrike.MatchString(s) ||
		reCodeSpan.MatchString(s) ||
		reFenceLine.MatchString(s) ||
		reHeadingDetect.MatchString(s) ||
		reQuoteLine.MatchString(s) ||
		reListDetect.MatchString(s) ||
		reHRDetect.MatchString(s) ||
		reItalicStar.MatchString(s) ||
		reItalicUnder.MatchString(s) {
		return true
	}
	lines := strings.Split(s, "\n")
	for i := 0; i+1 < len(lines); i++ {
		if strings.Contains(lines[i], "|") && reTableSepLine.MatchString(lines[i+1]) {
			return true
		}
	}
	return false
}

// RenderMarkdownHTML renders Markdown text into a complete, email-client-safe
// UTF-8 HTML document with inline styles suitable for Gmail, Outlook, Apple Mail,
// and standard desktop/mobile MUAs.
func RenderMarkdownHTML(body string) string {
	normalized := strings.ReplaceAll(body, "\r\n", "\n")
	inner := renderMarkdownBlocks(normalized, 0)
	return wrapHTMLDocument(inner)
}

func wrapHTMLDocument(inner string) string {
	trimmed := strings.TrimSpace(inner)
	lower := strings.ToLower(trimmed)
	if strings.HasPrefix(lower, "<!doctype") || strings.HasPrefix(lower, "<html") {
		return trimmed
	}
	return "<!DOCTYPE html><html><head><meta charset=\"UTF-8\"></head>" +
		"<body style=\"font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;font-size:14px;line-height:1.5;color:#1f2328;\">" +
		trimmed + "</body></html>"
}

// SanitizeOutgoingHTML strips active scripts, frames, forms, inline event
// attributes, and unsafe URI schemes from caller-supplied HTML before wrapping
// it in a UTF-8 HTML document.
func SanitizeOutgoingHTML(raw string) string {
	clean := reDangerousBlock.ReplaceAllString(raw, "")
	clean = reDangerousTag.ReplaceAllString(clean, "")
	clean = reEventAttr.ReplaceAllString(clean, "")
	clean = reUnsafeHref.ReplaceAllString(clean, `$1="#"`)
	return wrapHTMLDocument(clean)
}

func renderMarkdownBlocks(text string, depth int) string {
	lines := strings.Split(text, "\n")
	var out strings.Builder
	i := 0

	isBlockStart := func(idx int) bool {
		l := lines[idx]
		trimmed := strings.TrimSpace(l)
		if trimmed == "" {
			return true
		}
		if strings.HasPrefix(trimmed, "```") ||
			strings.HasPrefix(trimmed, ">") ||
			reHeadingLine.MatchString(l) ||
			reHRLine.MatchString(l) ||
			reListLine.MatchString(l) {
			return true
		}
		if idx+1 < len(lines) && strings.Contains(l, "|") && reTableSepLine.MatchString(lines[idx+1]) {
			return true
		}
		return false
	}

	var parseList func() string
	parseList = func() string {
		m := reListLine.FindStringSubmatch(lines[i])
		if m == nil {
			return ""
		}
		baseIndent := len(m[1])
		ordered := m[3] != ""
		var b strings.Builder
		if ordered {
			startNum, _ := strconv.Atoi(m[3])
			if startNum > 1 {
				fmt.Fprintf(&b, `<ol start="%d" style="margin:0 0 12px 0;padding-left:22px;">`, startNum)
			} else {
				b.WriteString(`<ol style="margin:0 0 12px 0;padding-left:22px;">`)
			}
		} else {
			b.WriteString(`<ul style="margin:0 0 12px 0;padding-left:22px;">`)
		}

		for i < len(lines) {
			cur := reListLine.FindStringSubmatch(lines[i])
			if cur == nil || (cur[3] != "") != ordered || len(cur[1]) < baseIndent {
				break
			}
			itemParts := []string{strings.TrimSpace(cur[4])}
			i++
			var nested strings.Builder
			for i < len(lines) && strings.TrimSpace(lines[i]) != "" {
				trimmedNext := strings.TrimSpace(lines[i])
				if strings.HasPrefix(trimmedNext, "```") ||
					strings.HasPrefix(trimmedNext, ">") ||
					reHeadingLine.MatchString(lines[i]) ||
					reHRLine.MatchString(lines[i]) ||
					(i+1 < len(lines) && strings.Contains(lines[i], "|") && reTableSepLine.MatchString(lines[i+1])) {
					break
				}
				if nextList := reListLine.FindStringSubmatch(lines[i]); nextList != nil {
					if len(nextList[1]) > baseIndent {
						nested.WriteString(parseList())
						continue
					}
					break
				}
				itemParts = append(itemParts, strings.TrimSpace(lines[i]))
				i++
			}
			b.WriteString(`<li style="margin:3px 0;">`)
			b.WriteString(renderInlineMarkdown(strings.Join(itemParts, " ")))
			b.WriteString(nested.String())
			b.WriteString(`</li>`)
		}

		if ordered {
			b.WriteString(`</ol>`)
		} else {
			b.WriteString(`</ul>`)
		}
		return b.String()
	}

	for i < len(lines) {
		line := lines[i]
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			i++
			continue
		}

		// Fenced code block.
		if strings.HasPrefix(trimmed, "```") {
			i++
			var codeLines []string
			for i < len(lines) && !strings.HasPrefix(strings.TrimSpace(lines[i]), "```") {
				codeLines = append(codeLines, lines[i])
				i++
			}
			if i < len(lines) {
				i++
			}
			out.WriteString(`<pre style="font-family:ui-monospace,SFMono-Regular,Menlo,Consolas,monospace;font-size:13px;background:#f6f8fa;padding:12px;border-radius:6px;overflow-x:auto;margin:0 0 12px 0;"><code>`)
			out.WriteString(html.EscapeString(strings.Join(codeLines, "\n")))
			out.WriteString("</code></pre>\r\n")
			continue
		}

		// Blockquote (bounded recursion).
		if strings.HasPrefix(trimmed, ">") {
			var qLines []string
			for i < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[i]), ">") {
				ql := strings.TrimSpace(lines[i])
				ql = strings.TrimPrefix(ql, ">")
				ql = strings.TrimPrefix(ql, " ")
				qLines = append(qLines, ql)
				i++
			}
			out.WriteString(`<blockquote style="margin:0 0 12px 0;padding:4px 12px;border-left:3px solid #d0d7de;color:#57606a;">`)
			if depth < 5 {
				out.WriteString(renderMarkdownBlocks(strings.Join(qLines, "\n"), depth+1))
			} else {
				out.WriteString(renderInlineMarkdown(strings.Join(qLines, "\n")))
			}
			out.WriteString("</blockquote>\r\n")
			continue
		}

		// Heading (# .. ######).
		if hm := reHeadingLine.FindStringSubmatch(line); hm != nil {
			level := len(hm[1])
			sizes := map[int]string{1: "22px", 2: "19px", 3: "16px", 4: "15px", 5: "14px", 6: "13px"}
			fmt.Fprintf(&out, `<h%d style="font-size:%s;font-weight:600;margin:14px 0 8px 0;line-height:1.3;">%s</h%d>`+"\r\n",
				level, sizes[level], renderInlineMarkdown(hm[2]), level)
			i++
			continue
		}

		// Horizontal rule.
		if reHRLine.MatchString(line) {
			out.WriteString(`<hr style="border:none;border-top:1px solid #d0d7de;margin:16px 0;">` + "\r\n")
			i++
			continue
		}

		// Pipe table.
		if i+1 < len(lines) && strings.Contains(line, "|") && reTableSepLine.MatchString(lines[i+1]) {
			headers := splitTableCells(line)
			aligns := parseTableAlignments(lines[i+1], len(headers))
			out.WriteString(`<table style="border-collapse:collapse;margin:0 0 12px 0;width:100%;"><thead><tr>`)
			for cIdx, hCell := range headers {
				fmt.Fprintf(&out, `<th style="border:1px solid #d0d7de;padding:6px 10px;background:#f6f8fa;font-weight:600;text-align:%s;">%s</th>`,
					aligns[cIdx], renderInlineMarkdown(hCell))
			}
			out.WriteString(`</tr></thead><tbody>`)
			i += 2
			for i < len(lines) && strings.TrimSpace(lines[i]) != "" && strings.Contains(lines[i], "|") {
				cells := splitTableCells(lines[i])
				out.WriteString(`<tr>`)
				for cIdx := range headers {
					val := ""
					if cIdx < len(cells) {
						val = cells[cIdx]
					}
					fmt.Fprintf(&out, `<td style="border:1px solid #d0d7de;padding:6px 10px;text-align:%s;">%s</td>`,
						aligns[cIdx], renderInlineMarkdown(val))
				}
				out.WriteString(`</tr>`)
				i++
			}
			out.WriteString("</tbody></table>\r\n")
			continue
		}

		// Unordered / ordered list.
		if reListLine.MatchString(line) {
			out.WriteString(parseList())
			out.WriteString("\r\n")
			continue
		}

		// Standard paragraph.
		var para []string
		for i < len(lines) && !isBlockStart(i) {
			para = append(para, lines[i])
			i++
		}
		if len(para) > 0 {
			var renderedLines []string
			for _, pl := range para {
				renderedLines = append(renderedLines, renderInlineMarkdown(pl))
			}
			out.WriteString(`<p style="margin:0 0 12px 0;">`)
			out.WriteString(strings.Join(renderedLines, "<br>\r\n"))
			out.WriteString("</p>\r\n")
		}
	}
	return strings.TrimSpace(out.String())
}

func splitTableCells(line string) []string {
	s := strings.TrimSpace(line)
	s = strings.TrimPrefix(s, "|")
	s = strings.TrimSuffix(s, "|")
	parts := strings.Split(s, "|")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

func parseTableAlignments(sepLine string, count int) []string {
	raw := splitTableCells(sepLine)
	aligns := make([]string, count)
	for i := 0; i < count; i++ {
		aligns[i] = "left"
		if i < len(raw) {
			c := strings.TrimSpace(raw[i])
			left := strings.HasPrefix(c, ":")
			right := strings.HasSuffix(c, ":")
			switch {
			case left && right:
				aligns[i] = "center"
			case right:
				aligns[i] = "right"
			}
		}
	}
	return aligns
}

func renderInlineMarkdown(text string) string {
	var stash []string
	put := func(fragment string) string {
		idx := len(stash)
		stash = append(stash, fragment)
		return fmt.Sprintf("\x01%d\x01", idx)
	}

	// 1. Stash inline code spans before escaping or emphasis.
	s := reCodeSpan.ReplaceAllStringFunc(text, func(m string) string {
		sub := reCodeSpan.FindStringSubmatch(m)
		if len(sub) != 2 {
			return m
		}
		return put(`<code style="font-family:ui-monospace,SFMono-Regular,Menlo,Consolas,monospace;background:#f3f4f6;padding:1px 4px;border-radius:4px;">` + html.EscapeString(sub[1]) + `</code>`)
	})

	// 2. Stash Markdown links [label](https://...|mailto:...).
	s = reMdLink.ReplaceAllStringFunc(s, func(m string) string {
		sub := reMdLink.FindStringSubmatch(m)
		if len(sub) != 3 {
			return m
		}
		labelHTML := applyInlineEmphasis(html.EscapeString(sub[1]))
		href := html.EscapeString(sub[2])
		return put(`<a href="` + href + `">` + labelHTML + `</a>`)
	})

	// 3. Escape remaining HTML and apply inline emphasis.
	s = applyInlineEmphasis(html.EscapeString(s))

	// 4. Restore stashed HTML fragments in reverse order.
	for i := len(stash) - 1; i >= 0; i-- {
		token := fmt.Sprintf("\x01%d\x01", i)
		s = strings.ReplaceAll(s, token, stash[i])
	}
	return s
}

func applyInlineEmphasis(escaped string) string {
	s := reBoldItalic.ReplaceAllString(escaped, `<strong><em>$1</em></strong>`)
	s = reBoldStar.ReplaceAllString(s, `<strong>$1</strong>`)
	s = reBoldUnder.ReplaceAllString(s, `$1<strong>$2</strong>`)
	s = reStrike.ReplaceAllString(s, `<del>$1</del>`)
	s = reItalicStar.ReplaceAllString(s, `$1<em>$2</em>`)
	s = reItalicUnder.ReplaceAllString(s, `$1<em>$2</em>`)
	return s
}

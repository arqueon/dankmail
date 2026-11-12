// Package mailtext distills rich HTML email bodies into clean, bounded
// Markdown-like text suitable for dankmail's non-WebEngine preview.
// Hidden preheader padding, tracking styles, scripts, and single-column
// layout tables are stripped or unwrapped while preserving links, lists,
// headings, inline emphasis, code blocks, and genuine multi-column tables.
package mailtext

import (
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/JohannesKaufmann/html-to-markdown/v2/converter"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/base"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/commonmark"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/strikethrough"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/table"
	"golang.org/x/net/html"
)

var (
	reInvisiblePadding = regexp.MustCompile(`[\x{00AD}\x{034F}\x{061C}\x{200B}-\x{200F}\x{202A}-\x{202E}\x{2060}-\x{206F}\x{FEFF}]+`)
	reBlankLines       = regexp.MustCompile(`\n{3,}`)
	rePlaceholderImg   = regexp.MustCompile(`(?i)^(?:spacer|pixel|1x1|blank|tracking|image\d*(?:\.[a-z0-9]+)?|img_\d+(?:\.[a-z0-9]+)?)$`)
)

// ExtractPreferredBody distills htmlBody when present and non-empty after
// conversion, falling back to plainBody for plain-text-only messages or if
// the HTML carries no visible text.
func ExtractPreferredBody(htmlBody, plainBody string) string {
	if strings.TrimSpace(htmlBody) != "" {
		if distilled := HTMLToText(htmlBody); distilled != "" {
			return distilled
		}
	}
	return CleanText(plainBody)
}

// HTMLToText converts an HTML email fragment or document into clean
// Markdown-flavored plain text. Errors degrade to an empty string.
func HTMLToText(rawHTML string) string {
	if strings.TrimSpace(rawHTML) == "" {
		return ""
	}
	doc, err := html.Parse(strings.NewReader(rawHTML))
	if err != nil {
		return ""
	}
	sanitizeDOM(doc, false)

	conv := converter.NewConverter(
		converter.WithPlugins(
			base.NewBasePlugin(),
			commonmark.NewCommonmarkPlugin(),
			strikethrough.NewStrikethroughPlugin(),
			table.NewTablePlugin(
				table.WithSkipEmptyRows(true),
				table.WithHeaderPromotion(true),
				table.WithCellPaddingBehavior(table.CellPaddingBehaviorMinimal),
			),
		),
	)
	out, err := conv.ConvertNode(doc)
	if err != nil {
		return ""
	}
	return CleanText(string(out))
}

// CleanText strips invisible email preheader padding and collapses runs of
// blank lines while keeping intentional paragraph breaks.
func CleanText(s string) string {
	if s == "" {
		return ""
	}
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	s = reInvisiblePadding.ReplaceAllString(s, "")
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " \t")
	}
	s = strings.Join(lines, "\n")
	s = reBlankLines.ReplaceAllString(s, "\n\n")
	return strings.TrimSpace(s)
}

// TruncateAtRune cuts s to at most capBytes bytes without splitting a UTF-8 rune.
func TruncateAtRune(s string, capBytes int) string {
	if capBytes <= 0 || len(s) <= capBytes {
		return s
	}
	cut := capBytes
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}

func sanitizeDOM(n *html.Node, inAnchor bool) {
	for c := n.FirstChild; c != nil; {
		next := c.NextSibling
		if shouldDropNode(c) {
			n.RemoveChild(c)
			c = next
			continue
		}
		if c.Type == html.ElementNode && strings.EqualFold(c.Data, "img") {
			replaceOrDropImage(n, c, inAnchor)
			c = next
			continue
		}
		isAnchor := c.Type == html.ElementNode && strings.EqualFold(c.Data, "a")
		sanitizeDOM(c, inAnchor || isAnchor)
		if c.Type == html.ElementNode {
			tag := strings.ToLower(c.Data)
			if tag == "table" && isLayoutTable(c) {
				unwrapTable(n, c)
				c = next
				continue
			}
			if inAnchor {
				switch tag {
				case "div", "p", "section", "article", "h1", "h2", "h3", "h4", "h5", "h6":
					c.Data = "span"
					n.InsertBefore(&html.Node{Type: html.TextNode, Data: " "}, c)
					if next != nil {
						n.InsertBefore(&html.Node{Type: html.TextNode, Data: " "}, next)
					} else {
						n.AppendChild(&html.Node{Type: html.TextNode, Data: " "})
					}
				}
			}
			if isAnchor && strings.TrimSpace(nodeText(c)) == "" {
				label := strings.TrimSpace(attrVal(c, "title"))
				if label == "" {
					label = anchorFallbackLabel(attrVal(c, "href"))
				}
				if label != "" {
					c.AppendChild(&html.Node{Type: html.TextNode, Data: label})
				} else {
					n.RemoveChild(c)
				}
			}
		}
		c = next
	}
}

func replaceOrDropImage(parent, img *html.Node, inAnchor bool) {
	w := strings.TrimSpace(attrVal(img, "width"))
	h := strings.TrimSpace(attrVal(img, "height"))
	if w == "0" || w == "1" || h == "0" || h == "1" {
		parent.RemoveChild(img)
		return
	}
	label := strings.TrimSpace(attrVal(img, "alt"))
	if label == "" && inAnchor {
		label = strings.TrimSpace(attrVal(img, "title"))
	}
	if label != "" && !rePlaceholderImg.MatchString(label) {
		parent.InsertBefore(&html.Node{Type: html.TextNode, Data: " " + label + " "}, img)
	}
	parent.RemoveChild(img)
}

func attrVal(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if strings.EqualFold(a.Key, key) {
			return a.Val
		}
	}
	return ""
}

func anchorFallbackLabel(rawHref string) string {
	href := strings.TrimSpace(rawHref)
	if href == "" {
		return ""
	}
	u, err := url.Parse(href)
	if err != nil {
		return ""
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
		host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
		return host
	case "mailto":
		return u.Opaque
	}
	return ""
}

func shouldDropNode(n *html.Node) bool {
	if n.Type == html.CommentNode || n.Type == html.DoctypeNode {
		return true
	}
	if n.Type != html.ElementNode {
		return false
	}
	switch strings.ToLower(n.Data) {
	case "head", "script", "style", "noscript", "svg", "meta", "link", "title":
		return true
	}
	for _, a := range n.Attr {
		switch strings.ToLower(a.Key) {
		case "hidden":
			return true
		case "aria-hidden":
			if strings.EqualFold(strings.TrimSpace(a.Val), "true") && strings.TrimSpace(nodeText(n)) == "" {
				return true
			}
		case "style":
			if isHiddenStyle(a.Val) {
				return true
			}
		}
	}
	return false
}

func isHiddenStyle(style string) bool {
	compact := strings.ToLower(strings.ReplaceAll(style, " ", ""))
	if strings.Contains(compact, "display:none") ||
		strings.Contains(compact, "visibility:hidden") ||
		strings.Contains(compact, "mso-hide:all") {
		return true
	}
	if (strings.Contains(compact, "max-height:0") || strings.Contains(compact, "height:0")) &&
		(strings.Contains(compact, "opacity:0") || strings.Contains(compact, "overflow:hidden") || strings.Contains(compact, "font-size:0")) {
		return true
	}
	return false
}

// isLayoutTable identifies HTML email wrapper tables that should become normal
// block flow rather than Markdown pipe tables: role=presentation/none, nested
// tables, single-column tables without explicit headers, or cells containing
// block-level layout containers.
func isLayoutTable(tableNode *html.Node) bool {
	for _, a := range tableNode.Attr {
		if strings.EqualFold(a.Key, "role") {
			role := strings.ToLower(strings.TrimSpace(a.Val))
			if role == "presentation" || role == "none" {
				return true
			}
		}
	}
	hasNestedTable := false
	hasBlockInCell := false
	hasTH := false
	maxCols := 0
	rowCount := 0

	var inspect func(cur *html.Node, inCell bool)
	inspect = func(cur *html.Node, inCell bool) {
		for ch := cur.FirstChild; ch != nil; ch = ch.NextSibling {
			if ch.Type != html.ElementNode {
				continue
			}
			tag := strings.ToLower(ch.Data)
			if tag == "table" {
				hasNestedTable = true
				return
			}
			if inCell {
				switch tag {
				case "div", "p", "h1", "h2", "h3", "h4", "h5", "h6", "ul", "ol", "blockquote", "hr", "section", "article":
					hasBlockInCell = true
				}
			}
			if tag == "tr" {
				cols := 0
				for cell := ch.FirstChild; cell != nil; cell = cell.NextSibling {
					if cell.Type != html.ElementNode {
						continue
					}
					ctag := strings.ToLower(cell.Data)
					if ctag == "th" {
						hasTH = true
						cols++
					} else if ctag == "td" {
						cols++
					}
				}
				if cols > 0 {
					rowCount++
				}
				if cols > maxCols {
					maxCols = cols
				}
			}
			inspect(ch, inCell || tag == "td" || tag == "th")
		}
	}
	inspect(tableNode, false)

	if hasNestedTable || hasBlockInCell {
		return true
	}
	if maxCols <= 1 && !hasTH {
		return true
	}
	if !hasTH && rowCount <= 1 {
		return true
	}
	return false
}

func unwrapTable(parent, tableNode *html.Node) {
	wrapper := &html.Node{Type: html.ElementNode, Data: "div"}
	var extractCells func(cur *html.Node)
	extractCells = func(cur *html.Node) {
		for ch := cur.FirstChild; ch != nil; ch = ch.NextSibling {
			if ch.Type == html.ElementNode {
				tag := strings.ToLower(ch.Data)
				if tag == "td" || tag == "th" {
					block := &html.Node{Type: html.ElementNode, Data: "div"}
					for inner := ch.FirstChild; inner != nil; {
						nextInner := inner.NextSibling
						ch.RemoveChild(inner)
						block.AppendChild(inner)
						inner = nextInner
					}
					wrapper.AppendChild(block)
					continue
				}
			}
			extractCells(ch)
		}
	}
	extractCells(tableNode)
	parent.InsertBefore(wrapper, tableNode)
	parent.RemoveChild(tableNode)
}

func nodeText(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(cur *html.Node) {
		if cur.Type == html.TextNode {
			b.WriteString(cur.Data)
		}
		for ch := cur.FirstChild; ch != nil; ch = ch.NextSibling {
			walk(ch)
		}
	}
	walk(n)
	return b.String()
}

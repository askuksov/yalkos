package plane

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
	"unicode"

	"github.com/JohannesKaufmann/html-to-markdown/v2/converter"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/base"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/commonmark"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/strikethrough"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/table"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// Refs resolves the ids that Plane writes into its HTML. Any field may be nil,
// which behaves like a lookup that always fails.
type Refs struct {
	// User returns the display name of a workspace member.
	User func(id string) (name string, ok bool)
	// Issue returns the Yalkos identifier (for example "ALP-3") of an issue.
	Issue func(id string) (ident string, ok bool)
	// Image returns the Markdown URL of an already downloaded image.
	Image func(assetID string) (markdownURL string, ok bool)
}

// Converted is the result of ToMarkdown.
type Converted struct {
	Markdown string
	// Images lists the asset ids of the embedded images in document order,
	// each id once.
	Images []string
	// Unknown counts the elements that neither the converter nor the Plane
	// rules know, by tag name. Their text is kept in the output.
	Unknown map[string]int
}

const (
	unknownUser    = "@unknown user"
	unknownMention = "[mention]"
	unknownIssue   = "[work item not imported]"
	unknownImage   = "[image not imported]"
	tagMention     = "mention-component"
	tagImage       = "image-component"
	tagIssueEmbed  = "issue-embed-component"
)

// knownTags are the standard HTML elements. The converter either renders
// them or deliberately drops them, so they are never reported as unknown.
var knownTags = func() map[string]bool {
	m := make(map[string]bool)
	for _, name := range strings.Fields(`
		html head body a abbr address article aside b bdi bdo blockquote br caption cite code col colgroup
		dd del details dfn div dl dt em fieldset figcaption figure footer form h1 h2 h3 h4 h5 h6 header hgroup
		hr i img input ins kbd label legend li main mark nav ol p pre q rp rt ruby s samp section small span
		strike strong sub summary sup table tbody td tfoot th thead time tr tt u ul var wbr
		button select option optgroup textarea iframe noscript script style link meta title base
		audio video source track picture canvas svg math object embed area map center font big
		#comment #text #doctype #document`) {
		m[name] = true
	}
	return m
}()

// ToMarkdown converts the HTML of a Plane description or comment to Markdown.
// It never panics; broken HTML yields the best-effort text.
func ToMarkdown(htmlText string, refs Refs) (res Converted, err error) {
	res = Converted{Unknown: map[string]int{}}
	defer func() {
		if r := recover(); r != nil {
			res.Markdown = strings.TrimSpace(plainText(htmlText))
			err = nil
		}
	}()

	doc, perr := html.Parse(strings.NewReader(htmlText))
	if perr != nil {
		res.Markdown = strings.TrimSpace(plainText(htmlText))
		return res, nil
	}
	prepare(doc, refs, &res)

	conv := newConverter(refs)
	out, cerr := conv.ConvertNode(doc)
	if cerr != nil {
		res.Markdown = strings.TrimSpace(plainText(htmlText))
		return res, fmt.Errorf("convert plane html: %w", cerr)
	}
	res.Markdown = strings.Trim(string(out), "\n")
	return res, nil
}

// prepare walks the tree once. It collects image ids, counts unknown elements,
// frees content that an HTML parser nested inside a custom element meant to be
// empty (a self-closed tag is not self-closed in HTML), turns user mentions
// into plain text, drops links and images whose URL is not safe, and bounds
// table spans. Nodes moved by these steps are visited again.
func prepare(doc *html.Node, refs Refs, res *Converted) {
	p := preparer{refs: refs, res: res, seen: map[string]bool{}}
	p.walk(doc)
}

type preparer struct {
	refs      Refs
	res       *Converted
	seen      map[string]bool
	spanSpent int
}

func (p *preparer) walk(n *html.Node) {
	for c := n.FirstChild; c != nil; {
		if c.Type != html.ElementNode {
			c = c.NextSibling
			continue
		}
		switch c.Data {
		case tagMention:
			// A mention is empty by definition, so whatever the parser nested
			// inside it (a self-closed tag) is content that follows it.
			hoist(c)
			next := c.NextSibling
			if kind := attr(c, "entity_name"); kind == "user_mention" {
				p.replaceWithText(c, p.userText(attr(c, "entity_identifier")))
			} else {
				if kind == "" {
					kind = "unknown"
				}
				p.res.Unknown[tagMention+":"+kind]++
				p.replaceWithText(c, unknownMention)
			}
			c = next
			continue
		case tagImage, tagIssueEmbed:
			hoist(c)
			if c.Data == tagImage {
				if src := attr(c, "src"); src != "" && !isURL(src) && !p.seen[src] {
					p.seen[src] = true
					p.res.Images = append(p.res.Images, src)
				}
			}
		case "a":
			if href := attr(c, "href"); href != "" && !safeURL(href) {
				// Visit the former children as part of this loop.
				first, next := c.FirstChild, c.NextSibling
				unwrap(c)
				if first != nil {
					next = first
				}
				c = next
				continue
			}
		case "img":
			if src := attr(c, "src"); src != "" && !safeURL(src) {
				alt := attr(c, "alt")
				if alt == "" {
					alt = "[image]"
				}
				next := c.NextSibling
				p.replaceWithText(c, alt)
				c = next
				continue
			}
		case "li":
			tightenItem(c)
		case "td", "th":
			p.boundSpans(c)
		}
		if !knownTags[c.Data] && c.Data != tagMention && c.Data != tagImage && c.Data != tagIssueEmbed {
			p.res.Unknown[c.Data]++
		}
		p.walk(c)
		next := c.NextSibling
		if c.Data == "table" {
			flattenWideTable(c)
		}
		c = next
	}
}

// maxGridCells bounds a GFM table: the renderer pads every row to the widest
// one, so width x rows is what the output grows with.
const maxGridCells = 20000

// flattenWideTable replaces a table whose grid is too large by one paragraph
// per row, with the cell texts joined by " | ". It runs after the spans of
// the table were bounded.
func flattenWideTable(table *html.Node) {
	var rows []*html.Node
	var collect func(n *html.Node)
	collect = func(n *html.Node) {
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type != html.ElementNode || c.Data == "table" {
				continue
			}
			if c.Data == "tr" {
				rows = append(rows, c)
				continue
			}
			collect(c)
		}
	}
	collect(table)

	width := 0
	for _, tr := range rows {
		w := 0
		for c := tr.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.ElementNode && (c.Data == "td" || c.Data == "th") {
				w += spanValue(c, "colspan")
			}
		}
		width = max(width, w)
	}
	if width*len(rows) <= maxGridCells || table.Parent == nil {
		return
	}

	box := &html.Node{Type: html.ElementNode, Data: "div", DataAtom: atom.Div}
	for _, tr := range rows {
		var texts []string
		for c := tr.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.ElementNode && (c.Data == "td" || c.Data == "th") {
				texts = append(texts, strings.Join(strings.Fields(textContent(c)), " "))
			}
		}
		line := strings.Join(texts, " | ")
		if strings.Trim(line, " |") == "" {
			continue
		}
		para := &html.Node{Type: html.ElementNode, Data: "p", DataAtom: atom.P}
		para.AppendChild(&html.Node{Type: html.TextNode, Data: line})
		box.AppendChild(para)
	}
	table.Parent.InsertBefore(box, table)
	table.Parent.RemoveChild(table)
}

func textContent(n *html.Node) string {
	var b strings.Builder
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return b.String()
}

// userText is the text a user mention turns into.
func (p *preparer) userText(id string) string {
	if p.refs.User != nil {
		if name, ok := p.refs.User(id); ok && strings.TrimSpace(name) != "" {
			return "@" + name
		}
	}
	return unknownUser
}

// replaceWithText swaps an element for a text node; the converter escapes the
// text for Markdown.
func (p *preparer) replaceWithText(n *html.Node, text string) {
	if n.Parent == nil {
		return
	}
	n.Parent.InsertBefore(&html.Node{Type: html.TextNode, Data: text}, n)
	n.Parent.RemoveChild(n)
}

// hoist moves the children of n to directly behind n.
func hoist(n *html.Node) {
	parent, next := n.Parent, n.NextSibling
	if parent == nil {
		return
	}
	for n.FirstChild != nil {
		child := n.FirstChild
		n.RemoveChild(child)
		parent.InsertBefore(child, next)
	}
}

// tightenItem makes a list item tight. TipTap wraps the text of every item in
// a paragraph (and, for task items, in a div), which the converter would
// render as a loose list with blank lines inside the item.
func tightenItem(li *html.Node) {
	if attr(li, "data-type") == "taskItem" {
		for c := li.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.ElementNode && c.Data == "div" {
				unwrap(c)
				break
			}
		}
	}
	for c := li.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && c.Data == "label" {
			continue
		}
		if c.Type == html.ElementNode && c.Data == "p" {
			unwrap(c)
		}
		break
	}
}

// unwrap replaces an element by its children.
func unwrap(n *html.Node) {
	parent, next := n.Parent, n.NextSibling
	if parent == nil {
		return
	}
	for n.FirstChild != nil {
		child := n.FirstChild
		n.RemoveChild(child)
		parent.InsertBefore(child, next)
	}
	parent.RemoveChild(n)
}

// The table renderer allocates per spanned cell, so hostile spans would stall
// the import and fill the output: every span is at most maxSpan, and all cells
// of one document together may span at most spanBudget grid cells. Real Plane
// tables have spans of 2 or 3.
const (
	maxSpan    = 10
	spanBudget = 10000
)

func (p *preparer) boundSpans(cell *html.Node) {
	col, row := spanValue(cell, "colspan"), spanValue(cell, "rowspan")
	if p.spanSpent+col*row > spanBudget {
		col, row = 1, 1
	}
	p.spanSpent += col * row
	setAttr(cell, "colspan", col)
	setAttr(cell, "rowspan", row)
}

// spanValue reads a span attribute as a number from 1 to maxSpan.
func spanValue(cell *html.Node, key string) int {
	n := 0
	for _, r := range attr(cell, key) {
		if r < '0' || r > '9' {
			return 1
		}
		n = n*10 + int(r-'0')
		if n > maxSpan {
			return maxSpan
		}
	}
	if n < 1 {
		return 1
	}
	return n
}

// setAttr rewrites an attribute that is already present; spans of 1 need no
// attribute at all.
func setAttr(n *html.Node, key string, v int) {
	for i, a := range n.Attr {
		if a.Key == key {
			n.Attr[i].Val = strconv.Itoa(v)
		}
	}
}

func newConverter(refs Refs) *converter.Converter {
	conv := converter.NewConverter(
		converter.WithPlugins(
			base.NewBasePlugin(),
			commonmark.NewCommonmarkPlugin(commonmark.WithHorizontalRule("---")),
			strikethrough.NewStrikethroughPlugin(),
			table.NewTablePlugin(
				table.WithCellPaddingBehavior(table.CellPaddingBehaviorMinimal),
				table.WithNewlineBehavior(table.NewlineBehaviorPreserve),
				table.WithHeaderPromotion(true),
			),
		),
	)

	conv.Register.RendererFor(tagIssueEmbed, converter.TagTypeInline,
		func(_ converter.Context, w converter.Writer, n *html.Node) converter.RenderStatus {
			ident, ok := "", false
			if refs.Issue != nil {
				ident, ok = refs.Issue(attr(n, "entity_identifier"))
			}
			if !ok || ident == "" {
				_, _ = w.WriteString(unknownIssue)
				return converter.RenderSuccess
			}
			_, _ = w.WriteString(ident)
			return converter.RenderSuccess
		}, converter.PriorityEarly)

	conv.Register.RendererFor(tagImage, converter.TagTypeBlock,
		func(_ converter.Context, w converter.Writer, n *html.Node) converter.RenderStatus {
			src := attr(n, "src")
			url, ok := "", false
			switch {
			case src == "":
			case isURL(src):
				url, ok = src, true
			case refs.Image != nil:
				url, ok = refs.Image(src)
			}
			if !ok || url == "" {
				_, _ = w.WriteString("\n\n" + unknownImage + "\n\n")
				return converter.RenderSuccess
			}
			_, _ = w.WriteString("\n\n![image](" + destEscaper.Replace(url) + ")\n\n")
			return converter.RenderSuccess
		}, converter.PriorityEarly)

	// A list nested in an item follows the item text directly. The converter
	// puts a blank line in between, which makes the outer list loose.
	rendering := map[*html.Node]bool{}
	conv.Register.Renderer(func(ctx converter.Context, w converter.Writer, n *html.Node) converter.RenderStatus {
		name := n.Data
		if n.Type != html.ElementNode || (name != "ul" && name != "ol") || rendering[n] || n.Parent == nil || n.Parent.Data != "li" {
			return converter.RenderTryNext
		}
		rendering[n] = true
		defer delete(rendering, n)
		var buf bytes.Buffer
		ctx.RenderNodes(ctx, &buf, n)
		_, _ = w.WriteString("\n")
		_, _ = w.Write(bytes.Trim(buf.Bytes(), "\n"))
		_, _ = w.WriteString("\n")
		return converter.RenderSuccess
	}, converter.PriorityEarly)

	// Task items: drop the checkbox label and prefix the content with its state.
	conv.Register.Renderer(func(ctx converter.Context, w converter.Writer, n *html.Node) converter.RenderStatus {
		if n.Type != html.ElementNode || n.Data != "li" || attr(n, "data-type") != "taskItem" {
			return converter.RenderTryNext
		}
		var buf bytes.Buffer
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.ElementNode && c.Data == "label" {
				continue
			}
			ctx.RenderNodes(ctx, &buf, c)
		}
		if attr(n, "data-checked") == "true" {
			_, _ = w.WriteString("[x] ")
		} else {
			_, _ = w.WriteString("[ ] ")
		}
		_, _ = w.Write(bytes.TrimSpace(buf.Bytes()))
		return converter.RenderSuccess
	}, converter.PriorityEarly)

	return conv
}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return strings.TrimSpace(a.Val)
		}
	}
	return ""
}

// safeURL reports whether a link or image URL may reach the output: http,
// https, mailto, or a relative reference. Whitespace and control characters
// are ignored the way browsers ignore them inside a scheme ("java\tscript:").
func safeURL(raw string) bool {
	var b strings.Builder
	for _, r := range raw {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			continue
		}
		b.WriteRune(unicode.ToLower(r))
	}
	u := b.String()
	i := strings.IndexByte(u, ':')
	if i < 0 {
		return true
	}
	for j, r := range u[:i] {
		letter := r >= 'a' && r <= 'z'
		if !letter && (j == 0 || ((r < '0' || r > '9') && r != '+' && r != '-' && r != '.')) {
			return true // not a scheme: the colon belongs to a relative path
		}
	}
	switch u[:i] {
	case "http", "https", "mailto":
		return true
	}
	return false
}

// destEscaper makes a URL safe inside the parentheses of a Markdown link.
var destEscaper = strings.NewReplacer(
	" ", "%20", "(", "%28", ")", "%29", "<", "%3C", ">", "%3E", "\n", "%0A", "\r", "%0D", "\t", "%09",
)

func isURL(s string) bool {
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

// plainText is the fallback for input the converter cannot handle: the text
// of the HTML with every tag stripped. It tokenizes instead of building a tree
// because the tree builder is what gives up on deeply nested input.
func plainText(s string) string {
	var b strings.Builder
	z := html.NewTokenizer(strings.NewReader(s))
	skip := 0 // inside script or style
	for {
		switch z.Next() {
		case html.ErrorToken:
			return collapseBlankLines(b.String())
		case html.TextToken:
			if skip == 0 {
				b.WriteString(escapeFallback(string(z.Text())))
			}
		case html.StartTagToken, html.EndTagToken, html.SelfClosingTagToken:
			tok := z.Token()
			switch tok.Data {
			case "script", "style":
				switch tok.Type {
				case html.StartTagToken:
					skip++
				case html.EndTagToken:
					if skip > 0 {
						skip--
					}
				}
			case "br", "p", "div", "li", "tr", "h1", "h2", "h3", "h4", "h5", "h6", "blockquote", "pre", "ul", "ol", "table":
				if tok.Type != html.StartTagToken || tok.Data == "br" {
					b.WriteByte('\n')
				}
			}
		}
	}
}

func collapseBlankLines(s string) string {
	for strings.Contains(s, "\n\n\n") {
		s = strings.ReplaceAll(s, "\n\n\n", "\n\n")
	}
	return s
}

// escapeFallback makes decoded text safe to show as Markdown: no markup, no
// links, no emphasis.
func escapeFallback(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		case '&':
			b.WriteString("&amp;")
		case '\\', '`', '*', '_', '[', ']', '(', ')', '#', '|', '~', '!':
			b.WriteByte('\\')
			b.WriteRune(r)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

package plane

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func testRefs(withImages bool) Refs {
	refs := Refs{
		User: func(id string) (string, bool) {
			switch id {
			case "u1":
				return "Ann Lee", true
			case "u2":
				return "A*b_ [x] | y", true
			}
			return "", false
		},
		Issue: func(id string) (string, bool) {
			if id == "i1" {
				return "ALP-3", true
			}
			return "", false
		},
		Image: func(string) (string, bool) { return "", false },
	}
	if withImages {
		refs.Image = func(id string) (string, bool) {
			switch id {
			case "a1":
				return "/api/attachments/x/a.png", true
			case "a2":
				return "/api/attachments/x/my file (1).png", true
			}
			return "", false
		}
	}
	return refs
}

func TestToMarkdown(t *testing.T) {
	tests := []struct {
		name       string
		html       string
		images     bool
		want       string
		wantImages []string
		wantUnk    map[string]int
	}{
		{
			name: "paragraphs and headings",
			html: `<h1>Title</h1><p class="editor-paragraph-block">One</p><p>Two</p><h3>Sub</h3>`,
			want: "# Title\n\nOne\n\nTwo\n\n### Sub",
		},
		{
			name: "bold italic code",
			html: `<p><strong>b</strong> <em>i</em> <code>c</code></p>`,
			want: "**b** *i* `c`",
		},
		{
			name: "nested lists",
			html: `<ul><li><p>a</p><ul><li><p>b</p></li></ul></li><li><p>c</p></li></ul>`,
			want: "- a\n  - b\n- c",
		},
		{
			name: "ordered list",
			html: `<ol><li><p>one</p></li><li><p>two</p></li></ol>`,
			want: "1. one\n2. two",
		},
		{
			name: "task list",
			html: `<ul data-type="taskList" class="not-prose pl-2 space-y-2">` +
				`<li data-type="taskItem" data-checked="true"><label><input type="checkbox" checked><span></span></label><div><p>done</p></div></li>` +
				`<li data-type="taskItem" data-checked="false"><label><input type="checkbox"><span></span></label><div><p>todo</p></div></li></ul>`,
			want: "- [x] done\n- [ ] todo",
		},
		{
			name: "code block keeps text",
			html: "<pre><code class=\"language-go\">a  &lt;b&gt;\n</code></pre>",
			want: "```go\na  <b>\n```",
		},
		{
			name: "table",
			html: `<table><tbody><tr><th><p>H1</p></th><th><p>H2</p></th></tr><tr><td><p>a</p></td><td><p>b</p></td></tr></tbody></table>`,
			want: "| H1 | H2 |\n|---|---|\n| a | b |",
		},
		{
			name: "link",
			html: `<p><a href="https://example.com/x" target="_blank">site</a></p>`,
			want: "[site](https://example.com/x)",
		},
		{
			name: "blockquote",
			html: `<blockquote><p>quoted</p></blockquote>`,
			want: "> quoted",
		},
		{
			name: "hr",
			html: `<p>a</p><hr><p>b</p>`,
			want: "a\n\n---\n\nb",
		},
		{name: "empty", html: ``, want: ""},
		{name: "empty paragraph", html: `<p class="editor-paragraph-block"></p>`, want: ""},
		{
			name: "mention known user",
			html: `<p>hi <mention-component entity_name="user_mention" entity_identifier="u1"></mention-component>!</p>`,
			want: "hi @Ann Lee!",
		},
		{
			name: "mention unknown user",
			html: `<p><mention-component entity_name="user_mention" entity_identifier="zz"></mention-component></p>`,
			want: "@unknown user",
		},
		{
			name:    "mention of other entity",
			html:    `<p>see <mention-component entity_name="page" entity_identifier="p1"></mention-component></p>`,
			want:    `see \[mention]`,
			wantUnk: map[string]int{"mention-component:page": 1},
		},
		{
			name:    "mention of other entity keeps its children after the placeholder",
			html:    `<p><mention-component entity_name="module" entity_identifier="m1">Sprint 4</mention-component></p>`,
			want:    `\[mention]Sprint 4`,
			wantUnk: map[string]int{"mention-component:module": 1},
		},
		{
			name:    "self-closed mention of another entity swallows nothing",
			html:    `<p>x <mention-component entity_name="page"/> after <b>bold</b></p>`,
			want:    `x \[mention] after **bold**`,
			wantUnk: map[string]int{"mention-component:page": 1},
		},
		{
			name:    "mention without entity name",
			html:    `<p>x <mention-component></mention-component></p>`,
			want:    `x \[mention]`,
			wantUnk: map[string]int{"mention-component:unknown": 1},
		},
		{
			name: "3 by 3 table is still GFM",
			html: `<table><tr><th>a</th><th>b</th><th>c</th></tr><tr><td>1</td><td>2</td><td>3</td></tr><tr><td>4</td><td>5</td><td>6</td></tr></table>`,
			want: "| a | b | c |\n|---|---|---|\n| 1 | 2 | 3 |\n| 4 | 5 | 6 |",
		},
		{
			name: "mention name with markdown specials",
			html: `<p><mention-component entity_name="user_mention" entity_identifier="u2"></mention-component></p>`,
			want: `@A\*b_ \[x] | y`,
		},
		{
			name:       "image url with spaces and parentheses is encoded",
			html:       `<image-component src="a2"></image-component>`,
			images:     true,
			want:       "![image](/api/attachments/x/my%20file%20%281%29.png)",
			wantImages: []string{"a2"},
		},
		{
			name:       "consecutive self-closed images are all collected",
			html:       `<image-component src="a1"/><image-component src="b2"/>`,
			images:     true,
			want:       "![image](/api/attachments/x/a.png)\n\n[image not imported]",
			wantImages: []string{"a1", "b2"},
		},
		{
			name:       "unknown tag after a self-closed tag is counted",
			html:       `<image-component src="a1"/><foo-x>t</foo-x>`,
			want:       "[image not imported]\n\nt",
			wantImages: []string{"a1"},
			wantUnk:    map[string]int{"foo-x": 1},
		},
		{
			name: "unsafe link schemes are unwrapped",
			html: `<p><a href="javascript:alert(1)">click</a> <a href="java&#9;script:x">tab</a> <a href=" JaVaScRiPt:x">sp</a> <a href="data:text/html,x">d</a> ` +
				`<a href="mailto:a@example.com">m</a> <a href="/rel/x">r</a> <a href="https://e.com">h</a></p>`,
			want: "click tab sp d [m](mailto:a@example.com) [r](/rel/x) [h](https://e.com)",
		},
		{
			name: "colon inside a relative path is not a scheme",
			html: `<p><a href="docs/a:b">r</a> <a href="MAILTO:a@example.com">m</a> <a href="#top">t</a></p>`,
			want: "[r](docs/a:b) [m](mailto:a@example.com) [t](#top)",
		},
		{
			name: "unsafe image schemes become text",
			html: `<p><img src="javascript:x" alt="pic"> and <img src="data:image/png;base64,AAA"></p>`,
			want: `pic and \[image]`,
		},
		{
			name: "adjacent lists keep the separator",
			html: `<ul><li><p>a</p></li></ul><ol><li><p>b</p></li></ol>`,
			want: "- a\n\n<!--THE END-->\n\n1. b",
		},
		{
			name: "list then task list keeps the separator",
			html: `<ul><li><p>a</p></li></ul><ul data-type="taskList"><li data-type="taskItem" data-checked="true"><label><input type="checkbox"></label><div><p>t</p></div></li></ul>`,
			want: "- a\n\n<!--THE END-->\n\n- [x] t",
		},
		{
			name: "issue embed",
			html: `<issue-embed-component entity_identifier="i1"></issue-embed-component>`,
			want: "ALP-3",
		},
		{
			name: "issue embed unknown",
			html: `<issue-embed-component entity_identifier="zz"></issue-embed-component>`,
			want: "[work item not imported]",
		},
		{
			name:       "image with url",
			html:       `<image-component src="a1"></image-component>`,
			images:     true,
			want:       "![image](/api/attachments/x/a.png)",
			wantImages: []string{"a1"},
		},
		{
			name:       "image without download",
			html:       `<p><image-component src="a1"></image-component></p>`,
			want:       "[image not imported]",
			wantImages: []string{"a1"},
		},
		{
			name:       "images in order without repeats",
			html:       `<image-component src="b2"></image-component><image-component src="a1"></image-component><image-component src="b2"></image-component>`,
			want:       "[image not imported]\n\n[image not imported]\n\n[image not imported]",
			wantImages: []string{"b2", "a1"},
		},
		{
			name: "legacy img stays an image",
			html: `<p><img src="https://example.com/p.png" alt="p"></p>`,
			want: "![p](https://example.com/p.png)",
		},
		{
			name:    "unknown tag kept as text",
			html:    `<p><foo-widget>hi</foo-widget></p>`,
			want:    "hi",
			wantUnk: map[string]int{"foo-widget": 1},
		},
		{
			name:    "unknown tags are counted per element",
			html:    `<foo-widget>a</foo-widget><p><foo-widget>b</foo-widget><bar-thing>c</bar-thing></p>`,
			want:    "a\n\nbc",
			wantUnk: map[string]int{"foo-widget": 2, "bar-thing": 1},
		},
		{
			name: "wrappers and colour spans are not unknown",
			html: `<div data-block-type="callout-component"><p>note</p></div><p><span data-text-color="red">x</span></p>`,
			want: "note\n\nx",
		},
		{
			name: "nested task list",
			html: `<ul data-type="taskList"><li data-type="taskItem" data-checked="false"><label><input type="checkbox"><span></span></label><div><p>parent</p>` +
				`<ul data-type="taskList"><li data-type="taskItem" data-checked="true"><label><input type="checkbox" checked><span></span></label><div><p>child</p></div></li></ul></div></li></ul>`,
			want: "- [ ] parent\n  - [x] child",
		},
		{
			name: "code block keeps blank lines and indentation",
			html: "<pre><code>a\n\n\n  b &amp; c\n\td</code></pre>",
			want: "```\na\n\n\n  b & c\n\td\n```",
		},
		{
			name: "strikethrough",
			html: `<p><s>gone</s></p>`,
			want: "~~gone~~",
		},
		{
			name: "table with a hostile colspan",
			html: `<table><tr><td colspan="999999999">x</td><td>y</td></tr></table>`,
			want: "| x |" + strings.Repeat("  |", 9) + " y |\n" + strings.Repeat("|---", 11) + "|",
		},
		{
			name:       "self-closed image keeps the text after it",
			html:       `<p>before <image-component src="a1"/> after</p>`,
			want:       "before\n\n[image not imported]\n\nafter",
			wantImages: []string{"a1"},
		},
		{
			name: "image with a full url is not downloaded",
			html: `<image-component src="https://example.com/p.png"></image-component>`,
			want: "![image](https://example.com/p.png)",
		},
		{
			name: "entities in text",
			html: `<p>a &amp; b &lt;c&gt;</p>`,
			want: "a & b &lt;c&gt;",
		},
		{
			name: "malformed html",
			html: `<p>open <b>bold <i>x</p><ul><li>y`,
			want: "open **bold *x***\n\n- ***y***",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ToMarkdown(tc.html, testRefs(tc.images))
			if err != nil {
				t.Fatalf("ToMarkdown: %v", err)
			}
			if got.Markdown != tc.want {
				t.Errorf("markdown mismatch\n got: %q\nwant: %q", got.Markdown, tc.want)
			}
			if !reflect.DeepEqual(got.Images, tc.wantImages) {
				t.Errorf("images: got %v, want %v", got.Images, tc.wantImages)
			}
			wantUnk := tc.wantUnk
			if wantUnk == nil {
				wantUnk = map[string]int{}
			}
			gotUnk := got.Unknown
			if gotUnk == nil {
				gotUnk = map[string]int{}
			}
			if !reflect.DeepEqual(gotUnk, wantUnk) {
				t.Errorf("unknown: got %v, want %v", gotUnk, wantUnk)
			}
		})
	}
}

// convertWithin runs ToMarkdown and fails the test when it takes longer than
// limit, so a runaway conversion shows up as a failure and not as a hung run.
func convertWithin(t *testing.T, in string, limit time.Duration) Converted {
	t.Helper()
	type result struct {
		c   Converted
		err error
	}
	done := make(chan result, 1)
	start := time.Now()
	go func() {
		c, err := ToMarkdown(in, Refs{})
		done <- result{c, err}
	}()
	select {
	case r := <-done:
		if r.err != nil {
			t.Fatalf("ToMarkdown: %v", r.err)
		}
		if el := time.Since(start); el > limit {
			t.Fatalf("took %v, limit %v", el, limit)
		}
		return r.c
	case <-time.After(limit):
		t.Fatalf("no result within %v", limit)
	}
	return Converted{}
}

func TestToMarkdownNeverPanics(t *testing.T) {
	inputs := []string{
		"<", "<<<>>>", "<p", "</p></div>", "<table><tr><td>", "<ul><li><ul><li>",
		"<pre><code>", "<image-component>", "<mention-component entity_name=>",
		strings.Repeat("<div>", 5000), "\x00\xff<p>\xc3(</p>", "<a href=>x</a>",
		"<table><tr><td colspan=999999999>x</td></tr></table>",
		strings.Repeat("<ul><li>a", 400),
		strings.Repeat("<table><tr><td>", 300),
	}
	for _, in := range inputs {
		got := convertWithin(t, in, time.Second)
		if len(got.Markdown) > 1<<20 {
			t.Errorf("ToMarkdown(%.40q): output is %d bytes", in, len(got.Markdown))
		}
	}
}

func spanRows(rows int) string {
	return "<table>" + strings.Repeat(`<tr><td colspan="100" rowspan="100">x</td></tr>`, rows) + "</table>"
}

func TestToMarkdownSpanBudget(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
	}{
		{"4 KB", spanRows(100)},
		{"100 KB", spanRows(2200)},
		{"spans after a self-closed tag", `<image-component src="a1"/>` + spanRows(100)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := convertWithin(t, tc.in, time.Second)
			if len(got.Markdown) > 1<<20 {
				t.Errorf("output is %d bytes", len(got.Markdown))
			}
			if !strings.Contains(got.Markdown, "x") {
				t.Errorf("cell text lost: %.80q", got.Markdown)
			}
		})
	}
}

func TestToMarkdownFallbackIsPlainText(t *testing.T) {
	// More than 512 open elements (400 items nest 800) make the HTML parser give up.
	in := strings.Repeat("<ul><li>a", 400)
	got := convertWithin(t, in, time.Second)
	if strings.ContainsAny(got.Markdown, "<>") {
		t.Errorf("raw HTML in the output: %.80q", got.Markdown)
	}
	if !strings.Contains(got.Markdown, "a") {
		t.Errorf("text lost: %.80q", got.Markdown)
	}
}

func TestToMarkdownWideTableIsFlattened(t *testing.T) {
	// One wide row followed by many short ones: padding every row to the
	// widest one would multiply the output.
	in := `<table><tr>` + strings.Repeat(`<td colspan="10">a</td>`, 900) + `</tr>` +
		strings.Repeat(`<tr><td>y</td></tr>`, 4000) + `</table>`
	got := convertWithin(t, in, time.Second)
	if len(got.Markdown) > 1<<20 {
		t.Errorf("output is %d bytes", len(got.Markdown))
	}
	if strings.Contains(got.Markdown, "|---") {
		t.Errorf("rendered as a GFM table")
	}
	if n := strings.Count(got.Markdown, "y"); n != 4000 {
		t.Errorf("row text: got %d y, want 4000", n)
	}
}

func TestToMarkdownTooManyCellsBecomeLines(t *testing.T) {
	// 3 x 7000 = 21,000 grid cells, over the limit.
	in := "<table>" + strings.Repeat(`<tr><td>a|</td><td><b>b</b></td><td>c</td></tr>`, 7000) + "</table>"
	got := convertWithin(t, in, time.Second)
	lines := strings.Split(got.Markdown, "\n\n")
	if len(lines) != 7000 {
		t.Fatalf("got %d rows, want 7000", len(lines))
	}
	if lines[0] != "a| | b | c" {
		t.Errorf("row: %q", lines[0])
	}
}

func TestToMarkdownFallbackEscapesText(t *testing.T) {
	in := strings.Repeat("<ul><li>", 300) + "&lt;b&gt;x [a](javascript:x)"
	got := convertWithin(t, in, time.Second)
	if strings.ContainsAny(got.Markdown, "<>") {
		t.Errorf("markup in the output: %.80q", got.Markdown)
	}
	if strings.Contains(got.Markdown, "](javascript:") {
		t.Errorf("live link in the output: %.80q", got.Markdown)
	}
	if !strings.Contains(got.Markdown, "x") {
		t.Errorf("text lost: %.80q", got.Markdown)
	}
}

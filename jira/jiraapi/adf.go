package jiraapi

import (
	"bytes"
	"encoding/json"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// The text model of descriptions and comments (§7.4) is a strict subset of
// Markdown, chosen so that text survives a trip through ADF unchanged:
//
//	blocks      separated by one blank line
//	paragraph   lines joined by hard breaks
//	# heading   one line, 1 to 6 '#', one space
//	---         a rule, alone in its block
//	> quote     every line "> x" or ">"; holds paragraphs, lists and code
//	- item      bullet list; "N. item" ordered, numbered consecutively;
//	            continuation lines indented by the marker's width, nested
//	            lists after the item's paragraph
//	```lang     code block, opening a block and closed by a line "```"
//	            that ends one
//	inline      **strong** _em_ ~~strike~~ [link](url) `code`, nested in
//	            that order only (code with link alone, as ADF demands),
//	            @[Name](accountId) mention, :shortname: emoji, <url> card
//
// There is no escaping. A construct is recognised only where reading it back
// reproduces the source exactly; anything else is literal text. So for any
// text t:
//
//	ADFToText(TextToADF(t)) == NormalizeText(t), lossless
//	TextToADF(ADFToText(TextToADF(t))) == TextToADF(t), byte for byte
//
// which is what stops a sync from re-exporting its own writes forever.

type adfNode struct {
	Type    string         `json:"type"`
	Version int            `json:"version,omitempty"`
	Attrs   map[string]any `json:"attrs,omitempty"`
	Content []*adfNode     `json:"content,omitempty"`
	Text    string         `json:"text,omitempty"`
	Marks   []adfMark      `json:"marks,omitempty"`
}

type adfMark struct {
	Type  string         `json:"type"`
	Attrs map[string]any `json:"attrs,omitempty"`
}

// NormalizeText is the text TextToADF preserves, exactly:
//
//  1. every byte of invalid UTF-8 becomes U+FFFD, as encoding/json does;
//  2. CRLF and lone CR become LF;
//  3. spaces and tabs are trimmed from the end of every line;
//  4. blank lines are dropped at the start and end, and runs of them
//     outside code blocks collapse to one;
//  5. inside a code block, blank lines right after the opening fence and
//     right before the closing one are dropped.
//
// A code block opens with a line starting "```" that begins a block (first
// line, or after a blank line), and closes at the next line that is exactly
// "```", provided that line is followed by a blank line or the end; else
// the opening line is ordinary text. NormalizeText is idempotent.
func NormalizeText(t string) string {
	var out []string
	for i, b := range splitBlocks(t) {
		if i > 0 {
			out = append(out, "")
		}
		out = append(out, b.lines...)
	}
	return strings.Join(out, "\n")
}

type textBlock struct {
	lines []string
	fence bool
}

func splitBlocks(t string) []textBlock {
	lines := strings.Split(strings.ReplaceAll(strings.ReplaceAll(validUTF8(t), "\r\n", "\n"), "\r", "\n"), "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " \t")
	}
	var blocks []textBlock
	for i := 0; i < len(lines); {
		if lines[i] == "" {
			i++
			continue
		}
		if end := fenceEnd(lines, i); end > 0 {
			body := lines[i+1 : end]
			for len(body) > 0 && body[0] == "" {
				body = body[1:]
			}
			for len(body) > 0 && body[len(body)-1] == "" {
				body = body[:len(body)-1]
			}
			b := append([]string{lines[i]}, body...)
			blocks = append(blocks, textBlock{append(b, lines[end]), true})
			i = end + 1
			continue
		}
		j := i
		for j < len(lines) && lines[j] != "" {
			j++
		}
		blocks = append(blocks, textBlock{lines: lines[i:j]})
		i = j
	}
	return blocks
}

// fenceEnd is the index of the line closing a code block opened at i, or 0.
func fenceEnd(lines []string, i int) int {
	if !strings.HasPrefix(lines[i], "```") {
		return 0
	}
	for j := i + 1; j < len(lines); j++ {
		if lines[j] == "```" {
			if j+1 == len(lines) || lines[j+1] == "" {
				return j
			}
			return 0
		}
	}
	return 0
}

func validUTF8(s string) string {
	if utf8.ValidString(s) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); {
		r, n := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && n == 1 {
			b.WriteRune(utf8.RuneError)
		} else {
			b.WriteString(s[i : i+n])
		}
		i += n
	}
	return b.String()
}

// TextToADF converts text in the model above to an ADF document.
func TextToADF(t string) json.RawMessage {
	d := struct {
		Type    string     `json:"type"`
		Version int        `json:"version"`
		Content []*adfNode `json:"content"`
	}{"doc", 1, parseBlocks(t)}
	if d.Content == nil {
		d.Content = []*adfNode{}
	}
	b, err := marshal(d)
	if err != nil {
		panic(err) // only strings, ints and nodes: cannot fail
	}
	return b
}

// ADFToText renders an ADF document in the text model, normalised. lossless
// reports whether TextToADF of the result reproduces the document, up to
// what Jira itself changes (localId attributes, split text nodes, mark
// order, empty paragraphs); it is false for tables, panels, media, colours,
// underline and the like, whose descriptions a sync must not overwrite.
// null and an empty input are the empty text; invalid JSON is ("", false).
func ADFToText(raw json.RawMessage) (text string, lossless bool) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return "", true
	}
	var d adfNode
	if err := json.Unmarshal(raw, &d); err != nil {
		return "", false
	}
	prune(&d)
	text = NormalizeText(renderBlocks(d.Content))
	if d.Type != "doc" {
		return text, false
	}
	var back adfNode
	if err := json.Unmarshal(TextToADF(text), &back); err != nil {
		return text, false
	}
	a, _ := json.Marshal(canonList(d.Content))
	b, _ := json.Marshal(canonList(back.Content))
	return text, bytes.Equal(a, b)
}

// prune drops null nodes, which json leaves as nil pointers.
func prune(n *adfNode) {
	n.Content = slices.DeleteFunc(n.Content, func(c *adfNode) bool { return c == nil })
	for _, c := range n.Content {
		prune(c)
	}
}

// ---- text → ADF

func parseBlocks(t string) []*adfNode {
	var out []*adfNode
	for _, b := range splitBlocks(t) {
		if b.fence {
			out = append(out, codeBlock(b.lines))
		} else {
			out = append(out, parseBlock(b.lines))
		}
	}
	return out
}

func codeBlock(lines []string) *adfNode {
	n := &adfNode{Type: "codeBlock"}
	if lang := lines[0][3:]; lang != "" {
		n.Attrs = map[string]any{"language": lang}
	}
	if body := strings.Join(lines[1:len(lines)-1], "\n"); body != "" {
		n.Content = []*adfNode{{Type: "text", Text: body}}
	}
	return n
}

// parseBlock reads a block as structure when that renders back to it, and
// as a paragraph otherwise.
func parseBlock(lines []string) *adfNode {
	if n := structured(lines); n != nil && renderBlock(n) == strings.Join(lines, "\n") {
		return n
	}
	return paragraph(lines)
}

func structured(lines []string) *adfNode {
	if len(lines) == 1 {
		l := lines[0]
		if l == "---" {
			return &adfNode{Type: "rule"}
		}
		level := len(l) - len(strings.TrimLeft(l, "#"))
		if level >= 1 && level <= 6 && len(l) > level+1 && l[level] == ' ' {
			return &adfNode{Type: "heading", Attrs: map[string]any{"level": level}, Content: inline(l[level+1:])}
		}
	}
	if w, _, _ := listMarker(lines[0]); w > 0 {
		return parseList(lines)
	}
	inner := make([]string, len(lines))
	for i, l := range lines {
		switch {
		case l == ">":
		case strings.HasPrefix(l, "> "):
			inner[i] = l[2:]
		default:
			return nil
		}
	}
	kids := parseBlocks(strings.Join(inner, "\n"))
	for _, k := range kids {
		switch k.Type {
		case "paragraph", "bulletList", "orderedList", "codeBlock":
		default:
			return nil
		}
	}
	if len(kids) == 0 {
		return nil
	}
	return &adfNode{Type: "blockquote", Content: kids}
}

// listMarker is the width of the item marker opening l ("- " or "N. "),
// whether it is ordered, and its number; width 0 when there is none.
func listMarker(l string) (width int, ordered bool, n int) {
	if strings.HasPrefix(l, "- ") && len(l) > 2 {
		return 2, false, 0
	}
	d := 0
	for d < len(l) && d < 10 && l[d] >= '0' && l[d] <= '9' {
		d++
	}
	if d == 0 || d > 9 || !strings.HasPrefix(l[d:], ". ") || len(l) <= d+2 {
		return 0, false, 0
	}
	n, _ = strconv.Atoi(l[:d])
	if strconv.Itoa(n) != l[:d] {
		return 0, false, 0
	}
	return d + 2, true, n
}

func parseList(lines []string) *adfNode {
	_, ordered, next := listMarker(lines[0])
	list := &adfNode{Type: "bulletList"}
	if ordered {
		list.Type = "orderedList"
		if next != 1 {
			list.Attrs = map[string]any{"order": next}
		}
	}
	for i := 0; i < len(lines); {
		w, o, n := listMarker(lines[i])
		if w == 0 || o != ordered || (ordered && n != next) {
			return nil
		}
		body := []string{lines[i][w:]}
		pad := strings.Repeat(" ", w)
		for i++; i < len(lines); i++ {
			if w2, _, _ := listMarker(lines[i]); w2 > 0 {
				break
			}
			if !strings.HasPrefix(lines[i], pad) {
				return nil
			}
			body = append(body, lines[i][w:])
		}
		item := &adfNode{Type: "listItem"}
		k := 0
		for k < len(body) {
			if w, _, _ := listMarker(body[k]); w > 0 {
				break
			}
			k++
		}
		if k > 0 {
			item.Content = append(item.Content, paragraph(body[:k]))
		}
		if k < len(body) {
			sub := parseList(body[k:])
			if sub == nil {
				return nil
			}
			item.Content = append(item.Content, sub)
		}
		list.Content = append(list.Content, item)
		next++
	}
	return list
}

func paragraph(lines []string) *adfNode {
	p := &adfNode{Type: "paragraph"}
	for i, l := range lines {
		if i > 0 {
			p.Content = append(p.Content, &adfNode{Type: "hardBreak"})
		}
		p.Content = append(p.Content, inline(l)...)
	}
	return p
}

// Mark precedence, outermost first: a span holds only later marks.
const (
	precStrong = iota
	precEm
	precStrike
	precLink
	precCode
)

var markPrec = map[string]int{"strong": precStrong, "em": precEm, "strike": precStrike, "link": precLink, "code": precCode}

// inline parses one line, falling back to literal text when the parse does
// not render back to the line.
func inline(l string) []*adfNode {
	ns := mergeText((&inlineParser{l}).run(0, len(l), nil, precStrong))
	if renderInline(ns) != l {
		return []*adfNode{{Type: "text", Text: l}}
	}
	return ns
}

type inlineParser struct{ s string }

func (p *inlineParser) run(lo, hi int, marks []adfMark, min int) []*adfNode {
	var out []*adfNode
	var buf strings.Builder
	flush := func() {
		if buf.Len() > 0 {
			out = append(out, &adfNode{Type: "text", Text: buf.String(), Marks: marks})
			buf.Reset()
		}
	}
	for i := lo; i < hi; {
		if end, ns := p.span(i, hi, marks, min); ns != nil {
			flush()
			out = append(out, ns...)
			i = end
			continue
		}
		_, n := utf8.DecodeRuneInString(p.s[i:hi])
		buf.WriteString(p.s[i : i+n])
		i += n
	}
	flush()
	return out
}

func (p *inlineParser) span(i, hi int, marks []adfMark, min int) (int, []*adfNode) {
	s := p.s[:hi]
	with := func(m adfMark) []adfMark { return append(slices.Clip(marks), m) }
	top := len(marks) == 0
	if top && strings.HasPrefix(s[i:], "@[") {
		if name, id, end := bracketed(s, i+1); name != "" {
			return end, []*adfNode{{Type: "mention", Attrs: map[string]any{"id": id, "text": "@" + name}}}
		}
	}
	if top && s[i] == ':' && !wordBefore(p.s, i, ':') {
		if j := strings.IndexByte(s[i+1:], ':'); j > 0 && shortName(s[i+1:i+1+j]) && !wordAfter(p.s, i+j+2, ':') {
			return i + j + 2, []*adfNode{{Type: "emoji", Attrs: map[string]any{"shortName": s[i : i+j+2]}}}
		}
	}
	if top && s[i] == '<' {
		if j := strings.IndexByte(s[i+1:], '>'); j > 0 {
			if u := s[i+1 : i+1+j]; strings.Contains(u, "://") && !strings.ContainsAny(u, "<") && !hasSpace(u) {
				return i + j + 2, []*adfNode{{Type: "inlineCard", Attrs: map[string]any{"url": u}}}
			}
		}
	}
	if s[i] == '`' && !slices.ContainsFunc(marks, func(m adfMark) bool { return m.Type != "link" }) {
		if j := strings.IndexByte(s[i+1:], '`'); j > 0 {
			return i + j + 2, []*adfNode{{Type: "text", Text: s[i+1 : i+1+j], Marks: with(adfMark{Type: "code"})}}
		}
	}
	for _, d := range []struct {
		delim, typ string
		prec       int
	}{{"**", "strong", precStrong}, {"~~", "strike", precStrike}} {
		if min > d.prec || !strings.HasPrefix(s[i:], d.delim) || i+len(d.delim) >= hi || isSpaceByte(s[i+len(d.delim)]) {
			continue
		}
		lo := i + len(d.delim)
		for j := lo + 1; j+len(d.delim) <= hi; j++ {
			if strings.HasPrefix(s[j:], d.delim) && !isSpaceByte(s[j-1]) {
				return j + len(d.delim), p.run(lo, j, with(adfMark{Type: d.typ}), d.prec+1)
			}
		}
	}
	// _em_ only between non-word characters, so snake_case stays text.
	if min <= precEm && s[i] == '_' && !wordBefore(p.s, i, '_') && i+1 < hi && !isSpaceByte(s[i+1]) && s[i+1] != '_' {
		for j := i + 2; j < hi; j++ {
			if s[j] == '_' && !isSpaceByte(s[j-1]) && s[j-1] != '_' && !wordAfter(p.s, j+1, '_') {
				return j + 1, p.run(i+1, j, with(adfMark{Type: "em"}), precEm+1)
			}
		}
	}
	if min <= precLink && s[i] == '[' {
		if text, href, end := bracketed(s, i); text != "" {
			return end, p.run(i+1, i+1+len(text), with(adfMark{Type: "link", Attrs: map[string]any{"href": href}}), precLink+1)
		}
	}
	return 0, nil
}

// bracketed reads "[text](target)" at i; text is "" when there is none.
func bracketed(s string, i int) (text, target string, end int) {
	k := strings.Index(s[i+1:], "](")
	if k <= 0 {
		return "", "", 0
	}
	k += i + 1
	m := strings.IndexByte(s[k+2:], ')')
	if m <= 0 || hasSpace(s[k+2:k+2+m]) {
		return "", "", 0
	}
	return s[i+1 : k], s[k+2 : k+2+m], k + 3 + m
}

func shortName(s string) bool {
	if s[0] < 'a' || s[0] > 'z' {
		return false
	}
	for _, c := range []byte(s) {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_' || c == '+' || c == '-') {
			return false
		}
	}
	return true
}

func isWord(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }

func wordBefore(s string, i int, also byte) bool {
	if i == 0 {
		return false
	}
	r, _ := utf8.DecodeLastRuneInString(s[:i])
	return isWord(r) || s[i-1] == also
}

func wordAfter(s string, i int, also byte) bool {
	if i >= len(s) {
		return false
	}
	r, _ := utf8.DecodeRuneInString(s[i:])
	return isWord(r) || s[i] == also
}

func isSpaceByte(c byte) bool { return c == ' ' || c == '\t' }

func hasSpace(s string) bool { return strings.IndexFunc(s, unicode.IsSpace) >= 0 }

func mergeText(ns []*adfNode) []*adfNode {
	var out []*adfNode
	for _, n := range ns {
		if last := len(out) - 1; last >= 0 && n.Type == "text" && out[last].Type == "text" && reflect.DeepEqual(n.Marks, out[last].Marks) {
			out[last] = &adfNode{Type: "text", Text: out[last].Text + n.Text, Marks: n.Marks}
			continue
		}
		out = append(out, n)
	}
	return out
}

// ---- ADF → text

func renderBlocks(ns []*adfNode) string {
	var parts []string
	for _, n := range ns {
		if s := renderBlock(n); s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, "\n\n")
}

func renderBlock(n *adfNode) string {
	switch n.Type {
	case "paragraph":
		return renderInline(n.Content)
	case "heading":
		level := min(max(intAttr(n.Attrs, "level", 1), 1), 6)
		return strings.Repeat("#", level) + " " + renderInline(n.Content)
	case "rule":
		return "---"
	case "codeBlock":
		lang, _ := n.Attrs["language"].(string)
		var body strings.Builder
		for _, c := range n.Content {
			body.WriteString(c.Text)
		}
		if body.Len() == 0 {
			return "```" + lang + "\n```"
		}
		return "```" + lang + "\n" + body.String() + "\n```"
	case "blockquote":
		lines := strings.Split(renderBlocks(n.Content), "\n")
		for i, l := range lines {
			if l == "" {
				lines[i] = ">"
			} else {
				lines[i] = "> " + l
			}
		}
		return strings.Join(lines, "\n")
	case "bulletList", "orderedList":
		num := intAttr(n.Attrs, "order", 1)
		var items []string
		for _, it := range n.Content {
			marker := "- "
			if n.Type == "orderedList" {
				marker = strconv.Itoa(num) + ". "
				num++
			}
			var kids []string
			for _, c := range it.Content {
				kids = append(kids, renderBlock(c))
			}
			lines := strings.Split(strings.Join(kids, "\n"), "\n")
			for i, l := range lines {
				if i == 0 {
					lines[i] = marker + l
				} else if l != "" {
					lines[i] = strings.Repeat(" ", len(marker)) + l
				}
			}
			items = append(items, strings.Join(lines, "\n"))
		}
		return strings.Join(items, "\n")
	case "table":
		var rows []string
		for _, r := range n.Content {
			var cells []string
			for _, c := range r.Content {
				cells = append(cells, strings.ReplaceAll(renderBlocks(c.Content), "\n", " "))
			}
			rows = append(rows, "| "+strings.Join(cells, " | ")+" |")
		}
		return strings.Join(rows, "\n")
	case "mediaSingle", "mediaGroup", "media":
		return "[attachment]"
	}
	if isInline(n.Type) {
		return renderInline([]*adfNode{n})
	}
	return renderBlocks(n.Content)
}

func isInline(t string) bool {
	switch t {
	case "text", "hardBreak", "mention", "emoji", "inlineCard", "date", "status", "mediaInline":
		return true
	}
	return false
}

// renderInline writes marks as delimiters, nested by precedence rather than
// by their order in the document, so Jira reordering marks changes nothing.
func renderInline(ns []*adfNode) string {
	var b strings.Builder
	var open []adfMark
	closeTo := func(k int) {
		for len(open) > k {
			m := open[len(open)-1]
			if m.Type == "link" {
				b.WriteString("](" + strAttr(m.Attrs, "href") + ")")
			} else {
				b.WriteString(delims[m.Type])
			}
			open = open[:len(open)-1]
		}
	}
	for _, n := range ns {
		var want []adfMark
		if n.Type == "text" {
			want = textMarks(n.Marks)
		}
		k := 0
		for k < len(open) && k < len(want) && sameMark(open[k], want[k]) {
			k++
		}
		closeTo(k)
		for _, m := range want[k:] {
			if m.Type == "link" {
				b.WriteString("[")
			} else {
				b.WriteString(delims[m.Type])
			}
			open = append(open, m)
		}
		switch n.Type {
		case "text":
			b.WriteString(n.Text)
		case "hardBreak":
			b.WriteString("\n")
		case "mention":
			b.WriteString("@[" + strings.TrimPrefix(strAttr(n.Attrs, "text"), "@") + "](" + strAttr(n.Attrs, "id") + ")")
		case "emoji":
			if s := strAttr(n.Attrs, "shortName"); s != "" {
				b.WriteString(s)
			} else {
				b.WriteString(strAttr(n.Attrs, "text"))
			}
		case "inlineCard":
			b.WriteString("<" + strAttr(n.Attrs, "url") + ">")
		case "status":
			b.WriteString("[" + strAttr(n.Attrs, "text") + "]")
		case "date":
			if ms, err := strconv.ParseInt(strAttr(n.Attrs, "timestamp"), 10, 64); err == nil {
				b.WriteString(time.UnixMilli(ms).UTC().Format(DateLayout))
			}
		default:
			b.WriteString(renderInline(n.Content))
		}
	}
	closeTo(0)
	return b.String()
}

var delims = map[string]string{"strong": "**", "em": "_", "strike": "~~", "code": "`"}

// textMarks keeps the marks the text model writes, once each, in precedence
// order.
func textMarks(ms []adfMark) []adfMark {
	var out []adfMark
	for _, m := range ms {
		if _, ok := markPrec[m.Type]; ok && !slices.ContainsFunc(out, func(o adfMark) bool { return o.Type == m.Type }) {
			out = append(out, m)
		}
	}
	slices.SortStableFunc(out, func(a, b adfMark) int { return markPrec[a.Type] - markPrec[b.Type] })
	return out
}

func sameMark(a, b adfMark) bool {
	return a.Type == b.Type && strAttr(a.Attrs, "href") == strAttr(b.Attrs, "href")
}

func strAttr(a map[string]any, k string) string {
	s, _ := a[k].(string)
	return s
}

func intAttr(a map[string]any, k string, def int) int {
	switch v := a[k].(type) {
	case float64:
		return int(v)
	case int:
		return v
	}
	return def
}

// ---- comparison up to Jira's normalisation

// keptAttrs are the attributes that matter per node type; other types keep
// all of theirs but localId.
var keptAttrs = map[string][]string{
	"heading": {"level"}, "codeBlock": {"language"}, "orderedList": {"order"},
	"mention": {"id", "text"}, "emoji": {"shortName"}, "inlineCard": {"url"},
	"paragraph": nil, "bulletList": nil, "listItem": nil, "blockquote": nil,
	"rule": nil, "text": nil, "hardBreak": nil,
}

func canonList(ns []*adfNode) []*adfNode {
	var out []*adfNode
	for _, n := range ns {
		c := canonNode(n)
		if c == nil {
			continue
		}
		if last := len(out) - 1; last >= 0 && c.Type == "text" && out[last].Type == "text" && reflect.DeepEqual(c.Marks, out[last].Marks) {
			out[last].Text += c.Text
			continue
		}
		out = append(out, c)
	}
	return out
}

func canonNode(n *adfNode) *adfNode {
	c := &adfNode{Type: n.Type, Text: n.Text, Content: canonList(n.Content)}
	if (n.Type == "paragraph" && len(c.Content) == 0 && len(n.Marks) == 0) || (n.Type == "text" && n.Text == "") {
		return nil
	}
	c.Attrs = canonAttrs(n.Type, n.Attrs)
	for _, m := range n.Marks {
		_, known := markPrec[m.Type]
		c.Marks = append(c.Marks, adfMark{Type: m.Type, Attrs: filterAttrs(m.Attrs, []string{"href", "title"}, !known)})
	}
	slices.SortFunc(c.Marks, func(a, b adfMark) int { return strings.Compare(a.Type, b.Type) })
	return c
}

func canonAttrs(typ string, a map[string]any) map[string]any {
	keep, known := keptAttrs[typ]
	out := filterAttrs(a, keep, !known)
	if lang, ok := out["language"]; ok && lang == "" {
		delete(out, "language")
	}
	if typ == "orderedList" && intAttr(out, "order", 1) == 1 {
		delete(out, "order")
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// filterAttrs keeps the keys in keep, or with all every key but localId.
func filterAttrs(a map[string]any, keep []string, all bool) map[string]any {
	out := map[string]any{}
	for k, v := range a {
		if (all && k != "localId") || slices.Contains(keep, k) {
			out[k] = v
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

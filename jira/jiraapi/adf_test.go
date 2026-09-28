package jiraapi

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

var roundTripSeeds = []string{
	"",
	"hello",
	"line one\nline two\n\nsecond paragraph",
	"trailing   \r\nspaces\t\r\n\r\n\r\n\r\nand CRLF\n\n\n",
	"# Title\n\n## Sub\n\n####### seven is text\n\n#nospace",
	"# heading\nwith a second line is a paragraph",
	"---\n\n- one\n- two\n  continued\n  - nested\n  - nested 2\n- three",
	"3. three\n4. four\n\n1. a\n3. not consecutive",
	"07. leading zero\n\n1.no space\n\n- \n-x",
	"> quoted\n>\n> - item\n\n> # heading in quote is text\n\n>no space",
	"```go\nfunc main() {\n\n\n\tfmt.Println(\"x\")   \n}\n```\n\nafter",
	"```\n\n\n```\n\n```unclosed\ncode",
	"```\na\n```\nnot followed by blank",
	"**bold** _em_ ~~strike~~ `code` [link](https://example.com) [`code link`](http://x)",
	"**bold _em ~~strike~~_** and **[bold link](u)** and _**wrong order**_",
	"snake_case_name __init__ a*b*c ** spaced ** _ x _ `` empty",
	"@[Mia Krystof](5b10a2844c20165700ede21g) :smile: <https://example.com/x> 10:30:45 a<b>c",
	"**a****b** _x__y_ [a](b c) [](x) @[](x)",
	"\xff\xfe invalid \xc3 utf8\x00nul",
	"- a\n1. b",
	"- - deep\n    - deeper",
	"> ```\n> code\n> ```",
	"***x***",
	"[a]b](u) (x) [unclosed](",
}

func checkRoundTrip(t *testing.T, s string) {
	t.Helper()
	norm := NormalizeText(s)
	if again := NormalizeText(norm); again != norm {
		t.Fatalf("NormalizeText not idempotent on %q:\n%q\n%q", s, norm, again)
	}
	adf := TextToADF(s)
	if !json.Valid(adf) {
		t.Fatalf("TextToADF(%q) is not JSON: %s", s, adf)
	}
	text, lossless := ADFToText(adf)
	if text != norm || !lossless {
		t.Fatalf("ADFToText(TextToADF(%q)) = %q, %v; want %q, true\nadf: %s", s, text, lossless, norm, adf)
	}
	if again := TextToADF(text); !bytes.Equal(again, adf) {
		t.Fatalf("TextToADF not idempotent on %q:\n%s\n%s", s, adf, again)
	}
}

func TestADFRoundTrip(t *testing.T) {
	for _, s := range roundTripSeeds {
		checkRoundTrip(t, s)
	}
}

func FuzzADFRoundTrip(f *testing.F) {
	for _, s := range roundTripSeeds {
		f.Add(s)
	}
	f.Fuzz(checkRoundTrip)
}

// Whatever Jira sends, ADFToText yields normalised text, and claims lossless
// only for what TextToADF gives back.
func FuzzADFToText(f *testing.F) {
	f.Add(`{"type":"doc","version":1,"content":[{"type":"paragraph","content":[{"type":"text","text":"a","marks":[{"type":"strong"}]}]}]}`)
	f.Add(`{"type":"doc","content":[null,{"type":"heading","attrs":{"level":"x"},"content":[null]},{"type":"orderedList","attrs":{"order":-3}}]}`)
	f.Add(`{"type":"doc","content":[{"type":"codeBlock","content":[{"type":"text","text":"x\n"}]},{"type":"blockquote"}]}`)
	f.Fuzz(func(t *testing.T, s string) {
		text, lossless := ADFToText(json.RawMessage(s))
		if NormalizeText(text) != text {
			t.Fatalf("not normalised: %q", text)
		}
		if lossless {
			if back, ok := ADFToText(TextToADF(text)); back != text || !ok {
				t.Fatalf("lossless %q came back as %q, %v", text, back, ok)
			}
		}
	})
}

func TestNormalizeText(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"\n\n a \t\r\n\r\n\r\nb\r", " a\n\nb"},
		{"x\ry", "x\ny"},
		{"```\n\n  code  \n\n\n  more\n\n```\n\n\n", "```\n  code\n\n\n  more\n```"},
		{"a\n```\n\n\nb\n```", "a\n```\n\nb\n```"}, // not at a block start: no code block
		{"\xffa", "\uFFFDa"},
	} {
		if got := NormalizeText(c.in); got != c.want {
			t.Errorf("NormalizeText(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestTextToADF(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"", `{"type":"doc","version":1,"content":[]}`},
		{"a\nb", `{"type":"doc","version":1,"content":[{"type":"paragraph","content":[{"type":"text","text":"a"},{"type":"hardBreak"},{"type":"text","text":"b"}]}]}`},
		{"## H", `{"type":"doc","version":1,"content":[{"type":"heading","attrs":{"level":2},"content":[{"type":"text","text":"H"}]}]}`},
		{"x **b** [`c`](u)", `{"type":"doc","version":1,"content":[{"type":"paragraph","content":[{"type":"text","text":"x "},{"type":"text","text":"b","marks":[{"type":"strong"}]},{"type":"text","text":" "},{"type":"text","text":"c","marks":[{"type":"link","attrs":{"href":"u"}},{"type":"code"}]}]}]}`},
		{"2. a\n3. b", `{"type":"doc","version":1,"content":[{"type":"orderedList","attrs":{"order":2},"content":[{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"a"}]}]},{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"b"}]}]}]}]}`},
		{"```go\nx\n```", `{"type":"doc","version":1,"content":[{"type":"codeBlock","attrs":{"language":"go"},"content":[{"type":"text","text":"x"}]}]}`},
		{"> q", `{"type":"doc","version":1,"content":[{"type":"blockquote","content":[{"type":"paragraph","content":[{"type":"text","text":"q"}]}]}]}`},
		{"@[Mia](acc) :tada: <https://x.io>", `{"type":"doc","version":1,"content":[{"type":"paragraph","content":[{"type":"mention","attrs":{"id":"acc","text":"@Mia"}},{"type":"text","text":" "},{"type":"emoji","attrs":{"shortName":":tada:"}},{"type":"text","text":" "},{"type":"inlineCard","attrs":{"url":"https://x.io"}}]}]}`},
		{"snake_case **x", `{"type":"doc","version":1,"content":[{"type":"paragraph","content":[{"type":"text","text":"snake_case **x"}]}]}`},
	} {
		if got := string(TextToADF(c.in)); got != c.want {
			t.Errorf("TextToADF(%q)\n got %s\nwant %s", c.in, got, c.want)
		}
	}
}

// Documents as Jira returns them: attrs it adds, marks in another order,
// text split differently, empty paragraphs.
func TestADFToText(t *testing.T) {
	for _, c := range []struct {
		name, adf, want string
		lossless        bool
	}{
		{"null", `null`, "", true},
		{"empty", `{"type":"doc","version":1,"content":[]}`, "", true},
		{"spec example", `{"type":"doc","version":1,"content":[{"type":"paragraph","content":[
			{"type":"text","text":"Hello "},{"type":"text","text":"bold","marks":[{"type":"strong"}]},
			{"type":"text","text":" and "},{"type":"text","text":"italic","marks":[{"type":"em"}]},{"type":"hardBreak"},
			{"type":"text","text":"x := 1","marks":[{"type":"code"}]},{"type":"text","text":" see ","marks":[]},
			{"type":"text","text":"docs","marks":[{"type":"link","attrs":{"href":"https://example.com"}}]}]}]}`,
			"Hello **bold** and _italic_\n`x := 1` see [docs](https://example.com)", true},
		{"jira normalisation", `{"version":1,"type":"doc","content":[
			{"type":"paragraph","attrs":{"localId":"a1"},"content":[{"type":"text","text":"a"},{"type":"text","text":"b","marks":[]}]},
			{"type":"paragraph","content":[]},
			{"type":"paragraph","content":[{"type":"text","text":"x","marks":[{"type":"em"},{"type":"strong"}]}]},
			{"type":"orderedList","attrs":{"order":1,"localId":"z"},"content":[{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"one"}]}]}]},
			{"type":"paragraph","content":[{"type":"mention","attrs":{"id":"acc","text":"@Mia","accessLevel":""}},
				{"type":"text","text":" "},{"type":"emoji","attrs":{"shortName":":smile:","id":"1f604","text":"😄"}}]}]}`,
			"ab\n\n**_x_**\n\n1. one\n\n@[Mia](acc) :smile:", true},
		{"block-level mention", `{"type":"doc","version":1,"content":[{"type":"mention","attrs":{"id":"acc","text":"@Mia"}}]}`,
			"@[Mia](acc)", false},
		{"nested list and code", `{"type":"doc","version":1,"content":[
			{"type":"bulletList","content":[{"type":"listItem","content":[
				{"type":"paragraph","content":[{"type":"text","text":"two"}]},
				{"type":"bulletList","content":[{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"nested"}]}]}]}]}]},
			{"type":"codeBlock","attrs":{"language":"go"},"content":[{"type":"text","text":"func main() {}\nfmt.Println()"}]},
			{"type":"rule"},
			{"type":"heading","attrs":{"level":3},"content":[{"type":"text","text":"End"}]}]}`,
			"- two\n  - nested\n\n```go\nfunc main() {}\nfmt.Println()\n```\n\n---\n\n### End", true},
		{"table", `{"type":"doc","version":1,"content":[{"type":"table","content":[{"type":"tableRow","content":[
			{"type":"tableHeader","content":[{"type":"paragraph","content":[{"type":"text","text":"a"}]}]},
			{"type":"tableCell","content":[{"type":"paragraph","content":[{"type":"text","text":"b"}]}]}]}]}]}`,
			"| a | b |", false},
		{"panel", `{"type":"doc","version":1,"content":[{"type":"panel","attrs":{"panelType":"info"},"content":[{"type":"paragraph","content":[{"type":"text","text":"note"}]}]}]}`,
			"note", false},
		{"colour", `{"type":"doc","version":1,"content":[{"type":"paragraph","content":[{"type":"text","text":"red","marks":[{"type":"textColor","attrs":{"color":"#ff0000"}}]}]}]}`,
			"red", false},
		{"underline", `{"type":"doc","version":1,"content":[{"type":"paragraph","content":[{"type":"text","text":"u","marks":[{"type":"underline"}]}]}]}`,
			"u", false},
		{"media", `{"type":"doc","version":1,"content":[{"type":"mediaSingle","content":[{"type":"media","attrs":{"id":"x","type":"file"}}]}]}`,
			"[attachment]", false},
		{"link title", `{"type":"doc","version":1,"content":[{"type":"paragraph","content":[{"type":"text","text":"d","marks":[{"type":"link","attrs":{"href":"u","title":"T"}}]}]}]}`,
			"[d](u)", false},
		{"literal asterisks", `{"type":"doc","version":1,"content":[{"type":"paragraph","content":[{"type":"text","text":"**x**"}]}]}`,
			"**x**", false},
		{"not json", `{"type":`, "", false},
	} {
		got, lossless := ADFToText(json.RawMessage(c.adf))
		if got != c.want || lossless != c.lossless {
			t.Errorf("%s: ADFToText = %q, %v; want %q, %v", c.name, got, lossless, c.want, c.lossless)
		}
	}
}

func TestADFCodeOnlyWithLink(t *testing.T) {
	// ADF allows code alongside link only; inside strong it stays literal.
	adf := string(TextToADF("**a `b` c**"))
	if strings.Contains(adf, `"code"`) {
		t.Fatalf("code mark combined with strong: %s", adf)
	}
}

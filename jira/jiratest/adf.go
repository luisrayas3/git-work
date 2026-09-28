package jiratest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// blockNodes get a localId when the fake normalises ADF, as Jira adds
// attrs to what it stores (jira-api-vetting.md §4.12).
var blockNodes = map[string]bool{
	"paragraph": true, "heading": true, "blockquote": true, "bulletList": true,
	"orderedList": true, "listItem": true, "codeBlock": true, "panel": true,
	"table": true, "tableRow": true, "tableCell": true, "tableHeader": true,
}

// adfValue checks that raw is an Atlassian Document and returns what Jira
// would store, or the field error: a JSON string is the documented
// "Operation value must be an Atlassian Document" (R17), any other
// malformed value INVALID_INPUT (a guess).
func (s *Server) adfValue(raw json.RawMessage) (json.RawMessage, string) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || raw[0] != '{' {
		return nil, msgADF
	}
	var doc map[string]any
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if err := d.Decode(&doc); err != nil {
		return nil, "INVALID_INPUT"
	}
	if doc["type"] != "doc" || fmt.Sprint(doc["version"]) != "1" {
		return nil, "INVALID_INPUT"
	}
	if _, ok := doc["content"].([]any); !ok {
		return nil, "INVALID_INPUT"
	}
	if !validNode(doc) {
		return nil, "INVALID_INPUT"
	}
	if s.cfg.adfLocalIDs {
		s.normalise(doc)
	}
	b, _ := json.Marshal(doc)
	return b, ""
}

// sameADF compares two stored documents without the localIds the fake
// added, so re-sending an unchanged document changes nothing.
func sameADF(a, b json.RawMessage) bool {
	return canonADF(a) == canonADF(b)
}

func canonADF(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var doc any
	if json.Unmarshal(raw, &doc) != nil {
		return string(raw)
	}
	var strip func(v any)
	strip = func(v any) {
		m, ok := v.(map[string]any)
		if !ok {
			return
		}
		if attrs, ok := m["attrs"].(map[string]any); ok {
			delete(attrs, "localId")
			if len(attrs) == 0 {
				delete(m, "attrs")
			}
		}
		list, _ := m["content"].([]any)
		for _, c := range list {
			strip(c)
		}
	}
	strip(doc)
	b, _ := json.Marshal(doc)
	return string(b)
}

func validNode(n map[string]any) bool {
	typ, ok := n["type"].(string)
	if !ok || typ == "" {
		return false
	}
	if typ == "text" {
		t, ok := n["text"].(string)
		return ok && t != ""
	}
	content, has := n["content"]
	if !has {
		return true
	}
	list, ok := content.([]any)
	if !ok {
		return false
	}
	for _, c := range list {
		m, ok := c.(map[string]any)
		if !ok || !validNode(m) {
			return false
		}
	}
	return true
}

// normalise merges adjacent text nodes with the same marks, drops empty
// marks, and gives block nodes a localId.
func (s *Server) normalise(n map[string]any) {
	if typ, _ := n["type"].(string); blockNodes[typ] {
		attrs, _ := n["attrs"].(map[string]any)
		if attrs == nil {
			attrs = map[string]any{}
		}
		if _, ok := attrs["localId"]; !ok {
			attrs["localId"] = fmt.Sprintf("%08x-0000-4000-8000-%012x", s.next.localID, s.next.localID)
			s.next.localID++
		}
		n["attrs"] = attrs
	}
	if m, ok := n["marks"].([]any); ok && len(m) == 0 {
		delete(n, "marks")
	}
	list, ok := n["content"].([]any)
	if !ok {
		return
	}
	out := []any{} // an empty content stays [], never null
	for _, c := range list {
		m := c.(map[string]any)
		s.normalise(m)
		if len(out) > 0 {
			prev := out[len(out)-1].(map[string]any)
			if prev["type"] == "text" && m["type"] == "text" && sameMarks(prev, m) {
				prev["text"] = prev["text"].(string) + m["text"].(string)
				continue
			}
		}
		out = append(out, m)
	}
	n["content"] = out
}

func sameMarks(a, b map[string]any) bool {
	x, _ := json.Marshal(a["marks"])
	y, _ := json.Marshal(b["marks"])
	return bytes.Equal(x, y)
}

// adfText is the plain text of a document, paragraphs on their own lines:
// what the changelog's fromString/toString carry for a description.
func adfText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var doc map[string]any
	if json.Unmarshal(raw, &doc) != nil {
		return ""
	}
	var b strings.Builder
	var walk func(n map[string]any)
	walk = func(n map[string]any) {
		switch n["type"] {
		case "text":
			b.WriteString(n["text"].(string))
		case "hardBreak":
			b.WriteString("\n")
		}
		list, _ := n["content"].([]any)
		for _, c := range list {
			if m, ok := c.(map[string]any); ok {
				walk(m)
			}
		}
		if typ, _ := n["type"].(string); blockNodes[typ] && typ != "listItem" {
			b.WriteString("\n")
		}
	}
	walk(doc)
	return strings.TrimRight(b.String(), "\n")
}

// TextADF is a document of one paragraph per line of text, the shape a
// client sends for plain text.
func TextADF(text string) json.RawMessage {
	var content []any
	for _, line := range strings.Split(text, "\n") {
		p := map[string]any{"type": "paragraph"}
		if line != "" {
			p["content"] = []any{map[string]any{"type": "text", "text": line}}
		} else {
			p["content"] = []any{}
		}
		content = append(content, p)
	}
	b, _ := json.Marshal(map[string]any{"type": "doc", "version": 1, "content": content})
	return b
}

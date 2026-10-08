package host

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/git-bug/git-bug/cache"
	"github.com/git-bug/git-bug/schema"
)

// quickstartGuide is the half of `git work quickstart` that is the same
// everywhere: the model, the command line and what each one is for.
//
// It is a file rather than a string literal so that it is edited as what it
// is, markdown, and it lives beside the code it describes so that a change to
// the command line and a change to the guide are one commit.
//
//go:embed quickstart.md
var quickstartGuide string

// QuickstartGuide is the static guide alone, without this repository's schema.
//
// It is exported for the test that checks every command the guide names is a
// command the binary has: a guide that drifts is worse than no guide, and the
// only way it stays true is for the tree to be asked.
func QuickstartGuide() string {
	return quickstartGuide
}

// Quickstart is `git work quickstart` and `work.quickstart()`:
// the guide, then the types and fields this repository actually has,
// then the flows it is meant to be looked at through.
//
// Both halves, in one call, because the point of the verb is that an agent
// reads it once and can then write a `git work issue new` document that the
// schema check accepts. The guide alone cannot do that — it does not know
// what a type is called here — and the schema alone does not say what to do
// with it.
//
// It is markdown on stdout rather than JSON, the one reader that is not:
// the output is prose for a reader, not data for a program, and a program
// that wants the schema as data has `git work schema --format json`.
func Quickstart(repo *cache.RepoCache) (string, error) {
	s, err := repo.LoadSchema()
	if err != nil {
		return "", err
	}

	var b strings.Builder
	b.WriteString(strings.TrimRight(quickstartGuide, "\n"))
	b.WriteString("\n\n")
	writeQuickstartSchema(&b, s)

	// A flow that does not parse is listed with no arguments, and the warning
	// is left to the flow commands: quickstart is a guide, not a diagnostic.
	flows, _, err := FlowList(repo)
	if err != nil {
		return "", err
	}
	b.WriteString("\n")
	writeQuickstartFlows(&b, flows)

	return b.String(), nil
}

// writeQuickstartSchema renders the live half: every type, and under it every
// field that is not one of the four built-ins, which the guide already named.
func writeQuickstartSchema(b *strings.Builder, s *schema.Schema) {
	b.WriteString("## This repository's types\n\n")

	if s.Empty() {
		b.WriteString("There is no schema in this repository yet,\n")
		b.WriteString("so no type exists and a write is not validated against anything.\n")
		b.WriteString("`git work schema init` creates a preset's types and fields,\n")
		b.WriteString("the `jira` one unless the `linear` one is named,\n")
		b.WriteString("and `git work schema import FILE` applies a document.\n")
		return
	}

	b.WriteString("Every type below carries the four built-in fields as well.\n")
	b.WriteString("`type` takes one of these keys.\n")

	for _, typeKey := range s.TypeKeys() {
		t, ok := s.Type(typeKey)
		if !ok {
			continue
		}

		b.WriteString("\n### ")
		b.WriteString(typeKey)
		if t.Name != "" && t.Name != typeKey {
			b.WriteString(" — ")
			b.WriteString(t.Name)
		}
		b.WriteString("\n\n")
		if t.Description != "" {
			b.WriteString(t.Description)
			b.WriteString("\n\n")
		}

		var fields int
		for _, fieldKey := range t.FieldKeys() {
			field, ok := t.Field(fieldKey)
			if !ok || field.Builtin {
				continue
			}
			b.WriteString(quickstartField(field))
			b.WriteString("\n")
			fields++
		}
		if fields == 0 {
			b.WriteString("No field of its own.\n")
		}
	}
}

// quickstartField is one field on one line:
// its key, its kind, and what the kind leaves open — the value ids of an
// enum, the types a relation may point at — because those are exactly what a
// write is refused for getting wrong.
func quickstartField(f *schema.Field) string {
	detail := string(f.Kind)

	switch {
	case f.Kind.IsEnum():
		if ids := f.ValueIds(); len(ids) > 0 {
			detail += ": " + strings.Join(ids, ", ")
		}
		if f.Freeform {
			detail += "; any other value too"
		}

	case f.Kind.IsRelation():
		if len(f.TargetTypes) > 0 {
			detail += " → " + strings.Join(f.TargetTypes, ", ")
		} else {
			detail += " → any type"
		}
		if f.Inverse != "" {
			detail += "; read back as " + f.Inverse
		}
	}

	return fmt.Sprintf("- `%s` (%s)", f.Key, detail)
}

// writeQuickstartFlows renders the second live section: every unarchived flow,
// by name, as the call that runs it and the first line of its docstring.
//
// The flows are how this repository is meant to be looked at, so an agent
// that knows the types but not the flows would rebuild an overview by hand.
func writeQuickstartFlows(b *strings.Builder, flows []FlowEntry) {
	b.WriteString("## This repository's flows\n\n")

	var listed []FlowEntry
	for _, entry := range flows {
		if !entry.Archived {
			listed = append(listed, entry)
		}
	}

	if len(listed) == 0 {
		b.WriteString("There is no flow in this repository yet.\n")
		b.WriteString("`git work flow import FILE|DIR` adds them,\n")
		b.WriteString("one Starlark function per `.star` file.\n")
		return
	}

	b.WriteString("Each runs with `git work flow run NAME [KWARGS]`,\n")
	b.WriteString("KWARGS one JSON object of the arguments shown, the defaults filling what it omits,\n")
	b.WriteString("and `git work flow export NAME` prints one's source.\n\n")

	sort.Slice(listed, func(i, j int) bool { return listed[i].Name < listed[j].Name })

	for _, entry := range listed {
		b.WriteString(quickstartFlow(entry))
		b.WriteString("\n")
	}
}

// quickstartFlow is one flow on one line: its signature, as the def spells it,
// and its summary, the docstring's first line as the bare `git work flow`
// prints it.
func quickstartFlow(entry FlowEntry) string {
	params := make([]string, 0, len(entry.Params))
	for _, param := range entry.Params {
		if param.Required {
			params = append(params, param.Name)
			continue
		}
		params = append(params, param.Name+"="+starlarkLiteral(param.Default))
	}

	line := fmt.Sprintf("- `%s(%s)`", entry.Name, strings.Join(params, ", "))

	summary, _, _ := strings.Cut(entry.Description, "\n")
	if summary = strings.TrimSpace(summary); summary != "" {
		line += " — " + summary
	}

	return line
}

// starlarkLiteral spells a default, which the flow package keeps as JSON,
// the way the def wrote it: `None`, `True` and `False` rather than JSON's
// words, and everything else as JSON, which Starlark reads the same.
func starlarkLiteral(raw json.RawMessage) string {
	var value any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return string(raw)
	}

	var b strings.Builder
	writeStarlarkLiteral(&b, value)
	return b.String()
}

func writeStarlarkLiteral(b *strings.Builder, value any) {
	switch v := value.(type) {
	case nil:
		b.WriteString("None")
	case bool:
		if v {
			b.WriteString("True")
		} else {
			b.WriteString("False")
		}
	case []any:
		b.WriteString("[")
		for i, item := range v {
			if i > 0 {
				b.WriteString(", ")
			}
			writeStarlarkLiteral(b, item)
		}
		b.WriteString("]")
	case map[string]any:
		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		b.WriteString("{")
		for i, key := range keys {
			if i > 0 {
				b.WriteString(", ")
			}
			writeStarlarkLiteral(b, key)
			b.WriteString(": ")
			writeStarlarkLiteral(b, v[key])
		}
		b.WriteString("}")
	default:
		var encoded bytes.Buffer
		encoder := json.NewEncoder(&encoded)
		encoder.SetEscapeHTML(false)
		if err := encoder.Encode(v); err != nil {
			fmt.Fprint(b, v)
			return
		}
		b.WriteString(strings.TrimRight(encoded.String(), "\n"))
	}
}

package host

import (
	_ "embed"
	"fmt"
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
// the guide, then the types and fields this repository actually has.
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

	return b.String(), nil
}

// writeQuickstartSchema renders the live half: every type, and under it every
// field that is not one of the three built-ins, which the guide already named.
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

	b.WriteString("Every type below carries the three built-in fields as well.\n")
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

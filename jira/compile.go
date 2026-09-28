package jira

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/git-bug/git-bug/schema"
	"github.com/git-bug/git-bug/util/sorted"
)

// Compile turns the aliases of a loaded schema into a Mapping against one
// project (JS2, JS7). It refuses when no type is mapped yet (the first
// mapping is reviewed, JS5), when the mapped types are another project's,
// and when two entities carry one alias (JS25). Aliases Jira no longer
// has, or that name something of the wrong kind, are notes, never errors.
func Compile(s *schema.Schema, p *Project) (*Mapping, []Note, error) {
	var notes []Note
	note := func(level Level, key, format string, args ...any) {
		notes = append(notes, Note{Level: level, Key: key, Message: fmt.Sprintf(format, args...)})
	}

	if err := Mapped(s); err != nil {
		return nil, nil, err
	}
	var aliases []string
	owners := map[string][]string{}
	for _, key := range s.TypeKeys() {
		if a := s.Types[key].Aliases[System]; a != "" {
			aliases = append(aliases, a)
			owners[a] = append(owners[a], key)
		}
	}
	sort.Strings(aliases)
	if err := checkBinding(aliases, p); err != nil {
		return nil, nil, err
	}
	var dups []string
	for a, keys := range owners {
		if len(keys) > 1 {
			dups = append(dups, fmt.Sprintf("types %s all map issue type %s", strings.Join(keys, ", "), a))
		}
	}

	m := &Mapping{project: p.Key, projectId: p.Id, types: map[string]*typeMap{}, byIssueType: map[string]string{}}
	points := storyPoints(p)
	request := map[string]bool{"summary": true, "issuetype": true, "description": true, "project": true,
		"updated": true, "created": true, "reporter": true, "creator": true}

	for _, key := range s.TypeKeys() {
		t := s.Types[key]
		it, ok := p.IssueType(t.Aliases[System])
		if !ok {
			continue
		}
		if len(owners[it.Id]) > 1 {
			continue
		}
		tm := &typeMap{key: key, issueType: it.Id, fields: map[string]*fieldMap{}, required: map[string]bool{}}
		for _, f := range it.Screen {
			tm.required[metaId(f)] = f.Required && !f.HasDefaultValue
		}
		m.types[key] = tm
		m.byIssueType[it.Id] = key

		refs := map[string][]string{}
		for _, fk := range t.FieldKeys() {
			f := t.Fields[fk]
			ref := f.Aliases[System]
			if f.Builtin || ref == "" {
				continue
			}
			where := key + "/" + fk
			refs[ref] = append(refs[ref], fk)
			fm, problem := compileField(f, ref, p, points)
			if problem != "" {
				note(Warn, where, "%s", problem)
				continue
			}
			ids := map[string]string{}
			for _, v := range f.Values {
				a := v.Aliases[System]
				if a == "" {
					if _, stated := v.Aliases[System]; stated {
						// Derive excluded the Jira value by name (JS6), so FromJira knows it by name too
						fm.excluded[Norm(v.Id)], fm.excluded[Norm(v.Name)] = true, true
					}
					continue
				}
				if other, twice := ids[a]; twice {
					dups = append(dups, fmt.Sprintf("values %s and %s of %s both map Jira %s", other, v.Id, where, a))
				}
				ids[a] = v.Id
				fm.toJira[v.Id] = a
				fm.fromJira[a] = v.Id
			}
			tm.fields[fk] = fm
			if f.Kind == schema.KindEnum && ref == refStatus {
				tm.status = fk
			}
			if fm.link != "" {
				request["issuelinks"] = true
			} else {
				request[ref] = true
			}
		}
		for ref, fks := range refs {
			if len(fks) > 1 {
				dups = append(dups, fmt.Sprintf("fields %s of %s all map Jira %s", strings.Join(fks, ", "), key, ref))
			}
		}
		tm.keys = append([]string{schema.TitleKey, schema.TypeKey}, sorted.Keys(tm.fields)...)
	}
	if len(dups) > 0 {
		sort.Strings(dups)
		return nil, notes, fmt.Errorf("the mapping is ambiguous (JS25): %s; state one alias per Jira id and import again",
			strings.Join(dups, "; "))
	}
	m.request = sorted.Keys(request)
	return m, notes, nil
}

// Mapped refuses a schema with no type mapped yet: the first mapping is
// reviewed, never derived and imported by a sync (JS5).
func Mapped(s *schema.Schema) error {
	for _, t := range s.Types {
		if t.Aliases[System] != "" {
			return nil
		}
	}
	return errors.New("no type of the schema is mapped to a Jira issue type yet; review the first mapping:\n" +
		"  git work jira schema > jira.yaml\n  $EDITOR jira.yaml\n  git work schema import jira.yaml --dry-run\n  git work schema import jira.yaml")
}

// fixedKinds is JS7's table: the kind each fixed Jira field maps to.
var fixedKinds = map[string]schema.Kind{
	refStatus: schema.KindEnum, refPriority: schema.KindOrdinalEnum, refAssignee: schema.KindIdentity,
	refLabels: schema.KindMultiEnum, refDue: schema.KindDate, refParent: schema.KindRelation,
}

// compileField checks one aliased field against the project and JS7's
// kinds; a problem is why it is not mapped.
func compileField(f *schema.Field, ref string, p *Project, points string) (*fieldMap, string) {
	fm := &fieldMap{key: f.Key, ref: ref, kind: f.Kind, field: f,
		toJira: map[string]string{}, fromJira: map[string]string{}, excluded: map[string]bool{}}
	want, fixed := fixedKinds[ref]
	switch {
	case fixed:
	case strings.HasPrefix(ref, linkPrefix):
		id := strings.TrimPrefix(ref, linkPrefix)
		found := false
		for _, lt := range p.LinkTypes {
			found = found || lt.ID == id
		}
		if !found {
			return nil, fmt.Sprintf("dead alias: %s has no link type %s", p.Key, id)
		}
		fm.link = id
		want = schema.KindMultiRelation
	default:
		jf, ok := p.Field(ref)
		if !ok || jf.Schema == nil {
			return nil, fmt.Sprintf("dead alias: %s has no field %s", p.Key, ref)
		}
		fm.jira = *jf.Schema
		if ref == points {
			want = schema.KindNumber
		} else if want, ok = customKind(*jf.Schema); !ok {
			return nil, fmt.Sprintf("Jira field %s (%s) cannot be synced", ref, customName(*jf.Schema))
		}
	}
	if f.Kind != want {
		return nil, fmt.Sprintf("Jira %s is a %s, this field a %s: not synced", ref, want, f.Kind)
	}
	return fm, ""
}

// isDatetime says a custom date field is Jira's datetime: RFC 3339 in UTC locally.
func (fm *fieldMap) isDatetime() bool { return fm.jira.Type == "datetime" }

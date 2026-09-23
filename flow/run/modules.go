package run

import (
	"bytes"
	"encoding/json"
	"fmt"

	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"

	"github.com/git-bug/git-bug/entities/issue"
	"github.com/git-bug/git-bug/host"
	"github.com/git-bug/git-bug/schema"
	"github.com/git-bug/git-bug/view"
)

// predeclared is the whole of what a flow can see.
//
// There is no `load`, so these globals are the only names a script has,
// and every one of them is a command the shell has too — `me` excepted.
func (r *runtime) predeclared() starlark.StringDict {
	return starlark.StringDict{
		"issue": &starlarkstruct.Module{
			Name: "issue",
			Members: starlark.StringDict{
				"list":    starlark.NewBuiltin("issue.list", r.issueList),
				"new":     starlark.NewBuiltin("issue.new", r.issueNew),
				"get":     starlark.NewBuiltin("issue.get", r.issueGet),
				"set":     starlark.NewBuiltin("issue.set", r.issueSet),
				"add":     starlark.NewBuiltin("issue.add", r.issueAdd),
				"remove":  starlark.NewBuiltin("issue.remove", r.issueRemove),
				"log":     starlark.NewBuiltin("issue.log", r.issueLog),
				"archive": starlark.NewBuiltin("issue.archive", r.issueArchive),
				"rm":      starlark.NewBuiltin("issue.rm", r.issueRm),
				"comment": &starlarkstruct.Module{
					Name: "issue.comment",
					Members: starlark.StringDict{
						"new":  starlark.NewBuiltin("issue.comment.new", r.issueCommentNew),
						"edit": starlark.NewBuiltin("issue.comment.edit", r.issueCommentEdit),
					},
				},
			},
		},
		// `import` is a reserved word in Starlark, so the one verb that can
		// not keep its name is spelled `import_` (cli-convention.md, E9).
		"schema": &starlarkstruct.Module{
			Name: "schema",
			Members: starlark.StringDict{
				"export":  starlark.NewBuiltin("schema.export", r.schemaExport),
				"import_": starlark.NewBuiltin("schema.import_", r.schemaImport),
				"init":    starlark.NewBuiltin("schema.init", r.schemaInit),
				"log":     starlark.NewBuiltin("schema.log", r.schemaLog),
				"archive": starlark.NewBuiltin("schema.archive", r.schemaArchive),
				"rm":      starlark.NewBuiltin("schema.rm", r.schemaRm),
			},
		},
		"flow": &starlarkstruct.Module{
			Name: "flow",
			Members: starlark.StringDict{
				"list": starlark.NewBuiltin("flow.list", r.flowList),
				"get":  starlark.NewBuiltin("flow.get", r.flowGet),
				"run":  starlark.NewBuiltin("flow.run", r.flowRun),
			},
		},
		"view": &starlarkstruct.Module{
			Name:    "view",
			Members: r.viewMembers(),
		},
		"me": starlark.NewBuiltin("me", r.me),
	}
}

// issue.list(program) — `git work issue [PROGRAM]`.
//
// One value comes back as itself, which is the array a program usually
// returns; several come back as a list, which is what a stream is.
func (r *runtime) issueList(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var program string
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "program?", &program); err != nil {
		return nil, err
	}

	values, err := host.IssueList(r.repo, program)
	if err != nil {
		return nil, hostError(b, err)
	}

	if len(values) == 1 {
		return toStarlark(values[0])
	}
	return toStarlark(values)
}

// issue.new(doc) — `git work issue new DOC`; returns the new id.
func (r *runtime) issueNew(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var value starlark.Value
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "doc", &value); err != nil {
		return nil, err
	}

	raw, err := marshalStarlark(value)
	if err != nil {
		return nil, hostError(b, err)
	}

	var doc host.IssueDocument
	if err := strictUnmarshal(raw, &doc); err != nil {
		return nil, hostError(b, err)
	}

	id, err := host.IssueNew(r.repo, doc)
	if err != nil {
		return nil, hostError(b, err)
	}
	return starlark.String(id.String()), nil
}

// issue.get(id) — `git work issue get ID`.
func (r *runtime) issueGet(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var id string
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "id", &id); err != nil {
		return nil, err
	}

	document, err := host.IssueGet(r.repo, id)
	if err != nil {
		return nil, hostError(b, err)
	}
	return reencode(b, document)
}

// issue.log(id) — `git work issue log ID`.
func (r *runtime) issueLog(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var id string
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "id", &id); err != nil {
		return nil, err
	}

	entries, err := host.IssueLog(r.repo, id)
	if err != nil {
		return nil, hostError(b, err)
	}
	return reencode(b, entries)
}

// issue.set(id, **fields) — `git work issue set ID FIELDS`; prints nothing.
func (r *runtime) issueSet(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	id, fields, err := idAndFields(b, args, kwargs)
	if err != nil {
		return nil, err
	}

	if _, err := host.IssueSet(r.repo, id, fields, false); err != nil {
		return nil, hostError(b, err)
	}
	return starlark.None, nil
}

// issue.add(id, **items) — `git work issue add ID ITEMS`.
func (r *runtime) issueAdd(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	id, items, err := idAndItems(b, args, kwargs)
	if err != nil {
		return nil, err
	}

	if _, err := host.IssueAdd(r.repo, id, items, false); err != nil {
		return nil, hostError(b, err)
	}
	return starlark.None, nil
}

// issue.remove(id, **items) — `git work issue remove ID ITEMS`.
func (r *runtime) issueRemove(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	id, items, err := idAndItems(b, args, kwargs)
	if err != nil {
		return nil, err
	}

	if _, err := host.IssueRemove(r.repo, id, items, false); err != nil {
		return nil, hostError(b, err)
	}
	return starlark.None, nil
}

// issue.archive(id) — `git work issue archive ID`.
func (r *runtime) issueArchive(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var id string
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "id", &id); err != nil {
		return nil, err
	}

	if _, err := host.IssueArchive(r.repo, id, false); err != nil {
		return nil, hostError(b, err)
	}
	return starlark.None, nil
}

// issue.rm(id) — `git work issue rm ID`, the local ref only.
func (r *runtime) issueRm(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var id string
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "id", &id); err != nil {
		return nil, err
	}

	if err := host.IssueRm(r.repo, id); err != nil {
		return nil, hostError(b, err)
	}
	return starlark.None, nil
}

// issue.comment.new(id, body) — returns the new comment's id.
func (r *runtime) issueCommentNew(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var id, body string
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "id", &id, "body", &body); err != nil {
		return nil, err
	}

	commentId, err := host.IssueCommentNew(r.repo, id, body)
	if err != nil {
		return nil, hostError(b, err)
	}
	return starlark.String(commentId.String()), nil
}

// issue.comment.edit(id, body) — id is the comment's own id.
func (r *runtime) issueCommentEdit(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var id, body string
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "id", &id, "body", &body); err != nil {
		return nil, err
	}

	if err := host.IssueCommentEdit(r.repo, id, body); err != nil {
		return nil, hostError(b, err)
	}
	return starlark.None, nil
}

// schema.export() — `git work schema export`.
//
// The document comes back as the dict `--format json` prints, which is the
// dict `schema.import_` takes, so a script edits a schema the way it reads one.
func (r *runtime) schemaExport(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if err := starlark.UnpackArgs(b.Name(), args, kwargs); err != nil {
		return nil, err
	}

	doc, warnings, err := host.SchemaExport(r.repo)
	r.warn(warnings)
	if err != nil {
		return nil, hostError(b, err)
	}

	// The document's members carry YAML tags and its mappings remember their
	// order, so its own marshaller is the only one that prints it whole,
	// and the ordered conversion is the only one that keeps what it printed.
	raw, err := doc.Marshal("json")
	if err != nil {
		return nil, hostError(b, err)
	}
	return decodeOrdered(raw)
}

// schema.import_(doc, prune=False, dry_run=False) — `git work schema import`.
//
// `import` is a reserved word in Starlark, so this one verb is spelled with a
// trailing underscore; everything else about it is the command.
// The changes come back whether they were committed or not, because they are
// what `--dry-run` prints and what the same call commits a moment later.
func (r *runtime) schemaImport(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var value starlark.Value
	var prune, dryRun bool
	if err := starlark.UnpackArgs(b.Name(), args, kwargs,
		"doc", &value, "prune?", &prune, "dry_run?", &dryRun); err != nil {
		return nil, err
	}

	if value == starlark.None {
		return nil, fmt.Errorf("%s: the document is None", b.Name())
	}
	raw, err := marshalOrdered(value)
	if err != nil {
		return nil, hostError(b, err)
	}

	doc, err := schema.ParseDocument(raw)
	if err != nil {
		return nil, hostError(b, err)
	}

	r.warn(host.SchemaDuplicates(r.repo))

	changes, _, err := host.SchemaImport(r.repo, doc, prune, dryRun)
	if err != nil {
		return nil, hostError(b, err)
	}
	return changeList(b, changes)
}

// schema.init(preset="jira", dry_run=False) — `git work schema init`.
//
// It returns the changes, as import_ does, rather than the ids the command
// prints: the two are one host call, and a script that wants the ids reads
// them off the creates.
func (r *runtime) schemaInit(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var preset string
	var dryRun bool
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "preset?", &preset, "dry_run?", &dryRun); err != nil {
		return nil, err
	}

	r.warn(host.SchemaDuplicates(r.repo))

	changes, _, err := host.SchemaInit(r.repo, preset, dryRun)
	if err != nil {
		return nil, hostError(b, err)
	}
	return changeList(b, changes)
}

// schema.log(key="") — `git work schema log [KEY]`, every entity by default.
func (r *runtime) schemaLog(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var key string
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "key?", &key); err != nil {
		return nil, err
	}

	r.warn(host.SchemaDuplicates(r.repo))

	entries, err := host.SchemaLog(r.repo, key)
	if err != nil {
		return nil, hostError(b, err)
	}
	return reencode(b, entries)
}

// schema.archive(key) — `git work schema archive KEY`, the replicated removal.
func (r *runtime) schemaArchive(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var key string
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "key", &key); err != nil {
		return nil, err
	}

	r.warn(host.SchemaDuplicates(r.repo))

	warnings, err := host.SchemaArchive(r.repo, key)
	r.warn(warnings)
	if err != nil {
		return nil, hostError(b, err)
	}
	return starlark.None, nil
}

// schema.rm(key) — `git work schema rm KEY`, the local ref only.
func (r *runtime) schemaRm(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var key string
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "key", &key); err != nil {
		return nil, err
	}

	r.warn(host.SchemaDuplicates(r.repo))

	if err := host.SchemaRm(r.repo, key); err != nil {
		return nil, hostError(b, err)
	}
	return starlark.None, nil
}

// flow.list() — `git work flow`.
func (r *runtime) flowList(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if err := starlark.UnpackArgs(b.Name(), args, kwargs); err != nil {
		return nil, err
	}

	entries, warnings, err := host.FlowList(r.repo)
	if err != nil {
		return nil, hostError(b, err)
	}
	r.warn(warnings)
	return reencode(b, entries)
}

// flow.get(name) — `git work flow get NAME`.
func (r *runtime) flowGet(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var name string
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "name", &name); err != nil {
		return nil, err
	}

	detail, warnings, err := host.FlowGet(r.repo, name)
	if err != nil {
		return nil, hostError(b, err)
	}
	r.warn(warnings)
	return reencode(b, detail)
}

// flow.run(name, **kwargs) — `git work flow run NAME KWARGS`.
//
// Flows compose, so this is re-entrant; MaxDepth is what stops a cycle.
func (r *runtime) flowRun(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%s: takes the flow's name and then its arguments by keyword", b.Name())
	}
	name, ok := starlark.AsString(args[0])
	if !ok {
		return nil, fmt.Errorf("%s: the flow's name is a string, not a %s", b.Name(), args[0].Type())
	}

	values, err := keywordJSON(b, kwargs)
	if err != nil {
		return nil, err
	}

	raw, err := newRuntime(r.ctx, r.repo, r.stderr, r.depth+1).flow(name, values)
	if err != nil {
		return nil, hostError(b, err)
	}
	if raw == nil {
		return starlark.None, nil
	}

	decoded, err := decodeJSON(raw)
	if err != nil {
		return nil, hostError(b, err)
	}
	return toStarlark(decoded)
}

// me() — the identity this repository writes as, the one script-only name.
func (r *runtime) me(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if err := starlark.UnpackArgs(b.Name(), args, kwargs); err != nil {
		return nil, err
	}

	identity, err := host.Me(r.repo)
	if err != nil {
		return nil, hostError(b, err)
	}
	return reencode(b, identity)
}

// viewMembers is one builtin per view kind, from the same table the renderers
// read, so a kind added there is callable here without another edit.
func (r *runtime) viewMembers() starlark.StringDict {
	members := make(starlark.StringDict, len(view.Kinds))
	for _, kind := range view.KindNames() {
		members[kind] = starlark.NewBuiltin("view."+kind, r.viewBuiltin(kind))
	}
	return members
}

func (r *runtime) viewBuiltin(kind string) func(*starlark.Thread, *starlark.Builtin, starlark.Tuple, []starlark.Tuple) (starlark.Value, error) {
	return func(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("%s: takes the items and then its bindings by keyword", b.Name())
		}

		converted, err := fromStarlark(args[0])
		if err != nil {
			return nil, hostError(b, err)
		}
		items, ok := converted.([]any)
		if !ok {
			return nil, fmt.Errorf("%s: the items are a list of issues, not a %s", b.Name(), args[0].Type())
		}

		bindings := make(map[string]string, len(kwargs))
		for _, kwarg := range kwargs {
			name, _ := starlark.AsString(kwarg[0])
			value, ok := starlark.AsString(kwarg[1])
			if !ok {
				return nil, fmt.Errorf("%s: binding %s names a field, so it is a string, not a %s",
					b.Name(), name, kwarg[1].Type())
			}
			bindings[name] = value
		}

		spec, err := view.Build(kind, items, bindings)
		if err != nil {
			return nil, hostError(b, err)
		}
		return reencode(b, spec)
	}
}

// idAndFields reads `verb(id, **fields)`, the shape every field writer has.
func idAndFields(b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (string, map[string]issue.Value, error) {
	id, err := firstString(b, args)
	if err != nil {
		return "", nil, err
	}

	values, err := keywordJSON(b, kwargs)
	if err != nil {
		return "", nil, err
	}
	if len(values) == 0 {
		return "", nil, fmt.Errorf("%s: no field to set", b.Name())
	}

	fields := make(map[string]issue.Value, len(values))
	for key, raw := range values {
		fields[key] = issue.Value(raw)
	}
	return id, fields, nil
}

// idAndItems reads `verb(id, **items)`, where every value is a list.
func idAndItems(b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (string, map[string][]issue.Value, error) {
	id, err := firstString(b, args)
	if err != nil {
		return "", nil, err
	}

	values, err := keywordJSON(b, kwargs)
	if err != nil {
		return "", nil, err
	}
	if len(values) == 0 {
		return "", nil, fmt.Errorf("%s: no item to change", b.Name())
	}

	items := make(map[string][]issue.Value, len(values))
	for key, raw := range values {
		var list []issue.Value
		if err := json.Unmarshal(raw, &list); err != nil {
			return "", nil, fmt.Errorf("%s: field %s takes a list of items", b.Name(), key)
		}
		if len(list) == 0 {
			return "", nil, fmt.Errorf("%s: field %s has no item", b.Name(), key)
		}
		items[key] = list
	}
	return id, items, nil
}

func firstString(b *starlark.Builtin, args starlark.Tuple) (string, error) {
	if len(args) != 1 {
		return "", fmt.Errorf("%s: takes the id and then its keys by keyword", b.Name())
	}
	id, ok := starlark.AsString(args[0])
	if !ok {
		return "", fmt.Errorf("%s: the id is a string, not a %s", b.Name(), args[0].Type())
	}
	return id, nil
}

// keywordJSON turns a call's keyword arguments into the JSON object the
// command line takes as one argument: that is the rule, one for one.
func keywordJSON(b *starlark.Builtin, kwargs []starlark.Tuple) (map[string]json.RawMessage, error) {
	values := make(map[string]json.RawMessage, len(kwargs))
	for _, kwarg := range kwargs {
		name, _ := starlark.AsString(kwarg[0])
		raw, err := marshalStarlark(kwarg[1])
		if err != nil {
			return nil, fmt.Errorf("%s: argument %s: %w", b.Name(), name, err)
		}
		if raw == nil {
			raw = json.RawMessage("null")
		}
		values[name] = raw
	}
	return values, nil
}

// warn prints a host's warnings where a command prints them, on stderr,
// so that a flow's diagnostics never end up in the JSON it returns.
func (r *runtime) warn(warnings []string) {
	for _, warning := range warnings {
		fmt.Fprintf(r.stderr, "warning: %s\n", warning)
	}
}

// changeList returns an import's changes, empty rather than None when a
// document and the store already agree — which is what a round trip returns.
func changeList(b *starlark.Builtin, changes []schema.Change) (starlark.Value, error) {
	if changes == nil {
		changes = []schema.Change{}
	}
	return reencode(b, changes)
}

// reencode turns a host result into a Starlark value through its JSON, so that
// a script reads exactly the document the command prints.
func reencode(b *starlark.Builtin, v any) (starlark.Value, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, hostError(b, err)
	}
	decoded, err := decodeJSON(raw)
	if err != nil {
		return nil, hostError(b, err)
	}
	return toStarlark(decoded)
}

// strictUnmarshal refuses an unknown key, as the command line does:
// a misspelled key is a mistake, never a silent no-op.
func strictUnmarshal(raw json.RawMessage, into any) error {
	if raw == nil {
		return fmt.Errorf("the document is None")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	return dec.Decode(into)
}

// hostError carries a host failure back into Starlark.
//
// The name is not added here: a builtin's error becomes an EvalError whose
// Backtrace() already opens with "Error in <builtin>", and saying it twice
// reads as two failures.
func hostError(b *starlark.Builtin, err error) error {
	return err
}

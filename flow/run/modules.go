package run

import (
	"encoding/json"
	"fmt"

	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"

	"github.com/git-bug/git-bug/entities/issue"
	"github.com/git-bug/git-bug/host"
	"github.com/git-bug/git-bug/schema"
	"github.com/git-bug/git-bug/view"
)

// A host failure is returned as it is, never wrapped with the builtin's name:
// a builtin's error becomes an EvalError whose Backtrace() already opens with
// "Error in <builtin>", and saying it twice reads as two failures.

// predeclared is the whole of what a flow can see.
//
// The SDK is one module, named after the binary:
// `work.issue.get(id)` is the command `issue get ID`, one for one
// (cli-convention.md, `b511c63`, `0740bf3`).
// One name rather than five leaves `issue`, `flow`, `schema`, `view` and `user`
// free for a script's own locals,
// and leaves no host name that is not also a command.
// There is no `load`, so `work` is the only global
// besides Starlark's own universe.
func (r *runtime) predeclared() starlark.StringDict {
	return starlark.StringDict{"work": r.work()}
}

// work is the module tree, the whole SDK in one table.
//
// Every builtin is named by its path through the tree,
// so a failure reads as `work.issue.comment.new`,
// which is the call as a script writes it.
func (r *runtime) work() *starlarkstruct.Module {
	return newModule("work",
		sub("issue",
			verb("list", r.issueList),
			verb("new", r.issueNew),
			verb("get", r.issueGet),
			verb("set", r.issueSet),
			verb("add", r.issueAdd),
			verb("remove", r.issueRemove),
			verb("log", r.issueLog),
			verb("archive", r.issueArchive),
			verb("rm", r.issueRm),
			sub("comment",
				verb("new", r.issueCommentNew),
				verb("edit", r.issueCommentEdit),
			),
		),
		// `import` is a reserved word in Starlark, so the one verb that can
		// not keep its name is spelled `import_` (cli-convention.md, E9),
		// here and in `work.schema`.
		sub("flow",
			verb("list", r.flowList),
			verb("export", r.flowExport),
			verb("import_", r.flowImport),
			verb("run", r.flowRun),
			verb("log", r.flowLog),
			verb("archive", r.flowArchive),
			verb("rm", r.flowRm),
		),
		sub("schema",
			verb("export", r.schemaExport),
			verb("import_", r.schemaImport),
			verb("init", r.schemaInit),
			verb("log", r.schemaLog),
			verb("archive", r.schemaArchive),
			verb("rm", r.schemaRm),
		),
		sub("view", r.viewMembers()...),
		sub("user",
			verb("me", r.userMe),
		),
	)
}

// builtinFunc is the shape every host verb has.
type builtinFunc = func(*starlark.Thread, *starlark.Builtin, starlark.Tuple, []starlark.Tuple) (starlark.Value, error)

// member is one name in the module tree: a verb, or a module of more names.
//
// A member does not know where it sits until the tree is built,
// so it is handed its path then
// and no name in the table is written twice.
type member struct {
	name  string
	build func(path string) starlark.Value
}

func verb(name string, fn builtinFunc) member {
	return member{name: name, build: func(path string) starlark.Value {
		return starlark.NewBuiltin(path, fn)
	}}
}

func sub(name string, members ...member) member {
	return member{name: name, build: func(path string) starlark.Value {
		return newModule(path, members...)
	}}
}

func newModule(path string, members ...member) *starlarkstruct.Module {
	module := &starlarkstruct.Module{
		Name:    path,
		Members: make(starlark.StringDict, len(members)),
	}
	for _, m := range members {
		module.Members[m.name] = m.build(path + "." + m.name)
	}
	return module
}

// work.issue.list(program) — `git work issue [PROGRAM]`.
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
		return nil, err
	}

	if len(values) == 1 {
		return toStarlark(values[0])
	}
	return toStarlark(values)
}

// work.issue.new(doc) — `git work issue new DOC`; returns the new id.
func (r *runtime) issueNew(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var value starlark.Value
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "doc", &value); err != nil {
		return nil, err
	}

	raw, err := marshalStarlark(value)
	if err != nil {
		return nil, err
	}

	var doc host.IssueDocument
	if err := strictUnmarshal(raw, &doc); err != nil {
		return nil, err
	}

	id, err := host.IssueNew(r.repo, doc)
	if err != nil {
		return nil, err
	}
	return starlark.String(id.String()), nil
}

// work.issue.get(id) — `git work issue get ID`.
func (r *runtime) issueGet(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var id string
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "id", &id); err != nil {
		return nil, err
	}

	document, err := host.IssueGet(r.repo, id)
	if err != nil {
		return nil, err
	}
	return reencode(b, document)
}

// work.issue.log(id) — `git work issue log ID`.
func (r *runtime) issueLog(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var id string
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "id", &id); err != nil {
		return nil, err
	}

	entries, err := host.IssueLog(r.repo, id)
	if err != nil {
		return nil, err
	}
	return reencode(b, entries)
}

// work.issue.set(id, **fields) — `git work issue set ID FIELDS`; prints nothing.
func (r *runtime) issueSet(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	id, fields, err := idAndFields(b, args, kwargs)
	if err != nil {
		return nil, err
	}

	if _, err := host.IssueSet(r.repo, id, fields, false); err != nil {
		return nil, err
	}
	return starlark.None, nil
}

// work.issue.add(id, **items) — `git work issue add ID ITEMS`.
func (r *runtime) issueAdd(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	id, items, err := idAndItems(b, args, kwargs)
	if err != nil {
		return nil, err
	}

	if _, err := host.IssueAdd(r.repo, id, items, false); err != nil {
		return nil, err
	}
	return starlark.None, nil
}

// work.issue.remove(id, **items) — `git work issue remove ID ITEMS`.
func (r *runtime) issueRemove(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	id, items, err := idAndItems(b, args, kwargs)
	if err != nil {
		return nil, err
	}

	if _, err := host.IssueRemove(r.repo, id, items, false); err != nil {
		return nil, err
	}
	return starlark.None, nil
}

// work.issue.archive(id) — `git work issue archive ID`.
func (r *runtime) issueArchive(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var id string
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "id", &id); err != nil {
		return nil, err
	}

	if _, err := host.IssueArchive(r.repo, id, false); err != nil {
		return nil, err
	}
	return starlark.None, nil
}

// work.issue.rm(id) — `git work issue rm ID`, the local ref only.
func (r *runtime) issueRm(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var id string
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "id", &id); err != nil {
		return nil, err
	}

	if err := host.IssueRm(r.repo, id); err != nil {
		return nil, err
	}
	return starlark.None, nil
}

// work.issue.comment.new(id, body) — returns the new comment's id.
func (r *runtime) issueCommentNew(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var id, body string
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "id", &id, "body", &body); err != nil {
		return nil, err
	}

	commentId, err := host.IssueCommentNew(r.repo, id, body)
	if err != nil {
		return nil, err
	}
	return starlark.String(commentId.String()), nil
}

// work.issue.comment.edit(id, body) — id is the comment's own id.
func (r *runtime) issueCommentEdit(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var id, body string
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "id", &id, "body", &body); err != nil {
		return nil, err
	}

	if err := host.IssueCommentEdit(r.repo, id, body); err != nil {
		return nil, err
	}
	return starlark.None, nil
}

// work.schema.export() — `git work schema export`.
//
// The document comes back as the dict `--format json` prints, which is the
// dict `work.schema.import_` takes,
// so a script edits a schema the way it reads one.
func (r *runtime) schemaExport(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if err := starlark.UnpackArgs(b.Name(), args, kwargs); err != nil {
		return nil, err
	}

	doc, warnings, err := host.SchemaExport(r.repo)
	r.warn(warnings)
	if err != nil {
		return nil, err
	}

	// The document's members carry YAML tags and its mappings remember their
	// order, so its own marshaller is the only one that prints it whole,
	// and the ordered conversion is the only one that keeps what it printed.
	raw, err := doc.Marshal("json")
	if err != nil {
		return nil, err
	}
	return decodeOrdered(raw)
}

// work.schema.import_(doc, prune=False, dry_run=False) — `git work schema import`.
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
		return nil, err
	}

	doc, err := schema.ParseDocument(raw)
	if err != nil {
		return nil, err
	}

	r.warn(host.SchemaDuplicates(r.repo))

	changes, _, err := host.SchemaImport(r.repo, doc, prune, dryRun)
	if err != nil {
		return nil, err
	}
	return changeList(b, changes)
}

// work.schema.init(preset="jira", dry_run=False) — `git work schema init`.
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
		return nil, err
	}
	return changeList(b, changes)
}

// work.schema.log(key="") — `git work schema log [KEY]`, every entity by default.
func (r *runtime) schemaLog(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var key string
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "key?", &key); err != nil {
		return nil, err
	}

	r.warn(host.SchemaDuplicates(r.repo))

	entries, err := host.SchemaLog(r.repo, key)
	if err != nil {
		return nil, err
	}
	return reencode(b, entries)
}

// work.schema.archive(key) — `git work schema archive KEY`, the replicated removal.
func (r *runtime) schemaArchive(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var key string
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "key", &key); err != nil {
		return nil, err
	}

	r.warn(host.SchemaDuplicates(r.repo))

	warnings, err := host.SchemaArchive(r.repo, key)
	r.warn(warnings)
	if err != nil {
		return nil, err
	}
	return starlark.None, nil
}

// work.schema.rm(key) — `git work schema rm KEY`, the local ref only.
func (r *runtime) schemaRm(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var key string
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "key", &key); err != nil {
		return nil, err
	}

	r.warn(host.SchemaDuplicates(r.repo))

	if err := host.SchemaRm(r.repo, key); err != nil {
		return nil, err
	}
	return starlark.None, nil
}

// work.flow.list() — `git work flow`.
func (r *runtime) flowList(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if err := starlark.UnpackArgs(b.Name(), args, kwargs); err != nil {
		return nil, err
	}

	entries, warnings, err := host.FlowList(r.repo)
	if err != nil {
		return nil, err
	}
	r.warn(warnings)
	return reencode(b, entries)
}

// work.flow.export(name) — `git work flow export NAME`: the script, verbatim.
func (r *runtime) flowExport(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var name string
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "name", &name); err != nil {
		return nil, err
	}

	script, err := host.FlowExport(r.repo, name)
	if err != nil {
		return nil, err
	}
	return starlark.String(script), nil
}

// work.flow.import_(scripts, prune=False, dry_run=False) — `git work flow import`.
//
// The command takes files and a script takes their contents, exactly as
// `work.schema.import_` takes the document the command reads from a file:
// finding the bytes is the shell's job, and one for one is about the verb and
// its arguments, not about who opens the file.
// The ids created come back, as the command prints them.
func (r *runtime) flowImport(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var value starlark.Value
	var prune, dryRun bool
	if err := starlark.UnpackArgs(b.Name(), args, kwargs,
		"scripts", &value, "prune?", &prune, "dry_run?", &dryRun); err != nil {
		return nil, err
	}

	sources, err := flowSources(b, value)
	if err != nil {
		return nil, err
	}

	r.warn(host.FlowWarnings(r.repo))

	_, created, err := host.FlowImport(r.repo, sources, prune, dryRun)
	if err != nil {
		return nil, err
	}

	ids := make([]string, 0, len(created))
	for _, id := range created {
		ids = append(ids, id.String())
	}
	return reencode(b, ids)
}

// flowSources reads the list of scripts an import takes.
//
// A single string is not accepted: the argument is a list, so that importing
// one flow and importing five read the same, as they do on the command line.
func flowSources(b *starlark.Builtin, value starlark.Value) ([]host.FlowSource, error) {
	list, ok := value.(*starlark.List)
	if !ok {
		return nil, fmt.Errorf("%s: the scripts are a list, not a %s", b.Name(), value.Type())
	}

	sources := make([]host.FlowSource, 0, list.Len())
	for i := range list.Len() {
		script, ok := starlark.AsString(list.Index(i))
		if !ok {
			return nil, fmt.Errorf("%s: script %d is a string, not a %s",
				b.Name(), i+1, list.Index(i).Type())
		}
		sources = append(sources, host.FlowSource{
			Origin: fmt.Sprintf("script %d", i+1),
			Script: script,
		})
	}
	return sources, nil
}

// work.flow.log(name="") — `git work flow log [NAME]`, every flow by default.
func (r *runtime) flowLog(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var name string
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "name?", &name); err != nil {
		return nil, err
	}

	r.warn(host.FlowWarnings(r.repo))

	entries, err := host.FlowLog(r.repo, name)
	if err != nil {
		return nil, err
	}
	return reencode(b, entries)
}

// work.flow.archive(name) — `git work flow archive NAME`, the replicated removal.
func (r *runtime) flowArchive(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var name string
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "name", &name); err != nil {
		return nil, err
	}

	r.warn(host.FlowWarnings(r.repo))

	if err := host.FlowArchive(r.repo, name); err != nil {
		return nil, err
	}
	return starlark.None, nil
}

// work.flow.rm(name) — `git work flow rm NAME`, the local ref only.
func (r *runtime) flowRm(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var name string
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "name", &name); err != nil {
		return nil, err
	}

	r.warn(host.FlowWarnings(r.repo))

	if err := host.FlowRm(r.repo, name); err != nil {
		return nil, err
	}
	return starlark.None, nil
}

// work.flow.run(name, **kwargs) — `git work flow run NAME KWARGS`.
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

	raw, err := newRuntime(r.ctx, r.repo, r.options(), r.depth+1).flow(name, values)
	if err != nil {
		return nil, err
	}
	if raw == nil {
		return starlark.None, nil
	}

	decoded, err := decodeJSON(raw)
	if err != nil {
		return nil, err
	}
	return toStarlark(decoded)
}

// work.user.me() — `git work user me`, the identity this repository writes as.
func (r *runtime) userMe(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if err := starlark.UnpackArgs(b.Name(), args, kwargs); err != nil {
		return nil, err
	}

	identity, err := host.UserMe(r.repo)
	if err != nil {
		return nil, err
	}
	return reencode(b, identity)
}

// viewMembers is one builtin per view kind, from the same table the renderers
// read, so a kind added there is callable here without another edit.
func (r *runtime) viewMembers() []member {
	members := make([]member, 0, len(view.Kinds))
	for _, kind := range view.KindNames() {
		members = append(members, verb(kind, r.viewBuiltin(kind)))
	}
	return members
}

func (r *runtime) viewBuiltin(kind string) builtinFunc {
	return func(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		if len(args) != 0 {
			return nil, fmt.Errorf("%s: takes its arguments by keyword", b.Name())
		}

		values, err := keywordJSON(b, kwargs)
		if err != nil {
			return nil, err
		}

		// The same call the command makes, through the same function: a view
		// renders where it is called from and blocks until the user quits,
		// and what it answers is what the script gets (decided 2026-09-24).
		answer, err := host.View(r.ctx, r.repo, r.renderer, kind, values)
		if err != nil {
			return nil, err
		}
		if answer == nil {
			return starlark.None, nil
		}

		decoded, err := decodeJSON(answer)
		if err != nil {
			return nil, err
		}
		return toStarlark(decoded)
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
		return nil, err
	}
	decoded, err := decodeJSON(raw)
	if err != nil {
		return nil, err
	}
	return toStarlark(decoded)
}

// strictUnmarshal refuses an unknown key, as the command line does:
// a misspelled key is a mistake, never a silent no-op. It is the command
// line's decoder, host.DecodeStrict, because it is the command line's rule.
func strictUnmarshal(raw json.RawMessage, into any) error {
	if raw == nil {
		return fmt.Errorf("the document is None")
	}
	return host.DecodeStrict(raw, into)
}

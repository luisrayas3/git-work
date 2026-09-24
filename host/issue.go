package host

import (
	"errors"
	"fmt"
	"sort"

	"github.com/git-bug/git-bug/cache"
	"github.com/git-bug/git-bug/commands/cmdjson"
	"github.com/git-bug/git-bug/entities/issue"
	"github.com/git-bug/git-bug/entity"
	"github.com/git-bug/git-bug/query/jq"
	"github.com/git-bug/git-bug/util/text"
)

// DefaultProgram is the list you get when you name no program:
// everything that is not archived, most recently edited first.
//
// It is written as a jq program rather than special-cased in Go
// so that `.` means the whole array and nothing is hidden from it.
// "mine" would be the better default, but it needs the schema's assignee
// field to know which one it is, so it waits for the schema (e8d6426).
const DefaultProgram = `map(select(.fields.archived != true))
	| sort_by(.edit_time.lamport, .edit_time.timestamp)
	| reverse`

// IssueDocument is what `issue new` takes: the issue as a document.
//
// Fields carries the whole of what the issue is, title included;
// body is the first comment, the one an issue always has;
// aliases are external ids, stored as create-op metadata (483dbe2).
type IssueDocument struct {
	Fields  map[string]issue.Value `json:"fields"`
	Body    string                 `json:"body"`
	Aliases map[string]string      `json:"aliases"`
}

// IssueList runs a jq program over the issues and returns every value it emits.
//
// An empty program is the default one,
// so that `git work issue` and `work.issue.list()` mean the same thing.
func IssueList(repo *cache.RepoCache, program string) ([]any, error) {
	if program == "" {
		program = DefaultProgram
	}

	input, err := IssueListInput(repo)
	if err != nil {
		return nil, err
	}

	return jq.Run(program, input)
}

// IssueListInput is the array a program runs over: every issue as an excerpt,
// oldest first, so that a program that does not sort still reads the same twice.
func IssueListInput(repo *cache.RepoCache) (any, error) {
	ids := repo.Issues().AllIds()

	excerpts := make([]*cache.IssueExcerpt, len(ids))
	for i, id := range ids {
		excerpt, err := repo.Issues().ResolveExcerpt(id)
		if err != nil {
			return nil, err
		}
		excerpts[i] = excerpt
	}
	sort.Sort(cache.IssuesByCreationTime(excerpts))

	out := make([]cmdjson.IssueExcerpt, len(excerpts))
	for i, excerpt := range excerpts {
		j, err := cmdjson.NewIssueExcerpt(repo, excerpt)
		if err != nil {
			return nil, err
		}
		out[i] = j
	}

	return jq.Input(out)
}

// IssueGet returns one issue whole, as the command prints it.
func IssueGet(repo *cache.RepoCache, id string) (*cmdjson.IssueSnapshot, error) {
	snap, err := IssueSnapshot(repo, id)
	if err != nil {
		return nil, err
	}

	out := cmdjson.NewIssueSnapshot(snap)
	return &out, nil
}

// IssueSnapshot returns one issue as the entity holds it.
//
// It is what a renderer wants where the JSON projection has lost something,
// an identity's email for instance,
// and it is the same resolution IssueGet does, by prefix or by alias.
func IssueSnapshot(repo *cache.RepoCache, id string) (*issue.Snapshot, error) {
	i, err := resolveIssue(repo, id)
	if err != nil {
		return nil, err
	}

	snap := i.Snapshot()
	if len(snap.Comments) == 0 {
		return nil, errors.New("invalid issue: no comment")
	}

	return snap, nil
}

// IssueLog returns the operations an issue is made of, oldest first.
func IssueLog(repo *cache.RepoCache, id string) ([]cmdjson.IssueOperation, error) {
	i, err := resolveIssue(repo, id)
	if err != nil {
		return nil, err
	}

	ops := i.Snapshot().AllOperations()
	entries := make([]cmdjson.IssueOperation, len(ops))
	for at, op := range ops {
		entry, err := cmdjson.NewIssueOperation(op)
		if err != nil {
			return nil, err
		}
		entries[at] = entry
	}
	return entries, nil
}

// IssueNew creates an issue from a document and returns its id.
func IssueNew(repo *cache.RepoCache, doc IssueDocument) (entity.Id, error) {
	title, fields, err := splitTitle(doc.Fields)
	if err != nil {
		return entity.UnsetId, err
	}

	metadata, err := aliasMetadata(doc.Aliases)
	if err != nil {
		return entity.UnsetId, err
	}

	i, _, err := repo.Issues().NewWithMetadata(
		text.CleanupOneLine(title),
		text.Cleanup(doc.Body),
		fields,
		metadata,
	)
	if err != nil {
		return entity.UnsetId, err
	}

	return i.Id(), nil
}

// IssueSet sets fields of an issue: one SetField operation per key, one commit.
//
// The operations are returned whether they are committed or not,
// because they are what `--dry-run` prints
// and what the same call commits a moment later.
func IssueSet(repo *cache.RepoCache, id string, fields map[string]issue.Value, dryRun bool) ([]issue.Operation, error) {
	i, err := resolveIssue(repo, id)
	if err != nil {
		return nil, err
	}

	ops, err := i.PlanSetFields(fields)
	if err != nil {
		return nil, err
	}

	return commit(i, ops, dryRun)
}

// IssueAdd adds items to list-valued fields, one AddValue operation per item.
func IssueAdd(repo *cache.RepoCache, id string, items map[string][]issue.Value, dryRun bool) ([]issue.Operation, error) {
	i, items, err := resolveIssueAndItems(repo, id, items)
	if err != nil {
		return nil, err
	}

	ops, err := i.PlanAddValues(items)
	if err != nil {
		return nil, err
	}

	return commit(i, ops, dryRun)
}

// IssueRemove is IssueAdd's mirror, one RemoveValue operation per item.
func IssueRemove(repo *cache.RepoCache, id string, items map[string][]issue.Value, dryRun bool) ([]issue.Operation, error) {
	i, items, err := resolveIssueAndItems(repo, id, items)
	if err != nil {
		return nil, err
	}

	ops, err := i.PlanRemoveValues(items)
	if err != nil {
		return nil, err
	}

	return commit(i, ops, dryRun)
}

// IssueArchive sets the archived field, the replicated removal.
func IssueArchive(repo *cache.RepoCache, id string, dryRun bool) ([]issue.Operation, error) {
	return IssueSet(repo, id, map[string]issue.Value{
		issue.ArchivedKey: issue.MustValue(true),
	}, dryRun)
}

// IssueRm deletes an issue's local ref; the issue returns on the next pull.
func IssueRm(repo *cache.RepoCache, id string) error {
	i, err := resolveIssue(repo, id)
	if err != nil {
		return err
	}
	return repo.Issues().Remove(i.Id().String())
}

// IssueCommentNew adds a comment to an issue and returns the comment's id.
func IssueCommentNew(repo *cache.RepoCache, id string, body string) (entity.CombinedId, error) {
	i, err := resolveIssue(repo, id)
	if err != nil {
		return entity.UnsetCombinedId, err
	}

	body = text.Cleanup(body)
	if body == "" {
		return entity.UnsetCombinedId, errors.New("a comment body is required")
	}

	commentId, _, err := i.AddComment(body)
	if err != nil {
		return entity.UnsetCombinedId, err
	}

	if err := i.Commit(); err != nil {
		return entity.UnsetCombinedId, err
	}

	return commentId, nil
}

// IssueCommentEdit replaces the body of one comment, named by its own id.
func IssueCommentEdit(repo *cache.RepoCache, commentId string, body string) error {
	i, target, err := repo.Issues().ResolveComment(commentId)
	if err != nil {
		return err
	}

	body = text.Cleanup(body)
	if body == "" {
		return errors.New("a comment body is required")
	}

	if _, err := i.EditComment(target, body); err != nil {
		return err
	}

	return i.Commit()
}

// commit is the end of every writer: --dry-run writes nothing,
// otherwise the planned operations land as one commit.
func commit(i *cache.IssueCache, ops []issue.Operation, dryRun bool) ([]issue.Operation, error) {
	if dryRun {
		return ops, nil
	}
	if err := i.CommitOperations(ops); err != nil {
		return nil, err
	}
	return ops, nil
}

// resolveIssueAndItems reads the two arguments add and remove share,
// resolving each item that names another issue by a prefix.
func resolveIssueAndItems(repo *cache.RepoCache, id string, items map[string][]issue.Value) (*cache.IssueCache, map[string][]issue.Value, error) {
	i, err := resolveIssue(repo, id)
	if err != nil {
		return nil, nil, err
	}

	for key, list := range items {
		for at, item := range list {
			list[at] = resolveItem(repo, key, item)
		}
	}

	return i, items, nil
}

// resolveItem turns one item of a list-valued field into what is stored.
//
// Without a schema this layer can not know which fields hold issue ids,
// so a string item that is the unambiguous prefix of exactly one issue
// is taken as that issue's full id; anything else is stored as it came.
// Once the schema names the relation kinds (bb9e89e) the lookup is confined to them.
func resolveItem(repo *cache.RepoCache, key string, item issue.Value) issue.Value {
	prefix, ok := issue.String(item)
	if !ok || len(prefix) < 4 {
		return item
	}
	if target, err := repo.Issues().ResolveExcerptPrefix(prefix); err == nil {
		return issue.StringValue(target.Id().String())
	}
	return item
}

// splitTitle takes the title out of the fields map,
// because the create operation carries it in its own member:
// that is what keeps an issue's id equal to the bug's it was migrated from (bf6f392).
func splitTitle(all map[string]issue.Value) (string, map[string]issue.Value, error) {
	raw, ok := all[issue.TitleKey]
	if !ok {
		return "", nil, fmt.Errorf("a title is required, in fields")
	}
	if err := issue.ValidateValue(issue.TitleKey, raw); err != nil {
		return "", nil, err
	}
	title, _ := issue.String(raw)

	fields := make(map[string]issue.Value, len(all))
	for key, value := range all {
		if key == issue.TitleKey {
			continue
		}
		if err := issue.ValidateKey(key); err != nil {
			return "", nil, err
		}
		if err := issue.ValidateValue(key, value); err != nil {
			return "", nil, fmt.Errorf("field %s: %w", key, err)
		}
		fields[key] = value
	}

	return title, fields, nil
}

// aliasMetadata turns the document's aliases into create-op metadata,
// one `alias:<name>` key each.
func aliasMetadata(aliases map[string]string) (map[string]string, error) {
	if len(aliases) == 0 {
		return nil, nil
	}
	metadata := make(map[string]string, len(aliases))
	for name, value := range aliases {
		if err := issue.ValidateKey(name); err != nil {
			return nil, fmt.Errorf("alias: %w", err)
		}
		if value == "" {
			return nil, fmt.Errorf("alias %s is empty", name)
		}
		metadata[cache.AliasMetadataPrefix+name] = value
	}
	return metadata, nil
}

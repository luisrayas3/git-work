package host

import (
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/git-bug/git-bug/cache"
	"github.com/git-bug/git-bug/commands/cmdjson"
	"github.com/git-bug/git-bug/entities/issue"
	"github.com/git-bug/git-bug/entity"
	"github.com/git-bug/git-bug/query/jq"
)

// The history half of the issue API: an issue as it stood at a moment,
// the list as it stood at a moment, and the operations of a window.
//
// A zero time means "no cut" everywhere here:
// a zero `at` is the present, a zero `from` is the beginning of time
// and a zero `to` is the end of it, which is what an absent flag means.
// Design: doc/design/report.md.

// IssueGetAt returns one issue whole as it stood at `at`.
func IssueGetAt(repo *cache.RepoCache, id string, at time.Time) (*cmdjson.IssueSnapshot, error) {
	snap, err := IssueSnapshotAt(repo, id, at)
	if err != nil {
		return nil, err
	}

	out := cmdjson.NewIssueSnapshot(snap)
	return &out, nil
}

// IssueSnapshotAt returns one issue as the entity held it at `at`.
//
// The replay is entities/issue's own (issue.SnapshotAt), because the apply
// rules are that package's and must have one implementation.
// An issue whose create operation is later than `at` did not exist yet,
// and that is an error here: the caller named it.
func IssueSnapshotAt(repo *cache.RepoCache, id string, at time.Time) (*issue.Snapshot, error) {
	i, err := repo.Issues().ResolvePrefixOrAlias(id)
	if err != nil {
		return nil, err
	}

	snap := i.Snapshot()
	if !at.IsZero() {
		snap = issue.SnapshotAt(snap.Operations, at)
		if snap == nil {
			return nil, fmt.Errorf("issue %s did not exist at %s",
				i.Id().Human(), at.Format(time.RFC3339))
		}
	}

	if len(snap.Comments) == 0 {
		return nil, errors.New("invalid issue: no comment")
	}

	return snap, nil
}

// IssueListAt runs a jq program over the issues as they stood at `at`.
func IssueListAt(repo *cache.RepoCache, program string, at time.Time) ([]any, error) {
	if program == "" {
		program = defaultProgram()
	}

	input, err := IssueListInputAt(repo, at)
	if err != nil {
		return nil, err
	}

	return jq.Run(program, input)
}

// IssueListInputAt is IssueListInput as of `at`: every issue replayed to that
// moment, in the same order and the same excerpt shape, so that a program
// written for the present reads the past unchanged.
//
// An issue created after `at` is simply absent, and `archived` is whatever it
// was then, so the default program's `select(.fields.archived != true)`
// filters on the value that stood at the time, with no special case here.
//
// This is linear in the store's operations: every issue is read and replayed
// once, in memory, with no index. Fine for the hundreds of issues this tool is
// built for, a full pass per call beyond that (doc/design/report.md).
func IssueListInputAt(repo *cache.RepoCache, at time.Time) (any, error) {
	if at.IsZero() {
		return IssueListInput(repo)
	}

	excerpts, err := sortedExcerpts(repo)
	if err != nil {
		return nil, err
	}

	out := make([]cmdjson.IssueExcerpt, 0, len(excerpts))
	for _, excerpt := range excerpts {
		i, err := repo.Issues().Resolve(excerpt.Id())
		if err != nil {
			return nil, err
		}
		snap := issue.SnapshotAt(i.Snapshot().Operations, at)
		if snap == nil {
			continue
		}
		out = append(out, cmdjson.NewIssueExcerptAt(snap, excerpt.CreateLamportTime))
	}

	return jq.Input(out)
}

// IssueLogBetween returns the operations of the selected issues written in the
// half-open window [from, to), oldest first.
//
// The argument is one issue — an id prefix or an alias — or a jq program over
// the same array the list runs on, and an empty one is the default program.
// Which it is, is decided by trying: see selectIssues.
//
// The order is each operation's own time, then the issue's id, then the
// operation's place in its own issue, so that two operations written in the
// same second still print in a fixed order.
func IssueLogBetween(repo *cache.RepoCache, idOrProgram string, from, to time.Time) ([]cmdjson.IssueOperation, error) {
	issues, err := selectIssues(repo, idOrProgram)
	if err != nil {
		return nil, err
	}

	// Never nil: an empty window is the empty list, and a reader that gets
	// `null` where it asked for a log has to branch on it.
	entries := []cmdjson.IssueOperation{}
	for _, i := range issues {
		for _, op := range issue.OperationsBetween(i.Snapshot().Operations, from, to) {
			entry, err := cmdjson.NewIssueOperation(i.Id(), op)
			if err != nil {
				return nil, err
			}
			entries = append(entries, entry)
		}
	}

	// Stable, so that the operations of one issue keep the DAG's order within
	// a second; the id breaks the tie between two issues whatever order they
	// were selected in.
	sort.SliceStable(entries, func(a, b int) bool {
		if entries[a].UnixTime != entries[b].UnixTime {
			return entries[a].UnixTime < entries[b].UnixTime
		}
		return entries[a].Issue < entries[b].Issue
	})

	return entries, nil
}

// selectIssues reads the argument every reader that takes "one or many" takes.
//
// An id prefix or an alias is resolved first, because that is the common case
// and because no id prefix is a valid jq program: an id is lowercase hex, and
// a bare hex word parses as nothing. What does not resolve is compiled as a
// program, and when that fails too the error names both attempts, so a
// mistyped id reads as a mistyped id (doc/design/report.md).
func selectIssues(repo *cache.RepoCache, idOrProgram string) ([]*cache.IssueCache, error) {
	if idOrProgram != "" {
		i, resolveErr := repo.Issues().ResolvePrefixOrAlias(idOrProgram)
		if resolveErr == nil {
			return []*cache.IssueCache{i}, nil
		}
		if _, err := jq.Compile(idOrProgram); err != nil {
			return nil, fmt.Errorf("%q is neither an issue (%v) nor a jq program (%v)",
				idOrProgram, resolveErr, err)
		}
	}

	values, err := IssueList(repo, idOrProgram)
	if err != nil {
		return nil, err
	}
	if emptyList(values) {
		return nil, nil
	}

	items, ok := IssueItems(values)
	if !ok {
		return nil, fmt.Errorf("the program did not return a list of issues")
	}

	out := make([]*cache.IssueCache, 0, len(items))
	for _, item := range items {
		i, err := repo.Issues().Resolve(entity.Id(StringOr(item["id"], "")))
		if err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, nil
}

// emptyList reports whether a program returned the empty array, which is a
// selection of no issues and not a shape problem.
func emptyList(values []any) bool {
	if len(values) == 0 {
		return true
	}
	if len(values) != 1 {
		return false
	}
	array, ok := values[0].([]any)
	return ok && len(array) == 0
}

// sortedExcerpts is every issue's live excerpt, oldest first, the order
// IssueListInput hands a program.
func sortedExcerpts(repo *cache.RepoCache) ([]*cache.IssueExcerpt, error) {
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

	return excerpts, nil
}

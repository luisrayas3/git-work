// Package migrate is the one-time store migration (bf6f392):
// the tracker's issues, written by git-bug under refs/issues/*,
// replayed as issues of the owned model under refs/work-issues/*,
// and the identities they are signed by copied to refs/work-users/*.
//
// Ids are preserved by construction and checked,
// lamport times are reproduced exactly,
// and the label taxonomy becomes fields;
// doc/design/store-migration.md is the design.
// The package leaves with entities/bug.
package migrate

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/git-bug/git-bug/entities/bug"
	"github.com/git-bug/git-bug/entities/common"
	"github.com/git-bug/git-bug/entities/identity"
	"github.com/git-bug/git-bug/entities/issue"
	"github.com/git-bug/git-bug/entity"
	"github.com/git-bug/git-bug/entity/dag"
	"github.com/git-bug/git-bug/repository"
	"github.com/git-bug/git-bug/schema"
	"github.com/git-bug/git-bug/util/lamport"
)

// oldRefPrefix is where git-bug wrote the tracker.
const oldRefPrefix = "refs/" + bug.Namespace + "/"

// oldIdentityPrefix is where git-bug wrote the identities,
// before the three constants in entities/identity moved them.
const oldIdentityPrefix = "refs/identities/"

// The clock names entity/dag derives from a namespace
// (creationClockPattern and editClockPattern there).
var (
	createClock = issue.Namespace + "-create"
	editClock   = issue.Namespace + "-edit"
)

// Issue is one issue as the plan will write it.
type Issue struct {
	Id       entity.Id
	Title    string
	Type     string
	Fields   map[string]issue.Value
	Comments int
	Packs    int
	// Problems are what the schema would refuse about the final fields:
	// facts about the old data, reported and not fatal.
	Problems []string

	commentIds []entity.CombinedId
	packs      []*issuePack
}

// issuePack is one original commit, converted.
type issuePack struct {
	issueId    entity.Id
	createTime lamport.Time
	editTime   lamport.Time
	ops        []issue.Operation
}

// Plan is everything the migration will write, computed and checked
// before anything is.
type Plan struct {
	// Issues in creation order.
	Issues []*Issue
	// packs in replay order: global edit order.
	packs []*issuePack
}

// CopyIdentities copies every refs/identities/* to refs/work-users/*,
// skipping the ones already there, and returns how many it copied.
//
// It runs before anything reads the old store,
// because the old issues resolve their authors through the new constants.
func CopyIdentities(repo repository.RepoData) (int, error) {
	refs, err := repo.ListRefs(oldIdentityPrefix)
	if err != nil {
		return 0, err
	}
	copied := 0
	for _, ref := range refs {
		dest := "refs/" + identity.Namespace + "/" + strings.TrimPrefix(ref, oldIdentityPrefix)
		exists, err := repo.RefExist(dest)
		if err != nil {
			return copied, err
		}
		if exists {
			continue
		}
		if err := repo.CopyRef(ref, dest); err != nil {
			return copied, err
		}
		copied++
	}
	return copied, nil
}

// Prepare reads the old store and computes the plan.
//
// It refuses when refs/work-issues/* holds anything,
// because the migration runs once,
// and when the schema lacks a key the mapping writes into.
func Prepare(repo repository.ClockedRepo, s *schema.Schema) (*Plan, error) {
	if err := CheckSchema(s); err != nil {
		return nil, err
	}

	existing, err := issue.ListLocalIds(repo)
	if err != nil {
		return nil, err
	}
	if len(existing) > 0 {
		return nil, fmt.Errorf("refs/%s/* already holds %d issues: the store is migrated", issue.Namespace, len(existing))
	}

	oldIds, err := bug.ListLocalIds(repo)
	if err != nil {
		return nil, err
	}
	resolve := prefixResolver(oldIds)

	identityIds, err := identity.ListLocalIds(repo)
	if err != nil {
		return nil, err
	}

	plan := &Plan{}
	for _, id := range oldIds {
		converted, err := convertBug(repo, id, resolve)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", id.Human(), err)
		}
		plan.Issues = append(plan.Issues, converted)
		plan.packs = append(plan.packs, converted.packs...)
	}

	sort.SliceStable(plan.Issues, func(a, b int) bool {
		return plan.Issues[a].packs[0].createTime < plan.Issues[b].packs[0].createTime
	})
	sort.SliceStable(plan.packs, func(a, b int) bool {
		pa, pb := plan.packs[a], plan.packs[b]
		if pa.editTime != pb.editTime {
			return pa.editTime < pb.editTime
		}
		if pa.createTime != pb.createTime {
			return pa.createTime < pb.createTime
		}
		return pa.issueId < pb.issueId
	})

	plan.check(s, identityIds)
	return plan, nil
}

// prefixResolver finds the one old id a story: label's prefix names.
func prefixResolver(ids []entity.Id) func(prefix string) (entity.Id, error) {
	return func(prefix string) (entity.Id, error) {
		var matches []entity.Id
		for _, id := range ids {
			if strings.HasPrefix(id.String(), prefix) {
				matches = append(matches, id)
			}
		}
		switch len(matches) {
		case 1:
			return matches[0], nil
		case 0:
			return "", fmt.Errorf("story:%s names no issue", prefix)
		default:
			return "", fmt.Errorf("story:%s is ambiguous", prefix)
		}
	}
}

// convertBug reads one old entity and converts its packs.
func convertBug(repo repository.ClockedRepo, id entity.Id, resolve func(string) (entity.Id, error)) (*Issue, error) {
	b, err := bug.Read(repo, id)
	if err != nil {
		return nil, err
	}
	packs, err := readPacks(repo, oldRefPrefix+id.String())
	if err != nil {
		return nil, err
	}

	byId := make(map[entity.Id]bug.Operation)
	var labelAdds [][]string
	for _, op := range b.Operations() {
		byId[op.Id()] = op
		if lc, ok := op.(*bug.LabelChangeOperation); ok {
			labelAdds = append(labelAdds, labelStrings(lc.Added))
		}
	}
	typeLabel, typeKey := firstType(labelAdds)

	converted := &Issue{Id: id, Type: typeKey}
	state := newLabelState()
	// the create pack sets the type, so the label that named it changes nothing when it arrives
	state.fields[schema.TypeKey] = typeLabel
	seen := 0
	for _, p := range packs {
		target := &issuePack{issueId: id, createTime: p.createTime, editTime: p.editTime}
		for _, opId := range p.opIds {
			op, ok := byId[opId]
			if !ok {
				return nil, fmt.Errorf("operation %s is in commit %s but not in the entity", opId.Human(), p.commit)
			}
			seen++
			ops, err := convertOp(op, typeKey, state, resolve)
			if err != nil {
				return nil, err
			}
			target.ops = append(target.ops, ops...)
		}
		converted.packs = append(converted.packs, target)
	}
	if seen != len(b.Operations()) {
		return nil, fmt.Errorf("%d operations in the commits, %d in the entity", seen, len(b.Operations()))
	}

	// the final state, as the ops will leave it
	snap := &issue.Snapshot{Fields: map[string]issue.Value{}}
	for _, p := range converted.packs {
		for _, op := range p.ops {
			op.Apply(snap)
		}
	}
	converted.Title = snap.Title()
	converted.Fields = snap.Fields
	converted.Comments = len(snap.Comments)
	converted.Packs = len(converted.packs)
	for _, comment := range b.Compile().Comments {
		converted.commentIds = append(converted.commentIds, comment.CombinedId())
	}
	return converted, nil
}

func labelStrings(labels []common.Label) []string {
	out := make([]string, len(labels))
	for i, l := range labels {
		out[i] = l.String()
	}
	return out
}

// convertOp gives one old operation its meaning in the new model.
//
// Create, AddComment and EditComment keep their bytes, and so their ids:
// the new operation takes the old one's base, nonce included,
// and the bytes are compared to be sure.
// The create pack also gets the issue's type and its status,
// because the new model has both from the start
// where git-bug had no type and an implicit open.
func convertOp(op bug.Operation, typeKey string, state *labelState, resolve func(string) (entity.Id, error)) ([]issue.Operation, error) {
	author := op.Author()
	switch old := op.(type) {
	case *bug.CreateOperation:
		created := issue.NewCreateOp(author, old.UnixTime, old.Title, old.Message, old.Files, nil)
		created.OpBase = old.OpBase
		if err := sameBytes(old, created); err != nil {
			return nil, err
		}
		return []issue.Operation{
			created,
			issue.NewSetFieldOp(author, old.UnixTime, schema.TypeKey, issue.StringValue(typeKey)),
			issue.NewSetFieldOp(author, old.UnixTime, statusKey, issue.StringValue(statusValues["open"])),
		}, nil

	case *bug.AddCommentOperation:
		added := issue.NewAddCommentOp(author, old.UnixTime, old.Message, old.Files)
		added.OpBase = old.OpBase
		if err := sameBytes(old, added); err != nil {
			return nil, err
		}
		return []issue.Operation{added}, nil

	case *bug.EditCommentOperation:
		edited := issue.NewEditCommentOp(author, old.UnixTime, old.Target, old.Message, old.Files)
		edited.OpBase = old.OpBase
		if err := sameBytes(old, edited); err != nil {
			return nil, err
		}
		return []issue.Operation{edited}, nil

	case *bug.SetTitleOperation:
		set := issue.NewSetFieldOp(author, old.UnixTime, issue.TitleKey, issue.StringValue(old.Title))
		set.Metadata = old.Metadata
		return []issue.Operation{set}, nil

	case *bug.SetStatusOperation:
		value, ok := statusValues[old.Status.String()]
		if !ok {
			return nil, fmt.Errorf("status %q has no mapping", old.Status.String())
		}
		set := issue.NewSetFieldOp(author, old.UnixTime, statusKey, issue.StringValue(value))
		set.Metadata = old.Metadata
		return []issue.Operation{set}, nil

	case *bug.LabelChangeOperation:
		ops, err := convertLabels(state, author, old.UnixTime, labelStrings(old.Added), labelStrings(old.Removed), resolve)
		if err != nil {
			return nil, err
		}
		if len(old.Metadata) > 0 {
			return nil, fmt.Errorf("label change %s carries metadata, which has no place to go", old.Id().Human())
		}
		return ops, nil

	default:
		return nil, fmt.Errorf("operation %s of type %d has no mapping", op.Id().Human(), op.Type())
	}
}

// sameBytes is the id guarantee, checked: two operations with the same JSON
// get the same id, because that is what an id hashes.
func sameBytes(old, converted dag.Operation) error {
	oldBytes, err := json.Marshal(old)
	if err != nil {
		return err
	}
	newBytes, err := json.Marshal(converted)
	if err != nil {
		return err
	}
	if !bytes.Equal(oldBytes, newBytes) {
		return fmt.Errorf("operation %s would not keep its id: %s became %s", old.Id().Human(), oldBytes, newBytes)
	}
	return nil
}

// check runs every issue's final fields through the schema,
// the way `git work issue new` would, and records what it refuses.
func (p *Plan) check(s *schema.Schema, identities []entity.Id) {
	resolver := planResolver{
		types:      make(map[entity.Id]string, len(p.Issues)),
		identities: make(map[string]struct{}, len(identities)),
	}
	for _, i := range p.Issues {
		resolver.types[i.Id] = i.Type
	}
	for _, id := range identities {
		resolver.identities[id.String()] = struct{}{}
	}
	checker := &schema.Checker{Schema: s, Resolver: resolver}

	for _, i := range p.Issues {
		if err := checker.CheckNew(i.Fields); err != nil {
			i.Problems = append(i.Problems, err.Error())
		}
	}
}

// planResolver answers the checker from the plan itself,
// because nothing is in the cache yet.
type planResolver struct {
	types      map[entity.Id]string
	identities map[string]struct{}
}

func (r planResolver) IdentityExists(id string) error {
	if _, ok := r.identities[id]; !ok {
		return fmt.Errorf("%s is not an identity this repository knows", id)
	}
	return nil
}

func (r planResolver) IssueType(id string) (string, error) {
	var matches []string
	for known, typeKey := range r.types {
		if strings.HasPrefix(known.String(), id) {
			matches = append(matches, typeKey)
		}
	}
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return "", fmt.Errorf("%s names no issue", id)
	default:
		return "", fmt.Errorf("%s is ambiguous", id)
	}
}

// Apply writes the plan: one commit per original pack, in global edit order,
// with the original lamport times, then reads every issue back
// and checks its id, its comment ids and its times.
// On any failure the refs written so far are removed,
// so the store is as it was.
func (p *Plan) Apply(repo repository.ClockedRepo) (err error) {
	sched := &scheduledClocks{ClockedRepo: repo}
	entities := make(map[entity.Id]*issue.Issue, len(p.Issues))
	var written []string

	defer func() {
		if err == nil {
			return
		}
		for _, ref := range written {
			_ = repo.RemoveRef(ref)
		}
	}()

	for _, pack := range p.packs {
		e, ok := entities[pack.issueId]
		if !ok {
			e = issue.NewIssue()
			entities[pack.issueId] = e
			sched.schedule(createClock, pack.createTime)
			written = append(written, "refs/"+issue.Namespace+"/"+pack.issueId.String())
		}
		for _, op := range pack.ops {
			e.Append(op)
		}
		sched.schedule(editClock, pack.editTime)
		if err := e.Commit(sched); err != nil {
			return fmt.Errorf("%s: %w", pack.issueId.Human(), err)
		}
	}

	for _, want := range p.Issues {
		got, err := issue.Read(repo, want.Id)
		if err != nil {
			return fmt.Errorf("%s: reading it back: %w", want.Id.Human(), err)
		}
		snap := got.Compile()
		if len(snap.Comments) != len(want.commentIds) {
			return fmt.Errorf("%s: %d comments, had %d", want.Id.Human(), len(snap.Comments), len(want.commentIds))
		}
		for i, comment := range snap.Comments {
			if comment.CombinedId() != want.commentIds[i] {
				return fmt.Errorf("%s: comment %s became %s", want.Id.Human(), want.commentIds[i].Human(), comment.CombinedId().Human())
			}
		}
		first, last := want.packs[0], want.packs[len(want.packs)-1]
		if got.CreateLamportTime() != first.createTime || got.EditLamportTime() != last.editTime {
			return fmt.Errorf("%s: lamport times %d/%d, had %d/%d", want.Id.Human(),
				got.CreateLamportTime(), got.EditLamportTime(), first.createTime, last.editTime)
		}
	}
	return nil
}

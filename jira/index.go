package jira

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/git-bug/git-bug/cache"
	"github.com/git-bug/git-bug/entities/issue"
	"github.com/git-bug/git-bug/entity"
	"github.com/git-bug/git-bug/schema"
)

// Index resolves Jira ids to entity ids and back
// (doc/design/jira.md, "Relations, and values that cannot convert"):
// issues by the jira-id of their create operation,
// identities by jira-account-id.
// It is built once per run and extended as the run imports and creates.
// Of several issues naming one Jira id,
// the one that reached Jira first is the Jira id's;
// a Jira id whose copies the rule cannot order has none, and is unresolved.
type Index struct {
	issues     map[string]entity.Id // Jira issue id -> issue
	jiraIssues map[entity.Id]string
	users      map[string]entity.Id // accountId -> identity
	accounts   map[entity.Id]string
	absent     map[string]Orphan      // Jira issue id -> the orphan the state remembers
	unresolved map[string][]entity.Id // Jira issue id -> copies the rule cannot order
}

// NewIndex reads every excerpt once.
func NewIndex(repo *cache.RepoCache) (*Index, error) {
	copies, users := map[string][]entity.Id{}, map[string]entity.Id{}
	for _, id := range repo.Issues().AllIds() {
		e, err := repo.Issues().ResolveExcerpt(id)
		if err != nil {
			return nil, err
		}
		if jid := e.CreateMetadata[MetaId]; jid != "" {
			copies[jid] = append(copies[jid], id)
		}
	}
	issues, unresolved := map[string]entity.Id{}, map[string][]entity.Id{}
	for jid, ids := range copies {
		// several issues naming one Jira id: the store's rule picks the
		// winner, and scan consolidates the others; copies the rule cannot
		// order are left to a person
		// (doc/design/jira.md, "Copies the rule cannot order are skipped")
		winner, err := repo.Issues().FirstReached(Reach, ids)
		switch {
		case entity.IsErrMultipleMatch(err):
			sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
			unresolved[jid] = ids
		case err != nil:
			return nil, err
		default:
			issues[jid] = winner
		}
	}
	var holders []entity.Id
	var accounts []string
	for _, id := range repo.Identities().AllIds() {
		e, err := repo.Identities().ResolveExcerpt(id)
		if err != nil {
			return nil, err
		}
		if account := e.ImmutableMetadata[MetaAccountId]; account != "" {
			holders = append(holders, id)
			accounts = append(accounts, account)
			// two identities naming one account (two clones,
			// doc/design/jira.md, "One bound clone"): the
			// lower id is the account's, and both export as it
			if other, ok := users[account]; !ok || id < other {
				users[account] = id
			}
		}
	}
	ix := indexOf(issues, users)
	for i, id := range holders {
		ix.accounts[id] = accounts[i]
	}
	ix.unresolved = unresolved
	return ix, nil
}

// Unresolved is the copies of a Jira id the rule cannot order, sorted:
// two or more issues naming it, none of which reached Jira, and not exactly
// one of them unarchived
// (doc/design/jira.md, "Copies the rule cannot order are skipped").
func (ix *Index) Unresolved(jiraId string) ([]entity.Id, bool) {
	ids, ok := ix.unresolved[jiraId]
	return ids, ok
}

// unorderable is the reason an unresolved Jira id is skipped, naming every
// copy.
func unorderable(ids []entity.Id) string {
	names := make([]string, len(ids))
	for i, id := range ids {
		names[i] = id.Human()
	}
	return fmt.Sprintf("%d local copies of one Jira issue (%s) and no way to order them; archive all but one",
		len(ids), strings.Join(names, ", "))
}

// indexOf builds an index from its tables, for tests.
func indexOf(issues, users map[string]entity.Id) *Index {
	ix := &Index{
		issues: map[string]entity.Id{}, jiraIssues: map[entity.Id]string{},
		users: map[string]entity.Id{}, accounts: map[entity.Id]string{},
	}
	for jid, id := range issues {
		ix.addIssue(jid, id)
	}
	for account, id := range users {
		ix.addUser(account, id)
	}
	return ix
}

func isArchived(fields map[string]issue.Value) bool {
	return string(canon(fields[schema.ArchivedKey])) == "true"
}

func (ix *Index) Issue(jiraId string) (entity.Id, bool) {
	id, ok := ix.issues[jiraId]
	return id, ok
}

func (ix *Index) jiraIssue(id entity.Id) (string, bool) {
	jid, ok := ix.jiraIssues[id]
	return jid, ok
}

func (ix *Index) User(accountId string) (entity.Id, bool) {
	id, ok := ix.users[accountId]
	return id, ok
}

func (ix *Index) Account(id entity.Id) (string, bool) {
	account, ok := ix.accounts[id]
	return account, ok
}

func (ix *Index) addIssue(jiraId string, id entity.Id) {
	ix.issues[jiraId] = id
	ix.jiraIssues[id] = jiraId
}

func (ix *Index) addUser(accountId string, id entity.Id) {
	ix.users[accountId] = id
	ix.accounts[id] = accountId
}

// Absent is the orphan the state remembers for a Jira id:
// an issue created from an entity this clone does not have.
func (ix *Index) Absent(jiraId string) (Orphan, bool) {
	o, ok := ix.absent[jiraId]
	return o, ok
}

// waiting is why a relation target is not imported yet,
// naming the orphan skip that causes it when the state remembers one.
func (ix *Index) waiting(jiraId, key string) string {
	if ids, ok := ix.unresolved[jiraId]; ok {
		return key + " is skipped: " + unorderable(ids)
	}
	o, ok := ix.absent[jiraId]
	if !ok {
		return key + " is not imported yet"
	}
	return fmt.Sprintf("%s is not imported yet: created in Jira on %s from issue %s, which this clone has not pulled; pull first, or --adopt takes it",
		key, o.Created.UTC().Format("2006-01-02"), o.From.Human())
}

// Namespace is the alias namespace the sync writes Jira keys in:
// the <name> of MetaAlias.
const Namespace = "jira"

// Register gives the store Jira's ordering hook, so that a Jira key names
// the copy consolidation keeps (doc/design/cli.md, "The copy that reached
// the system first is canonical"). Whatever opens a store for a command
// calls it; the sync's own consolidation passes Reach itself.
func Register(repo *cache.RepoCache) {
	repo.Issues().SetReach(Namespace, Reach)
}

// Reach is Jira's ordering hook: a copy's first contact with Jira, on
// Jira's clock, to the second, an export's attempt ranked before an import's
// read at one second, since its POST is what the read saw; false when the
// copy has never reached Jira
// (doc/design/jira.md, "Consolidation keeps the copy that reached Jira first").
// Every input is an immutable fact of the copy's own history,
// so every clone holding the copies agrees, whatever any has archived.
func Reach(snap *issue.Snapshot) (cache.Reach, bool) {
	first := firstSync(snap)
	if first.at.IsZero() {
		return cache.Reach{}, false
	}
	jid, _ := snap.GetCreateMetadata(MetaId)
	rank := 1
	if first.export {
		rank = 0
	}
	return cache.Reach{Issue: jid, At: first.at, Rank: rank}, true
}

// firstToSync is the winner of two issues naming one Jira id: the store's
// rule over Reach, which breaks a tie by the lower entity id.
func firstToSync(repo *cache.RepoCache, a, b entity.Id) (entity.Id, error) {
	return repo.Issues().FirstReached(Reach, []entity.Id{a, b})
}

// contact is an issue's first contact with Jira:
// when, on Jira's clock, truncated to the second like a create attempt,
// and whether it was an export's attempt rather than an import's read.
type contact struct {
	at     time.Time
	export bool
}

// firstSync is the earliest of an issue's jira-create attempts
// and its markers' Updated; zero when it has neither.
func firstSync(snap *issue.Snapshot) contact {
	var first contact
	earliest := func(t time.Time, export bool) {
		t = t.UTC().Truncate(time.Second)
		if t.IsZero() {
			return
		}
		if first.at.IsZero() || t.Before(first.at) {
			first = contact{t, export}
		} else if t.Equal(first.at) && export {
			first.export = true
		}
	}
	for _, op := range snap.Operations {
		if v, ok := op.GetMetadata(MetaCreate); ok {
			if t, err := time.Parse(time.RFC3339, v); err == nil {
				earliest(t, true)
			}
		}
		if raw, ok := op.GetMetadata(MetaSync); ok {
			var b Base
			if json.Unmarshal([]byte(raw), &b) == nil {
				earliest(b.Updated, false)
			}
		}
	}
	return first
}

package jira

import (
	"github.com/git-bug/git-bug/cache"
	"github.com/git-bug/git-bug/entities/issue"
	"github.com/git-bug/git-bug/entity"
	"github.com/git-bug/git-bug/schema"
)

// Index resolves Jira ids to entity ids and back (JS17): issues by the
// jira-id of their create operation, identities by jira-account-id. It is
// built once per run and extended as the run imports and creates.
type Index struct {
	issues     map[string]entity.Id // Jira issue id -> issue
	jiraIssues map[entity.Id]string
	users      map[string]entity.Id // accountId -> identity
	accounts   map[entity.Id]string
}

// NewIndex reads every excerpt once.
func NewIndex(repo *cache.RepoCache) (*Index, error) {
	issues, users := map[string]entity.Id{}, map[string]entity.Id{}
	archived := map[entity.Id]bool{}
	for _, id := range repo.Issues().AllIds() {
		e, err := repo.Issues().ResolveExcerpt(id)
		if err != nil {
			return nil, err
		}
		archived[id] = isArchived(e.Fields)
		if jid := e.CreateMetadata[MetaId]; jid != "" {
			// two issues naming one Jira id: the unarchived, then the lower
			// entity id wins; scan reports the other (JS25)
			if other, ok := issues[jid]; !ok || archived[other] && !archived[id] ||
				archived[other] == archived[id] && id < other {
				issues[jid] = id
			}
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
			// two identities naming one account (two clones, JS25): the
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
	return ix, nil
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

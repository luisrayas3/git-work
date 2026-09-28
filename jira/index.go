package jira

import (
	"github.com/git-bug/git-bug/cache"
	"github.com/git-bug/git-bug/entities/issue"
	"github.com/git-bug/git-bug/entity"
	"github.com/git-bug/git-bug/schema"
)

// Index resolves Jira ids to entity ids and back (JS17): issues by the
// jira-id of their create operation, identities by jira-account-id, and the
// unlinked issues this run may export. It is built once per run and
// extended as the run imports and creates.
type Index struct {
	issues     map[string]entity.Id // Jira issue id -> issue
	jiraIssues map[entity.Id]string
	users      map[string]entity.Id // accountId -> identity
	accounts   map[entity.Id]string
	exportable map[entity.Id]bool
}

// NewIndex reads every excerpt once.
func NewIndex(repo *cache.RepoCache, m *Mapping) (*Index, error) {
	issues, users := map[string]entity.Id{}, map[string]entity.Id{}
	var exportable []entity.Id
	for _, id := range repo.Issues().AllIds() {
		e, err := repo.Issues().ResolveExcerpt(id)
		if err != nil {
			return nil, err
		}
		if jid := e.CreateMetadata[MetaId]; jid != "" {
			// two issues naming one Jira id: the lower entity id wins (JS25)
			if other, ok := issues[jid]; !ok || id < other {
				issues[jid] = id
			}
			continue
		}
		typeKey, _ := issue.String(e.Fields[schema.TypeKey])
		archived := string(e.Fields[schema.ArchivedKey]) == "true"
		if _, mapped := m.IssueType(typeKey); mapped && !archived {
			exportable = append(exportable, id)
		}
	}
	for _, id := range repo.Identities().AllIds() {
		e, err := repo.Identities().ResolveExcerpt(id)
		if err != nil {
			return nil, err
		}
		if account := e.ImmutableMetadata[MetaAccountId]; account != "" {
			if other, ok := users[account]; !ok || id < other {
				users[account] = id
			}
		}
	}
	return IndexOf(issues, users, exportable), nil
}

// IndexOf builds an index from its tables, for tests.
func IndexOf(issues, users map[string]entity.Id, exportable []entity.Id) *Index {
	ix := &Index{
		issues: map[string]entity.Id{}, jiraIssues: map[entity.Id]string{},
		users: map[string]entity.Id{}, accounts: map[entity.Id]string{},
		exportable: map[entity.Id]bool{},
	}
	for jid, id := range issues {
		ix.AddIssue(jid, id)
	}
	for account, id := range users {
		ix.AddUser(account, id)
	}
	for _, id := range exportable {
		ix.exportable[id] = true
	}
	return ix
}

func (ix *Index) Issue(jiraId string) (entity.Id, bool) {
	id, ok := ix.issues[jiraId]
	return id, ok
}

func (ix *Index) JiraIssue(id entity.Id) (string, bool) {
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

// WillExport says an unlinked issue of a mapped type, unarchived, will be
// created this run: a relation to it is a Skip to retry, not a dead end.
func (ix *Index) WillExport(id entity.Id) bool { return ix.exportable[id] }

func (ix *Index) AddIssue(jiraId string, id entity.Id) {
	ix.issues[jiraId] = id
	ix.jiraIssues[id] = jiraId
	delete(ix.exportable, id)
}

func (ix *Index) AddUser(accountId string, id entity.Id) {
	ix.users[accountId] = id
	ix.accounts[id] = accountId
}

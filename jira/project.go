package jira

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/git-bug/git-bug/jira/jiraapi"
)

// Project is one Jira project, normalised and sorted at read (JS4),
// so that everything derived from it is deterministic.
type Project struct {
	Id, Key    string
	Me         jiraapi.User            // TimeZone formats JQL literals (JS20)
	IssueTypes []IssueType             // (Level desc, Id)
	Fields     []jiraapi.Field         // visible, scope-filtered, by Id
	Priorities []jiraapi.Priority      // Jira's order
	LinkTypes  []jiraapi.IssueLinkType // nil when linking is disabled
}

// IssueType is one issue type of the project, with its workflow's statuses
// and its create screen.
type IssueType struct {
	Id, Name, Description string
	Level                 int
	Statuses              []jiraapi.Status    // workflow order
	Screen                []jiraapi.FieldMeta // the create screen
}

// Discover reads one project in seven kinds of call (JS4). Any failure fails
// it naming the permission; only a 404 on the link types is not one, because
// it means linking is disabled.
func Discover(ctx context.Context, c *jiraapi.Client, projectKey string) (*Project, error) {
	fail := func(what, permission string, err error) error {
		return fmt.Errorf("jira discovery: %s (needs %s): %w", what, permission, err)
	}

	me, err := c.Myself(ctx)
	if err != nil {
		return nil, fail("reading /myself", "a valid API token", err)
	}
	if me.AccountID == "" {
		return nil, fmt.Errorf("jira discovery: /myself answered with no account; the request was anonymous (I4)")
	}

	rp, err := c.Project(ctx, projectKey)
	if err != nil {
		return nil, fail("reading project "+projectKey, "Browse projects", err)
	}
	p := &Project{Id: rp.ID, Key: rp.Key, Me: *me}

	statuses, err := c.ProjectStatuses(ctx, rp.Key)
	if err != nil {
		return nil, fail("reading the statuses of "+rp.Key, "Browse projects", err)
	}
	byType := map[string][]jiraapi.Status{}
	for _, s := range statuses {
		byType[s.ID] = s.Statuses
	}

	for _, it := range rp.IssueTypes {
		screen, err := c.CreateMetaFields(ctx, rp.Key, it.ID)
		if err != nil {
			return nil, fail("reading the create screen of "+it.Name, "Create issues", err)
		}
		if len(screen) == 0 {
			// Jira answers a caller without Create issues with an empty screen
			return nil, fail("reading the create screen of "+it.Name, "Create issues", errors.New("the screen is empty"))
		}
		slices.SortFunc(screen, func(a, b jiraapi.FieldMeta) int { return cmpId(metaId(a), metaId(b)) })
		p.IssueTypes = append(p.IssueTypes, IssueType{
			Id: it.ID, Name: it.Name, Description: it.Description, Level: it.HierarchyLevel,
			Statuses: byType[it.ID], Screen: screen,
		})
	}
	slices.SortFunc(p.IssueTypes, func(a, b IssueType) int {
		if a.Level != b.Level {
			return b.Level - a.Level
		}
		return cmpId(a.Id, b.Id)
	})

	fields, err := c.Fields(ctx)
	if err != nil {
		return nil, fail("reading /field", "Browse projects", err)
	}
	for _, f := range fields {
		// a team-managed field belongs to one project: keep only ours
		if f.Scope == nil || f.Scope.Project == nil || f.Scope.Project.ID == rp.ID {
			p.Fields = append(p.Fields, f)
		}
	}
	slices.SortFunc(p.Fields, func(a, b jiraapi.Field) int { return cmpId(a.ID, b.ID) })

	if p.Priorities, err = c.Priorities(ctx, rp.ID); err != nil {
		return nil, fail("reading the priorities of "+rp.Key, "Browse projects", err)
	}

	switch links, err := c.IssueLinkTypes(ctx); {
	case jiraapi.StatusCode(err) == http.StatusNotFound:
	case err != nil:
		return nil, fail("reading /issueLinkType", "Browse projects", err)
	default:
		slices.SortFunc(links, func(a, b jiraapi.IssueLinkType) int { return cmpId(a.ID, b.ID) })
		p.LinkTypes = links
	}

	return p, nil
}

// Field returns one visible field by id.
func (p *Project) Field(id string) (jiraapi.Field, bool) {
	i := slices.IndexFunc(p.Fields, func(f jiraapi.Field) bool { return f.ID == id })
	if i < 0 {
		return jiraapi.Field{}, false
	}
	return p.Fields[i], true
}

// IssueType returns one issue type by id.
func (p *Project) IssueType(id string) (IssueType, bool) {
	i := slices.IndexFunc(p.IssueTypes, func(t IssueType) bool { return t.Id == id })
	if i < 0 {
		return IssueType{}, false
	}
	return p.IssueTypes[i], true
}

// OnScreen reports whether a field is on the create screen.
func (t IssueType) OnScreen(fieldId string) (jiraapi.FieldMeta, bool) {
	i := slices.IndexFunc(t.Screen, func(m jiraapi.FieldMeta) bool { return metaId(m) == fieldId })
	if i < 0 {
		return jiraapi.FieldMeta{}, false
	}
	return t.Screen[i], true
}

func metaId(m jiraapi.FieldMeta) string {
	if m.FieldID != "" {
		return m.FieldID
	}
	return m.Key
}

// Owns says a Jira key is this project's, by its prefix.
func (p *Project) Owns(key string) bool { return strings.HasPrefix(key, p.Key+"-") }

// cmpId orders Jira ids numerically where they are numbers, "3" before
// "10000", and textually otherwise, `customfield_9` before `customfield_10`.
func cmpId(a, b string) int {
	pa, na := splitDigits(a)
	pb, nb := splitDigits(b)
	if c := strings.Compare(pa, pb); c != 0 {
		return c
	}
	if len(na) != len(nb) {
		return len(na) - len(nb)
	}
	return strings.Compare(na, nb)
}

func splitDigits(s string) (prefix, digits string) {
	i := len(s)
	for i > 0 && s[i-1] >= '0' && s[i-1] <= '9' {
		i--
	}
	return s[:i], strings.TrimLeft(s[i:], "0")
}

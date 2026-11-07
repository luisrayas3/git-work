package jira

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/jira/jiraapi"
	"github.com/git-bug/git-bug/jira/jiratest"
)

var sites = map[string]struct {
	site func() jiratest.Site
	key  string
}{
	"company": {jiratest.CompanySite, "PROJ"},
	"team":    {jiratest.TeamSite, "TEAM"},
}

// discover runs Discover against a fake of one fixture site.
func discover(t *testing.T, name string, opts ...jiratest.Option) (*Project, *jiratest.Server) {
	t.Helper()
	s := sites[name]
	srv := jiratest.New(t, append([]jiratest.Option{jiratest.WithSite(s.site())}, opts...)...)
	email, token := srv.Credentials()
	c := jiraapi.New(jiraapi.Config{BaseURL: srv.URL(), Email: email, Token: token, MaxRetries: -1})
	p, err := Discover(context.Background(), c, s.key)
	require.NoError(t, err)
	return p, srv
}

// one golden per fixture (doc/design/jira.md, "Discovery").
func TestDiscover(t *testing.T) {
	for name := range sites {
		t.Run(name, func(t *testing.T) {
			p, srv := discover(t, name)
			out, err := json.MarshalIndent(p, "", "  ")
			require.NoError(t, err)
			checkGolden(t, "discover-"+name+".json", bytes.ReplaceAll(out, []byte(srv.URL()), []byte("https://jira.test")))
		})
	}
}

// v1 must create issues, so a forbidden create screen fails discovery
// (doc/design/jira.md, "Discovery").
func TestDiscoverForbiddenCreateMeta(t *testing.T) {
	s := sites["company"]
	srv := jiratest.New(t, jiratest.WithSite(s.site()), jiratest.WithDenied("getCreateIssueMetaIssueTypeId"))
	email, token := srv.Credentials()
	c := jiraapi.New(jiraapi.Config{BaseURL: srv.URL(), Email: email, Token: token, MaxRetries: -1})
	_, err := Discover(context.Background(), c, s.key)
	require.ErrorContains(t, err, "Create issues")
}

// a forbidden read fails naming the permission; nothing degrades
// (doc/design/jira.md, "Discovery").
func TestDiscoverForbiddenProject(t *testing.T) {
	s := sites["company"]
	srv := jiratest.New(t, jiratest.WithSite(s.site()), jiratest.WithDenied("getProject"))
	email, token := srv.Credentials()
	c := jiraapi.New(jiraapi.Config{BaseURL: srv.URL(), Email: email, Token: token, MaxRetries: -1})
	_, err := Discover(context.Background(), c, s.key)
	require.ErrorContains(t, err, "Browse projects")
}

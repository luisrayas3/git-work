package jiraapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// Search is the body of POST /rest/api/3/search/jql (§2.1), minus the page
// token, which SearchJQL manages.
type Search struct {
	JQL string
	// Fields defaults to id only on the server; always name them.
	Fields     []string
	Expand     []string // sent comma-delimited, as the endpoint wants
	Properties []string // at most 5
	MaxResults int      // advisory; pages can be shorter
	// ReconcileIssues are numeric issue ids (at most 50) read with strong
	// consistency; the same list goes with every page (§2.4, §17.7).
	ReconcileIssues []int64
}

// SearchPage is one page of results. Names and Schema are set when Expand
// asks for them.
type SearchPage struct {
	Issues []Issue                `json:"issues"`
	Names  map[string]string      `json:"names"`
	Schema map[string]FieldSchema `json:"schema"`
}

// SearchJQL runs s and calls fn with each page, in order, until the last
// page or until fn returns an error, which SearchJQL returns. The last page
// is the one with isLast true or with nextPageToken missing, null or empty
// (R13); a page may be empty without being the last.
func (c *Client) SearchJQL(ctx context.Context, s Search, fn func(SearchPage) error) error {
	body := struct {
		JQL             string   `json:"jql"`
		Fields          []string `json:"fields,omitempty"`
		Expand          string   `json:"expand,omitempty"`
		Properties      []string `json:"properties,omitempty"`
		MaxResults      int      `json:"maxResults,omitempty"`
		ReconcileIssues []int64  `json:"reconcileIssues,omitempty"`
		NextPageToken   string   `json:"nextPageToken,omitempty"`
	}{s.JQL, s.Fields, strings.Join(s.Expand, ","), s.Properties, s.MaxResults, s.ReconcileIssues, ""}
	seen := map[string]bool{}
	for {
		var page struct {
			SearchPage
			NextPageToken *string `json:"nextPageToken"`
			IsLast        bool    `json:"isLast"`
		}
		// A search only reads, so a 503 may be retried like a GET.
		_, err := c.do(ctx, request{method: http.MethodPost, path: "/rest/api/3/search/jql", body: body, out: &page, safe: true})
		if err != nil {
			return err
		}
		if err := fn(page.SearchPage); err != nil {
			return err
		}
		if page.IsLast || page.NextPageToken == nil || *page.NextPageToken == "" {
			return nil
		}
		if seen[*page.NextPageToken] {
			return errors.New("jira: search returned a page token twice")
		}
		seen[*page.NextPageToken] = true
		body.NextPageToken = *page.NextPageToken
	}
}

// GetIssue reads one issue. An old key finds the moved issue, whose Key is
// then the new one (§3.1). nil fields means all of them.
func (c *Client) GetIssue(ctx context.Context, idOrKey string, fields, expand []string, properties ...string) (*Issue, error) {
	q := url.Values{}
	if len(fields) > 0 {
		q.Set("fields", strings.Join(fields, ","))
	}
	if len(expand) > 0 {
		q.Set("expand", strings.Join(expand, ","))
	}
	if len(properties) > 0 {
		q.Set("properties", strings.Join(properties, ","))
	}
	var is Issue
	if err := c.get(ctx, "/rest/api/3/issue/"+esc(idOrKey), q, &is); err != nil {
		return nil, err
	}
	return &is, nil
}

// CreateIssue creates an issue (§5.1) and returns its id and key. User
// fields take {"accountId": …}, issuetype {"id": …}.
func (c *Client) CreateIssue(ctx context.Context, fields map[string]any, properties []Property) (IssueRef, error) {
	body := map[string]any{"fields": fields}
	if len(properties) > 0 {
		body["properties"] = properties
	}
	var ref IssueRef
	_, err := c.do(ctx, request{method: http.MethodPost, path: "/rest/api/3/issue", body: body, out: &ref})
	return ref, err
}

// EditIssue sets fields and applies update operations in one PUT, one
// per-issue write (§5.3, §16.11). A field may not appear in both. Status is
// not editable here; see DoTransition. notifyUsers is never sent: false
// fails the whole edit without admin rights (C5).
func (c *Client) EditIssue(ctx context.Context, idOrKey string, fields map[string]any, update map[string][]Op) error {
	body := map[string]any{}
	if fields != nil {
		body["fields"] = fields
	}
	if update != nil {
		body["update"] = update
	}
	_, err := c.do(ctx, request{method: http.MethodPut, path: "/rest/api/3/issue/" + esc(idOrKey), body: body})
	return err
}

// DeleteIssue deletes an issue; with sub-tasks it fails unless
// deleteSubtasks (§5.4).
func (c *Client) DeleteIssue(ctx context.Context, idOrKey string, deleteSubtasks bool) error {
	q := url.Values{}
	if deleteSubtasks {
		q.Set("deleteSubtasks", "true")
	}
	_, err := c.do(ctx, request{method: http.MethodDelete, path: "/rest/api/3/issue/" + esc(idOrKey), query: q})
	return err
}

// Transitions lists the transitions available from the issue's current
// status, with their screen fields (§6.1).
func (c *Client) Transitions(ctx context.Context, idOrKey string) ([]Transition, error) {
	var out struct {
		Transitions []Transition `json:"transitions"`
	}
	q := url.Values{"expand": {"transitions.fields"}}
	err := c.get(ctx, "/rest/api/3/issue/"+esc(idOrKey)+"/transitions", q, &out)
	return out.Transitions, err
}

// DoTransition performs a transition, with the screen fields it requires
// (e.g. resolution). A 409 means a concurrent transition: re-read
// Transitions before retrying (C9).
func (c *Client) DoTransition(ctx context.Context, idOrKey, transitionID string, fields map[string]any) error {
	body := map[string]any{"transition": map[string]string{"id": transitionID}}
	if fields != nil {
		body["fields"] = fields
	}
	_, err := c.do(ctx, request{method: http.MethodPost, path: "/rest/api/3/issue/" + esc(idOrKey) + "/transitions", body: body})
	return err
}

// CreateIssueLink links source to destination so that it reads
// "source <outward> destination": ("Blocks", A, B) means A blocks B.
// Jira calls the source inwardIssue and the destination outwardIssue (C1).
// linkType and the issues are ids when all digits, else a name and keys.
// No comment is ever sent (C11), and the response has no link id: re-read
// fields.issuelinks for it (§9.1).
func (c *Client) CreateIssueLink(ctx context.Context, linkType, source, destination string) error {
	body := map[string]any{
		"type":         ref(linkType, "name"),
		"inwardIssue":  ref(source, "key"),
		"outwardIssue": ref(destination, "key"),
	}
	// A duplicate request creates no second link (§9.1), so a 503 may be retried.
	_, err := c.do(ctx, request{method: http.MethodPost, path: "/rest/api/3/issueLink", body: body, safe: true})
	return err
}

// DeleteIssueLink removes a link by id; both 200 and 204 are success (C13).
func (c *Client) DeleteIssueLink(ctx context.Context, linkID string) error {
	_, err := c.do(ctx, request{method: http.MethodDelete, path: "/rest/api/3/issueLink/" + esc(linkID)})
	return err
}

// GetProperty reads an issue property's value. A missing property is a 404
// *Error, as is a missing issue.
func (c *Client) GetProperty(ctx context.Context, idOrKey, key string) (json.RawMessage, error) {
	var out struct {
		Value json.RawMessage `json:"value"`
	}
	err := c.get(ctx, "/rest/api/3/issue/"+esc(idOrKey)+"/properties/"+esc(key), nil, &out)
	return out.Value, err
}

// SetProperty writes an issue property, value being any non-empty JSON of at
// most 32768 characters, and reports whether it was created rather than
// replaced (jira-api-vetting.md §4.1). It neither bumps updated nor enters the
// changelog.
func (c *Client) SetProperty(ctx context.Context, idOrKey, key string, value any) (created bool, err error) {
	status, err := c.do(ctx, request{method: http.MethodPut, path: "/rest/api/3/issue/" + esc(idOrKey) + "/properties/" + esc(key), body: value})
	return status == http.StatusCreated, err
}

func (c *Client) DeleteProperty(ctx context.Context, idOrKey, key string) error {
	_, err := c.do(ctx, request{method: http.MethodDelete, path: "/rest/api/3/issue/" + esc(idOrKey) + "/properties/" + esc(key)})
	return err
}

// ref is {"id": v} when v is all digits, else {alt: v}.
func ref(v, alt string) map[string]string {
	if _, err := strconv.ParseUint(v, 10, 64); err == nil {
		return map[string]string{"id": v}
	}
	return map[string]string{alt: v}
}

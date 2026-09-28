package jiraapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
)

// Myself is the caller. Call it first: a missing Authorization runs calls
// anonymously and answers 200 with nothing in it (jira-api-vetting.md §4.14).
func (c *Client) Myself(ctx context.Context) (*User, error) {
	var u User
	if err := c.get(ctx, "/rest/api/3/myself", nil, &u); err != nil {
		return nil, err
	}
	return &u, nil
}

func (c *Client) ServerInfo(ctx context.Context) (*ServerInfo, error) {
	var s ServerInfo
	if err := c.get(ctx, "/rest/api/3/serverInfo", nil, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// User reads one user; without Browse users and groups it is a 403 (§10.1).
func (c *Client) User(ctx context.Context, accountID string) (*User, error) {
	var u User
	if err := c.get(ctx, "/rest/api/3/user", url.Values{"accountId": {accountID}}, &u); err != nil {
		return nil, err
	}
	return &u, nil
}

func (c *Client) Fields(ctx context.Context) ([]Field, error) {
	var fs []Field
	err := c.get(ctx, "/rest/api/3/field", nil, &fs)
	return fs, err
}

func (c *Client) Project(ctx context.Context, idOrKey string) (*Project, error) {
	var p Project
	if err := c.get(ctx, "/rest/api/3/project/"+esc(idOrKey), nil, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

// ProjectStatuses lists the valid statuses per issue type (§8.3).
func (c *Client) ProjectStatuses(ctx context.Context, idOrKey string) ([]IssueTypeStatuses, error) {
	var out []IssueTypeStatuses
	err := c.get(ctx, "/rest/api/3/project/"+esc(idOrKey)+"/statuses", nil, &out)
	return out, err
}

// CreateMetaIssueTypes lists the types an issue can be created with in the
// project, every page (§8.8).
func (c *Client) CreateMetaIssueTypes(ctx context.Context, projectIDOrKey string) ([]IssueType, error) {
	return pages[IssueType](ctx, c, "/rest/api/3/issue/createmeta/"+esc(projectIDOrKey)+"/issuetypes", nil, 200,
		"issueTypes", "createMetaIssueType")
}

// CreateMetaFields lists the create-screen fields of one issue type, every
// page (§8.8).
func (c *Client) CreateMetaFields(ctx context.Context, projectIDOrKey, issueTypeID string) ([]FieldMeta, error) {
	return pages[FieldMeta](ctx, c, "/rest/api/3/issue/createmeta/"+esc(projectIDOrKey)+"/issuetypes/"+esc(issueTypeID), nil, 200,
		"fields", "results")
}

// Priorities lists priorities through /priority/search, the non-deprecated
// endpoint, limited to those valid in projectIDs when any are given (§8.5).
func (c *Client) Priorities(ctx context.Context, projectIDs ...string) ([]Priority, error) {
	q := url.Values{}
	for _, id := range projectIDs {
		q.Add("projectId", id)
	}
	return pages[Priority](ctx, c, "/rest/api/3/priority/search", q, 50, "values")
}

func (c *Client) IssueLinkTypes(ctx context.Context) ([]IssueLinkType, error) {
	var out struct {
		IssueLinkTypes []IssueLinkType `json:"issueLinkTypes"`
	}
	err := c.get(ctx, "/rest/api/3/issueLinkType", nil, &out)
	return out.IssueLinkTypes, err
}

// Comments reads every comment of an issue, oldest first (§7.1).
func (c *Client) Comments(ctx context.Context, idOrKey string) ([]Comment, error) {
	return pages[Comment](ctx, c, "/rest/api/3/issue/"+esc(idOrKey)+"/comment", url.Values{"orderBy": {"created"}, "expand": {"properties"}}, 100, "comments")
}

// AddComment posts an ADF body, with properties set inline (§7.2), and
// returns the comment.
func (c *Client) AddComment(ctx context.Context, idOrKey string, body json.RawMessage, properties ...Property) (*Comment, error) {
	var cm Comment
	in := map[string]any{"body": body}
	if len(properties) > 0 {
		in["properties"] = properties
	}
	_, err := c.do(ctx, request{method: http.MethodPost, path: "/rest/api/3/issue/" + esc(idOrKey) + "/comment",
		body: in, out: &cm})
	if err != nil {
		return nil, err
	}
	return &cm, nil
}

// UpdateComment replaces a comment's ADF body. Lacking permission is a 400,
// not a 403 (§7.2).
func (c *Client) UpdateComment(ctx context.Context, idOrKey, commentID string, body json.RawMessage) (*Comment, error) {
	var cm Comment
	_, err := c.do(ctx, request{method: http.MethodPut, path: "/rest/api/3/issue/" + esc(idOrKey) + "/comment/" + esc(commentID),
		body: map[string]any{"body": body}, out: &cm})
	if err != nil {
		return nil, err
	}
	return &cm, nil
}

func (c *Client) DeleteComment(ctx context.Context, idOrKey, commentID string) error {
	_, err := c.do(ctx, request{method: http.MethodDelete, path: "/rest/api/3/issue/" + esc(idOrKey) + "/comment/" + esc(commentID)})
	return err
}

// maxPaged bounds pages: a server ignoring startAt and sending neither
// isLast nor total would otherwise page forever.
const maxPaged = 10000

// pages collects an offset-paged resource. Both shapes are handled (§0):
// the page bean with isLast, and the legacy one with only total and a named
// array, whose name is the first of keys present. Paging stops on isLast,
// on reaching total, or on an empty page; never on a short page.
func pages[T any](ctx context.Context, c *Client, path string, q url.Values, size int, keys ...string) ([]T, error) {
	q = cloneValues(q)
	var all []T
	for start := 0; ; {
		q.Set("startAt", strconv.Itoa(start))
		q.Set("maxResults", strconv.Itoa(size))
		var raw map[string]json.RawMessage
		if err := c.get(ctx, path, q, &raw); err != nil {
			return nil, err
		}
		var items []T
		for _, k := range keys {
			if v, ok := raw[k]; ok {
				if err := json.Unmarshal(v, &items); err != nil {
					return nil, err
				}
				break
			}
		}
		var meta struct {
			Total  *int  `json:"total"`
			IsLast *bool `json:"isLast"`
		}
		for k, dst := range map[string]any{"total": &meta.Total, "isLast": &meta.IsLast} {
			if v, ok := raw[k]; ok {
				_ = json.Unmarshal(v, dst)
			}
		}
		all = append(all, items...)
		start += len(items)
		if len(all) > maxPaged {
			return nil, fmt.Errorf("jira: %s: more than %d items; the server may be ignoring startAt", path, maxPaged)
		}
		switch {
		case len(items) == 0,
			meta.IsLast != nil && *meta.IsLast,
			meta.IsLast == nil && meta.Total != nil && start >= *meta.Total:
			return all, nil
		}
	}
}

func cloneValues(q url.Values) url.Values {
	out := url.Values{}
	for k, v := range q {
		out[k] = append([]string(nil), v...)
	}
	return out
}

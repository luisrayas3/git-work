package jiratest

import (
	"net/http"
	"strings"
)

type handler func(s *Server, c *call) (int, any, error)

type route struct {
	method    string
	pattern   string
	op        string // operationId in platform.json
	anonymous bool   // runs for an anonymous caller (the handler hides what it must)
	write     bool   // counts in Writes and moves the clock
	h         handler
	segs      []string
}

const v3 = "/rest/api/3"

// routes are matched in order, so literal segments come before a variable
// in the same position (createmeta before issue/{issueIdOrKey}).
var routes = []*route{
	{method: "GET", pattern: v3 + "/myself", op: "getCurrentUser", h: (*Server).getMyself},
	{method: "GET", pattern: v3 + "/serverInfo", op: "getServerInfo", anonymous: true, h: (*Server).getServerInfo},

	{method: "POST", pattern: v3 + "/search/jql", op: "searchAndReconsileIssuesUsingJqlPost", anonymous: true, h: (*Server).searchPost},
	{method: "GET", pattern: v3 + "/search", op: "searchForIssuesUsingJql", anonymous: true, h: gone},
	{method: "POST", pattern: v3 + "/search", op: "searchForIssuesUsingJqlPost", anonymous: true, h: gone},

	{method: "POST", pattern: v3 + "/issue", op: "createIssue", write: true, h: (*Server).createIssue},
	{method: "GET", pattern: v3 + "/issue/createmeta/{projectIdOrKey}/issuetypes", op: "getCreateIssueMetaIssueTypes", h: (*Server).createMetaTypes},
	{method: "GET", pattern: v3 + "/issue/createmeta/{projectIdOrKey}/issuetypes/{issueTypeId}", op: "getCreateIssueMetaIssueTypeId", h: (*Server).createMetaFields},
	{method: "GET", pattern: v3 + "/issue/{issueIdOrKey}", op: "getIssue", anonymous: true, h: (*Server).getIssue},
	{method: "PUT", pattern: v3 + "/issue/{issueIdOrKey}", op: "editIssue", write: true, h: (*Server).editIssue},
	{method: "DELETE", pattern: v3 + "/issue/{issueIdOrKey}", op: "deleteIssue", write: true, h: (*Server).deleteIssue},
	{method: "GET", pattern: v3 + "/issue/{issueIdOrKey}/transitions", op: "getTransitions", anonymous: true, h: (*Server).getTransitions},
	{method: "POST", pattern: v3 + "/issue/{issueIdOrKey}/transitions", op: "doTransition", write: true, h: (*Server).doTransition},
	{method: "GET", pattern: v3 + "/issue/{issueIdOrKey}/comment", op: "getComments", anonymous: true, h: (*Server).getComments},
	{method: "POST", pattern: v3 + "/issue/{issueIdOrKey}/comment", op: "addComment", write: true, h: (*Server).addComment},
	{method: "PUT", pattern: v3 + "/issue/{issueIdOrKey}/comment/{id}", op: "updateComment", write: true, h: (*Server).updateComment},
	{method: "DELETE", pattern: v3 + "/issue/{issueIdOrKey}/comment/{id}", op: "deleteComment", anonymous: true, write: true, h: (*Server).deleteComment},
	{method: "GET", pattern: v3 + "/issue/{issueIdOrKey}/properties/{propertyKey}", op: "getIssueProperty", h: (*Server).getProperty},
	{method: "PUT", pattern: v3 + "/issue/{issueIdOrKey}/properties/{propertyKey}", op: "setIssueProperty", write: true, h: (*Server).setProperty},
	{method: "DELETE", pattern: v3 + "/issue/{issueIdOrKey}/properties/{propertyKey}", op: "deleteIssueProperty", write: true, h: (*Server).deleteProperty},

	{method: "POST", pattern: v3 + "/issueLink", op: "linkIssues", write: true, h: (*Server).linkIssues},
	{method: "DELETE", pattern: v3 + "/issueLink/{linkId}", op: "deleteIssueLink", write: true, h: (*Server).deleteIssueLink},
	{method: "GET", pattern: v3 + "/issueLinkType", op: "getIssueLinkTypes", h: (*Server).getLinkTypes},

	{method: "GET", pattern: v3 + "/field", op: "getFields", h: (*Server).getFields},
	{method: "GET", pattern: v3 + "/project/{projectIdOrKey}", op: "getProject", h: (*Server).getProject},
	{method: "GET", pattern: v3 + "/project/{projectIdOrKey}/statuses", op: "getAllStatuses", h: (*Server).getProjectStatuses},
	{method: "GET", pattern: v3 + "/priority/search", op: "searchPriorities", h: (*Server).searchPriorities},
	{method: "GET", pattern: v3 + "/user", op: "getUser", h: (*Server).getUser},
}

func init() {
	for _, rt := range routes {
		rt.segs = strings.Split(strings.Trim(rt.pattern, "/"), "/")
	}
}

// match finds the route for a request. pathKnown tells a 405 from a 404.
func match(method, path string) (rt *route, vars map[string]string, pathKnown bool) {
	segs := strings.Split(strings.Trim(path, "/"), "/")
	for _, r := range routes {
		v, ok := matchSegs(r.segs, segs)
		if !ok {
			continue
		}
		pathKnown = true
		if r.method == method {
			return r, v, true
		}
	}
	return nil, nil, pathKnown
}

func matchSegs(pattern, segs []string) (map[string]string, bool) {
	if len(pattern) != len(segs) {
		return nil, false
	}
	vars := map[string]string{}
	for i, p := range pattern {
		if strings.HasPrefix(p, "{") {
			if segs[i] == "" {
				return nil, false
			}
			vars[p[1:len(p)-1]] = segs[i]
			continue
		}
		if p != segs[i] {
			return nil, false
		}
	}
	return vars, true
}

// gone is the removed search API (C7).
func gone(*Server, *call) (int, any, error) {
	return 0, nil, &apiError{status: http.StatusGone, messages: []string{msgRemovedAPI}}
}

// selfDenying operations answer a denial themselves, with an empty result
// or a filtered one, because that is how Jira hides what the caller may not see.
var selfDenying = map[string]bool{
	"searchAndReconsileIssuesUsingJqlPost": true,
	"getFields":                            true, // system fields only
	"getCreateIssueMetaIssueTypes":         true, // no types
	"getCreateIssueMetaIssueTypeId":        true,
	"getTransitions":                       true, // [] without Transition issues (§6.1)
}

// denials are the documented answers of the other operations to a caller
// without the permission (the 404s hide existence, jira-api.md §12.3).
var denials = map[string]func(c *call) *apiError{
	"getIssue":            deny404Issue,
	"getComments":         deny404Issue,
	"getIssueProperty":    deny404Issue,
	"deleteIssueProperty": deny404Issue,
	"editIssue":           deny(http.StatusBadRequest, "You do not have permission to edit issues in this project."),
	"doTransition":        deny(http.StatusBadRequest, "You do not have permission to transition this issue."),
	"addComment":          deny(http.StatusBadRequest, "You do not have the permission to comment on this issue."),
	"updateComment":       deny(http.StatusBadRequest, "You do not have the permission to edit this comment."),
	"deleteComment":       deny(http.StatusBadRequest, "You do not have the permission to delete this comment."),
	"createIssue":         deny(http.StatusForbidden, "You do not have permission to create issues in this project."),
	"deleteIssue":         deny(http.StatusForbidden, "You do not have permission to delete issues in this project."),
	"setIssueProperty":    deny(http.StatusForbidden, "You do not have permission to edit this issue."),
	"linkIssues":          deny(http.StatusNotFound, "No Link Issue Permission for issue."),
	"deleteIssueLink":     deny(http.StatusNotFound, "No issue link with id exists."),
	"getIssueLinkTypes":   deny(http.StatusNotFound, "Issue linking is currently disabled."),
	"getProject":          denyProject,
	"getAllStatuses":      denyProject,
	"getUser":             deny(http.StatusForbidden, "You do not have the permission to see the specified user."),
}

func deny(status int, msg string) func(*call) *apiError {
	return func(*call) *apiError { return &apiError{status: status, messages: []string{msg}} }
}

func deny404Issue(*call) *apiError { return issueNotFound() }

func denyProject(c *call) *apiError {
	k := c.v("projectIdOrKey")
	if k == "" {
		k = c.v("projectId")
	}
	return notFound("No project could be found with key '" + k + "'.")
}

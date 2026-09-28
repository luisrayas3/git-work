package jiratest

import (
	"encoding/json"
	"net/http"
	"strings"
)

// apiError is Jira's ErrorCollection with the status it goes out with (jira-api.md §12.2).
type apiError struct {
	status   int
	messages []string
	errors   map[string]string
	headers  map[string]string
}

func (e *apiError) Error() string {
	var parts []string
	parts = append(parts, e.messages...)
	for k, v := range e.errors {
		parts = append(parts, k+": "+v)
	}
	return strings.Join(parts, "; ")
}

func badRequest(msg string) *apiError {
	return &apiError{status: http.StatusBadRequest, messages: []string{msg}}
}

func notFound(msg string) *apiError {
	return &apiError{status: http.StatusNotFound, messages: []string{msg}}
}

func fieldErrors(errs map[string]string) *apiError {
	return &apiError{status: http.StatusBadRequest, errors: errs}
}

func issueNotFound() *apiError {
	return notFound("Issue does not exist or you do not have permission to see it.")
}

func writeError(w http.ResponseWriter, e *apiError) {
	msgs := e.messages
	if msgs == nil {
		msgs = []string{}
	}
	errs := e.errors
	if errs == nil {
		errs = map[string]string{}
	}
	for k, v := range e.headers {
		w.Header().Set(k, v)
	}
	b, _ := json.Marshal(struct {
		ErrorMessages []string          `json:"errorMessages"`
		Errors        map[string]string `json:"errors"`
	}{msgs, errs})
	w.WriteHeader(e.status)
	_, _ = w.Write(b)
}

// Messages that are Jira's own where community reports quote them, and the
// fake's best guess where no source does (see the package's report).
const (
	msgADF          = "Operation value must be an Atlassian Document (see the Atlassian Document Format)"
	msgNotifyUsers  = "To discard the user notification either admin or project admin permissions are required."
	msgRemovedAPI   = "The requested API has been removed. Please use the newer, enhanced search-based API instead. See https://developer.atlassian.com/changelog/#CHANGE-2046 for more details."
	msgUnbounded    = "Unbounded JQL queries are not allowed here. Please add a search restriction to your query."
	msgCannotSetFmt = "Field '%s' cannot be set. It is not on the appropriate screen, or unknown."
)

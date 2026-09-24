package view

import "encoding/json"

// DefaultQuery is the listing you get when you name no program:
// everything that is not archived, most recently edited first.
//
// It is written as a jq program rather than special-cased in Go
// so that `.` means the whole array and nothing is hidden from it.
// "mine" would be the better default,
// but it needs the schema's assignee field to know which one it is,
// so it waits for the schema (e8d6426).
//
// It lives here, in the lowest package that needs it,
// because two callers apply it and they must apply the same one:
// `host.IssueList` when a program is empty,
// and every view kind whose `query` argument was not given.
const DefaultQuery = `map(select(.fields.archived != true))
	| sort_by(.edit_time.lamport, .edit_time.timestamp)
	| reverse`

// defaultQueryJSON is DefaultQuery as the table's Default holds it, JSON.
var defaultQueryJSON = func() string {
	raw, err := json.Marshal(DefaultQuery)
	if err != nil {
		panic(err)
	}
	return string(raw)
}()

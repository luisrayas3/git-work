package jiraapi

import (
	"strings"
	"time"
)

// JQLTime formats t as a JQL date literal, quoted: "yyyy/MM/dd HH:mm" in
// loc, floored to the minute. Jira reads the literal in the searching
// user's profile zone, so loc is Myself's Location, not the offset of the
// timestamps Jira returns (§2.5, C6). The literal has no seconds: query with
// >= on a floored watermark and dedupe. In a DST fold two instants format
// alike, which the recommended overlap absorbs (§16.2). nil loc is UTC.
func JQLTime(t time.Time, loc *time.Location) string {
	if loc == nil {
		loc = time.UTC
	}
	return `"` + t.In(loc).Format("2006/01/02 15:04") + `"`
}

// JQLQuote quotes s as a JQL string value, escaping backslash, double quote
// and the control characters JQL has escapes for.
func JQLQuote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"', '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

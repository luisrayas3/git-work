package jiraapi

import (
	"strings"
	"time"
)

// JQLTime formats t as a JQL date literal, quoted: "yyyy/MM/dd HH:mm" in
// loc, floored to the minute. Jira reads the literal in the searching
// user's profile zone, so loc is Myself's Location, not the offset of the
// timestamps Jira returns (§2.5, C6). The literal has no seconds: query with
// >= on a floored watermark and dedupe. nil loc is UTC.
//
// In a DST fold two instants format alike, and the server may read the
// literal as the later one (Go and java.util.Calendar both do), an hour
// past t, which no minutes of overlap absorb. So when t is in the first
// pass of a fold, the literal is of t moved back by the shift, before the
// fold: at worst an hour too early, never late.
func JQLTime(t time.Time, loc *time.Location) string {
	const layout = "2006/01/02 15:04"
	if loc == nil {
		loc = time.UTC
	}
	t = t.In(loc)
	floor, lit := t.Truncate(time.Minute), t.Format(layout)
	_, cur := t.Zone()
	_, later := t.Add(3 * time.Hour).Zone()
	if other := floor.Add(time.Duration(cur-later) * time.Second); other.After(floor) && other.In(loc).Format(layout) == lit {
		lit = t.Add(floor.Sub(other)).Format(layout)
	}
	return `"` + lit + `"`
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

package host

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// TimeForms is what every TIME argument accepts, named once for the help text.
const TimeForms = "a date (2026-09-21), an RFC 3339 time, or a duration back from now (7d, 2w, 12h)"

// ParseTime reads a TIME argument: `--at`, `--from` and `--to` all take this.
//
// Three forms, tried in this order (doc/design/report.md):
// a date, read as local midnight;
// an RFC 3339 timestamp;
// a duration back from `now`, so that `--from 7d` is the last seven days
// and a script needs no date arithmetic.
//
// An empty string is the zero time, which is what an absent flag means:
// the caller decides whether that is "now" or "the beginning of time".
func ParseTime(s string, now time.Time) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, nil
	}

	if t, err := time.ParseInLocation(time.DateOnly, s, time.Local); err == nil {
		return t, nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	if d, err := ParseDuration(s); err == nil {
		return now.Add(-d), nil
	}

	return time.Time{}, fmt.Errorf("%q is not a time: %s", s, TimeForms)
}

// ParseDuration is Go's, with days and weeks:
// the natural units of a report window and of `jira sync --adopt`.
//
// One parser for the whole tool, so that `7d` means the same thing
// wherever it is written.
func ParseDuration(s string) (time.Duration, error) {
	for suffix, unit := range map[string]time.Duration{
		"d": 24 * time.Hour,
		"w": 7 * 24 * time.Hour,
	} {
		n, ok := strings.CutSuffix(s, suffix)
		if !ok {
			continue
		}
		count, err := strconv.Atoi(n)
		if err != nil || count < 0 {
			return 0, fmt.Errorf("%q is not a duration (7d, 2w, 12h, 0)", s)
		}
		return time.Duration(count) * unit, nil
	}

	d, err := time.ParseDuration(s)
	if err != nil || d < 0 {
		return 0, fmt.Errorf("%q is not a duration (7d, 2w, 12h, 0)", s)
	}
	return d, nil
}

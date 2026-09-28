package jiraapi

import (
	"encoding/json"
	"testing"
	"time"
)

func TestTimeParsing(t *testing.T) {
	want := time.Date(2015, 12, 2, 15, 39, 15, 0, time.UTC)
	for _, c := range []struct {
		in     string
		want   time.Time
		offset int
	}{
		{`"2015-12-02T07:39:15.000-0800"`, want, -8 * 3600}, // api-vetting.md C4
		{`"2015-12-02T07:39:15-0800"`, want, -8 * 3600},
		{`"2015-12-02T15:39:15.000+0000"`, want, 0},
		{`"2015-12-02T15:39:15.000Z"`, want, 0},
		{`"2015-12-02T15:39:15Z"`, want, 0},
		{`"2015-12-03T01:39:15.000+10:00"`, want, 10 * 3600},                                  // agile spec examples
		{`"2025-03-12T01:36:46.600Z"`, time.Date(2025, 3, 12, 1, 36, 46, 600e6, time.UTC), 0}, // C3
		{`"2026-09-27T10:03:11.123456+0200"`, time.Date(2026, 9, 27, 8, 3, 11, 123456e3, time.UTC), 2 * 3600},
		{`1449070755`, want, -1},    // bulkfetch epoch seconds (§4.2)
		{`1449070755000`, want, -1}, // and milliseconds
		{`null`, time.Time{}, -1},
		{`""`, time.Time{}, -1},
	} {
		var got Time
		if err := json.Unmarshal([]byte(c.in), &got); err != nil {
			t.Errorf("%s: %v", c.in, err)
			continue
		}
		if !got.Equal(c.want) {
			t.Errorf("%s = %v, want %v", c.in, got, c.want)
		}
		if _, off := got.Zone(); c.offset >= 0 && off != c.offset {
			t.Errorf("%s: offset %d, want %d", c.in, off, c.offset)
		}
	}
	for _, bad := range []string{`"2015-12-02"`, `"yesterday"`, `true`} {
		var got Time
		if err := json.Unmarshal([]byte(bad), &got); err == nil {
			t.Errorf("%s parsed as %v", bad, got)
		}
	}
	b, _ := json.Marshal(struct{ A, B Time }{Time{time.Date(2026, 9, 27, 10, 3, 11, 123e6, time.FixedZone("", -7*3600))}, Time{}})
	if string(b) != `{"A":"2026-09-27T10:03:11.123-0700","B":null}` {
		t.Errorf("marshal %s", b)
	}
}

func TestDate(t *testing.T) {
	var d struct{ A, B, C Date }
	if err := json.Unmarshal([]byte(`{"A":"2026-10-15","B":null}`), &d); err != nil {
		t.Fatal(err)
	}
	if d.A.String() != "2026-10-15" || !d.B.IsZero() || !d.C.IsZero() {
		t.Errorf("%+v", d)
	}
	b, _ := json.Marshal(d)
	if string(b) != `{"A":"2026-10-15","B":null,"C":null}` {
		t.Errorf("marshal %s", b)
	}
	if err := json.Unmarshal([]byte(`{"A":"15/Oct/26"}`), &d); err == nil {
		t.Error("legacy date accepted")
	}
}

func TestJQLTime(t *testing.T) {
	load := func(name string) *time.Location {
		loc, err := time.LoadLocation(name)
		if err != nil {
			t.Skip(err)
		}
		return loc
	}
	ny, berlin, sydney := load("America/New_York"), load("Europe/Berlin"), load("Australia/Sydney")
	for _, c := range []struct {
		t    time.Time
		loc  *time.Location
		want string
	}{
		{time.Date(2026, 9, 27, 10, 3, 59, 999e6, time.UTC), nil, `"2026/09/27 10:03"`}, // floored, not rounded
		{time.Date(2026, 9, 27, 10, 3, 11, 0, time.UTC), berlin, `"2026/09/27 12:03"`},
		{time.Date(2026, 9, 27, 10, 3, 11, 0, time.FixedZone("", -7*3600)), berlin, `"2026/09/27 19:03"`}, // the response's zone is irrelevant
		{time.Date(2026, 9, 27, 14, 3, 0, 0, time.UTC), sydney, `"2026/09/28 00:03"`},                     // crosses the date
		// New York springs forward at 2026-03-08 07:00 UTC: 01:59 EST, then 03:00 EDT.
		{time.Date(2026, 3, 8, 6, 59, 0, 0, time.UTC), ny, `"2026/03/08 01:59"`},
		{time.Date(2026, 3, 8, 7, 0, 0, 0, time.UTC), ny, `"2026/03/08 03:00"`},
		// and falls back at 2026-11-01 06:00 UTC: 01:30 happens twice. The
		// first pass, which may be read as the second, steps back an hour.
		{time.Date(2026, 11, 1, 4, 59, 0, 0, time.UTC), ny, `"2026/11/01 00:59"`},
		{time.Date(2026, 11, 1, 5, 30, 0, 0, time.UTC), ny, `"2026/11/01 00:30"`},
		{time.Date(2026, 11, 1, 6, 30, 0, 0, time.UTC), ny, `"2026/11/01 01:30"`},
		{time.Date(2026, 11, 1, 7, 0, 0, 0, time.UTC), ny, `"2026/11/01 02:00"`},
		// Berlin falls back at 2026-10-25 01:00 UTC.
		{time.Date(2026, 10, 25, 0, 0, 0, 0, time.UTC), berlin, `"2026/10/25 01:00"`},
		{time.Date(2026, 10, 25, 0, 59, 30, 0, time.UTC), berlin, `"2026/10/25 01:59"`},
		{time.Date(2026, 10, 25, 1, 0, 0, 0, time.UTC), berlin, `"2026/10/25 02:00"`},
	} {
		if got := JQLTime(c.t, c.loc); got != c.want {
			t.Errorf("JQLTime(%v, %v) = %s, want %s", c.t, c.loc, got, c.want)
		}
	}
}

func TestJQLQuote(t *testing.T) {
	for in, want := range map[string]string{
		`PROJ`:           `"PROJ"`,
		`say "hi"`:       `"say \"hi\""`,
		`back\slash`:     `"back\\slash"`,
		"two\nlines\t\r": `"two\nlines\t\r"`,
		`ünïcode`:        `"ünïcode"`,
	} {
		if got := JQLQuote(in); got != want {
			t.Errorf("JQLQuote(%q) = %s, want %s", in, got, want)
		}
	}
}

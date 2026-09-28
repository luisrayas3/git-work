package jira

import (
	"strconv"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"

	"github.com/git-bug/git-bug/schema"
)

// maxSlug leaves room for `alias_jira/` inside a 64-byte attribute name (JS6).
const maxSlug = 48

// fold decomposes a name, drops its combining marks and lowers it:
// "Café" is "cafe".
func fold(name string) string {
	var b strings.Builder
	for _, r := range norm.NFKD.String(name) {
		if !unicode.Is(unicode.Mn, r) {
			b.WriteRune(unicode.ToLower(r))
		}
	}
	return b.String()
}

func alnum(r rune) bool { return r >= 'a' && r <= 'z' || r >= '0' && r <= '9' }

// normName is what names are matched by (JS6): lower-case letters and digits
// only, so "Sub-task", "Subtask" and `subtask` are one name.
// An empty result matches nothing.
func normName(name string) string {
	var b strings.Builder
	for _, r := range fold(name) {
		if alnum(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// slug makes a key from a Jira name (JS6): "Won't Do" is `wont-do`.
// It is empty when nothing Latin is left; keyFor supplies that fallback,
// the leading-letter rule and the collision suffix.
func slug(name string) string {
	var b strings.Builder
	dash := false
	for _, r := range fold(name) {
		switch {
		case r == '\'' || r == '’':
		case alnum(r):
			if dash && b.Len() > 0 {
				b.WriteByte('-')
			}
			dash = false
			b.WriteRune(r)
		default:
			dash = true
		}
	}
	s := b.String()
	if len(s) > maxSlug {
		if i := strings.LastIndexByte(s[:maxSlug+1], '-'); i > 0 {
			s = s[:i]
		} else {
			s = s[:maxSlug]
		}
	}
	return s
}

// keyFor makes a new key in one namespace: the type keys, one type's field
// keys or one field's value ids. prefix is "t-" or "f-" for a key that must
// start with a letter, "" for a value id. taken says whether a key is used;
// a built-in field key always is.
func keyFor(name, jiraId, prefix string, taken func(string) bool) string {
	base := slug(name)
	if base == "" {
		base = "jira-" + slug(jiraId)
	}
	if prefix != "" && (base[0] < 'a' || base[0] > 'z') {
		base = prefix + base
	}
	used := func(k string) bool { return taken(k) || prefix == "f-" && schema.IsBuiltin(k) }
	key := base
	for n := 2; used(key); n++ {
		key = base + "-" + strconv.Itoa(n)
	}
	return key
}

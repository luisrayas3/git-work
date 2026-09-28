package jiratest

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// The JQL the fake understands is the shapes the sync sends,
//
//	project = "K" [AND (updated >= "T" OR id in (1, 2))] ORDER BY updated ASC, id ASC
//	project = "K" AND created >= "T" ORDER BY created ASC, id ASC
//
// and a little around them, in this grammar:
//
//	query   = [or] [ORDER BY key [ASC|DESC] {, key [ASC|DESC]}]
//	or      = and {OR and}
//	and     = ( or ) | clause {AND ( or ) | clause}
//	clause  = field op value | field IN ( value {, value} )
//
// Fields: project, key/issuekey, id, updated, created. A syntax error is a
// 400 in Jira's words; a field or an operator the fake does not implement
// is a 400 saying so. Dates are read in the caller's profile zone (api.md
// §2.5, C6).

type tokKind int

const (
	tEOF tokKind = iota
	tWord
	tString
	tOp
	tLParen
	tRParen
	tComma
)

type token struct {
	kind tokKind
	text string
	pos  int // 1-based character, as Jira reports it
}

func (t token) is(word string) bool { return t.kind == tWord && strings.EqualFold(t.text, word) }

func jqlError(format string, args ...any) *apiError {
	return badRequest(fmt.Sprintf(format, args...))
}

func lex(src string) ([]token, *apiError) {
	var toks []token
	rs := []rune(src)
	for i := 0; i < len(rs); {
		r := rs[i]
		switch {
		case unicode.IsSpace(r):
			i++
		case r == '(':
			toks = append(toks, token{tLParen, "(", i + 1})
			i++
		case r == ')':
			toks = append(toks, token{tRParen, ")", i + 1})
			i++
		case r == ',':
			toks = append(toks, token{tComma, ",", i + 1})
			i++
		case r == '"' || r == '\'':
			j := i + 1
			var b strings.Builder
			for j < len(rs) && rs[j] != r {
				if rs[j] == '\\' && j+1 < len(rs) {
					j++
				}
				b.WriteRune(rs[j])
				j++
			}
			if j >= len(rs) {
				return nil, jqlError("Error in the JQL Query: The quoted string '%s' has not been completed. (line 1, character %d)", string(rs[i:]), i+1)
			}
			toks = append(toks, token{tString, b.String(), i + 1})
			i = j + 1
		case strings.ContainsRune("=!<>~", r):
			j := i + 1
			if j < len(rs) && (rs[j] == '=' || rs[j] == '~') {
				j++
			}
			toks = append(toks, token{tOp, string(rs[i:j]), i + 1})
			i = j
		default:
			j := i
			for j < len(rs) && !unicode.IsSpace(rs[j]) && !strings.ContainsRune("(),=!<>~\"'", rs[j]) {
				j++
			}
			toks = append(toks, token{tWord, string(rs[i:j]), i + 1})
			i = j
		}
	}
	return append(toks, token{tEOF, "", len(rs) + 1}), nil
}

type jqlClause struct {
	field string
	op    string // = != > >= < <= in
	vals  []string
}

type jqlNode struct {
	op          string // and, or, clause
	left, right *jqlNode
	clause      *jqlClause
}

type orderKey struct {
	field string
	desc  bool
}

type jqlQuery struct {
	where *jqlNode
	order []orderKey
}

type parser struct {
	toks []token
	i    int
}

func (p *parser) peek() token { return p.toks[p.i] }
func (p *parser) next() token { t := p.toks[p.i]; p.i++; return t }

func unexpected(t token, want string) *apiError {
	if t.kind == tEOF {
		return jqlError("Error in the JQL Query: Expecting %s but reached the end of the query.", want)
	}
	return jqlError("Error in the JQL Query: Expecting %s but got '%s'. (line 1, character %d)", want, t.text, t.pos)
}

func parseJQL(src string) (*jqlQuery, *apiError) {
	toks, err := lex(src)
	if err != nil {
		return nil, err
	}
	p := &parser{toks: toks}
	q := &jqlQuery{}
	if t := p.peek(); t.kind != tEOF && !t.is("order") {
		if q.where, err = p.or(); err != nil {
			return nil, err
		}
	}
	if p.peek().is("order") {
		p.next()
		if !p.next().is("by") {
			return nil, unexpected(p.toks[p.i-1], "'by'")
		}
		for {
			t := p.next()
			if t.kind != tWord && t.kind != tString {
				return nil, unexpected(t, "a field name")
			}
			k := orderKey{field: t.text}
			if n := p.peek(); n.is("asc") || n.is("desc") {
				k.desc = p.next().is("desc")
			}
			q.order = append(q.order, k)
			if p.peek().kind != tComma {
				break
			}
			p.next()
		}
	}
	if t := p.peek(); t.kind != tEOF {
		return nil, unexpected(t, "either 'OR' or 'AND'")
	}
	return q, nil
}

func (p *parser) or() (*jqlNode, *apiError) {
	l, err := p.and()
	for err == nil && p.peek().is("or") {
		p.next()
		var r *jqlNode
		if r, err = p.and(); err == nil {
			l = &jqlNode{op: "or", left: l, right: r}
		}
	}
	return l, err
}

func (p *parser) and() (*jqlNode, *apiError) {
	l, err := p.term()
	for err == nil && p.peek().is("and") {
		p.next()
		var r *jqlNode
		if r, err = p.term(); err == nil {
			l = &jqlNode{op: "and", left: l, right: r}
		}
	}
	return l, err
}

func (p *parser) term() (*jqlNode, *apiError) {
	if p.peek().kind == tLParen {
		p.next()
		n, err := p.or()
		if err != nil {
			return nil, err
		}
		if c := p.next(); c.kind != tRParen {
			return nil, unexpected(c, "')'")
		}
		return n, nil
	}
	c, err := p.clause()
	return &jqlNode{op: "clause", clause: c}, err
}

func (p *parser) clause() (*jqlClause, *apiError) {
	f := p.next()
	if f.kind != tWord && f.kind != tString {
		return nil, unexpected(f, "a field name")
	}
	c := &jqlClause{field: f.text}
	switch t := p.next(); {
	case t.kind == tOp:
		c.op = t.text
		v, err := p.value()
		c.vals = []string{v}
		return c, err
	case t.is("in"):
		c.op = "in"
	default:
		return nil, unexpected(t, "operator")
	}
	if l := p.next(); l.kind != tLParen {
		return nil, unexpected(l, "'('")
	}
	for {
		v, err := p.value()
		if err != nil {
			return nil, err
		}
		c.vals = append(c.vals, v)
		n := p.next()
		if n.kind == tRParen {
			return c, nil
		}
		if n.kind != tComma {
			return nil, unexpected(n, "')' or ','")
		}
	}
}

func (p *parser) value() (string, *apiError) {
	t := p.next()
	if t.kind != tString && t.kind != tWord {
		return "", unexpected(t, "a value")
	}
	if p.peek().kind == tLParen {
		return "", jqlError("jiratest: the fake does not implement JQL functions (%s).", t.text)
	}
	return t.text, nil
}

// jqlEnv is what a query is compiled against.
type jqlEnv struct {
	s    *Server
	user *User // nil when anonymous
	loc  *time.Location
	// hidden makes every project invisible (no Browse projects), which
	// Jira reports as the project not existing.
	hidden bool
}

type pred func(st *issueState) bool

func (e *jqlEnv) compile(n *jqlNode) (pred, *apiError) {
	if n.op == "clause" {
		return e.clause(n.clause)
	}
	l, err := e.compile(n.left)
	if err != nil {
		return nil, err
	}
	r, err := e.compile(n.right)
	if err != nil {
		return nil, err
	}
	if n.op == "and" {
		return func(st *issueState) bool { return l(st) && r(st) }, nil
	}
	return func(st *issueState) bool { return l(st) || r(st) }, nil
}

func (e *jqlEnv) clause(c *jqlClause) (pred, *apiError) {
	field := strings.ToLower(c.field)
	switch field {
	case "updated", "created":
		return e.dateClause(c, field)
	case "project", "key", "issuekey", "id":
	default:
		return nil, jqlError("jiratest: the fake does not implement the JQL field '%s'.", c.field)
	}
	if c.op != "=" && c.op != "in" {
		return nil, jqlError("The operator '%s' is not supported by the '%s' field.", strings.ToUpper(c.op), c.field)
	}
	var match []pred
	for _, v := range c.vals {
		switch field {
		case "project":
			p := e.s.projectByKey(v)
			if (p == nil || e.hidden) && e.user != nil {
				return nil, jqlError("The value '%s' does not exist for the field 'project'.", v)
			}
			match = append(match, func(st *issueState) bool { return st.project == p })
		case "id":
			n, err := strconv.Atoi(v)
			if err != nil {
				return nil, jqlError("The value '%s' for field 'id' is invalid.", v)
			}
			// Jira validates an id the way it validates a key: one that names no
			// issue the caller can see, deleted or hidden, fails the whole query.
			// The vetted docs (api.md, api-vetting.md) are silent on id, so the
			// fake takes the answer worse for a client, the 400 a key gets.
			if e.user != nil && (e.hidden || e.s.lookup(v) == nil) {
				return nil, jqlError("An issue with key '%s' does not exist for field '%s'.", v, c.field)
			}
			match = append(match, func(st *issueState) bool { return st.id == n })
		default:
			// Old keys resolve to the moved issue (api-vetting.md §4.7).
			rec := e.s.lookup(v)
			if rec == nil {
				return nil, jqlError("An issue with key '%s' does not exist for field '%s'.", v, c.field)
			}
			match = append(match, func(st *issueState) bool { return st.id == rec.cur.id })
		}
	}
	return func(st *issueState) bool {
		return slices.ContainsFunc(match, func(m pred) bool { return m(st) })
	}, nil
}

// dateClause reads a literal in the caller's zone (api.md §2.5): to the
// minute, or a date alone as midnight.
func (e *jqlEnv) dateClause(c *jqlClause, field string) (pred, *apiError) {
	var t time.Time
	cmp, ok := map[string]func(a time.Time) bool{
		"=":  func(a time.Time) bool { return a.Equal(t) },
		"!=": func(a time.Time) bool { return !a.Equal(t) },
		">":  func(a time.Time) bool { return a.After(t) },
		">=": func(a time.Time) bool { return !a.Before(t) },
		"<":  func(a time.Time) bool { return a.Before(t) },
		"<=": func(a time.Time) bool { return !a.After(t) },
	}[c.op]
	if !ok {
		return nil, jqlError("The operator '%s' is not supported by the '%s' field.", strings.ToUpper(c.op), c.field)
	}
	var err error
	for _, layout := range []string{"2006/01/02 15:04", "2006-01-02 15:04", "2006/01/02", "2006-01-02"} {
		if t, err = time.ParseInLocation(layout, strings.TrimSpace(c.vals[0]), e.loc); err == nil {
			break
		}
	}
	if err != nil {
		return nil, jqlError("Date value '%s' for field '%s' is invalid. Valid formats include: 'yyyy/MM/dd HH:mm', 'yyyy-MM-dd HH:mm', 'yyyy/MM/dd', 'yyyy-MM-dd', or a period format e.g. '-5d', '4w 2d'.", c.vals[0], c.field)
	}
	if field == "created" {
		return func(st *issueState) bool { return cmp(st.created) }, nil
	}
	return func(st *issueState) bool { return cmp(st.updated) }, nil
}

// sorter compiles ORDER BY; the default is created DESC (a guess), and id
// breaks every tie so pages are stable.
func (e *jqlEnv) sorter(keys []orderKey) (func(a, b *issueState) int, *apiError) {
	if len(keys) == 0 {
		keys = []orderKey{{field: "created", desc: true}}
	}
	var cmps []func(a, b *issueState) int
	for _, k := range keys {
		var f func(a, b *issueState) int
		switch strings.ToLower(k.field) {
		case "updated":
			f = func(a, b *issueState) int { return a.updated.Compare(b.updated) }
		case "created":
			f = func(a, b *issueState) int { return a.created.Compare(b.created) }
		case "key", "issuekey":
			f = compareKeys
		case "id":
			f = func(a, b *issueState) int { return a.id - b.id }
		default:
			return nil, jqlError("Not able to sort using field '%s'.", k.field)
		}
		if k.desc {
			g := f
			f = func(a, b *issueState) int { return -g(a, b) }
		}
		cmps = append(cmps, f)
	}
	return func(a, b *issueState) int {
		for _, f := range cmps {
			if r := f(a, b); r != 0 {
				return r
			}
		}
		return a.id - b.id
	}, nil
}

func compareKeys(a, b *issueState) int {
	pa, na, _ := strings.Cut(a.key, "-")
	pb, nb, _ := strings.Cut(b.key, "-")
	if c := strings.Compare(pa, pb); c != 0 {
		return c
	}
	x, _ := strconv.Atoi(na)
	y, _ := strconv.Atoi(nb)
	return x - y
}

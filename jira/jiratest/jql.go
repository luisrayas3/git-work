package jiratest

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// The JQL the fake understands: enough for a sync, and a 400 in Jira's
// words for the rest.
//
//	query   = [or] [ORDER BY key [ASC|DESC] {, key [ASC|DESC]}]
//	or      = and {OR and}
//	and     = not {AND not}
//	not     = NOT not | ( or ) | clause
//	clause  = field op value | field [NOT] IN ( value {, value} ) | field IS [NOT] EMPTY
//
// Fields: project, key/issuekey, id, updated, created, parent, status,
// issuetype/type, statusCategory, assignee, reporter, labels.
// Dates are read in the caller's profile zone (api.md §2.5, C6).

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
			op := string(rs[i:j])
			if op == "!" {
				return nil, jqlError("Error in the JQL Query: The character '!' is a reserved JQL character. (line 1, character %d)", i+1)
			}
			toks = append(toks, token{tOp, op, i + 1})
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

type jqlValue struct {
	text   string
	fn     bool // a function call, text is its name
	quoted bool
}

type jqlClause struct {
	field string
	op    string // = != > >= < <= ~ !~ in "not in" "is" "is not"
	vals  []jqlValue
	pos   int
}

type jqlNode struct {
	op          string // and, or, not, clause
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
	got := t.text
	if t.kind == tEOF {
		return jqlError("Error in the JQL Query: Expecting %s but reached the end of the query.", want)
	}
	return jqlError("Error in the JQL Query: Expecting %s but got '%s'. (line 1, character %d)", want, got, t.pos)
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
	l, err := p.not()
	for err == nil && p.peek().is("and") {
		p.next()
		var r *jqlNode
		if r, err = p.not(); err == nil {
			l = &jqlNode{op: "and", left: l, right: r}
		}
	}
	return l, err
}

func (p *parser) not() (*jqlNode, *apiError) {
	t := p.peek()
	switch {
	case t.is("not"):
		p.next()
		n, err := p.not()
		return &jqlNode{op: "not", left: n}, err
	case t.kind == tLParen:
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
	c := &jqlClause{field: f.text, pos: f.pos}
	t := p.next()
	switch {
	case t.kind == tOp:
		c.op = t.text
		v, err := p.value()
		if err != nil {
			return nil, err
		}
		c.vals = []jqlValue{v}
		return c, nil
	case t.is("in"):
		c.op = "in"
	case t.is("not"):
		if !p.next().is("in") {
			return nil, unexpected(p.toks[p.i-1], "'IN'")
		}
		c.op = "not in"
	case t.is("is"):
		c.op = "is"
		if p.peek().is("not") {
			p.next()
			c.op = "is not"
		}
		v := p.next()
		if !v.is("empty") && !v.is("null") {
			return nil, unexpected(v, "'EMPTY' or 'NULL'")
		}
		return c, nil
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

func (p *parser) value() (jqlValue, *apiError) {
	t := p.next()
	switch t.kind {
	case tString:
		return jqlValue{text: t.text, quoted: true}, nil
	case tWord:
		if p.peek().kind == tLParen {
			p.next()
			if c := p.next(); c.kind != tRParen {
				return jqlValue{}, jqlError("jiratest: JQL function arguments are not supported (%s)", t.text)
			}
			return jqlValue{text: t.text, fn: true}, nil
		}
		return jqlValue{text: t.text}, nil
	}
	return jqlValue{}, unexpected(t, "a value")
}

// jqlEnv is what a query is compiled against.
type jqlEnv struct {
	s    *Server
	user *User // nil when anonymous
	loc  *time.Location
	now  time.Time
	// hidden makes every project invisible (no Browse projects), which
	// Jira reports as the project not existing.
	hidden bool
}

type pred func(st *issueState) bool

// knownJiraFields are real JQL fields the fake does not implement, so the
// error says so instead of claiming the field does not exist.
var knownJiraFields = []string{"summary", "description", "text", "comment", "sprint", "rank", "priority",
	"creator", "resolution", "resolved", "due", "duedate", "environment", "fixversion", "component",
	"watcher", "voter", "epic link", "filter", "attachments", "worklogdate"}

func (e *jqlEnv) compile(n *jqlNode) (pred, *apiError) {
	switch n.op {
	case "and", "or":
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
	case "not":
		l, err := e.compile(n.left)
		if err != nil {
			return nil, err
		}
		return func(st *issueState) bool { return !l(st) }, nil
	}
	return e.clause(n.clause)
}

func (e *jqlEnv) clause(c *jqlClause) (pred, *apiError) {
	field := strings.ToLower(c.field)
	for _, v := range c.vals {
		if v.fn && !strings.EqualFold(v.text, "currentUser") {
			return nil, jqlError("Unable to find JQL function '%s()'.", v.text)
		}
	}
	switch field {
	case "project":
		return e.setClause(c, []string{"=", "!=", "in", "not in"}, func(v jqlValue) (func(*issueState) bool, *apiError) {
			p := e.s.projectByKey(v.text)
			if p == nil {
				for _, q := range e.s.projects {
					if strings.EqualFold(q.def.Name, v.text) || strings.EqualFold(q.def.Key, v.text) {
						p = q
					}
				}
			}
			if (p == nil || e.hidden) && e.user != nil {
				return nil, jqlError("The value '%s' does not exist for the field 'project'.", v.text)
			}
			return func(st *issueState) bool { return st.project == p }, nil
		})
	case "key", "issuekey", "id":
		return e.issueClause(c, field)
	case "parent":
		return e.setClause(c, []string{"=", "!=", "in", "not in", "is", "is not"}, func(v jqlValue) (func(*issueState) bool, *apiError) {
			if v.text == "" {
				return func(st *issueState) bool { return st.parent == 0 }, nil
			}
			rec := e.s.lookup(v.text)
			if rec == nil {
				return nil, jqlError("An issue with key '%s' does not exist for field 'parent'.", v.text)
			}
			return func(st *issueState) bool { return st.parent == rec.cur.id }, nil
		})
	case "status":
		return e.setClause(c, []string{"=", "!=", "in", "not in"}, func(v jqlValue) (func(*issueState) bool, *apiError) {
			found := false
			for _, p := range e.s.projects {
				for _, st := range p.def.Statuses {
					found = found || st.ID == v.text || strings.EqualFold(st.Name, v.text)
				}
			}
			if !found {
				return nil, jqlError("The value '%s' does not exist for the field 'status'.", v.text)
			}
			return func(st *issueState) bool { return st.status.ID == v.text || strings.EqualFold(st.status.Name, v.text) }, nil
		})
	case "issuetype", "type":
		return e.setClause(c, []string{"=", "!=", "in", "not in"}, func(v jqlValue) (func(*issueState) bool, *apiError) {
			found := false
			for _, p := range e.s.projects {
				found = found || p.issueType(v.text) != nil
			}
			if !found {
				return nil, jqlError("The value '%s' does not exist for the field 'issuetype'.", v.text)
			}
			return func(st *issueState) bool { return st.typ.ID == v.text || strings.EqualFold(st.typ.Name, v.text) }, nil
		})
	case "statuscategory":
		return e.setClause(c, []string{"=", "!=", "in", "not in"}, func(v jqlValue) (func(*issueState) bool, *apiError) {
			for _, cat := range categories {
				if cat.key == strings.ToLower(v.text) || strings.EqualFold(cat.name, v.text) || strconv.Itoa(cat.id) == v.text {
					return func(st *issueState) bool { return st.status.Category == cat.key }, nil
				}
			}
			return nil, jqlError("The value '%s' does not exist for the field 'statusCategory'.", v.text)
		})
	case "assignee", "reporter":
		return e.setClause(c, []string{"=", "!=", "in", "not in", "is", "is not"}, func(v jqlValue) (func(*issueState) bool, *apiError) {
			id := v.text
			if v.fn {
				if e.user == nil {
					return func(*issueState) bool { return false }, nil
				}
				id = e.user.AccountID
			}
			get := func(st *issueState) string { return st.assignee }
			if field == "reporter" {
				get = func(st *issueState) string { return st.reporter }
			}
			return func(st *issueState) bool { return get(st) == id }, nil
		})
	case "labels":
		return e.setClause(c, []string{"=", "!=", "in", "not in", "is", "is not"}, func(v jqlValue) (func(*issueState) bool, *apiError) {
			if v.text == "" {
				return func(st *issueState) bool { return len(st.labels) == 0 }, nil
			}
			return func(st *issueState) bool { return slices.Contains(st.labels, v.text) }, nil
		})
	case "updated", "updateddate", "created", "createddate":
		return e.dateClause(c, strings.TrimSuffix(field, "date"))
	}
	for _, k := range knownJiraFields {
		if field == k {
			return nil, jqlError("jiratest: the fake does not implement the JQL field '%s'.", c.field)
		}
	}
	return nil, jqlError("Field '%s' does not exist or you do not have permission to view it.", c.field)
}

func opNotSupported(c *jqlClause) *apiError {
	op := strings.ToUpper(c.op)
	return jqlError("The operator '%s' is not supported by the '%s' field.", op, c.field)
}

// setClause compiles =, !=, IN, NOT IN and IS [NOT] EMPTY from a matcher
// of one value; the empty value stands for EMPTY.
func (e *jqlEnv) setClause(c *jqlClause, ops []string, one func(jqlValue) (func(*issueState) bool, *apiError)) (pred, *apiError) {
	if !slices.Contains(ops, c.op) {
		return nil, opNotSupported(c)
	}
	vals := c.vals
	if c.op == "is" || c.op == "is not" {
		vals = []jqlValue{{}}
	}
	var ms []func(*issueState) bool
	for _, v := range vals {
		m, err := one(v)
		if err != nil {
			return nil, err
		}
		ms = append(ms, m)
	}
	matchAny := func(st *issueState) bool {
		for _, m := range ms {
			if m(st) {
				return true
			}
		}
		return false
	}
	switch c.op {
	case "!=", "not in", "is not":
		return func(st *issueState) bool { return !matchAny(st) }, nil
	}
	return matchAny, nil
}

func (e *jqlEnv) issueClause(c *jqlClause, field string) (pred, *apiError) {
	if !slices.Contains([]string{"=", "!=", "in", "not in"}, c.op) {
		return nil, opNotSupported(c)
	}
	ids := map[int]bool{}
	for _, v := range c.vals {
		if field == "id" {
			n, err := strconv.Atoi(v.text)
			if err != nil {
				return nil, jqlError("The value '%s' for field 'id' is invalid.", v.text)
			}
			ids[n] = true
			continue
		}
		// Old keys resolve to the moved issue (api-vetting.md §4.7).
		rec := e.s.lookup(v.text)
		if rec == nil {
			return nil, jqlError("An issue with key '%s' does not exist for field '%s'.", v.text, c.field)
		}
		ids[rec.cur.id] = true
	}
	neg := c.op == "!=" || c.op == "not in"
	return func(st *issueState) bool { return ids[st.id] != neg }, nil
}

var datePeriod = regexp.MustCompile(`^([-+]?)((?:\s*\d+\s*[wdhm]?)+)$`)
var periodPart = regexp.MustCompile(`(\d+)\s*([wdhm]?)`)

// jqlDate reads a date literal in loc (api.md §2.5): absolute to the
// minute, date-only as midnight, or a period relative to now.
func jqlDate(v string, loc *time.Location, now time.Time) (time.Time, bool) {
	v = strings.TrimSpace(v)
	for _, layout := range []string{"2006/01/02 15:04", "2006-01-02 15:04", "2006/01/02", "2006-01-02"} {
		if t, err := time.ParseInLocation(layout, v, loc); err == nil {
			return t, true
		}
	}
	m := datePeriod.FindStringSubmatch(v)
	if m == nil {
		return time.Time{}, false
	}
	var d time.Duration
	for _, part := range periodPart.FindAllStringSubmatch(m[2], -1) {
		n, _ := strconv.Atoi(part[1])
		unit := map[string]time.Duration{"w": 7 * 24 * time.Hour, "d": 24 * time.Hour, "h": time.Hour, "m": time.Minute, "": time.Minute}[part[2]]
		d += time.Duration(n) * unit
	}
	if m[1] == "-" {
		d = -d
	}
	return now.Add(d), true
}

func (e *jqlEnv) dateClause(c *jqlClause, field string) (pred, *apiError) {
	if !slices.Contains([]string{"=", "!=", ">", ">=", "<", "<="}, c.op) {
		return nil, opNotSupported(c)
	}
	t, ok := jqlDate(c.vals[0].text, e.loc, e.now)
	if !ok || c.vals[0].fn {
		return nil, jqlError("Date value '%s' for field '%s' is invalid. Valid formats include: 'yyyy/MM/dd HH:mm', 'yyyy-MM-dd HH:mm', 'yyyy/MM/dd', 'yyyy-MM-dd', or a period format e.g. '-5d', '4w 2d'.", c.vals[0].text, c.field)
	}
	get := func(st *issueState) time.Time { return st.updated }
	if field == "created" {
		get = func(st *issueState) time.Time { return st.created }
	}
	cmp := map[string]func(a time.Time) bool{
		"=":  func(a time.Time) bool { return a.Equal(t) },
		"!=": func(a time.Time) bool { return !a.Equal(t) },
		">":  func(a time.Time) bool { return a.After(t) },
		">=": func(a time.Time) bool { return !a.Before(t) },
		"<":  func(a time.Time) bool { return a.Before(t) },
		"<=": func(a time.Time) bool { return !a.After(t) },
	}[c.op]
	return func(st *issueState) bool { return cmp(get(st)) }, nil
}

// sorter compiles ORDER BY; the default is created DESC (a guess), and id
// breaks every tie so pages are stable.
func (e *jqlEnv) sorter(keys []orderKey) (func(a, b *issueState) int, *apiError) {
	if len(keys) > 7 {
		return nil, jqlError("The ORDER BY clause can contain a maximum of 7 fields.")
	}
	if len(keys) == 0 {
		keys = []orderKey{{field: "created", desc: true}}
	}
	var cmps []func(a, b *issueState) int
	for _, k := range keys {
		var f func(a, b *issueState) int
		switch strings.ToLower(k.field) {
		case "updated", "updateddate":
			f = func(a, b *issueState) int { return a.updated.Compare(b.updated) }
		case "created", "createddate":
			f = func(a, b *issueState) int { return a.created.Compare(b.created) }
		case "key", "issuekey":
			f = compareKeys
		case "id":
			f = func(a, b *issueState) int { return a.id - b.id }
		case "rank":
			rank := e.s.rankField()
			f = func(a, b *issueState) int {
				return strings.Compare(fmt.Sprint(a.custom[rank]), fmt.Sprint(b.custom[rank]))
			}
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

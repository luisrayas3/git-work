// Package jiratest is an in-memory fake of Jira Cloud's REST API (platform v3),
// served over net/http/httptest, for the tests of the Jira client, of schema
// discovery and of the sync engine.
//
// It is written from doc/design/bridges/jira-api.md and jira-api-vetting.md, not from the
// client: it imports nothing from jira/jiraapi and speaks raw JSON with its
// own types, so a wrong struct tag in the client fails a test instead of
// being mirrored. Where the documentation leaves a behaviour open, the fake
// takes the harder side by default and offers the other as an Option.
package jiratest

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	_ "time/tzdata" // the zones of the fixtures, whatever the host has
)

// Server is a running fake Jira site.
type Server struct {
	t   testing.TB
	srv *httptest.Server
	cfg config

	mu       sync.Mutex
	manual   time.Time
	siteZone *time.Location
	site     Site
	rng      *rand.Rand

	users       []*User
	projects    []*project
	priorities  []Priority
	resolutions []Resolution
	linkTypes   []LinkType
	fields      []*CustomField

	issues   map[int]*record
	keys     map[string]int // upper-case key, old keys included, to issue id
	links    map[int]*link
	cursors  map[string]*cursor
	next     counters
	tick     int // search requests served; the index lags by it
	requests []Request
	writes   int

	rateNext     int
	window       time.Time
	windowCount  int
	conflictNext int
	denied       map[string]bool
}

type counters struct {
	issue, link, comment, history, cursor, rank, localID int
}

// Request is one request the fake served, for asserting what a client did.
type Request struct {
	Op     string // the operationId of the matched route, "" when none matched
	Method string
	Path   string
	Query  url.Values
	Header http.Header
	Body   []byte
	Status int
}

// New starts a fake Jira seeded with CompanySite unless WithSite says
// otherwise, and closes it when the test ends.
func New(t testing.TB, opts ...Option) *Server {
	t.Helper()
	cfg := defaultConfig()
	for _, o := range opts {
		o(&cfg)
	}
	s := &Server{
		t:       t,
		cfg:     cfg,
		manual:  cfg.start,
		rng:     rand.New(rand.NewSource(cfg.seed)),
		issues:  map[int]*record{},
		keys:    map[string]int{},
		links:   map[int]*link{},
		cursors: map[string]*cursor{},
		denied:  map[string]bool{},
		next: counters{issue: 10000, link: 10100, comment: 10200, history: 10300,
			rank: 1000, localID: 1},
	}
	for _, op := range cfg.denied {
		s.denied[op] = true
	}
	site := CompanySite()
	if cfg.site != nil {
		site = *cfg.site
	}
	if err := s.seed(site); err != nil {
		t.Fatalf("jiratest: %v", err)
	}
	s.srv = httptest.NewServer(s)
	t.Cleanup(s.srv.Close)
	return s
}

// URL is the site's base URL, e.g. http://127.0.0.1:1234.
func (s *Server) URL() string { return s.srv.URL }

// Credentials are the email and API token of Site.Me, for Basic auth.
func (s *Server) Credentials() (email, token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u := s.user(s.site.Me)
	if u == nil {
		return "", ""
	}
	return u.Email, u.APIToken
}

// AuthHeader is the Authorization header value for Credentials().
func (s *Server) AuthHeader() string {
	email, token := s.Credentials()
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(email+":"+token))
}

// Now is the server clock.
func (s *Server) Now() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.clock()
}

// Advance moves the manual clock forward. It fails the test when the
// clock was injected with WithClock.
func (s *Server) Advance(d time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cfg.clock != nil {
		s.t.Fatalf("jiratest: Advance on an injected clock")
	}
	s.manual = s.manual.Add(d)
}

// Requests is the log of every request served, in order.
func (s *Server) Requests() []Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Request(nil), s.requests...)
}

// ResetRequests clears the request log and the write count.
func (s *Server) ResetRequests() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requests = nil
	s.writes = 0
}

// Writes is the number of successful writes made over HTTP
// (the helpers acting as a web user are not counted).
func (s *Server) Writes() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.writes
}

// RateLimitNext answers the next n requests with a burst 429.
func (s *Server) RateLimitNext(n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rateNext = n
}

// ConflictNextTransitions answers the next n transition POSTs with 409,
// as Jira does for simultaneous transitions on one issue (C9).
func (s *Server) ConflictNextTransitions(n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.conflictNext = n
}

// Deny makes the caller lack the permission each operation needs
// (named by its operationId, e.g. "getFields", "getUser"), answered the
// way Jira documents for it: 404 where Jira hides existence, an empty
// result where it filters, 400 or 403 where the spec says so.
func (s *Server) Deny(ops ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, op := range ops {
		s.denied[op] = true
	}
}

// Allow undoes Deny.
func (s *Server) Allow(ops ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, op := range ops {
		delete(s.denied, op)
	}
}

func (s *Server) clock() time.Time {
	if s.cfg.clock != nil {
		return s.cfg.clock()
	}
	return s.manual
}

// tock is the clock moving on after a write, so that successive writes
// get distinct timestamps.
func (s *Server) tock() {
	if s.cfg.clock == nil {
		s.manual = s.manual.Add(s.cfg.autoAdvance)
	}
}

// call is one request on its way through a handler.
type call struct {
	r      *http.Request
	w      http.ResponseWriter
	user   *User // nil when anonymous
	vars   map[string]string
	q      url.Values
	body   []byte
	denied bool
}

func (c *call) v(name string) string { return c.vars[name] }

// decode reads the JSON body into v. An empty body is a 400, as Jira's.
func (c *call) decode(v any) error {
	if len(bytes.TrimSpace(c.body)) == 0 {
		return badRequest("The request body is missing.")
	}
	d := json.NewDecoder(bytes.NewReader(c.body))
	d.UseNumber()
	if err := d.Decode(v); err != nil {
		return badRequest(fmt.Sprintf("Unexpected character in the request body: %v", err))
	}
	return nil
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()

	body, _ := io.ReadAll(r.Body)
	rec := &recorder{ResponseWriter: w, status: http.StatusOK}
	c := &call{r: r, w: rec, q: r.URL.Query(), body: body}
	rt, vars, pathKnown := match(r.Method, r.URL.Path)
	c.vars = vars
	op := ""
	if rt != nil {
		op = rt.op
	}
	s.serve(c, rt, pathKnown)

	s.requests = append(s.requests, Request{
		Op: op, Method: r.Method, Path: r.URL.Path, Query: r.URL.Query(),
		Header: r.Header.Clone(), Body: body, Status: rec.status,
	})
	if rt != nil && rt.write && rec.status < 300 {
		s.writes++
		s.tock()
	}
}

func (s *Server) serve(c *call, rt *route, pathKnown bool) {
	h := c.w.Header()
	h.Set("Date", s.clock().UTC().Format(http.TimeFormat))
	h.Set("Content-Type", "application/json;charset=UTF-8")
	if s.rateLimited(c) {
		return
	}

	user, ok := s.authenticate(c.r)
	if !ok {
		writeError(c.w, &apiError{status: http.StatusUnauthorized,
			messages: []string{"Client must be authenticated to access this resource."}})
		return
	}
	c.user = user
	if user != nil {
		h.Set("X-AAccountId", user.AccountID)
	}

	switch {
	case rt == nil && !pathKnown:
		writeError(c.w, notFound("null for uri: "+c.r.URL.String()))
		return
	case rt == nil:
		writeError(c.w, &apiError{status: http.StatusMethodNotAllowed,
			messages: []string{"Method " + c.r.Method + " is not allowed."}})
		return
	}
	if user == nil && !rt.anonymous {
		writeError(c.w, &apiError{status: http.StatusUnauthorized,
			messages: []string{"You are not authenticated. Authentication required to perform this operation."}})
		return
	}
	if s.denied[rt.op] {
		c.denied = true
		if deny, ok := denials[rt.op]; ok {
			if err := deny(c); err != nil {
				writeError(c.w, err)
				return
			}
		} else if !selfDenying[rt.op] {
			writeError(c.w, &apiError{status: http.StatusForbidden,
				messages: []string{"You do not have permission to perform this operation."}})
			return
		}
	}

	status, out, err := rt.h(s, c)
	if err != nil {
		var ae *apiError
		if !errors.As(err, &ae) {
			ae = &apiError{status: http.StatusInternalServerError, messages: []string{err.Error()}}
		}
		writeError(c.w, ae)
		return
	}
	writeJSON(c.w, status, out)
}

// authenticate answers the caller, nil for anonymous, and false for
// credentials that do not match an account (401).
func (s *Server) authenticate(r *http.Request) (*User, bool) {
	auth := r.Header.Get("Authorization")
	if auth == "" {
		// Many operations run anonymously when the header is missing (§17.5).
		return nil, s.cfg.missingAuth == AnonymousOnMissingAuth
	}
	scheme, cred, _ := strings.Cut(auth, " ")
	switch strings.ToLower(scheme) {
	case "basic":
		raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(cred))
		if err != nil {
			return nil, false
		}
		email, token, _ := strings.Cut(string(raw), ":")
		for _, u := range s.users {
			if u.APIToken != "" && strings.EqualFold(u.Email, email) && u.APIToken == token {
				return u, true
			}
		}
	case "bearer":
		for _, u := range s.users {
			if u.APIToken != "" && u.APIToken == strings.TrimSpace(cred) {
				return u, true
			}
		}
	}
	return nil, false
}

// rateLimited answers 429 for RateLimitNext and for the WithRateLimit window.
func (s *Server) rateLimited(c *call) bool {
	now := s.clock()
	if s.rateNext > 0 {
		s.rateNext--
		s.write429(c.w, "jira-burst-based", s.cfg.retryAfter, 350, now.Add(time.Duration(s.cfg.retryAfter)*time.Second))
		return true
	}
	if s.cfg.rateLimit <= 0 {
		return false
	}
	if s.window.IsZero() || !now.Before(s.window.Add(s.cfg.rateWindow)) {
		s.window, s.windowCount = now, 0
	}
	reset := s.window.Add(s.cfg.rateWindow)
	if s.windowCount >= s.cfg.rateLimit {
		s.write429(c.w, "jira-burst-based", secondsUntil(now, reset), s.cfg.rateLimit, reset)
		return true
	}
	s.windowCount++
	h := c.w.Header()
	h.Set("X-RateLimit-Limit", strconv.Itoa(s.cfg.rateLimit))
	h.Set("X-RateLimit-Remaining", strconv.Itoa(s.cfg.rateLimit-s.windowCount))
	near := s.cfg.rateLimit-s.windowCount < s.cfg.rateLimit/5
	h.Set("X-RateLimit-NearLimit", strconv.FormatBool(near))
	return false
}

// write429 is the documented 429 (jira-api.md §12.1, jira-api-vetting.md R19).
func (s *Server) write429(w http.ResponseWriter, reason string, retryAfter, limit int, reset time.Time) {
	writeError(w, rateError(reason, retryAfter, limit, reset))
}

func rateError(reason string, retryAfter, limit int, reset time.Time) *apiError {
	return &apiError{status: http.StatusTooManyRequests, messages: []string{"Rate limit exceeded."},
		headers: map[string]string{
			"Retry-After":           strconv.Itoa(retryAfter),
			"X-RateLimit-Limit":     strconv.Itoa(limit),
			"X-RateLimit-Remaining": "0",
			"X-RateLimit-Reset":     reset.UTC().Format(time.RFC3339),
			"RateLimit-Reason":      reason,
			"Content-Type":          "application/json",
		}}
}

func secondsUntil(now, t time.Time) int {
	d := t.Sub(now)
	n := int((d + time.Second - 1) / time.Second)
	if n < 1 {
		n = 1
	}
	return n
}

type recorder struct {
	http.ResponseWriter
	status int
}

func (r *recorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	if v == nil {
		w.Header().Del("Content-Type")
		w.WriteHeader(status)
		return
	}
	b, err := json.Marshal(v)
	if err != nil {
		writeError(w, &apiError{status: http.StatusInternalServerError, messages: []string{err.Error()}})
		return
	}
	w.WriteHeader(status)
	_, _ = w.Write(b)
}

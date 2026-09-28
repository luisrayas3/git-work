// Package jiraapi is a small client for the Jira Cloud platform REST API v3,
// standard library only. It speaks JSON, retries what Jira asks it to retry,
// and leaves the meaning of site-specific fields to its caller.
//
// The authority for every behaviour below is doc/design/bridges/jira-api.md and its
// vetting, jira-api-vetting.md; section numbers in comments refer to jira-api.md.
package jiraapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"math/rand/v2"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Config is what New needs. Only BaseURL, Email and Token are required.
type Config struct {
	// BaseURL is https://<site>.atlassian.net, or
	// https://api.atlassian.com/ex/jira/<cloudId> for scoped tokens (§1.1).
	BaseURL string
	Email   string
	Token   string

	HTTPClient *http.Client // nil means one with DefaultTimeout

	// MaxRetries bounds the retries of one call; 0 means 4 (§12.1), and a
	// negative value disables retrying.
	MaxRetries int
	// MaxRetryWait is the longest Retry-After the client will sleep through;
	// a longer one is returned as an *Error carrying RetryAfter. 0 means 60s.
	MaxRetryWait time.Duration

	// Sleep waits d or until ctx is done; nil means a real timer. Tests
	// replace it to observe the delays without waiting.
	Sleep func(ctx context.Context, d time.Duration) error
	// Rand returns a number in [0, 1) for jitter; nil means math/rand/v2.
	Rand func() float64
}

// Client is safe for concurrent use.
type Client struct {
	base       string
	auth       string
	hc         *http.Client
	maxRetries int
	maxWait    time.Duration
	sleep      func(context.Context, time.Duration) error
	rand       func() float64

	mu   sync.Mutex
	date time.Time
}

// DefaultTimeout bounds one request, so a stalled connection cannot hang a
// cron run forever; retries then apply as to any transport error.
const DefaultTimeout = 60 * time.Second

// New returns a client authenticating with Basic email:token (§1.1).
func New(cfg Config) *Client {
	c := &Client{
		base:       strings.TrimRight(cfg.BaseURL, "/"),
		auth:       "Basic " + basic(cfg.Email, cfg.Token),
		hc:         cfg.HTTPClient,
		maxRetries: cfg.MaxRetries,
		maxWait:    cfg.MaxRetryWait,
		sleep:      cfg.Sleep,
		rand:       cfg.Rand,
	}
	if c.hc == nil {
		c.hc = &http.Client{Timeout: DefaultTimeout}
	}
	switch {
	case c.maxRetries == 0:
		c.maxRetries = 4
	case c.maxRetries < 0:
		c.maxRetries = 0
	}
	if c.maxWait == 0 {
		c.maxWait = time.Minute
	}
	if c.sleep == nil {
		c.sleep = sleep
	}
	if c.rand == nil {
		c.rand = rand.Float64
	}
	return c
}

func basic(email, token string) string {
	return base64.StdEncoding.EncodeToString([]byte(email + ":" + token))
}

// ServerDate is the Date header of the last response received, the server's
// clock, zero before the first response. Compare it with the local clock
// before computing a JQL watermark (jira-api-vetting.md §4.4).
func (c *Client) ServerDate() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.date
}

// Error is a non-2xx response. Jira's body is an ErrorCollection (§12.2),
// decoded leniently: Messages and Fields are empty when the body is not one.
type Error struct {
	Method     string
	Path       string
	StatusCode int
	Messages   []string          `json:"errorMessages"`
	Fields     map[string]string `json:"errors"`
	Body       []byte
	// RetryAfter is the server's Retry-After, when it sent one.
	RetryAfter time.Duration
}

func (e *Error) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "jira: %s %s: %d %s", e.Method, e.Path, e.StatusCode, http.StatusText(e.StatusCode))
	for _, m := range e.Messages {
		b.WriteString(": ")
		b.WriteString(m)
	}
	for _, k := range slices.Sorted(maps.Keys(e.Fields)) {
		fmt.Fprintf(&b, ": %s: %s", k, e.Fields[k])
	}
	if e.StatusCode == http.StatusUnauthorized {
		b.WriteString(" (API token invalid or expired?)")
	}
	return b.String()
}

// StatusCode is the HTTP status of an *Error in err's chain, or 0.
func StatusCode(err error) int {
	var e *Error
	if errors.As(err, &e) {
		return e.StatusCode
	}
	return 0
}

// request is one call. Safe marks a request that may be repeated after a 503:
// every GET, PUT and DELETE, and the few POSTs that only read or are
// idempotent by contract. Any request is repeated after a 429, because a
// rate-limited request is "rejected" before it is processed (§12.1).
type request struct {
	method string
	path   string
	query  url.Values
	body   any
	out    any
	safe   bool
}

func (c *Client) get(ctx context.Context, path string, q url.Values, out any) error {
	_, err := c.do(ctx, request{method: http.MethodGet, path: path, query: q, out: out})
	return err
}

func (c *Client) do(ctx context.Context, r request) (int, error) {
	var payload []byte
	if r.body != nil {
		var err error
		if payload, err = marshal(r.body); err != nil {
			return 0, err
		}
	}
	safe := r.safe || r.method != http.MethodPost
	u := c.base + r.path
	if len(r.query) > 0 {
		u += "?" + r.query.Encode()
	}
	for attempt := 0; ; attempt++ {
		status, body, hdr, err := c.once(ctx, r.method, u, payload)
		if err != nil {
			return 0, err
		}
		if status >= 200 && status < 300 {
			if r.out != nil && len(bytes.TrimSpace(body)) > 0 {
				if err := json.Unmarshal(body, r.out); err != nil {
					return status, fmt.Errorf("jira: %s %s: decoding response: %w", r.method, r.path, err)
				}
			}
			return status, nil
		}
		e := &Error{Method: r.method, Path: r.path, StatusCode: status, Body: body}
		_ = json.Unmarshal(body, e)
		ra, hasRA := retryAfter(hdr.Get("Retry-After"), c.ServerDate())
		e.RetryAfter = ra
		retryable := status == http.StatusTooManyRequests || (status == http.StatusServiceUnavailable && safe)
		if !retryable || attempt >= c.maxRetries || ra > c.maxWait {
			return status, e
		}
		if err := c.sleep(ctx, c.backoff(attempt, ra, hasRA)); err != nil {
			return status, err
		}
	}
}

func (c *Client) once(ctx context.Context, method, u string, payload []byte) (int, []byte, http.Header, error) {
	var body io.Reader
	if payload != nil {
		body = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return 0, nil, nil, err
	}
	req.Header.Set("Authorization", c.auth)
	req.Header.Set("Accept", "application/json")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return 0, nil, nil, err
	}
	defer resp.Body.Close()
	if d, err := http.ParseTime(resp.Header.Get("Date")); err == nil {
		c.mu.Lock()
		c.date = d
		c.mu.Unlock()
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, nil, nil, err
	}
	return resp.StatusCode, b, resp.Header, nil
}

// backoff follows the rate-limiting page (§12.1): Retry-After is the minimum
// delay; without it, 2s doubling to a 30s cap; both jittered by up to 30%.
func (c *Client) backoff(attempt int, ra time.Duration, hasRA bool) time.Duration {
	if hasRA {
		return ra + time.Duration(float64(ra)*0.3*c.rand())
	}
	d := min(2*time.Second<<attempt, 30*time.Second)
	return time.Duration(float64(d) * (0.7 + 0.6*c.rand()))
}

// retryAfter reads delay-seconds or an HTTP date, the latter against the
// server's own clock when known.
func retryAfter(v string, now time.Time) (time.Duration, bool) {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0, false
	}
	if n, err := strconv.Atoi(v); err == nil && n >= 0 {
		return time.Duration(n) * time.Second, true
	}
	if t, err := http.ParseTime(v); err == nil {
		if now.IsZero() {
			now = time.Now()
		}
		return max(t.Sub(now), 0), true
	}
	return 0, false
}

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// marshal is json.Marshal without HTML escaping, so ADF text stays legible
// on the wire.
func marshal(v any) ([]byte, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(b.Bytes(), []byte("\n")), nil
}

func esc(s string) string { return url.PathEscape(s) }

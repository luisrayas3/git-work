package jiratest

import "time"

// Option configures New. Each knob defaults to the documented behaviour,
// or to the harder side where the documentation leaves it open.
type Option func(*config)

// MissingAuth is what a request without an Authorization header gets.
type MissingAuth int

const (
	// AnonymousOnMissingAuth runs the request as anonymous: search answers
	// 200 with no issues, issue reads 404, writes and /myself 401 (§17.5).
	AnonymousOnMissingAuth MissingAuth = iota
	// RejectMissingAuth answers 401, as for bad credentials.
	RejectMissingAuth
)

// WriteLimit is a per-issue write limit: at most N writes within Window.
type WriteLimit struct {
	N      int
	Window time.Duration
}

type config struct {
	site        *Site
	clock       func() time.Time
	start       time.Time
	autoAdvance time.Duration
	siteZone    string

	lagSearches int
	lagDuration time.Duration
	staleReads  bool
	pageCap     int

	missingAuth        MissingAuth
	commentEditBumps   bool
	commentDeleteBumps bool
	commentProperties  bool
	shuffleChangelog   bool
	omitCustomFieldIDs bool
	adfLocalIDs        bool

	retryAfter int
	rateLimit  int
	rateWindow time.Duration
	perIssue   []WriteLimit

	denied []string
	seed   int64

	looseHierarchy bool
}

func defaultConfig() config {
	return config{
		start:              time.Date(2026, 7, 1, 16, 0, 0, 0, time.UTC),
		autoAdvance:        time.Second,
		lagSearches:        1,
		pageCap:            37,
		shuffleChangelog:   true,
		omitCustomFieldIDs: true,
		adfLocalIDs:        true,
		commentProperties:  true,
		retryAfter:         1,
		// jira-api.md §12.1: 20 writes per 2 s and 100 per 30 s on one issue.
		perIssue: []WriteLimit{{N: 20, Window: 2 * time.Second}, {N: 100, Window: 30 * time.Second}},
		seed:     1,
	}
}

// WithCommentProperties sets whether GET …/comment?expand=properties returns
// the properties a comment was created with. jira-api.md documents the field on
// Comment but not the expand on the list (JS12); default true.
func WithCommentProperties(b bool) Option { return func(c *config) { c.commentProperties = b } }

// WithSite seeds the fake with site instead of CompanySite.
func WithSite(site Site) Option { return func(c *config) { c.site = &site } }

// WithClock injects the server clock; Advance then fails the test.
func WithClock(now func() time.Time) Option { return func(c *config) { c.clock = now } }

// WithStart sets the manual clock's start (default 2026-07-01T16:00:00Z).
func WithStart(t time.Time) Option { return func(c *config) { c.start = t } }

// WithAutoAdvance sets how far the manual clock moves after every write,
// over HTTP or through a helper (default 1s; 0 freezes it).
func WithAutoAdvance(d time.Duration) Option { return func(c *config) { c.autoAdvance = d } }

// WithSiteTimeZone overrides Site.TimeZone, the zone timestamps render in.
func WithSiteTimeZone(name string) Option { return func(c *config) { c.siteZone = name } }

// WithIndexLag makes a write invisible to search for the next `searches`
// search requests and until d has passed on the server clock, unless the
// issue is named in reconcileIssues (jira-api.md §2.4). Default 1 search, 0.
func WithIndexLag(searches int, d time.Duration) Option {
	return func(c *config) { c.lagSearches, c.lagDuration = searches, d }
}

// WithStaleReads makes GET /issue read the lagging index too, for tests of
// a client that must not trust read-after-write (design-sync Risks).
func WithStaleReads() Option { return func(c *config) { c.staleReads = true } }

// WithSearchPageCap caps a search page below the requested maxResults
// (default 37: maxResults is advisory, jira-api-vetting.md §4.9).
func WithSearchPageCap(n int) Option { return func(c *config) { c.pageCap = n } }

// WithMissingAuth sets what a request without credentials gets.
func WithMissingAuth(m MissingAuth) Option { return func(c *config) { c.missingAuth = m } }

// WithCommentBumps sets whether a comment edit and a comment delete bump the
// issue's updated. Adding one always does. Default: neither (the harder case).
func WithCommentBumps(edit, del bool) Option {
	return func(c *config) { c.commentEditBumps, c.commentDeleteBumps = edit, del }
}

// WithOrderedChangelog returns expand=changelog histories newest first,
// as the spec says, instead of shuffled (C8).
func WithOrderedChangelog() Option { return func(c *config) { c.shuffleChangelog = false } }

// WithCustomFieldIDs keeps fieldId on every custom-field changelog item,
// instead of omitting it on some (C8). IssueParentAssociation, Link and
// Key items never carry one.
func WithCustomFieldIDs() Option { return func(c *config) { c.omitCustomFieldIDs = false } }

// WithVerbatimADF stores ADF as sent, instead of normalising it the way
// Jira does (added localId attrs, merged text nodes; §4.12).
func WithVerbatimADF() Option { return func(c *config) { c.adfLocalIDs = false } }

// WithRetryAfter sets the Retry-After seconds of RateLimitNext's 429s (default 1).
func WithRetryAfter(seconds int) Option { return func(c *config) { c.retryAfter = seconds } }

// WithRateLimit allows n requests per window of the server clock and
// answers 429 beyond (burst limit). Off by default.
func WithRateLimit(n int, window time.Duration) Option {
	return func(c *config) { c.rateLimit, c.rateWindow = n, window }
}

// WithPerIssueWriteLimits replaces the per-issue write limits; none disables them.
func WithPerIssueWriteLimits(limits ...WriteLimit) Option {
	return func(c *config) { c.perIssue = limits }
}

// WithDenied is Deny at start.
func WithDenied(ops ...string) Option {
	return func(c *config) { c.denied = append(c.denied, ops...) }
}

// WithLooseHierarchy accepts any parent in the same project, whatever the
// types' declared hierarchy levels: a site whose data disagrees with its
// hierarchy, as AUT's Tasks with an Epic parent under a Task declared a
// sub-task type (pull-schema-check.md). Off by default.
func WithLooseHierarchy() Option { return func(c *config) { c.looseHierarchy = true } }

// WithSeed seeds the fake's pseudo-randomness (shuffles, omitted fieldIds).
func WithSeed(seed int64) Option { return func(c *config) { c.seed = seed } }

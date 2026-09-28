package host

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	_ "time/tzdata" // JQL literals are in the account's zone, any IANA zone (JS20)

	"github.com/gofrs/flock"

	"github.com/git-bug/git-bug/cache"
	"github.com/git-bug/git-bug/jira"
	"github.com/git-bug/git-bug/jira/jiraapi"
	"github.com/git-bug/git-bug/repository"
	"github.com/git-bug/git-bug/schema"
)

// The binding of a clone to one Jira project, in git config (JS3).
const (
	JiraURLKey     = "git-work.jira.url"
	JiraProjectKey = "git-work.jira.project"
	JiraEmailKey   = "git-work.jira.email"
	// JiraTokenEnv overrides git's credential helpers, for cron.
	JiraTokenEnv = "JIRA_API_TOKEN"
)

// JiraBinding is which site, project and account a clone syncs with.
type JiraBinding struct {
	URL, Project, Email string
}

func jiraBinding(repo *cache.RepoCache) (JiraBinding, error) {
	var b JiraBinding
	for _, kv := range []struct {
		key  string
		into *string
	}{{JiraURLKey, &b.URL}, {JiraProjectKey, &b.Project}, {JiraEmailKey, &b.Email}} {
		v, err := repo.AnyConfig().ReadString(kv.key)
		if err != nil && !errors.Is(err, repository.ErrNoConfigEntry) {
			return b, err
		}
		*kv.into = strings.TrimSpace(v)
	}
	if b.URL == "" || b.Project == "" || b.Email == "" {
		return b, fmt.Errorf("this clone is not bound to a Jira project: set %s, %s and %s with git config",
			JiraURLKey, JiraProjectKey, JiraEmailKey)
	}
	b.URL = strings.TrimRight(b.URL, "/")
	return b, checkJiraURL(b.URL)
}

// checkJiraURL accepts Jira Cloud only: REST v3 and account ids. A loopback
// address is accepted too, for a fake site.
func checkJiraURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("%s: %w", JiraURLKey, err)
	}
	host := u.Hostname()
	switch {
	case u.Scheme == "https" && strings.HasSuffix(host, ".atlassian.net"):
	case u.Scheme == "https" && host == "api.atlassian.com" && strings.HasPrefix(u.Path, "/ex/jira/"):
	case host == "localhost" || net.ParseIP(host).IsLoopback():
	default:
		return fmt.Errorf("%s = %s: only Jira Cloud is supported (https://<site>.atlassian.net or https://api.atlassian.com/ex/jira/<cloudId>); Data Center is not", JiraURLKey, raw)
	}
	return nil
}

// jiraToken is JIRA_API_TOKEN, else git's credential helpers; it never
// reaches argv.
func jiraToken(repo *cache.RepoCache, b JiraBinding) (string, error) {
	if t := os.Getenv(JiraTokenEnv); t != "" {
		return t, nil
	}
	t, err := repo.Credential(b.URL, b.Email)
	if err != nil {
		return "", err
	}
	if t == "" {
		return "", fmt.Errorf("no Jira API token: set %s, or store one with `git credential approve` (protocol, host, username=%s, password=<token>)", JiraTokenEnv, b.Email)
	}
	return t, nil
}

// jiraConnect binds, authenticates and discovers the project (JS3, JS4).
func jiraConnect(ctx context.Context, repo *cache.RepoCache) (JiraBinding, *jiraapi.Client, *jira.Project, error) {
	b, err := jiraBinding(repo)
	if err != nil {
		return b, nil, nil, err
	}
	token, err := jiraToken(repo, b)
	if err != nil {
		return b, nil, nil, err
	}
	c := jiraapi.New(jiraapi.Config{BaseURL: b.URL, Email: b.Email, Token: token})
	p, err := jira.Discover(ctx, c, b.Project)
	return b, c, p, err
}

// jiraDerive is the live schema with Jira's types, fields and values
// adopted or added (JS5).
func jiraDerive(repo *cache.RepoCache, p *jira.Project) (*schema.Document, []jira.Note, error) {
	current, _, err := SchemaExport(repo)
	if err != nil {
		return nil, nil, err
	}
	return jira.Derive(current, p)
}

// JiraSchema is `git work jira schema`: the derived document, for review
// before the first sync.
func JiraSchema(ctx context.Context, repo *cache.RepoCache) (*schema.Document, []jira.Note, error) {
	_, _, p, err := jiraConnect(ctx, repo)
	if err != nil {
		return nil, nil, err
	}
	return jiraDerive(repo, p)
}

// JiraSync is `git work jira sync`: derive and import the schema, compile
// the mapping, and run the engine; the state file is saved even when the run
// stops, since its cursor stops at the failure (JS20, JS23). It never pushes.
func JiraSync(ctx context.Context, repo *cache.RepoCache, opts jira.Options, emit func(jira.Line)) (jira.Summary, []jira.Note, error) {
	if !opts.DryRun {
		unlock, err := lockJiraSync(repo)
		if err != nil {
			return jira.Summary{}, nil, err
		}
		defer unlock()
	}
	b, c, p, err := jiraConnect(ctx, repo)
	if err != nil {
		return jira.Summary{}, nil, err
	}

	// JS5: the first mapping is reviewed, never derived and imported here
	live, err := repo.LoadSchema()
	if err != nil {
		return jira.Summary{}, nil, err
	}
	if err := jira.Mapped(live); err != nil {
		return jira.Summary{}, nil, err
	}

	doc, notes, err := jiraDerive(repo, p)
	if err != nil {
		return jira.Summary{}, notes, err
	}
	changes, _, err := SchemaImport(repo, doc, false, opts.DryRun)
	if len(changes) > 0 {
		emit(jira.Line{Schema: changes, DryRun: opts.DryRun})
	}
	if err != nil {
		return jira.Summary{}, notes, err
	}

	s, err := repo.LoadSchema()
	if err != nil {
		return jira.Summary{}, notes, err
	}
	m, more, err := jira.Compile(s, p)
	notes = append(notes, more...)
	if err != nil {
		return jira.Summary{}, notes, err
	}

	st, err := jira.LoadState(repo.LocalStorage())
	if err != nil {
		return jira.Summary{}, notes, err
	}
	st.Bind(b.URL, b.Project)
	sum, err := jira.Sync(ctx, repo, c, p, m, st, opts, emit)
	if !opts.DryRun {
		if serr := st.Save(repo.LocalStorage()); serr != nil && err == nil {
			err = serr
		}
	}
	return sum, notes, err
}

// jiraSyncLock excludes a second run for the whole of one, beside the
// state file: two runs would both POST one new issue (JS15). It never
// waits, and the kernel drops it when the process dies, like the write lock.
const jiraSyncLock = "jira/sync.lock"

func lockJiraSync(repo *cache.RepoCache) (func(), error) {
	root := repo.LocalStorage().Root()
	if fi, err := os.Stat(root); err != nil || !fi.IsDir() {
		return func() {}, nil // in-memory storage: no other process can share it
	}
	path := filepath.Join(root, filepath.FromSlash(jiraSyncLock))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	lock := flock.New(path)
	locked, err := lock.TryLock()
	if err != nil {
		return nil, err
	}
	if !locked {
		holder := ""
		if pid, err := os.ReadFile(path); err == nil && len(pid) > 0 {
			holder = " (pid " + strings.TrimSpace(string(pid)) + ")"
		}
		return nil, fmt.Errorf("a jira sync is already running in this clone%s; this run did nothing", holder)
	}
	_ = os.WriteFile(path, []byte(strconv.Itoa(os.Getpid())), 0o644)
	return func() { _ = lock.Unlock() }, nil
}

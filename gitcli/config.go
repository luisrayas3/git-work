package gitcli

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/git-bug/git-bug/repository"
)

// scope selects which git config files a reader consults.
type scope int

const (
	// scopeAny reads the merged view git itself would use:
	// system, global, local, worktree and command-line,
	// in that precedence.
	scopeAny scope = iota
	// scopeLocal reads the repository's own config:
	// local, then worktree, which overrides it.
	scopeLocal
	scopeGlobal
)

// flag returns the arguments selecting the scope.
// git only follows include.* directives by default when searching all files;
// a specific scope needs --includes explicitly.
// git has no flag for local and worktree together,
// so scopeLocal searches all files and keeps what sees allows.
func (s scope) flag() []string {
	switch s {
	case scopeGlobal:
		return []string{"--global", "--includes"}
	default:
		return nil
	}
}

// sees reports whether a value git reports from the named scope
// (as --show-scope names it) belongs to s.
func (s scope) sees(name string) bool {
	switch s {
	case scopeLocal:
		return name == "local" || name == "worktree"
	default:
		return true
	}
}

var _ repository.ConfigRead = &reader{}

// reader implements repository.ConfigRead by invoking `git config`.
type reader struct {
	git   runner
	scope scope
}

// config runs `git config` with the reader's scope and the given arguments.
// It returns stdout and the process exit code;
// `git config` uses exit 1 to signal "no such key",
// which callers interpret themselves.
func (r *reader) config(args ...string) ([]byte, int, error) {
	full := append([]string{"config"}, r.scope.flag()...)
	return r.git.output(append(full, args...)...)
}

// values returns every value git reports for key in the reader's scope,
// in file order.
// For scopeAny only the values from the highest-precedence scope
// that defines the key are returned,
// so a key set both globally and locally yields the local value(s) only
// — mirroring how git resolves it and how repository.mergedConfig behaves.
func (r *reader) values(key string, extra ...string) ([]string, error) {
	if !strings.Contains(key, ".") {
		return nil, fmt.Errorf("invalid key")
	}

	args := append([]string{"-z", "--show-scope", "--get-all"}, extra...)
	args = append(args, key)
	out, code, err := r.config(args...)
	if err != nil {
		return nil, err
	}
	if code == 1 {
		return nil, nil
	}
	if code != 0 {
		return nil, fmt.Errorf("git config: exit status %d", code)
	}

	// Output is a flat sequence of NUL-terminated fields:
	// scope, value, scope, value...
	fields := splitNul(out)
	if len(fields)%2 != 0 {
		return nil, fmt.Errorf("git config: unexpected output for key %s", key)
	}

	var values []string
	lastScope := ""
	for i := 0; i < len(fields); i += 2 {
		if !r.scope.sees(fields[i]) {
			continue
		}
		if fields[i] != lastScope {
			// git lists scopes in ascending precedence;
			// a new scope supersedes.
			lastScope = fields[i]
			values = values[:0]
		}
		values = append(values, fields[i+1])
	}
	return values, nil
}

// single reduces values to the one entry the repository.ConfigRead contract expects,
// surfacing ErrNoConfigEntry / ErrMultipleConfigEntry.
func (r *reader) single(key string, extra ...string) (string, error) {
	values, err := r.values(key, extra...)
	if err != nil {
		return "", err
	}
	switch len(values) {
	case 0:
		return "", fmt.Errorf("%w: missing key %s", repository.ErrNoConfigEntry, key)
	case 1:
		return values[0], nil
	default:
		return "", fmt.Errorf("%w: duplicated key %s", repository.ErrMultipleConfigEntry, key)
	}
}

func (r *reader) ReadAll(keyPrefix string) (map[string]string, error) {
	out, code, err := r.config("-z", "--show-scope", "--list")
	if err != nil {
		return nil, err
	}
	if code != 0 {
		return nil, fmt.Errorf("git config: exit status %d", code)
	}

	// Output alternates a scope and a key-value entry.
	fields := splitNul(out)
	if len(fields)%2 != 0 {
		return nil, fmt.Errorf("git config: unexpected output")
	}

	result := make(map[string]string)
	for i := 0; i < len(fields); i += 2 {
		if !r.scope.sees(fields[i]) {
			continue
		}
		key, value := splitEntry(fields[i+1])
		if strings.HasPrefix(key, keyPrefix) {
			// Later entries win,
			// which is git's own precedence order.
			result[key] = value
		}
	}
	return result, nil
}

func (r *reader) ReadBool(key string) (bool, error) {
	// --type=bool canonicalizes git's accepted spellings (yes/on/1 …)
	// and errors on anything else.
	val, err := r.single(key, "--type=bool")
	if err != nil {
		return false, err
	}
	return strconv.ParseBool(val)
}

func (r *reader) ReadString(key string) (string, error) {
	return r.single(key)
}

func (r *reader) ReadTimestamp(key string) (time.Time, error) {
	val, err := r.single(key)
	if err != nil {
		return time.Time{}, err
	}
	return repository.ParseTimestamp(val)
}

// splitNul splits `git config -z` output into its NUL-terminated records.
func splitNul(out []byte) []string {
	if len(out) == 0 {
		return nil
	}
	s := strings.TrimSuffix(string(out), "\x00")
	return strings.Split(s, "\x00")
}

// splitEntry splits a `git config -z --list` record into key and value.
// With -z the key and value are separated by a newline;
// a key declared without `=` has no newline and an empty value.
func splitEntry(entry string) (string, string) {
	key, value, _ := strings.Cut(entry, "\n")
	return key, value
}

var _ repository.Config = &config{}

// config pairs a CLI-backed reader with a writer:
// the CLI-backed one for the local scope,
// the wrapped repository's for the global scope.
type config struct {
	repository.ConfigRead
	repository.ConfigWrite
}

var _ repository.ConfigWrite = &writer{}

// writer implements repository.ConfigWrite for the local scope
// by invoking `git config --local`,
// so a value lands in the repository's own config file, as git puts it.
// go-git's writer cannot be trusted with it:
// once a config.worktree exists,
// go-git's config drops every section it does not model
// and its writes go to config.worktree instead.
type writer struct {
	git runner
}

// config runs `git config --local` with the given arguments,
// treating any non-zero exit as an error.
func (w *writer) config(args ...string) error {
	_, code, err := w.git.output(append([]string{"config", "--local"}, args...)...)
	if err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("git config: exit status %d", code)
	}
	return nil
}

func (w *writer) StoreString(key, value string) error {
	if !strings.Contains(key, ".") {
		return fmt.Errorf("invalid key")
	}
	return w.config("--replace-all", "--", key, value)
}

func (w *writer) StoreTimestamp(key string, value time.Time) error {
	return w.StoreString(key, strconv.Itoa(int(value.Unix())))
}

func (w *writer) StoreBool(key string, value bool) error {
	return w.StoreString(key, strconv.FormatBool(value))
}

// RemoveAll removes the key named keyPrefix,
// the section or subsection named keyPrefix,
// and, for a bare section name, every subsection of it;
// an empty keyPrefix removes everything.
// This is what go-git's writer removes.
func (w *writer) RemoveAll(keyPrefix string) error {
	out, code, err := w.git.output("config", "--local", "-z", "--name-only", "--list")
	if err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("git config: exit status %d", code)
	}

	var keys, sections []string
	for _, key := range splitNul(out) {
		section := key[:strings.LastIndex(key, ".")]
		switch {
		case key == keyPrefix:
			if !slices.Contains(keys, key) {
				keys = append(keys, key)
			}
		case keyPrefix == "",
			section == keyPrefix,
			!strings.Contains(keyPrefix, ".") && strings.HasPrefix(section, keyPrefix+"."):
			if !slices.Contains(sections, section) {
				sections = append(sections, section)
			}
		}
	}

	for _, key := range keys {
		if err := w.config("--unset-all", "--", key); err != nil {
			return err
		}
	}
	for _, section := range sections {
		if err := w.config("--remove-section", "--", section); err != nil {
			return err
		}
	}
	return nil
}

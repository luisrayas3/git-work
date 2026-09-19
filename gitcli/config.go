package gitcli

import (
	"fmt"
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
	scopeLocal
	scopeGlobal
)

// flag returns the arguments selecting the scope.
// git only follows include.* directives by default when searching all files;
// a specific scope needs --includes explicitly.
func (s scope) flag() []string {
	switch s {
	case scopeLocal:
		return []string{"--local", "--includes"}
	case scopeGlobal:
		return []string{"--global", "--includes"}
	default:
		return nil
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
	out, code, err := r.config("-z", "--list")
	if err != nil {
		return nil, err
	}
	if code != 0 {
		return nil, fmt.Errorf("git config: exit status %d", code)
	}

	result := make(map[string]string)
	for _, entry := range splitNul(out) {
		key, value := splitEntry(entry)
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

// config pairs a CLI-backed reader with the wrapped repository's writer.
type config struct {
	repository.ConfigRead
	repository.ConfigWrite
}

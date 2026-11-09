package execenv

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/cache"
	"github.com/git-bug/git-bug/repository"
)

var _ In = &TestIn{}

type TestIn struct {
	*bytes.Buffer
	forceIsTerminal bool
}

func (t *TestIn) IsTerminal() bool {
	return t.forceIsTerminal
}

func (t *TestIn) ForceIsTerminal(value bool) {
	t.forceIsTerminal = value
}

var _ Out = &TestOut{}

type TestOut struct {
	*bytes.Buffer
	forceIsTerminal bool
}

func (te *TestOut) Printf(format string, a ...interface{}) {
	_, _ = fmt.Fprintf(te.Buffer, format, a...)
}

func (te *TestOut) Print(a ...interface{}) {
	_, _ = fmt.Fprint(te.Buffer, a...)
}

func (te *TestOut) Println(a ...interface{}) {
	_, _ = fmt.Fprintln(te.Buffer, a...)
}

func (te *TestOut) PrintJSON(v interface{}) error {
	raw, err := json.MarshalIndent(v, "", "    ")
	if err != nil {
		return err
	}
	te.Println(string(raw))
	return nil
}

func (te *TestOut) IsTerminal() bool {
	return te.forceIsTerminal
}

func (te *TestOut) Width() int {
	return 80
}

func (te *TestOut) Raw() io.Writer {
	return te.Buffer
}

func (te *TestOut) ForceIsTerminal(value bool) {
	te.forceIsTerminal = value
}

func NewTestEnv(t *testing.T) *Env {
	t.Helper()
	return newTestEnv(t, false)
}

func NewTestEnvTerminal(t *testing.T) *Env {
	t.Helper()
	return newTestEnv(t, true)
}

func newTestEnv(t *testing.T, isTerminal bool) *Env {
	repo := repository.CreateGoGitTestRepo(t, false)

	backend, err := cache.NewRepoCacheNoEvents(repo)
	require.NoError(t, err)

	t.Cleanup(func() {
		backend.Close()
	})

	return &Env{
		Ctx:     t.Context(),
		Repo:    repo,
		Backend: backend,
		In:      &TestIn{Buffer: &bytes.Buffer{}, forceIsTerminal: isTerminal},
		Out:     &TestOut{Buffer: &bytes.Buffer{}, forceIsTerminal: isTerminal},
		Err:     &TestOut{Buffer: &bytes.Buffer{}, forceIsTerminal: isTerminal},
	}
}

// ExecuteTest runs a command tree from argv against a test env,
// so that a test reaches what cobra parses — which subcommand an argument
// names, which format a flag defaults to — and not only the run function.
//
// A command loads its backend before it runs and closes it after;
// here the load reopens a cache over the test repository when the last run
// closed it, and the env is left with an open one for the next assertion.
func ExecuteTest(t *testing.T, env *Env, cmd *cobra.Command, args ...string) error {
	t.Helper()

	reopen := func() error {
		if env.Backend != nil {
			return nil
		}
		backend, err := cache.NewRepoCacheNoEvents(env.Repo)
		if err != nil {
			return err
		}
		env.Backend = backend
		t.Cleanup(func() { _ = backend.Close() })
		return nil
	}

	var walk func(*cobra.Command)
	walk = func(c *cobra.Command) {
		if c.PreRunE != nil {
			c.PreRunE = func(*cobra.Command, []string) error { return reopen() }
		}
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	walk(cmd)

	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	if args == nil {
		args = []string{} // nil would have cobra read the test binary's own os.Args
	}
	cmd.SetArgs(args)
	err := cmd.Execute()

	require.NoError(t, reopen())
	return err
}

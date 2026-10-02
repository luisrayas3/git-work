package commands

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/commands/execenv"
	"github.com/git-bug/git-bug/host"
	"github.com/git-bug/git-bug/schema"
)

func newQuickstartTestEnv(t *testing.T) *execenv.Env {
	t.Helper()

	env := execenv.NewTestEnv(t)

	i, err := env.Backend.Identities().New("John Doe", "jdoe@example.com")
	require.NoError(t, err)
	require.NoError(t, env.Backend.SetUserIdentity(i))

	return env
}

// quickstart runs the command and returns what it printed.
func quickstart(t *testing.T, env *execenv.Env) string {
	t.Helper()

	env.Out.Reset()
	require.NoError(t, runQuickstart(env))

	return env.Out.String()
}

// TestQuickstartNoSchema: an empty store has no type to name,
// so the live half says so and says what creates one,
// rather than printing an empty heading an agent would read as "no fields".
func TestQuickstartNoSchema(t *testing.T) {
	env := newQuickstartTestEnv(t)

	out := quickstart(t, env)

	require.Contains(t, out, "There is no schema in this repository yet")
	require.Contains(t, out, "git work schema init")
	require.NotContains(t, out, "\n### ")
}

// TestQuickstartLiveTypes: the live half is this repository's schema,
// so that one call is enough to write a document `issue new` accepts.
func TestQuickstartLiveTypes(t *testing.T) {
	env := newQuickstartTestEnv(t)

	doc, err := schema.ParseDocument([]byte(`
types:
  story:
    name: Story
    description: A body of work.
  task:
    name: Task
    fields:
      status:
        kind: enum
        values:
          - {id: to-do, name: To Do, category: unstarted}
          - {id: done, name: Done, category: completed}
      parent:
        kind: relation
        inverse: children
        target_types: [story]
      estimate: {kind: number}
`))
	require.NoError(t, err)
	_, _, err = host.SchemaImport(env.Backend, doc, false, false)
	require.NoError(t, err)

	out := quickstart(t, env)

	require.Contains(t, out, "### story — Story")
	require.Contains(t, out, "A body of work.")
	require.Contains(t, out, "### task — Task")
	require.Contains(t, out, "- `status` (enum: to-do, done)")
	require.Contains(t, out, "- `parent` (relation → story; read back as children)")
	require.Contains(t, out, "- `estimate` (number)")

	// the three built-ins are named once, in the guide, not on every type
	require.NotContains(t, out, "- `title` (text)")
	require.Contains(t, out, "No field of its own.")
}

// TestQuickstartNamesRealCommands walks the command tree for every
// `git work …` the guide spells out.
//
// The guide is the one piece of documentation the binary ships, so the one
// thing that can keep it honest is the binary itself: a command renamed or
// dropped fails here rather than sending an agent after a verb that is gone.
func TestQuickstartNamesRealCommands(t *testing.T) {
	env := newQuickstartTestEnv(t)
	root := NewRootCommand(context.Background(), "test")

	named := quickstartCommands(host.QuickstartGuide() + quickstart(t, env))
	require.Greater(t, len(named), 10, "the guide is meant to name the command line")

	for _, path := range named {
		t.Run(strings.Join(path, " "), func(t *testing.T) {
			cmd := root
			for _, name := range path {
				// A leaf's arguments are shaped like command names — a flow's
				// name in `flow run report`, a schema key in `schema log
				// task/status` — so the walk stops where the tree does.
				if !cmd.HasSubCommands() {
					break
				}
				child, ok := childCommand(cmd, name)
				require.Truef(t, ok, "`git work %s` names no command", strings.Join(path, " "))
				cmd = child
			}
		})
	}
}

// quickstartCommands is every `git work …` the text spells out, as the path of
// sub-command names it is.
//
// Only code is read — an inline span, or a line of a block — because prose
// words are shaped exactly like command names and a sentence that happens to
// carry on past a command would otherwise be read as part of it. Inside code
// there is nothing after the command but its arguments, and a path ends at
// the first word that is not a name: `ID`, `'PROGRAM'`, `<id>`, `--help`.
func quickstartCommands(text string) [][]string {
	var found [][]string

	for _, snippet := range codeSnippets(text) {
		words := strings.Fields(snippet)
		if len(words) < 2 || words[0] != "git" || words[1] != "work" {
			continue
		}

		var path []string
		for _, word := range words[2:] {
			if !commandWord.MatchString(word) {
				break
			}
			path = append(path, word)
		}
		if len(path) > 0 {
			found = append(found, path)
		}
	}

	return found
}

// codeSnippets is every inline code span of the text, plus every line of it
// that is itself a command, which is what a fenced block holds.
func codeSnippets(text string) []string {
	snippets := inlineCode.FindAllString(text, -1)
	for i, snippet := range snippets {
		snippets[i] = strings.Trim(snippet, "`")
	}

	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "git work ") {
			snippets = append(snippets, line)
		}
	}

	return snippets
}

var inlineCode = regexp.MustCompile("`[^`\n]+`")

// commandWord is what a command name looks like, so that a flag, a
// placeholder and a quoted argument are never mistaken for one.
var commandWord = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// childCommand finds one sub-command by name, aliases included.
func childCommand(cmd *cobra.Command, name string) (*cobra.Command, bool) {
	for _, child := range cmd.Commands() {
		if child.Name() == name || child.HasAlias(name) {
			return child, true
		}
	}
	return nil, false
}

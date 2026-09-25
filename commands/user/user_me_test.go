package usercmd

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/commands/bug/testenv"
	"github.com/git-bug/git-bug/commands/cmdjson"
)

// TestUserMeIsTheRowUserPrints pins `me` to the listing it narrows:
// the same line, and the same JSON document, for the current identity.
func TestUserMeIsTheRowUserPrints(t *testing.T) {
	env, userID := testenv.NewTestEnvAndUser(t)

	env.Out.Reset()
	require.NoError(t, runUserMe(env, userOptions{format: "text"}))
	line := env.Out.String()

	env.Out.Reset()
	require.NoError(t, runUser(env, userOptions{format: "text"}))
	require.Equal(t, env.Out.String(), line, "the store has one identity, which is me")
	require.Contains(t, line, userID.Human())
	require.Contains(t, line, "John Doe")

	env.Out.Reset()
	require.NoError(t, runUserMe(env, userOptions{format: "json"}))

	var me cmdjson.Identity
	require.NoError(t, json.Unmarshal(env.Out.Bytes(), &me))
	require.Equal(t, userID.String(), me.Id)
	require.Equal(t, userID.Human(), me.HumanId)
	require.Equal(t, "John Doe", me.Name)

	// and it is one document, not the list's array of one
	require.True(t, strings.HasPrefix(strings.TrimSpace(env.Out.String()), "{"))
}

func TestUserMeRefusesAnUnknownFormat(t *testing.T) {
	env, _ := testenv.NewTestEnvAndUser(t)

	require.ErrorContains(t, runUserMe(env, userOptions{format: "yaml"}), "yaml")
}

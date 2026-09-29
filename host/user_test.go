package host

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/cache"
	"github.com/git-bug/git-bug/repository"
)

// TestUserName: a name when the identity has one, its login when it has
// none (a Jira user Jira gave no name, JS16), and the short id for what the
// store does not resolve.
func TestUserName(t *testing.T) {
	repo := repository.CreateGoGitTestRepo(t, false)
	backend, err := cache.NewRepoCacheNoEvents(repo)
	require.NoError(t, err)
	t.Cleanup(func() { _ = backend.Close() })

	named, err := backend.Identities().NewRaw("Ada Lovelace", "", "", "", nil, map[string]string{"jira-account-id": "5b10ac8d"})
	require.NoError(t, err)
	loginOnly, err := backend.Identities().NewRaw("", "", "5b10ac8d82e05b22", "", nil, nil)
	require.NoError(t, err)

	require.Equal(t, "Ada Lovelace", UserName(backend, named.Id().String()))
	require.Equal(t, "Ada Lovelace", UserName(backend, named.Id().String()[:10]), "a stored prefix")
	require.Equal(t, "5b10ac8d82e05b22", UserName(backend, loginOnly.Id().String()))

	unknown := strings.Repeat("ab", 32)
	require.Equal(t, unknown[:7], UserName(backend, unknown))
	require.Equal(t, "", UserName(backend, ""))
}

package cache

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestIdentityByImmutableMetadata pins the two paths the Jira sync's
// identities take through the existing API (jira-sync.md, JS16): an external
// user created with the key, and the current user tagged with it afterwards.
// Both resolve, from this cache and from a fresh one.
func TestIdentityByImmutableMetadata(t *testing.T) {
	c, repo := newConfigTestCache(t)

	ada, err := c.Identities().NewRaw("Ada", "", "", "", nil,
		map[string]string{"jira-account-id": "5b10ac8d82e05b22cc7d4ef5"})
	require.NoError(t, err)

	// a Cloud user with no display name is still a valid identity, by login
	anon, err := c.Identities().NewRaw("", "", "5b10ac8d82e05b22cc7d0000", "", nil,
		map[string]string{"jira-account-id": "5b10ac8d82e05b22cc7d0000"})
	require.NoError(t, err)

	me, err := c.GetUserIdentity()
	require.NoError(t, err)
	me.SetMetadata("jira-account-id", "me-on-jira")
	require.NoError(t, me.Commit())

	fresh, err := NewRepoCacheNoEvents(repo)
	require.NoError(t, err)

	for _, cache := range []*RepoCache{c, fresh} {
		for id, want := range map[string]*IdentityCache{
			"5b10ac8d82e05b22cc7d4ef5": ada,
			"5b10ac8d82e05b22cc7d0000": anon,
			"me-on-jira":               me,
		} {
			got, err := cache.Identities().ResolveIdentityImmutableMetadata("jira-account-id", id)
			require.NoError(t, err, id)
			require.Equal(t, want.Id(), got.Id())
		}
		_, err := cache.Identities().ResolveIdentityImmutableMetadata("jira-account-id", "nobody")
		require.Error(t, err)
	}
}

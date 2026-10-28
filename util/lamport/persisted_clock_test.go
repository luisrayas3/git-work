package lamport

import (
	"testing"

	"github.com/go-git/go-billy/v6/memfs"
	"github.com/stretchr/testify/require"
)

func TestPersistedClock(t *testing.T) {
	root := memfs.New()

	c, err := NewPersistedClock(root, "test-clock")
	require.NoError(t, err)

	testClock(t, c)
}

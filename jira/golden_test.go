package jira

import (
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

var updateGolden = flag.Bool("update", false, "rewrite the golden files under testdata/")

// checkGolden compares out with testdata/name, or rewrites it under -update.
func checkGolden(t *testing.T, name string, out []byte) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *updateGolden {
		require.NoError(t, os.MkdirAll("testdata", 0o755))
		require.NoError(t, os.WriteFile(path, out, 0o644))
		return
	}
	want, err := os.ReadFile(path)
	require.NoError(t, err, "run go test ./jira/ -update to create it")
	require.Equal(t, string(want), string(out))
}

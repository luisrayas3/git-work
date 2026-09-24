package rank

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBetween(t *testing.T) {
	cases := []struct {
		name   string
		lo, hi string
	}{
		{"an empty list", "", ""},
		{"before everything", "", "m"},
		{"after everything", "m", ""},
		{"a wide gap", "a", "z"},
		{"adjacent keys, so the key grows", "a", "b"},
		{"adjacent digits", "1", "2"},
		{"adjacent at the top of the alphabet", "y", "z"},
		{"a longer lo", "azz", "b"},
		{"a longer hi", "a", "a1"},
		{"deep agreement", "aaa1", "aaa2"},
		{"lo ends at the bottom digit", "a0", "a1"},
		{"hi has a leading zero", "", "01"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Between(tc.lo, tc.hi)
			require.NoError(t, err)
			require.NotEmpty(t, got)

			if tc.lo != "" {
				require.Greater(t, got, tc.lo, "the key sorts above lo")
			}
			if tc.hi != "" {
				require.Less(t, got, tc.hi, "the key sorts below hi")
			}
		})
	}
}

// TestBetweenSubdividesForever is the cost the decision accepted: keys grow,
// and they keep working while they do (441dcbb).
func TestBetweenSubdividesForever(t *testing.T) {
	lo, hi := "a", "b"
	for i := 0; i < 50; i++ {
		mid, err := Between(lo, hi)
		require.NoError(t, err)
		require.Greater(t, mid, lo)
		require.Less(t, mid, hi)
		// insert again into the lower half, the worst case for length
		hi = mid
	}
}

// TestBetweenIsDeterministic is what makes two concurrent drags agree:
// the same neighbours give the same key, and (rank, id) breaks the tie.
func TestBetweenIsDeterministic(t *testing.T) {
	first, err := Between("a", "c")
	require.NoError(t, err)
	second, err := Between("a", "c")
	require.NoError(t, err)
	require.Equal(t, first, second)
}

func TestBetweenRefusals(t *testing.T) {
	_, err := Between("z", "a")
	require.ErrorContains(t, err, "does not sort below")

	_, err = Between("m", "m")
	require.ErrorContains(t, err, "does not sort below")

	_, err = Between("a", "A")
	require.ErrorContains(t, err, "not one of")

	// nothing sorts below a key that is all zeroes
	_, err = Between("", "0")
	require.ErrorContains(t, err, "no rank sorts below")
}

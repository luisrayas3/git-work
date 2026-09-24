// Package rank computes the keys of a LexoRank-style fractional index
// (`441dcbb`).
//
// A rank is a lexicographic string.
// Moving one issue between two others means computing a key strictly between
// theirs, so a drag is one SetField operation on one issue:
// no renumbering, and no multi-entity commit, which this store does not have.
//
// The property that makes it conflict-free is in the reader, not here:
// a list is sorted by (rank, id), never by rank alone,
// so two people dragging different issues into the same gap
// compute the same key, tie-break deterministically by id,
// and both drags survive.
//
// Keys grow as repeated inserts subdivide one gap.
// Rebalancing — renumbering a whole list — is the answer, it is rare,
// and it is order-preserving, so a half-applied rebalance still reads right.
// It is not implemented yet: nothing here needs it to be correct.
package rank

import (
	"fmt"
	"strings"
)

// Alphabet is base 36, digits before letters,
// which is the order a plain string comparison already puts them in.
const Alphabet = "0123456789abcdefghijklmnopqrstuvwxyz"

// Between returns a key strictly between lo and hi.
//
// An empty lo means "before everything" and an empty hi "after everything",
// so the four cases of an insert — into an empty list, at the top, at the
// bottom, in the middle — are this one function.
//
// It errors when lo does not sort strictly below hi,
// because that is a caller holding its neighbours the wrong way round,
// and inventing a key for it would put the issue where nobody asked.
func Between(lo, hi string) (string, error) {
	if err := check("lo", lo); err != nil {
		return "", err
	}
	if err := check("hi", hi); err != nil {
		return "", err
	}
	if lo != "" && hi != "" && lo >= hi {
		return "", fmt.Errorf("rank %q does not sort below %q", lo, hi)
	}
	if hi != "" && strings.Trim(hi, "0") == "" {
		return "", fmt.Errorf("no rank sorts below %q", hi)
	}

	// The invariant of the loop: what has been written so far is a prefix of
	// lo's, and of hi's for as long as the two agree. Once a digit below hi's
	// is written, every longer key starting with it is below hi, so hi stops
	// constraining and the rest is only about staying above lo.
	var out strings.Builder
	below := hi == ""
	for at := 0; ; at++ {
		low := digitAt(lo, at, 0)
		high := len(Alphabet)
		if !below {
			high = digitAt(hi, at, len(Alphabet))
		}

		if high-low > 1 {
			// Room at this digit: the midpoint is the key, and it is never
			// the lowest digit, so it never needs trimming.
			out.WriteByte(Alphabet[(low+high)/2])
			return out.String(), nil
		}

		// No room: keep lo's digit and subdivide the next one.
		out.WriteByte(Alphabet[low])
		if low < high {
			below = true
		}
	}
}

// digitAt is the value of a key's digit at a position,
// or the fallback where the key is shorter than that.
func digitAt(key string, at int, fallback int) int {
	if at >= len(key) {
		return fallback
	}
	return strings.IndexByte(Alphabet, key[at])
}

// check refuses a key this alphabet can not order.
func check(name, key string) error {
	for at := 0; at < len(key); at++ {
		if strings.IndexByte(Alphabet, key[at]) < 0 {
			return fmt.Errorf("%s rank %q has %q, which is not one of %s",
				name, key, key[at:at+1], Alphabet)
		}
	}
	return nil
}

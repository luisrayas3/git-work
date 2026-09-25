// Package sorted is the ordering a map has to be given before it is written.
//
// Go's map iteration order is deliberately random, so anything that turns a
// map into output — a pack of operations, a rendered row, a JSON document —
// has to sort its keys first or it writes something different every time.
// Four packages had a copy of this; it is one rule, so it is one function.
package sorted

import (
	"cmp"
	"sort"
)

// Keys returns a map's keys in order.
func Keys[K cmp.Ordered, V any](m map[K]V) []K {
	keys := make([]K, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	return keys
}

package view

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/git-bug/git-bug/schema"
)

// Layer is one level of `expand`: the relation whose targets nest under a
// row, and the list's own arguments for the rows it brings
// (doc/design/terminal-renderer.md, Nesting; Luis, 2026-10-02).
//
// `depth` is gone. A number said how far to go and nothing about what was
// down there, so a second level nobody could describe was the only thing it
// could draw; a layer is the description, and `expand` on a layer is the
// level below it.
type Layer struct {
	// Relation is the stored relation field, or the name its `inverse`
	// gives the derived side: `children` is read off every `parent`.
	// Empty, the rows above list their own children, and a row that does
	// not has none (doc/design/query-rows.md, R3).
	Relation string `json:"relation,omitempty"`
	// Query is a jq program over the array of the row's candidate children,
	// every unarchived one; empty, they are all this layer's rows.
	Query string `json:"query,omitempty"`
	// IncludeArchive brings the archived children back among the candidates,
	// as the call's own `include_archive` does for `query`. Nil, it is the
	// layer above's, the first layer's being the call's: unlike `query`, which
	// differs per layer because children can be of other types, whether the
	// archived show is one choice for the whole view, and a layer that names
	// it overrides it for itself and every level below
	// (doc/design/include-archive.md, I5).
	IncludeArchive *bool `json:"include_archive,omitempty"`
	// Fields, Details and GroupBy are the layer's columns, its dim second
	// line and the field its rows are sectioned by. Fields and Details that
	// are not named are the layer above's: a layer that says nothing draws
	// what its parent draws.
	Fields  []string `json:"fields,omitempty"`
	Details []string `json:"details,omitempty"`
	GroupBy string   `json:"group_by,omitempty"`
	// Expand is the layer below; nil, this layer's rows are leaves.
	Expand *Layer `json:"expand,omitempty"`
	// Repeat is the integer form of `expand`: the level below is this layer
	// again, and so on for that many more levels, 0 being as far down as the
	// relation goes. Nil when `expand` is a layer or absent.
	//
	// It is a number and not a name because every string in that slot is a
	// relation, and a layer that repeats cannot describe what is under it
	// anyway: a tree of unknown height has one layer, drawn at every level
	// (Luis, 2026-10-04).
	Repeat *int `json:"-"`
}

// layerKeys are the keys a layer takes, in the order an error names them.
//
// `rank` is not one of them: rows are drawn and dragged in the order of the
// built-in `rank`, always, an implementation detail no call names
// (Luis, 2026-10-08).
var layerKeys = []string{"relation", "query", "include_archive", "fields", "details", "group_by", "expand"}

// parseExpand reads the `expand` argument: a relation name, or a layer.
//
// A number is a layer's answer to "and the same again", so it has nothing to
// repeat at the top.
func parseExpand(raw json.RawMessage) (*Layer, error) {
	if _, err := asRepeat(raw); err == nil {
		return nil, fmt.Errorf(`is a number, which repeats the layer it is on, so it cannot be the first one`)
	}
	return parseLayer(raw, 1)
}

// asRepeat reads the integer form of a layer's `expand`.
func asRepeat(raw json.RawMessage) (int, error) {
	var n float64
	if err := json.Unmarshal(raw, &n); err != nil {
		return 0, fmt.Errorf("is not a number")
	}
	if n != float64(int(n)) || n < 0 {
		return 0, fmt.Errorf("is %s, it is 0 for every level down or how many more levels this layer draws", strings.TrimSpace(string(raw)))
	}
	return int(n), nil
}

// parseLayer reads one layer, at a level counted from 1 for the errors.
func parseLayer(raw json.RawMessage, at int) (*Layer, error) {
	where := ""
	if at > 1 {
		where = fmt.Sprintf("layer %d ", at)
	}

	if name, err := asString(raw); err == nil {
		if strings.TrimSpace(name) == "" {
			return nil, fmt.Errorf("%sis empty, it names a relation", where)
		}
		return &Layer{Relation: name}, nil
	}

	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil {
		return nil, fmt.Errorf("%sis a relation name or an object of %s, not %s",
			where, strings.Join(layerKeys, ", "), jsonKind(raw))
	}
	for key := range object {
		if !contains(layerKeys, key) {
			return nil, fmt.Errorf("%stakes no key %s, it takes %s", where, key, strings.Join(layerKeys, ", "))
		}
	}

	// relation is required only where a row above lists no children of its
	// own, which only the rows can say, so its absence is the renderer's to
	// report (doc/design/query-rows.md, R3)
	layer := &Layer{}
	if relation, ok := object["relation"]; ok && !isNull(relation) {
		name, err := asString(relation)
		if err != nil {
			return nil, fmt.Errorf("%srelation %w", where, err)
		}
		if strings.TrimSpace(name) == "" {
			return nil, fmt.Errorf("%srelation is empty", where)
		}
		layer.Relation = name
	}

	for _, key := range []struct {
		name  string
		value *string
	}{{"query", &layer.Query}, {"group_by", &layer.GroupBy}} {
		raw, ok := object[key.name]
		if !ok || isNull(raw) {
			continue
		}
		s, err := asString(raw)
		if err != nil {
			return nil, fmt.Errorf("%s%s %w", where, key.name, err)
		}
		if key.name != "query" && strings.TrimSpace(s) == "" {
			return nil, fmt.Errorf("%s%s is empty", where, key.name)
		}
		*key.value = s
	}

	if raw, ok := object["include_archive"]; ok && !isNull(raw) {
		var include bool
		if err := json.Unmarshal(raw, &include); err != nil {
			return nil, fmt.Errorf("%sinclude_archive is true or false, not %s", where, jsonKind(raw))
		}
		layer.IncludeArchive = &include
	}

	for _, key := range []struct {
		name  string
		value *[]string
	}{{"fields", &layer.Fields}, {"details", &layer.Details}} {
		raw, ok := object[key.name]
		if !ok || isNull(raw) {
			continue
		}
		var list []string
		if err := json.Unmarshal(raw, &list); err != nil {
			return nil, fmt.Errorf("%s%s is a list of strings, not %s", where, key.name, jsonKind(raw))
		}
		for _, item := range list {
			if strings.TrimSpace(item) == "" {
				return nil, fmt.Errorf("%s%s has an empty field key", where, key.name)
			}
		}
		*key.value = list
	}

	if raw, ok := object["expand"]; ok && !isNull(raw) {
		if jsonKind(raw) == "a number" {
			n, err := asRepeat(raw)
			if err != nil {
				return nil, fmt.Errorf("%sexpand %w", where, err)
			}
			layer.Repeat = &n
			return layer, nil
		}
		below, err := parseLayer(raw, at+1)
		if err != nil {
			return nil, err
		}
		layer.Expand = below
	}
	return layer, nil
}

// Expand returns `expand` as parsed, nil when it is absent.
//
// The call was checked when it was parsed, so a failure here is impossible
// and reads as absence rather than as a second error nobody can act on.
func (c *Call) Expand() *Layer {
	raw, ok := c.Args["expand"]
	if !ok || c.Kind == KindShow {
		// show's is a table per element, read by SideTables (side.go)
		return nil
	}
	layer, err := parseExpand(raw)
	if err != nil {
		return nil
	}
	return layer
}

// Layers flattens a spec into one layer per level below the roots, the last
// of them repeated as its integer `expand` says: that many more times, or,
// for 0, at every level further down (`forever`).
//
// Level n of the tree is Layers()[n-1], or the last one when it goes on
// forever, so a renderer reads a row's layer off its level and never walks
// the spec. A repeated layer is the same pointer at every level it draws.
func (l *Layer) Layers() (layers []*Layer, forever bool) {
	for layer := l; layer != nil; layer = layer.Expand {
		layers = append(layers, layer)
		if layer.Repeat == nil {
			continue
		}
		if *layer.Repeat == 0 {
			return layers, true
		}
		for i := 0; i < *layer.Repeat; i++ {
			layers = append(layers, layer)
		}
		return layers, false
	}
	return layers, false
}

// checkExpand reads the layers against the live schema: every relation is one
// some type has, as the stored side or as an inverse, and every key a layer
// draws is a field of a type its rows can be.
//
// It is the half the argument table cannot do: the table knows the shape of a
// call, the schema knows what the words in it mean.
func checkExpand(s *schema.Schema, root *Layer) error {
	if s == nil || s.Empty() {
		return fmt.Errorf("needs a schema, and no type is defined")
	}

	layers, _ := root.Layers()
	for at, layer := range layers {
		if at > 0 && layers[at-1] == layer {
			break // a repeated layer was checked where it was written
		}
		where := ""
		if at > 0 {
			where = fmt.Sprintf("layer %d ", at+1)
		}

		// a layer with no relation draws the rows listed above it, which can
		// be of any type
		types := s.TypeKeys()
		if layer.Relation != "" {
			var err error
			types, err = relationRows(s, layer.Relation)
			if err != nil {
				return fmt.Errorf("%s%w", where, err)
			}
		}

		for _, key := range keysOf(layer) {
			found := false
			for _, typeKey := range types {
				if _, ok := s.Field(typeKey, key); ok {
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("%snames field %s, which is not a field of %s",
					where, key, strings.Join(types, " or "))
			}
		}
	}
	return nil
}

// keysOf is every field key one layer names.
func keysOf(layer *Layer) []string {
	keys := append(append([]string(nil), layer.Fields...), layer.Details...)
	if layer.GroupBy != "" {
		keys = append(keys, layer.GroupBy)
	}
	return keys
}

// relationRows is what a layer's rows can be: the types a stored relation may
// point at, and the types holding a relation whose inverse is this name.
//
// Both sides answer to one word, which is the rule `expand` has had since it
// was built: the derived side is never stored (`schema.yaml`, D4), so
// `children` is read off every `parent`.
func relationRows(s *schema.Schema, name string) ([]string, error) {
	var types []string
	seen := map[string]bool{}
	add := func(keys ...string) {
		for _, key := range keys {
			if _, ok := s.Type(key); ok && !seen[key] {
				seen[key] = true
				types = append(types, key)
			}
		}
	}

	for _, typeKey := range s.TypeKeys() {
		t, _ := s.Type(typeKey)
		if field, ok := t.Field(name); ok && field.Kind.IsRelation() {
			if len(field.TargetTypes) == 0 {
				add(s.TypeKeys()...)
			} else {
				add(field.TargetTypes...)
			}
		}
		for _, key := range t.FieldKeys() {
			field, _ := t.Field(key)
			if field.Kind.IsRelation() && field.Inverse == name {
				add(typeKey)
			}
		}
	}

	if len(types) == 0 {
		return nil, fmt.Errorf("names %s, which no type has as a relation, nor as one's inverse; the relations are %s",
			name, relationNames(s, s.TypeKeys()))
	}
	return types, nil
}

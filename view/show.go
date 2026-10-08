package view

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/git-bug/git-bug/schema"
)

// A view's `show`: a type key mapped to the show call `Enter` opens on an
// issue of that type, its KWARGS less the `id` the row gives
// (doc/design/show-from-a-view.md, approved 2026-10-08).
//
// The tables a type's page draws are the flow's, not the schema's
// (show-side-table.md), and a flow that draws a list cannot reach the show its
// `Enter` opens: this is how it says them. The type is the stored issue's,
// never a row's shaped `fields.type`, and an unlisted type opens a bare show.
// Every page opened from there carries the same map, so a drill-down reads
// the same at every step; on show itself the map is for the pages it opens,
// never for the shown issue (V3).

// showIdPlaceholder stands for the row's id while an entry is checked as
// show's call, which needs one.
const showIdPlaceholder = "0"

// parseShowMap reads `show`: an object of type keys, each an object of show's
// arguments without `id`. Each entry is parsed against show's own table, so
// what show refuses an entry refuses, the type named (V2).
func parseShowMap(raw json.RawMessage) (map[string]map[string]json.RawMessage, error) {
	var entries map[string]json.RawMessage
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil, fmt.Errorf(`is an object of type keys, each the show arguments without id: {"epic":{"expand":"children"}}, not %s`, jsonKind(raw))
	}
	out := make(map[string]map[string]json.RawMessage, len(entries))
	for _, typeKey := range sortedKeys(entries) {
		if strings.TrimSpace(typeKey) == "" {
			return nil, fmt.Errorf("has an empty type key")
		}
		entry := entries[typeKey]
		var args map[string]json.RawMessage
		if err := json.Unmarshal(entry, &args); err != nil || args == nil {
			return nil, fmt.Errorf("%s is an object of show arguments, not %s", typeKey, jsonKind(entry))
		}
		if _, ok := args["id"]; ok {
			return nil, fmt.Errorf("%s takes no id: the row Enter is on gives it", typeKey)
		}
		if _, ok := args["show"]; ok {
			return nil, fmt.Errorf("%s takes no show: the pages it opens open by this same map", typeKey)
		}
		// an unknown argument is named against what an entry takes, which
		// is show's arguments less id and show, never show's own list
		for name := range args {
			if !entryTakes(name) {
				return nil, fmt.Errorf("%s takes no argument %s, an entry takes %s", typeKey, name, argNames(entryArgs()))
			}
		}
		if _, err := entryCall(args, showIdPlaceholder); err != nil {
			return nil, fmt.Errorf("%s: %w", typeKey, err)
		}
		out[typeKey] = args
	}
	return out, nil
}

// entryArgs is show's argument table less what an entry may not give: the
// id the row gives, and show, which the pages carry.
func entryArgs() []Arg {
	var out []Arg
	for _, arg := range Kinds[KindShow] {
		if arg.Name != "id" && arg.Name != "show" {
			out = append(out, arg)
		}
	}
	return out
}

func entryTakes(name string) bool {
	for _, arg := range entryArgs() {
		if arg.Name == name {
			return true
		}
	}
	return false
}

// entryCall is the show call an entry makes for one id, parsed against
// show's table.
func entryCall(args map[string]json.RawMessage, id string) (*Call, error) {
	kwargs := make(map[string]json.RawMessage, len(args)+1)
	for name, raw := range args {
		kwargs[name] = raw
	}
	kwargs["id"] = mustString(id)
	return Parse(KindShow, kwargs)
}

// ShowFor is the show call `Enter` opens on an issue of a stored type, by a
// view's `show` map given as the argument's JSON: the type's entry with the
// id, or a bare show where the type is not listed or there is no map. The
// map rides along as the call's own `show`, so that the page carries it to
// every page it opens (V3).
func ShowFor(shows json.RawMessage, typeKey, id string) (*Call, error) {
	args := map[string]json.RawMessage{}
	if !isNull(shows) {
		entries, err := parseShowMap(shows)
		if err != nil {
			return nil, fmt.Errorf("view show: show %w", err)
		}
		if entry, ok := entries[typeKey]; ok {
			args = entry
		}
	}
	call, err := entryCall(args, id)
	if err != nil {
		return nil, err
	}
	if !isNull(shows) {
		call.Args["show"] = compact(shows)
	}
	return call, nil
}

// checkShowMap checks `show` against the live schema: every key a type the
// schema has, and every entry what CheckSchema accepts of show's own call.
func checkShowMap(call *Call, s *schema.Schema) error {
	raw, ok := call.Args["show"]
	if !ok {
		return nil
	}
	entries, err := parseShowMap(raw)
	if err != nil {
		return fmt.Errorf("view %s: show %w", call.Kind, err)
	}
	for _, typeKey := range sortedKeys(entries) {
		if s == nil || s.Empty() {
			return fmt.Errorf("view %s: show %s needs a schema, and no type is defined", call.Kind, typeKey)
		}
		if _, ok := s.Type(typeKey); !ok {
			return fmt.Errorf("view %s: show names type %s, which the schema does not have; the types are %s",
				call.Kind, typeKey, strings.Join(s.TypeKeys(), ", "))
		}
		entry, err := entryCall(entries[typeKey], showIdPlaceholder)
		if err != nil {
			return fmt.Errorf("view %s: show %s: %w", call.Kind, typeKey, err)
		}
		if err := CheckSchema(entry, s); err != nil {
			return fmt.Errorf("view %s: show %s: %w", call.Kind, typeKey, err)
		}
	}
	return nil
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func mustString(s string) json.RawMessage {
	raw, _ := json.Marshal(s)
	return raw
}

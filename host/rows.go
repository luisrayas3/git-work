package host

import "fmt"

// Rows a view's query makes (doc/design/query-rows.md, 2026-10-08).
//
// A row is an issue as `git work issue` prints it, or as a program shaped it,
// with two top-level keys an issue never has:
// `key`, the row's identity on the screen, and
// `children`, what nests under it.
// The `id` stays the issue the row acts on,
// and is optional where a `key` is given:
// such a row stands for nothing, and only draws.

// KeyPrefix starts the ghost's key, which no row a query makes may start
// with (doc/design/create.md, C6).
const KeyPrefix = "+"

// ViewRows reads what a view's query emitted as rows, and reports whether
// it is a list of them.
//
// It is IssueItems with the two keys a row may carry besides: an object with
// a fields map, and an id, which may be left out where a `key` is given; a
// `children` that is an array of ids or of rows, read the same way, all the
// way down. Anything else is not a list of rows, which a view draws as
// nothing, as it always has.
func ViewRows(values []any) ([]map[string]any, bool) {
	items := values
	if len(values) == 1 {
		if array, ok := values[0].([]any); ok {
			items = array
		}
	}
	if len(items) == 0 {
		return nil, false
	}

	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		object, ok := item.(map[string]any)
		if !ok || !isRow(object) {
			return nil, false
		}
		out = append(out, object)
	}
	return out, true
}

// isRow is the shape of one row, its listed children included.
func isRow(object map[string]any) bool {
	if _, ok := object["fields"].(map[string]any); !ok {
		return false
	}
	_, hasId := object["id"].(string)
	if raw, ok := object["id"]; ok && raw != nil && !hasId {
		return false
	}
	key, hasKey := object["key"]
	if hasKey && key != nil {
		if _, ok := key.(string); !ok {
			return false
		}
	}
	if !hasId && !(hasKey && key != nil) {
		return false
	}

	raw, ok := object["children"]
	if !ok || raw == nil {
		return true
	}
	children, ok := raw.([]any)
	if !ok {
		return false
	}
	for _, child := range children {
		switch child := child.(type) {
		case string:
		case map[string]any:
			if !isRow(child) {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// RowId is the issue a row acts on, "" for a row that stands for nothing.
func RowId(row map[string]any) string {
	return StringOr(row["id"], "")
}

// RowKey is a row's identity on the screen: its `key`, else its id.
func RowKey(row map[string]any) string {
	if key, ok := row["key"].(string); ok {
		return key
	}
	return RowId(row)
}

// RowChildren is a row's listed children, and whether it lists them:
// an empty list is a row that lists none, which is not a row that says
// nothing.
func RowChildren(row map[string]any) ([]any, bool) {
	children, ok := row["children"].([]any)
	return children, ok
}

// CheckRowKeys refuses the keys a view cannot tell rows apart by:
// one given twice, among the rows and every row listed under them, and one
// starting with the ghost's prefix.
//
// A key that was never given is the row's id, and two rows of one issue with
// no key between them are the same row, which the drawing keeps once, as it
// always has; a given key is a promise that the row is its own.
func CheckRowKeys(rows []map[string]any) error {
	given := map[string]bool{}
	plain := map[string]bool{}
	var walk func(rows []any) error
	walk = func(rows []any) error {
		for _, raw := range rows {
			row, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			key, keyed := row["key"].(string)
			switch {
			case !keyed:
				id := RowId(row)
				if given[id] {
					return fmt.Errorf("key %s is given twice", id)
				}
				plain[id] = true
			case key == "":
				return fmt.Errorf("a row has an empty key")
			case len(key) >= len(KeyPrefix) && key[:len(KeyPrefix)] == KeyPrefix:
				return fmt.Errorf("key %s starts with %s, which is the ghost's", key, KeyPrefix)
			case given[key] || plain[key]:
				return fmt.Errorf("key %s is given twice", key)
			default:
				given[key] = true
			}
			if children, ok := RowChildren(row); ok {
				if err := walk(children); err != nil {
					return err
				}
			}
		}
		return nil
	}

	top := make([]any, len(rows))
	for at, row := range rows {
		top[at] = row
	}
	return walk(top)
}

// IssueRows is ViewRows for a view that draws issues and nothing else, the
// board and the matrix (doc/design/query-rows.md, Out of scope): `key` and
// `children` mean nothing there, a row with no id is dropped, and an issue
// two rows stand for is drawn once, the first, because a board shows each
// issue in one place and a matrix that counted it twice would be wrong.
func IssueRows(values []any) []map[string]any {
	rows, ok := ViewRows(values)
	if !ok {
		return nil
	}
	out := make([]map[string]any, 0, len(rows))
	seen := map[string]bool{}
	for _, row := range rows {
		id := RowId(row)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, row)
	}
	return out
}

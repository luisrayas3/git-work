// Package view builds render specs, the contract between a view and a renderer.
//
// A view is a module, not a surface (doc/design/cli-convention.md):
// `view.board(items, columns="status")` and
// `git work view board '{"columns":"status"}' < items.json`
// are the same call, and both return a spec.
// A renderer — the terminal one (`84dfbde`), the HTTP one (`8b06191`) —
// consumes specs and nothing else,
// so a view exists on every surface or on none.
//
// A spec is a plain JSON document:
//
//	{"view": "board", "bindings": {"columns": "status"}, "items": [...]}
//
// `view` is the kind, `bindings` maps a view's slots to the field keys that
// fill them, and `items` are the issues, in the shape `git work issue` prints.
// Which bindings a kind takes is the table in kinds.go,
// so a renderer reads one contract rather than a kind's documentation.
// There are no field roles on the schema: a flow's script names the fields it
// means when it calls the view (`d56e6f1`, `f4bac00`).
package view

import (
	"fmt"
	"sort"
	"strings"
)

// Spec is what a view returns and a renderer consumes.
type Spec struct {
	View     string            `json:"view"`
	Bindings map[string]string `json:"bindings"`
	Items    []any             `json:"items"`
}

// List builds a list spec: one line per item.
func List(items []any, bindings map[string]string) (*Spec, error) {
	return Build(KindList, items, bindings)
}

// Board builds a board spec: a column per value of the `columns` field.
func Board(items []any, bindings map[string]string) (*Spec, error) {
	return Build(KindBoard, items, bindings)
}

// Gantt builds a gantt spec: a bar per item, from `start` to `end`.
func Gantt(items []any, bindings map[string]string) (*Spec, error) {
	return Build(KindGantt, items, bindings)
}

// Build validates items and bindings against a kind's table and returns a spec.
//
// Everything a renderer can be told at build time is checked here,
// so that a bad view fails where it is written
// rather than in the renderer of whichever surface ran it first.
func Build(kind string, items []any, bindings map[string]string) (*Spec, error) {
	slots, ok := Kinds[kind]
	if !ok {
		return nil, fmt.Errorf("no view named %s, the views are %s", kind, strings.Join(KindNames(), ", "))
	}

	checked, err := checkBindings(kind, slots, bindings)
	if err != nil {
		return nil, err
	}

	checked2, err := checkItems(items)
	if err != nil {
		return nil, err
	}

	return &Spec{View: kind, Bindings: checked, Items: checked2}, nil
}

// IsSpec reports whether a decoded JSON value is a spec,
// which is how a printer decides to render rather than to print JSON.
func IsSpec(v any) bool {
	object, ok := v.(map[string]any)
	if !ok {
		return false
	}
	kind, ok := object["view"].(string)
	if !ok {
		return false
	}
	if _, ok := Kinds[kind]; !ok {
		return false
	}
	_, ok = object["items"].([]any)
	return ok
}

// checkBindings refuses an unknown slot and a required one that is absent.
func checkBindings(kind string, slots []Binding, bindings map[string]string) (map[string]string, error) {
	allowed := make(map[string]struct{}, len(slots))
	for _, slot := range slots {
		allowed[slot.Name] = struct{}{}
	}

	for name := range bindings {
		if _, ok := allowed[name]; !ok {
			return nil, fmt.Errorf("view %s takes no binding %s, it takes %s",
				kind, name, strings.Join(slotNames(slots), ", "))
		}
	}

	checked := make(map[string]string, len(bindings))
	for _, slot := range slots {
		value, given := bindings[slot.Name]
		if !given {
			if slot.Required {
				return nil, fmt.Errorf("view %s needs a binding for %s: %s", kind, slot.Name, slot.Doc)
			}
			continue
		}
		if strings.TrimSpace(value) == "" {
			return nil, fmt.Errorf("view %s: binding %s is empty, it names a field", kind, slot.Name)
		}
		checked[slot.Name] = value
	}

	return checked, nil
}

// checkItems refuses anything that is not issue-shaped.
//
// A renderer reads `id` and looks fields up by the bindings' keys,
// so an item without either is a mistake the script made,
// and one it made several steps before the renderer would notice.
func checkItems(items []any) ([]any, error) {
	if items == nil {
		return []any{}, nil
	}

	for at, item := range items {
		object, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("item %d is %s, not an issue", at, jsonKind(item))
		}
		if _, ok := object["id"].(string); !ok {
			return nil, fmt.Errorf("item %d has no id, so it is not an issue", at)
		}
		if _, ok := object["fields"].(map[string]any); !ok {
			return nil, fmt.Errorf("item %d has no fields, so it is not an issue", at)
		}
	}

	return items, nil
}

func slotNames(slots []Binding) []string {
	names := make([]string, 0, len(slots))
	for _, slot := range slots {
		name := slot.Name
		if slot.Required {
			name += " (required)"
		}
		names = append(names, name)
	}
	return names
}

// KindNames lists the view kinds, in a stable order.
func KindNames() []string {
	names := make([]string, 0, len(Kinds))
	for name := range Kinds {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func jsonKind(v any) string {
	switch v.(type) {
	case nil:
		return "null"
	case bool:
		return "a boolean"
	case string:
		return "a string"
	case []any:
		return "a list"
	default:
		return "a number"
	}
}

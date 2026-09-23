package run

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/big"
	"sort"

	"go.starlark.net/starlark"
)

// This file is the whole boundary between JSON and Starlark.
//
// Everything a host function returns arrives as the shape encoding/json
// decodes into, and everything a script passes back leaves as that shape,
// so a flow reads exactly what `--format json` prints (`0740bf3` E9).
// The conversion is on the hot path — a board flow over a thousand issues
// spends most of its 3.3 ms here — so it pre-sizes every dict and list
// and never re-marshals a value it already holds.

// decodeJSON decodes into the shape the converters expect,
// keeping numbers as json.Number so that an integer stays an integer.
func decodeJSON(raw []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var out any
	if err := dec.Decode(&out); err != nil {
		return nil, err
	}
	return out, nil
}

// toStarlark converts a decoded JSON value into a Starlark value.
func toStarlark(v any) (starlark.Value, error) {
	switch value := v.(type) {
	case nil:
		return starlark.None, nil
	case bool:
		return starlark.Bool(value), nil
	case string:
		return starlark.String(value), nil
	case float64:
		return starlark.Float(value), nil
	case int:
		return starlark.MakeInt(value), nil
	case int64:
		return starlark.MakeInt64(value), nil
	case json.Number:
		return numberToStarlark(value)

	case []any:
		items := make([]starlark.Value, len(value))
		for at, item := range value {
			converted, err := toStarlark(item)
			if err != nil {
				return nil, err
			}
			items[at] = converted
		}
		return starlark.NewList(items), nil

	case map[string]any:
		// Keys go in sorted, so that iterating a dict in a script reads the
		// same twice; Starlark dicts keep insertion order.
		dict := starlark.NewDict(len(value))
		for _, key := range sortedKeys(value) {
			converted, err := toStarlark(value[key])
			if err != nil {
				return nil, err
			}
			if err := dict.SetKey(starlark.String(key), converted); err != nil {
				return nil, err
			}
		}
		return dict, nil

	default:
		return nil, fmt.Errorf("cannot convert %T to a Starlark value", v)
	}
}

// numberToStarlark keeps an integer an integer and a fraction a float,
// which is what jq does and what a script comparing an estimate expects.
func numberToStarlark(n json.Number) (starlark.Value, error) {
	if i, err := n.Int64(); err == nil {
		return starlark.MakeInt64(i), nil
	}
	if big, ok := new(big.Int).SetString(n.String(), 10); ok {
		return starlark.MakeBigInt(big), nil
	}
	f, err := n.Float64()
	if err != nil {
		return nil, fmt.Errorf("not a number: %s", n.String())
	}
	return starlark.Float(f), nil
}

// fromStarlark converts a Starlark value into the shape encoding/json prints.
func fromStarlark(v starlark.Value) (any, error) {
	switch value := v.(type) {
	case nil, starlark.NoneType:
		return nil, nil
	case starlark.Bool:
		return bool(value), nil
	case starlark.String:
		return string(value), nil
	case starlark.Float:
		return float64(value), nil
	case starlark.Int:
		// json.Number so that a big integer survives, which float64 would not.
		return json.Number(value.String()), nil

	case *starlark.List:
		items := make([]any, value.Len())
		for at := 0; at < value.Len(); at++ {
			converted, err := fromStarlark(value.Index(at))
			if err != nil {
				return nil, err
			}
			items[at] = converted
		}
		return items, nil

	case starlark.Tuple:
		items := make([]any, value.Len())
		for at := 0; at < value.Len(); at++ {
			converted, err := fromStarlark(value.Index(at))
			if err != nil {
				return nil, err
			}
			items[at] = converted
		}
		return items, nil

	case *starlark.Dict:
		out := make(map[string]any, value.Len())
		for _, item := range value.Items() {
			key, ok := starlark.AsString(item[0])
			if !ok {
				// A JSON object's keys are strings, so a dict's have to be.
				return nil, fmt.Errorf("a dict key must be a string, %s is not", item[0].Type())
			}
			converted, err := fromStarlark(item[1])
			if err != nil {
				return nil, err
			}
			out[key] = converted
		}
		return out, nil

	default:
		return nil, fmt.Errorf("cannot convert a %s to JSON", v.Type())
	}
}

// marshalStarlark is fromStarlark straight to bytes, which is what Run returns.
func marshalStarlark(v starlark.Value) (json.RawMessage, error) {
	converted, err := fromStarlark(v)
	if err != nil {
		return nil, err
	}
	if converted == nil {
		return nil, nil
	}
	return json.Marshal(converted)
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

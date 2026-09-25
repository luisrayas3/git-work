// Package host is the one code path behind the command line and a flow's script.
//
// Every `git work <module> <verb>` is a function here,
// taking the repository cache and JSON-shaped values,
// and returning what the command prints.
// `commands/` parses argv and formats the result;
// `flow/run` binds the same functions as Starlark builtins.
// The host API mirrors the command line one to one
// (doc/design/cli-convention.md, `3556569` E9),
// and this package is what makes that true by construction
// rather than by two implementations agreeing.
//
// Nothing here writes an entity ref directly:
// every write goes through `cache/`,
// which holds the write lock and re-reads inside it (AGENTS.md).
//
// The author of a write is not an argument.
// `cache` resolves it once, from the identity `EnsureUserIdentity` set,
// and the planning functions of `cache.IssueCache` take no author,
// so passing one here would be a second source of truth
// that half the writers could not honour.
package host

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// DecodeStrict decodes one JSON document and refuses anything after it,
// so that a truncated or doubled document is an error rather than half a write.
// Unknown keys are refused too: a misspelled key is a mistake, never a no-op.
//
// It is the one rule, because a document written at the shell and the same
// document built in a script have to be refused for the same reasons.
func DecodeStrict(data []byte, into any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(into); err != nil {
		return fmt.Errorf("invalid JSON document: %w", err)
	}
	var trailing json.RawMessage
	if err := dec.Decode(&trailing); !errors.Is(err, io.EOF) {
		return fmt.Errorf("invalid JSON document: more than one value")
	}
	return nil
}

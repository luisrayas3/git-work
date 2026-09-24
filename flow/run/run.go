// Package run is the Starlark runtime: it executes a flow.
//
// A flow is one Starlark function (`3556569` E2, `b511c63`, `0740bf3`).
// `flow.Parse` reads its name, description and parameters from the syntax tree
// without executing anything; this package executes it,
// with the host modules as the only globals.
//
// Those modules mirror the command line one to one
// (doc/design/cli-convention.md):
// every `git work <module> <verb>` is `work.<module>.<verb>(...)`,
// taking the same arguments and returning what the command prints,
// and every one of them is a call into package `host`,
// which is also what `commands/` calls.
// A flow is therefore exactly a shell script that runs in-process,
// with no name a shell does not have.
//
// Nothing here is a sandbox against a malicious script:
// a flow comes from the team's own refs and can write to the store by design.
// The caps are against a mistake — a runaway loop, a flow that calls itself —
// not against an attacker.
package run

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"go.starlark.net/starlark"
	"go.starlark.net/syntax"

	"github.com/git-bug/git-bug/cache"
	"github.com/git-bug/git-bug/flow"
	"github.com/git-bug/git-bug/host"
)

// MaxSteps bounds one flow's execution.
//
// A flow is a projection over a store a team can read in a terminal,
// so fifty million steps is orders of magnitude more than any real one needs
// and still trips in about a second on a loop that will never end.
const MaxSteps = 50_000_000

// MaxDepth bounds `work.flow.run` calling `work.flow.run`.
//
// Flows compose — a report calls a board — but a cycle between two of them is
// a mistake, and without a cap it is a mistake that fills memory.
const MaxDepth = 8

// fileOptions are the dialect a flow is written in.
//
// `set` is on because it is the natural way to collect distinct field values;
// top-level reassignment is off because a flow's one global is the SDK
// and shadowing it silently is how a script stops meaning what it reads as;
// recursion stays off, which is Starlark's default and what makes MaxSteps
// the only thing that can run long.
var fileOptions = &syntax.FileOptions{
	Set:            true,
	While:          false,
	GlobalReassign: false,
	Recursion:      false,
}

// Run executes a flow and returns what it returned, as JSON.
//
// A flow that returns None returns nil, which is a command that prints nothing.
func Run(ctx context.Context, repo *cache.RepoCache, stderr io.Writer, def *flow.Def, script string, kwargs map[string]json.RawMessage) (json.RawMessage, error) {
	return newRuntime(ctx, repo, stderr, 0).run(def, script, kwargs)
}

// Flow loads a flow by name and runs it, which is `git work flow run NAME`
// and the `work.flow.run(name, ...)` a script calls.
func Flow(ctx context.Context, repo *cache.RepoCache, stderr io.Writer, name string, kwargs map[string]json.RawMessage) (json.RawMessage, error) {
	return newRuntime(ctx, repo, stderr, 0).flow(name, kwargs)
}

// runtime is one execution, and the depth it is at.
type runtime struct {
	ctx    context.Context
	repo   *cache.RepoCache
	stderr io.Writer
	depth  int
}

func newRuntime(ctx context.Context, repo *cache.RepoCache, stderr io.Writer, depth int) *runtime {
	if stderr == nil {
		stderr = io.Discard
	}
	return &runtime{ctx: ctx, repo: repo, stderr: stderr, depth: depth}
}

// flow resolves a name to its script and runs it.
func (r *runtime) flow(name string, kwargs map[string]json.RawMessage) (json.RawMessage, error) {
	excerpt, err := host.FlowExcerpt(r.repo, name)
	if err != nil {
		return nil, err
	}

	script, err := host.FlowScript(excerpt)
	if err != nil {
		return nil, err
	}

	def, err := flow.Parse(script)
	if err != nil {
		return nil, fmt.Errorf("flow %s: %w", name, err)
	}

	return r.run(def, script, kwargs)
}

// run executes one flow: the module once, then the def by name.
func (r *runtime) run(def *flow.Def, script string, kwargs map[string]json.RawMessage) (json.RawMessage, error) {
	if r.depth >= MaxDepth {
		return nil, fmt.Errorf("flow %s: flows are nested more than %d deep, which is a cycle", def.Name, MaxDepth)
	}

	args, err := bindArgs(def, kwargs)
	if err != nil {
		return nil, fmt.Errorf("flow %s: %w", def.Name, err)
	}

	thread := &starlark.Thread{
		Name: def.Name,
		// print() is a diagnostic, and diagnostics go to stderr, so that a
		// flow that prints still pipes its JSON somewhere useful.
		Print: func(_ *starlark.Thread, msg string) {
			fmt.Fprintln(r.stderr, msg)
		},
	}
	thread.SetMaxExecutionSteps(MaxSteps)

	// The step cap catches a loop; ctx catches a host call that hangs and the
	// interrupt a user typed. Cancel is safe from another goroutine.
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		select {
		case <-r.ctx.Done():
			thread.Cancel(r.ctx.Err().Error())
		case <-stop:
		}
	}()

	globals, err := starlark.ExecFileOptions(fileOptions, thread, def.Name+".star", script, r.predeclared())
	if err != nil {
		return nil, evalError(def.Name, err)
	}

	fn, ok := globals[def.Name]
	if !ok {
		// Parse guarantees the def is there, so this is a different file.
		return nil, fmt.Errorf("flow %s: the script defines no function named %s", def.Name, def.Name)
	}

	result, err := starlark.Call(thread, fn, nil, args)
	if err != nil {
		return nil, evalError(def.Name, err)
	}

	raw, err := marshalStarlark(result)
	if err != nil {
		return nil, fmt.Errorf("flow %s returned something that is not JSON: %w", def.Name, err)
	}

	return raw, nil
}

// bindArgs matches a call's keyword arguments to the def's parameters.
//
// Defaults in the signature fill what the object omits,
// an unknown key is an error naming the parameters,
// and a parameter with no default that nobody named is an error too
// (cli-convention.md).
func bindArgs(def *flow.Def, kwargs map[string]json.RawMessage) ([]starlark.Tuple, error) {
	known := make(map[string]struct{}, len(def.Params))
	for _, param := range def.Params {
		known[param.Name] = struct{}{}
	}
	for name := range kwargs {
		if _, ok := known[name]; !ok {
			return nil, fmt.Errorf("unknown argument %s, the arguments are %s", name, paramList(def))
		}
	}

	args := make([]starlark.Tuple, 0, len(def.Params))
	for _, param := range def.Params {
		raw, given := kwargs[param.Name]
		if !given {
			if !param.HasDefault {
				return nil, fmt.Errorf("argument %s is required, the arguments are %s", param.Name, paramList(def))
			}
			// The signature's default is applied by Starlark itself, so that
			// what a flow does with no argument is what its source says.
			continue
		}

		decoded, err := decodeJSON(raw)
		if err != nil {
			return nil, fmt.Errorf("argument %s: %w", param.Name, err)
		}
		value, err := toStarlark(decoded)
		if err != nil {
			return nil, fmt.Errorf("argument %s: %w", param.Name, err)
		}
		args = append(args, starlark.Tuple{starlark.String(param.Name), value})
	}

	return args, nil
}

func paramList(def *flow.Def) string {
	if len(def.Params) == 0 {
		return "none"
	}
	names := make([]string, 0, len(def.Params))
	for _, param := range def.Params {
		if param.HasDefault {
			names = append(names, param.Name+"="+string(param.Default))
			continue
		}
		names = append(names, param.Name)
	}
	return strings.Join(names, ", ")
}

// evalError carries the flow's name and, for a Starlark failure, the
// backtrace, which is where the line a reader needs is written.
func evalError(name string, err error) error {
	var evalErr *starlark.EvalError
	if errors.As(err, &evalErr) {
		return fmt.Errorf("flow %s: %s", name, evalErr.Backtrace())
	}
	return fmt.Errorf("flow %s: %w", name, err)
}

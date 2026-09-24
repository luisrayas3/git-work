package flowcmd

import (
	"github.com/git-bug/git-bug/commands/execenv"
)

// printText prints what a flow returned for a reader rather than for a pipe.
//
// A flow returns data, not a picture: drawing is what a view call does, and it
// has already happened by the time this runs (decided 2026-09-24). So there is
// little to do — a string is printed bare, because a flow that returns a
// sentence should not print it in quotes, and a list of strings is one per
// line. Everything else is the JSON, which is the honest answer for a report
// or a number.
func printText(env *execenv.Env, value any) error {
	switch typed := value.(type) {
	case string:
		env.Out.Println(typed)
		return nil

	case []any:
		strings := make([]string, 0, len(typed))
		for _, item := range typed {
			s, ok := item.(string)
			if !ok {
				return env.Out.PrintJSON(value)
			}
			strings = append(strings, s)
		}
		for _, s := range strings {
			env.Out.Println(s)
		}
		return nil

	default:
		return env.Out.PrintJSON(value)
	}
}

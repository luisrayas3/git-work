package usercmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/git-bug/git-bug/commands/cmdjson"
	"github.com/git-bug/git-bug/commands/completion"
	"github.com/git-bug/git-bug/commands/execenv"
	"github.com/git-bug/git-bug/host"
	"github.com/git-bug/git-bug/util/colors"
)

// newUserMeCommand is `git work user me`, the identity this repository writes as.
//
// It is the row the bare `git work user` prints for that one identity,
// in the same two formats,
// and it is the command side of `work.user.me()`:
// both are one call to `host.UserMe`,
// so a script and a shell can never disagree about who is writing
// (doc/design/cli-convention.md).
func newUserMeCommand(env *execenv.Env) *cobra.Command {
	options := userOptions{}

	cmd := &cobra.Command{
		Use:     "me",
		Short:   "Display the identity you write as",
		Args:    cobra.NoArgs,
		PreRunE: execenv.LoadBackendEnsureUser(env),
		RunE: execenv.CloseBackend(env, func(cmd *cobra.Command, args []string) error {
			return runUserMe(env, options)
		}),
	}

	flags := cmd.Flags()
	flags.SortFlags = false

	flags.StringVarP(&options.format, "format", "f", "default",
		"Select the output formatting style. Valid values are [default,json]")
	cmd.RegisterFlagCompletionFunc("format", completion.From([]string{"default", "json"}))

	return cmd
}

func runUserMe(env *execenv.Env, opts userOptions) error {
	me, err := host.UserMe(env.Backend)
	if err != nil {
		return err
	}

	switch opts.format {
	case "json":
		return env.Out.PrintJSON(me)
	case "default":
		env.Out.Printf("%s %s\n", colors.Cyan(me.HumanId), meDisplayName(*me))
		return nil
	default:
		return fmt.Errorf("unknown format %s", opts.format)
	}
}

// meDisplayName is `IdentityExcerpt.DisplayName` over the JSON shape the host
// returns, so one identity reads the same whichever command printed it.
func meDisplayName(i cmdjson.Identity) string {
	switch {
	case i.Name == "":
		return i.Login
	case i.Login == "":
		return i.Name
	default:
		return fmt.Sprintf("%s (%s)", i.Name, i.Login)
	}
}

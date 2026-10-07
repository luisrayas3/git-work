package usercmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/git-bug/git-bug/commands/cmdjson"
	"github.com/git-bug/git-bug/commands/execenv"
	"github.com/git-bug/git-bug/host"
	"github.com/git-bug/git-bug/util/colors"
)

type userOptions struct {
	format string
}

func NewUserCommand(env *execenv.Env) *cobra.Command {
	options := userOptions{}

	cmd := &cobra.Command{
		Use:   "user",
		Short: "List identities",
		Long: `List the identities this repository knows about.

JSON out by default, like every other reader; --format text prints one line
per identity, the id and the display name.`,
		PreRunE: execenv.LoadBackend(env),
		RunE: execenv.CloseBackend(env, func(cmd *cobra.Command, args []string) error {
			return runUser(env, options)
		}),
	}

	cmd.AddCommand(newUserNewCommand(env))
	cmd.AddCommand(newUserMeCommand(env))
	cmd.AddCommand(newUserAdoptCommand(env))

	flags := cmd.Flags()
	flags.SortFlags = false

	execenv.AddFormatFlag(cmd, &options.format, "json", "text")

	return cmd
}

func runUser(env *execenv.Env, opts userOptions) error {
	users, err := host.UserList(env.Backend)
	if err != nil {
		return err
	}

	switch opts.format {
	case "json":
		return userJsonFormatter(env, users)
	case "text":
		return userTextFormatter(env, users)
	default:
		return fmt.Errorf("unknown format %s", opts.format)
	}
}

func userTextFormatter(env *execenv.Env, users []cmdjson.Identity) error {
	for _, user := range users {
		env.Out.Printf("%s %s\n",
			colors.Cyan(user.HumanId),
			meDisplayName(user),
		)
	}

	return nil
}

func userJsonFormatter(env *execenv.Env, users []cmdjson.Identity) error {
	return env.Out.PrintJSON(users)
}

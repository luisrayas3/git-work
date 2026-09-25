package usercmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/git-bug/git-bug/cache"
	"github.com/git-bug/git-bug/commands/cmdjson"
	"github.com/git-bug/git-bug/commands/execenv"
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
	ids := env.Backend.Identities().AllIds()
	var users []*cache.IdentityExcerpt
	for _, id := range ids {
		user, err := env.Backend.Identities().ResolveExcerpt(id)
		if err != nil {
			return err
		}
		users = append(users, user)
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

func userTextFormatter(env *execenv.Env, users []*cache.IdentityExcerpt) error {
	for _, user := range users {
		env.Out.Printf("%s %s\n",
			colors.Cyan(user.Id().Human()),
			user.DisplayName(),
		)
	}

	return nil
}

func userJsonFormatter(env *execenv.Env, users []*cache.IdentityExcerpt) error {
	jsonUsers := make([]cmdjson.Identity, len(users))
	for i, user := range users {
		jsonUsers[i] = cmdjson.NewIdentityFromExcerpt(user)
	}

	return env.Out.PrintJSON(jsonUsers)
}

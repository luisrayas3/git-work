package flowcmd

import (
	"github.com/git-bug/git-bug/commands/execenv"
	"github.com/git-bug/git-bug/entity"
	"github.com/git-bug/git-bug/util/colors"
	"github.com/git-bug/git-bug/view"
)

// printText is the placeholder standing where a renderer will be.
//
// A view returns a spec and a renderer draws it; neither renderer exists yet
// (`84dfbde` in the terminal, `8b06191` in the browser), so --format text says
// which view was asked for and lists the items the way `git work issue
// --format text` does. Anything that is not a spec is printed as JSON, which
// is the honest answer for a flow that returned a report or a number.
func printText(env *execenv.Env, value any) error {
	if !view.IsSpec(value) {
		return env.Out.PrintJSON(value)
	}

	spec, _ := value.(map[string]any)
	kind, _ := spec["view"].(string)
	items, _ := spec["items"].([]any)

	env.Out.Printf("view %s (%d items)\n", kind, len(items))

	for _, item := range items {
		object, ok := item.(map[string]any)
		if !ok {
			continue
		}
		fields, _ := object["fields"].(map[string]any)
		env.Out.Printf("%s\t%s\t%s\n",
			colors.Cyan(humanIdOf(object)),
			colors.Yellow(stringOr(fields["status"], "-")),
			stringOr(fields["title"], ""),
		)
	}

	return nil
}

func humanIdOf(object map[string]any) string {
	if human, ok := object["human_id"].(string); ok {
		return human
	}
	id, _ := object["id"].(string)
	if len(id) > entity.HumanIdLength {
		return id[:entity.HumanIdLength]
	}
	return id
}

func stringOr(v any, fallback string) string {
	if s, ok := v.(string); ok {
		return s
	}
	return fallback
}

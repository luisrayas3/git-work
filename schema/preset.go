package schema

import (
	"embed"
	"fmt"
	"path"
	"sort"
	"strings"
)

// presets are embedded, so that `git work schema init jira` needs no file and
// no network: a preset is instantiated as config entities like any import (D7).
//
//go:embed presets/*.yaml
var presets embed.FS

// DefaultPreset is what `git work schema init` uses when nothing is named.
//
// jira, because this repository dogfoods it (D7):
// an unverified preset exercised daily beats one exercised never.
const DefaultPreset = "jira"

// PresetNames lists the embedded presets.
func PresetNames() []string {
	entries, err := presets.ReadDir("presets")
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, strings.TrimSuffix(path.Base(e.Name()), ".yaml"))
	}
	sort.Strings(names)
	return names
}

// PresetBytes returns a preset's YAML as it is shipped.
func PresetBytes(name string) ([]byte, error) {
	if name == "" {
		name = DefaultPreset
	}
	data, err := presets.ReadFile("presets/" + name + ".yaml")
	if err != nil {
		return nil, fmt.Errorf("unknown preset %q; shipped presets: %s",
			name, strings.Join(PresetNames(), ", "))
	}
	return data, nil
}

// Preset parses and validates an embedded preset.
func Preset(name string) (*Document, error) {
	data, err := PresetBytes(name)
	if err != nil {
		return nil, err
	}
	doc, err := ParseDocument(data)
	if err != nil {
		return nil, fmt.Errorf("preset %s: %w", name, err)
	}
	if err := doc.Validate(nil); err != nil {
		return nil, fmt.Errorf("preset %s: %w", name, err)
	}
	return doc, nil
}

package repository

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	gogit "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/config"
	"github.com/go-git/go-git/v6/x/plugin"
	xconfig "github.com/go-git/go-git/v6/x/plugin/config"
)

func init() {
	err := plugin.Register(plugin.ConfigLoader(), hostConfigSource)
	if err != nil {
		panic(err)
	}
}

// hostConfigSource loads the global and system config as go-git's default does,
// but reads the files through hostFS:
// go-billy's sandboxed filesystem refuses a config reached through an absolute symlink,
// such as a ~/.config/git/config that home-manager links into /nix/store.
func hostConfigSource() plugin.ConfigSource {
	return xconfig.NewAuto(xconfig.WithFilesystem(hostFS{}))
}

// errWorktreeConfig is returned for a local config go-git cannot represent.
var errWorktreeConfig = errors.New("go-git cannot read a local config that has a config.worktree")

// localConfig returns the repository's local config.
// When a config.worktree exists,
// go-git merges it into a config that has lost every section go-git does not model,
// so that config is refused rather than read or written back.
func localConfig(repo *gogit.Repository) (*config.Config, error) {
	cfg, err := repo.Config()
	if err != nil {
		return nil, err
	}
	if cfg.Raw == nil {
		return nil, errWorktreeConfig
	}
	return cfg, nil
}

var _ Config = &goGitConfig{}

type goGitConfig struct {
	ConfigRead
	ConfigWrite
}

func newGoGitLocalConfig(repo *gogit.Repository) *goGitConfig {
	return &goGitConfig{
		ConfigRead: &goGitConfigReader{getConfig: func() (*config.Config, error) {
			return localConfig(repo)
		}},
		ConfigWrite: &goGitConfigWriter{repo: repo},
	}
}

func newGoGitGlobalConfig() *goGitConfig {
	return &goGitConfig{
		ConfigRead: &goGitConfigReader{getConfig: func() (*config.Config, error) {
			storer, err := hostConfigSource().Load(config.GlobalScope)
			if err != nil {
				return nil, err
			}
			return storer.Config()
		}},
		ConfigWrite: &configPanicWriter{},
	}
}

var _ ConfigRead = &goGitConfigReader{}

type goGitConfigReader struct {
	getConfig func() (*config.Config, error)
}

func (cr *goGitConfigReader) ReadAll(keyPrefix string) (map[string]string, error) {
	cfg, err := cr.getConfig()
	if err != nil {
		return nil, err
	}

	split := strings.Split(keyPrefix, ".")
	result := make(map[string]string)

	switch {
	case keyPrefix == "":
		for _, section := range cfg.Raw.Sections {
			for _, option := range section.Options {
				result[fmt.Sprintf("%s.%s", section.Name, option.Key)] = option.Value
			}
			for _, subsection := range section.Subsections {
				for _, option := range subsection.Options {
					result[fmt.Sprintf("%s.%s.%s", section.Name, subsection.Name, option.Key)] = option.Value
				}
			}
		}
	case len(split) == 1:
		if !cfg.Raw.HasSection(split[0]) {
			return nil, nil
		}
		section := cfg.Raw.Section(split[0])
		for _, option := range section.Options {
			result[fmt.Sprintf("%s.%s", section.Name, option.Key)] = option.Value
		}
		for _, subsection := range section.Subsections {
			for _, option := range subsection.Options {
				result[fmt.Sprintf("%s.%s.%s", section.Name, subsection.Name, option.Key)] = option.Value
			}
		}
	default:
		if !cfg.Raw.HasSection(split[0]) {
			return nil, nil
		}
		section := cfg.Raw.Section(split[0])
		rest := strings.Join(split[1:], ".")
		rest = strings.TrimSuffix(rest, ".")
		for _, subsection := range section.Subsections {
			if strings.HasPrefix(subsection.Name, rest) {
				for _, option := range subsection.Options {
					result[fmt.Sprintf("%s.%s.%s", section.Name, subsection.Name, option.Key)] = option.Value
				}
			}
		}
	}

	return result, nil
}

func (cr *goGitConfigReader) ReadBool(key string) (bool, error) {
	val, err := cr.ReadString(key)
	if err != nil {
		return false, err
	}

	return strconv.ParseBool(val)
}

func (cr *goGitConfigReader) ReadString(key string) (string, error) {
	cfg, err := cr.getConfig()
	if err != nil {
		return "", err
	}

	split := strings.Split(key, ".")

	if len(split) <= 1 {
		return "", fmt.Errorf("invalid key")
	}

	sectionName := split[0]
	if !cfg.Raw.HasSection(sectionName) {
		return "", newErrNoConfigEntry(key)
	}
	section := cfg.Raw.Section(sectionName)

	switch {
	case len(split) == 2:
		optionName := split[1]
		if !section.HasOption(optionName) {
			return "", newErrNoConfigEntry(key)
		}
		if len(section.OptionAll(optionName)) > 1 {
			return "", newErrMultipleConfigEntry(key)
		}
		return section.Option(optionName), nil
	default:
		subsectionName := strings.Join(split[1:len(split)-1], ".")
		optionName := split[len(split)-1]
		if !section.HasSubsection(subsectionName) {
			return "", newErrNoConfigEntry(key)
		}
		subsection := section.Subsection(subsectionName)
		if !subsection.HasOption(optionName) {
			return "", newErrNoConfigEntry(key)
		}
		if len(subsection.OptionAll(optionName)) > 1 {
			return "", newErrMultipleConfigEntry(key)
		}
		return subsection.Option(optionName), nil
	}
}

func (cr *goGitConfigReader) ReadTimestamp(key string) (time.Time, error) {
	value, err := cr.ReadString(key)
	if err != nil {
		return time.Time{}, err
	}
	return ParseTimestamp(value)
}

var _ ConfigWrite = &goGitConfigWriter{}

// Only works for the local config as go-git only support that
type goGitConfigWriter struct {
	repo *gogit.Repository
}

func (cw *goGitConfigWriter) StoreString(key, value string) error {
	cfg, err := localConfig(cw.repo)
	if err != nil {
		return err
	}

	split := strings.Split(key, ".")

	switch {
	case len(split) <= 1:
		return fmt.Errorf("invalid key")
	case len(split) == 2:
		cfg.Raw.Section(split[0]).SetOption(split[1], value)
	default:
		section := split[0]
		subsection := strings.Join(split[1:len(split)-1], ".")
		option := split[len(split)-1]
		cfg.Raw.Section(section).Subsection(subsection).SetOption(option, value)
	}

	return cw.repo.SetConfig(cfg)
}

func (cw *goGitConfigWriter) StoreTimestamp(key string, value time.Time) error {
	return cw.StoreString(key, strconv.Itoa(int(value.Unix())))
}

func (cw *goGitConfigWriter) StoreBool(key string, value bool) error {
	return cw.StoreString(key, strconv.FormatBool(value))
}

func (cw *goGitConfigWriter) RemoveAll(keyPrefix string) error {
	cfg, err := localConfig(cw.repo)
	if err != nil {
		return err
	}

	split := strings.Split(keyPrefix, ".")

	switch {
	case keyPrefix == "":
		cfg.Raw.Sections = nil
		// warning: this does not actually remove everything as go-git config hold
		// some entries in multiple places (cfg.User ...)
	case len(split) == 1:
		if cfg.Raw.HasSection(split[0]) {
			cfg.Raw.RemoveSection(split[0])
		}
	case cfg.Raw.HasSection(split[0]):
		section := cfg.Raw.Section(split[0])
		rest := strings.Join(split[1:], ".")

		if section.HasSubsection(rest) {
			section.RemoveSubsection(rest)
		}
		if section.HasOption(rest) {
			section.RemoveOption(rest)
		}
	}

	return cw.repo.SetConfig(cfg)
}

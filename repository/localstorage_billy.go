package repository

import (
	"errors"

	"github.com/go-git/go-billy/v6"
	"github.com/go-git/go-billy/v6/util"
)

var _ LocalStorage = &billyLocalStorage{}

type billyLocalStorage struct {
	billy.Filesystem
}

func (b billyLocalStorage) RemoveAll(path string) error {
	err := util.RemoveAll(b.Filesystem, path)
	if !errors.Is(err, billy.ErrBaseDirCannotBeRemoved) {
		return err
	}

	// go-billy v6 refuses to remove the base directory itself,
	// so empty it instead.
	entries, err := b.Filesystem.ReadDir(path)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		err = util.RemoveAll(b.Filesystem, b.Filesystem.Join(path, entry.Name()))
		if err != nil {
			return err
		}
	}
	return nil
}

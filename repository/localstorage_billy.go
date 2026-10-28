package repository

import (
	"errors"
	"os"

	"github.com/go-git/go-billy/v6"
	"github.com/go-git/go-billy/v6/osfs"
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

	// go-billy v6 refuses to remove the base directory itself.
	// On disk, remove it directly;
	// in memory, where there is no directory to remove, empty it.
	if _, ok := b.Filesystem.(*osfs.BoundOS); ok {
		return os.RemoveAll(b.Root())
	}
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

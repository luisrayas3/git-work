package repository

import (
	"os"
	"path/filepath"

	"github.com/go-git/go-billy/v6"
)

var _ billy.Basic = hostFS{}

// hostFS is a billy.Basic over the host filesystem, unsandboxed:
// paths are taken as the os package takes them, and symlinks are followed wherever they lead.
// It is for reading files git itself reads from anywhere, such as the global config.
type hostFS struct{}

func (hostFS) Create(filename string) (billy.File, error) {
	f, err := os.Create(filename)
	if err != nil {
		return nil, err
	}
	return f, nil
}

func (hostFS) Open(filename string) (billy.File, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	return f, nil
}

func (hostFS) OpenFile(filename string, flag int, perm os.FileMode) (billy.File, error) {
	f, err := os.OpenFile(filename, flag, perm)
	if err != nil {
		return nil, err
	}
	return f, nil
}

func (hostFS) Stat(filename string) (os.FileInfo, error) {
	return os.Stat(filename)
}

func (hostFS) Rename(oldpath, newpath string) error {
	return os.Rename(oldpath, newpath)
}

func (hostFS) Remove(filename string) error {
	return os.Remove(filename)
}

func (hostFS) Join(elem ...string) string {
	return filepath.Join(elem...)
}

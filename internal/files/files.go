// Package files writes state the way a Raspberry Pi needs it written: a power cut or a reader
// in the middle must find the old file or the new one, never half of either.
package files

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
)

// WriteAtomic writes to a temporary file beside the target, flushes it to disk and renames it
// over the target, which the filesystem does in one step.
func WriteAtomic(path string, data []byte, perm os.FileMode) error {
	temp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	name := temp.Name()
	defer os.Remove(name) // a no-op once renamed
	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Chmod(perm); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

// Update rewrites a file that several processes read, change and write, holding an exclusive
// lock across the three so none of them writes over a change it never read. change gets the
// current content, nil when there is none.
func Update(path string, perm os.FileMode, change func(current []byte) ([]byte, error)) error {
	// The lock lives on a file of its own: the rename in WriteAtomic replaces the target's inode.
	lock, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	current, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	next, err := change(current)
	if err != nil {
		return err
	}
	return WriteAtomic(path, next, perm)
}

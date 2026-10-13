// Package files writes state the way a Raspberry Pi needs it written: a power cut or a reader
// in the middle must find the old file or the new one, never half of either.
package files

import (
	"os"
	"path/filepath"
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

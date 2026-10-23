// Package atomicfile replaces files so that a crash, of the program or of
// the system, leaves either the file as it was or the file as it was to be,
// never a part of either.
package atomicfile

import (
	"io/fs"
	"os"
	"path/filepath"
)

// Write replaces the file at path with data, which has the permissions perm.
// The data goes to a temporary file beside it, which is flushed to the disk
// and then renamed into place, which is atomic; the folder is then flushed
// too, so that the rename outlasts a crash of the system. Nothing is left
// behind if it fails, and the file at path is then as it was.
func Write(path string, data []byte, perm fs.FileMode) error {
	dir := filepath.Dir(path)
	// The temporary file is made readable by the user alone, so that what
	// is secret is never readable by others, even for a moment.
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if perm != 0o600 {
		if err := tmp.Chmod(perm); err != nil {
			tmp.Close()
			return err
		}
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return err
	}
	syncDir(dir)
	return nil
}

// syncDir flushes a folder to the disk, so that a file renamed into it stays
// there after a crash of the system. Not every system can flush a folder
// (Windows cannot open one), and the file is in place either way, so a
// failure is not reported.
func syncDir(dir string) {
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		d.Close()
	}
}

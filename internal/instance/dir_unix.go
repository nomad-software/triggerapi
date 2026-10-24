//go:build unix

package instance

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// Dir is the folder the lock and the socket are kept in: the user's runtime
// folder (XDG_RUNTIME_DIR), which only they can open and which goes when they
// log out, or else a folder of their own in the temporary folder (macOS has a
// temporary folder for each user, Linux one for all).
func Dir() (string, error) {
	if dir := os.Getenv("XDG_RUNTIME_DIR"); dir != "" {
		return dir, nil
	}
	dir := filepath.Join(os.TempDir(), fmt.Sprintf("triggerapi-%d", os.Getuid()))
	if err := os.Mkdir(dir, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		return "", err
	}
	// Anyone can make a folder in a temporary folder for all, so one made
	// before is used only if it is the user's and only theirs.
	info, err := os.Lstat(dir)
	if err != nil {
		return "", err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !info.IsDir() || info.Mode().Perm() != 0o700 || !ok || int(stat.Uid) != os.Getuid() {
		return "", fmt.Errorf("%s is not a folder only this user can open", dir)
	}
	return dir, nil
}

// tryLock takes the lock on the file, and says whether it could: it cannot
// while another copy holds it.
func tryLock(f *os.File) (bool, error) {
	err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if errors.Is(err, syscall.EWOULDBLOCK) {
		return false, nil
	}
	return err == nil, err
}

// Package homedir reads paths as a user writes them in the settings, where a
// path can start with ~ for the home folder.
package homedir

import (
	"os"
	"path/filepath"
	"strings"
)

// Expand returns a path with the ~ it starts with, if it does, replaced by
// the home folder of the user. A ~ followed by anything but a separator, as
// in ~user, is left as it is.
func Expand(path string) string {
	rest, ok := strings.CutPrefix(path, "~")
	if !ok || (rest != "" && !os.IsPathSeparator(rest[0])) {
		return path
	}
	dir, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	return filepath.Join(dir, rest)
}

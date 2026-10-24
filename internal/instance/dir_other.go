//go:build !unix

package instance

import (
	"errors"
	"os"
)

// Dir says that there is no lock here: only Linux and macOS are kept to one
// copy, and the program runs as it would otherwise.
func Dir() (string, error) {
	return "", errors.ErrUnsupported
}

// tryLock is not called where Dir has no folder.
func tryLock(*os.File) (bool, error) {
	return false, errors.ErrUnsupported
}

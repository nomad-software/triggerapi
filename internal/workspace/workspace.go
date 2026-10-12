// Package workspace keeps the workspaces, each of which has its own
// environments and requests. A workspace is a folder, named after it, in the
// folder all the workspaces are kept in, and holds a file for each. It has no
// dependency on the UI.
package workspace

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
)

// Default is the name of the workspace there is to begin with.
const Default = "Default"

// The names of the files in the folder of a workspace.
const (
	environmentsFile = "environments.json"
	requestsFile     = "requests.json"
)

// The folders and files of the workspaces can be read and written by the
// user alone.
const (
	folderMode = 0o700
	fileMode   = 0o600
)

// appName is the name of the folder of the program, in the place the system
// has for the data of programs.
const appName = "triggerapi"

// DefaultDir returns the folder the workspaces are kept in unless another is
// chosen: a folder for the program in the place the system has for the data
// of programs, which is ~/.local/share on Linux (or $XDG_DATA_HOME) and
// ~/Library/Application Support on macOS.
func DefaultDir() (string, error) {
	dir, err := dataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, appName, "workspaces"), nil
}

// dataDir returns the place the system has for the data of programs.
func dataDir() (string, error) {
	switch runtime.GOOS {
	case "darwin", "ios", "windows", "plan9":
		// These keep data where they keep configuration.
		return os.UserConfigDir()
	}

	// The rest follow the XDG base directory specification, in which a
	// path that is not absolute is to be ignored.
	if dir := os.Getenv("XDG_DATA_HOME"); filepath.IsAbs(dir) {
		return dir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share"), nil
}

// Environments returns the path of the file the environments of a workspace
// are kept in.
func Environments(dir, name string) string {
	return filepath.Join(dir, name, environmentsFile)
}

// Requests returns the path of the file the requests of a workspace are kept
// in.
func Requests(dir, name string) string {
	return filepath.Join(dir, name, requestsFile)
}

// List returns the names of the workspaces in a folder, in alphabetical
// order ignoring case. There are none in a folder that does not exist.
func List(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}

	var names []string
	for _, entry := range entries {
		// Hidden folders are not workspaces, as none can be given such a
		// name.
		if entry.IsDir() && !strings.HasPrefix(entry.Name(), ".") {
			names = append(names, entry.Name())
		}
	}
	slices.SortFunc(names, func(a, b string) int {
		return strings.Compare(strings.ToLower(a), strings.ToLower(b))
	})
	return names, nil
}

// Valid reports why a workspace cannot be given a name, which has to be
// usable as the name of a folder on any system.
func Valid(name string) error {
	switch {
	case name == "":
		return errors.New("a workspace needs a name")
	case name != strings.TrimSpace(name):
		return errors.New("the name of a workspace cannot start or end with a space")
	case strings.HasPrefix(name, ".") || strings.HasSuffix(name, "."):
		return errors.New("the name of a workspace cannot start or end with a dot")
	case len(name) > 100:
		return errors.New("the name of a workspace cannot be longer than 100 characters")
	}
	if strings.ContainsFunc(name, func(r rune) bool { return r < ' ' || strings.ContainsRune(`/\:*?"<>|`, r) }) {
		return errors.New(`the name of a workspace cannot contain any of / \ : * ? " < > |`)
	}
	return nil
}

// has reports if a folder has a workspace of a name, ignoring case, as some
// systems do in the names of folders.
func has(dir, name string) (bool, error) {
	names, err := List(dir)
	if err != nil {
		return false, err
	}
	return slices.ContainsFunc(names, func(other string) bool { return strings.EqualFold(other, name) }), nil
}

// Unique returns a name that no workspace in a folder has yet: the one
// given, or that with a number after it.
func Unique(dir, name string) string {
	for n := 1; ; n++ {
		candidate := name
		if n > 1 {
			candidate = fmt.Sprintf("%s %d", name, n)
		}
		if taken, err := has(dir, candidate); err != nil || !taken {
			return candidate
		}
	}
}

// Create adds an empty workspace to a folder, which is made if need be.
func Create(dir, name string) error {
	if err := Valid(name); err != nil {
		return err
	}
	if taken, err := has(dir, name); err != nil {
		return err
	} else if taken {
		return fmt.Errorf("there is already a workspace named %s", name)
	}
	return os.MkdirAll(filepath.Join(dir, name), folderMode)
}

// Rename gives a workspace another name.
func Rename(dir, from, to string) error {
	if from == to {
		return nil
	}
	if err := Valid(to); err != nil {
		return err
	}
	// A change to the case of the letters alone is not a clash with itself.
	if !strings.EqualFold(from, to) {
		if taken, err := has(dir, to); err != nil {
			return err
		} else if taken {
			return fmt.Errorf("there is already a workspace named %s", to)
		}
	}
	return os.Rename(filepath.Join(dir, from), filepath.Join(dir, to))
}

// Remove deletes a workspace with its environments and requests. A folder
// which has anything else in it is left alone, so that nothing is deleted
// which was not put there by the program.
func Remove(dir, name string) error {
	if err := Valid(name); err != nil {
		return err
	}

	folder := filepath.Join(dir, name)
	entries, err := os.ReadDir(folder)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || (entry.Name() != environmentsFile && entry.Name() != requestsFile) {
			return fmt.Errorf("its folder has other files in it, such as %s", entry.Name())
		}
	}

	for _, entry := range entries {
		if err := os.Remove(filepath.Join(folder, entry.Name())); err != nil {
			return err
		}
	}
	return os.Remove(folder)
}

// Duplicate makes a copy of a workspace, with its environments and requests,
// and returns the name it was given.
func Duplicate(dir, name string) (string, error) {
	if err := Valid(name); err != nil {
		return "", err
	}
	if taken, err := has(dir, name); err != nil {
		return "", err
	} else if !taken {
		return "", fmt.Errorf("there is no workspace named %s", name)
	}

	// A long name is cut short to leave room for what is added to it.
	base := name
	if len(base) > 80 {
		base = strings.TrimSpace(strings.ToValidUTF8(base[:80], ""))
	}
	copied := Unique(dir, base+" copy")
	if err := Create(dir, copied); err != nil {
		return "", err
	}
	return copied, Import(dir, copied, Environments(dir, name), Requests(dir, name))
}

// Import copies the files of environments and requests from before there
// were workspaces into a workspace. A file that does not exist is skipped,
// and nothing already in the workspace is replaced.
func Import(dir, name, environments, requests string) error {
	return errors.Join(
		copyFile(environments, Environments(dir, name)),
		copyFile(requests, Requests(dir, name)),
	)
}

// CopyAll copies every workspace of one folder into another, leaving the
// first as it was. Nothing already in the second is replaced.
func CopyAll(from, to string) error {
	names, err := List(from)
	if err != nil {
		return err
	}
	for _, name := range names {
		if err := os.MkdirAll(filepath.Join(to, name), folderMode); err != nil {
			return err
		}
		if err := Import(to, name, Environments(from, name), Requests(from, name)); err != nil {
			return err
		}
	}
	return nil
}

// copyFile copies a file, unless there is none to copy or there is already
// one where it would go.
func copyFile(from, to string) error {
	data, err := os.ReadFile(from)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}

	file, err := os.OpenFile(to, os.O_WRONLY|os.O_CREATE|os.O_EXCL, fileMode)
	if errors.Is(err, fs.ErrExist) {
		return nil
	} else if err != nil {
		return err
	}
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}

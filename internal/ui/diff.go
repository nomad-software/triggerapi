package ui

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/nomad-software/triggerapi/internal/collection"
	"github.com/nomad-software/triggerapi/internal/history"
	"github.com/nomad-software/triggerapi/internal/shell"
)

// Comparing the responses of entries of the history with the diff tool the
// settings name. The command is the user's own, from the settings, never
// from a workspace, so nothing anyone else has written is run.

// errNoDiffCommand is the error of comparing with no diff tool in the settings.
var errNoDiffCommand = errors.New("there is no diff tool to compare them with: set its command under History in File > Settings")

// diffExtension returns the extension of the file the response of an entry
// is compared in, which a diff tool may colour it by.
func diffExtension(e *history.Entry) string {
	switch {
	case e.Error != "", e.WebSocket != nil:
		return ".txt"
	case e.HTTP != nil:
		return filepath.Ext(responseFileName(e.HTTP.Headers.Get("Content-Type")))
	}
	return ".json"
}

// writeDiffFiles writes the response of each entry, oldest first, to a file
// of its own in a new folder, which only the user can read as responses can
// hold secrets, and returns the folder and the paths of the files. Each is
// named as a saved response is, after a number which keeps them in order.
func writeDiffFiles(entries []*history.Entry) (dir string, paths []string, err error) {
	entries = slices.Clone(entries)
	slices.SortStableFunc(entries, func(a, b *history.Entry) int { return a.Time.Compare(b.Time) })

	dir, err = os.MkdirTemp("", "triggerapi-diff-")
	if err != nil {
		return "", nil, err
	}
	for i, e := range entries {
		name := ""
		if e.Request != nil {
			name = e.Request.Name
		}
		if strings.TrimSpace(name) == "" {
			name = untitledName(e.Request)
		}
		path := filepath.Join(dir, fmt.Sprintf("%d_%s", i+1, saveFileName(name, e.Time.Local(), diffExtension(e))))
		if err := os.WriteFile(path, []byte(e.Text()), 0o600); err != nil {
			os.RemoveAll(dir)
			return "", nil, err
		}
		paths = append(paths, path)
	}
	return dir, paths, nil
}

// untitledName is what the file of the response of a request that was not
// saved is named after: its type, so that files of different types can be
// told apart.
func untitledName(r *collection.Request) string {
	if r == nil {
		return ""
	}
	return typeLabels[r.Type]
}

// startDiff starts the diff command with the paths of the files to compare
// after its own arguments, without waiting for it to finish.
func startDiff(command string, paths []string) error {
	words, err := shell.Split(command)
	if err != nil {
		return fmt.Errorf("the diff command cannot be read, as %w", err)
	}
	if len(words) == 0 {
		return errNoDiffCommand
	}
	cmd := exec.Command(words[0], append(words[1:], paths...)...)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("the diff tool could not be started: %w", err)
	}
	// It is waited for in the background, so that it does not linger once
	// it has finished. A tool which reports differences exits with a
	// failure, which is not one.
	go cmd.Wait()
	return nil
}

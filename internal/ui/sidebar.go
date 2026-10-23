package ui

import (
	"fmt"
	"os"
	"path/filepath"

	qt "github.com/mappu/miqt/qt6"
	"github.com/mappu/miqt/qt6/mainthread"

	"github.com/nomad-software/triggerapi/internal/crypt"
	"github.com/nomad-software/triggerapi/internal/jsonfile"
)

// importKind describes a kind of file that something of type T can be
// imported from.
type importKind[T any] struct {
	label  string
	filter string
	// colour and glyph make the icon it is offered with. See drawIcon.
	colour string
	glyph  func(*qt.QPainter)
	read   func([]byte) (T, error)
}

// newSourceDialog returns a dialog which asks which of the kinds of file, in
// the order given, to import from.
func newSourceDialog[K comparable, T any](parent *qt.QWidget, prompt string, order []K, kinds map[K]importKind[T]) *choiceDialog[K] {
	choices := make([]choice[K], len(order))
	for i, source := range order {
		kind := kinds[source]
		choices[i] = choice[K]{source, kind.label, drawIcon(iconSize*2, kind.colour, kind.glyph)}
	}
	return newChoiceDialog(parent, "Import", prompt, choices)
}

// chooseImportFile asks the user for a file of a kind to import. It reports
// false if they decide against choosing one.
func chooseImportFile[T any](parent *qt.QWidget, kind importKind[T]) (string, bool) {
	path := qt.QFileDialog_GetOpenFileName4(parent, "Import "+kind.label, "", kind.filter+";;All files (*)")
	return path, path != ""
}

// readImport reads what the file at path, of a kind, holds.
func readImport[T any](kind importKind[T], path string) (T, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		var none T
		return none, err
	}
	return kind.read(data)
}

// importDocument asks what kind of file to import, and for the file, and
// imports it, warning of a failure.
func importDocument[K comparable](chooseSource func() (K, bool), chooseFile func(K) (string, bool), importFile func(K, string) error, warn func(string)) {
	source, ok := chooseSource()
	if !ok {
		return
	}
	path, ok := chooseFile(source)
	if !ok {
		return
	}
	if err := importFile(source, path); err != nil {
		warn(fmt.Sprintf("%s could not be imported: %v.", filepath.Base(path), err))
	}
}

// writer writes the files of the stores of the sidebar in the background,
// so that the GUI thread does not wait for the encryption and the disk.
// writeFiles waits for it.
var writer = jsonfile.NewWriter()

// saveStore has a store of the sidebar written to its file with the keys
// that keys returns, if it has a file, and reports a failure to onError on
// the GUI thread. The store is turned into JSON at once; it is encrypted and
// written by writer.
func saveStore(path string, keys func() *crypt.Keys, store any, onError func(error)) {
	if path == "" {
		return
	}
	var with *crypt.Keys
	if keys != nil {
		with = keys()
	}
	report := func(err error) {
		if err != nil && onError != nil {
			mainthread.Start(func() { onError(err) })
		}
	}
	if err := writer.Save(path, store, with, report); err != nil && onError != nil {
		onError(err)
	}
}

// copyName returns a name for a copy of something, which is its own name
// with " copy" added as often as it takes for it not to be taken.
func copyName(name string, taken func(string) bool) string {
	for taken(name) {
		name += " copy"
	}
	return name
}

// plural returns a count of things, such as "1 folder" or "3 folders".
func plural(n int, thing string) string {
	if n == 1 {
		return "1 " + thing
	}
	return fmt.Sprintf("%d %ss", n, thing)
}

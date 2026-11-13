package ui

import (
	"github.com/nomad-software/qute/qtwidgets"
)

// The dialogs which ask for a file or a folder, over a widget, or over no
// window if it is nil. Each returns the path chosen, or nothing if the
// dialog was cancelled.

// askOpenFile asks for a file to read, of those filter lets through.
func askOpenFile(parent *qtwidgets.QWidget, caption, dir, filter string) string {
	path, _ := qtwidgets.QFileDialogGetOpenFileNameFull(parent, caption, dir, filter, "", 0)
	return path
}

// askSaveFile asks for a file to write, of those filter lets through.
func askSaveFile(parent *qtwidgets.QWidget, caption, dir, filter string) string {
	path, _ := qtwidgets.QFileDialogGetSaveFileNameFull(parent, caption, dir, filter, "", 0)
	return path
}

// askDirectory asks for a folder.
func askDirectory(parent *qtwidgets.QWidget, caption, dir string) string {
	return qtwidgets.QFileDialogGetExistingDirectoryFull(parent, caption, dir, qtwidgets.QFileDialogShowDirsOnly)
}

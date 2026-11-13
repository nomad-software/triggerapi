package ui

import (
	"github.com/nomad-software/qute/qtwidgets"
)

// openDialog shows a dialog which keeps the window from being used until it
// is answered, without waiting for it, and calls finished once it is, after
// which it is deleted. It returns what closes it, rejected, if it is still
// open.
func openDialog(d *qtwidgets.QDialog, finished func()) (close func()) {
	done := false
	d.OnFinished(func(int) {
		done = true
		finished()
		d.DeleteLater()
	})
	d.Open()
	return func() {
		if !done {
			d.Reject()
		}
	}
}

package ui

import (
	qt "github.com/mappu/miqt/qt6"

	"github.com/nomad-software/triggerapi/internal/rest"
)

// formatButton is a button which sets out the JSON in an editor with each
// member and element on a line of its own.
type formatButton struct {
	button *qt.QPushButton
	edit   *qt.QPlainTextEdit

	// warn tells the user of a problem.
	warn func(message string)
}

// newFormatButton returns the button which sets out the JSON in an editor.
func newFormatButton(edit *qt.QPlainTextEdit) *formatButton {
	f := &formatButton{button: qt.NewQPushButton3("Format"), edit: edit}
	f.button.SetToolTip("Set out the JSON with each value on a line of its own")
	f.button.OnClicked(f.format)

	// A problem is shown beneath the button, and goes away by itself.
	f.warn = func(message string) {
		below := qt.NewQPoint2(0, f.button.Height())
		defer below.Delete()
		qt.QToolTip_ShowText2(f.button.MapToGlobalWithQPoint(below), message, f.button.QWidget)
	}
	return f
}

// format sets out the JSON in the editor, which is left as it is if it is
// not JSON. The change is one that the editor can undo.
func (f *formatButton) format() {
	text := f.edit.ToPlainText()
	formatted, err := rest.FormatJSON(text)
	if err != nil {
		f.warn("This is not valid JSON, so it cannot be formatted: " + err.Error())
		return
	}
	if formatted == text {
		return
	}

	// Setting the text of the editor would forget what could be undone.
	cursor := f.edit.TextCursor()
	defer release(cursor)
	cursor.Select(qt.QTextCursor__Document)
	cursor.InsertText(formatted)
}

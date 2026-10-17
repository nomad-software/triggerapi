package ui

import (
	qt "github.com/mappu/miqt/qt6"

	"github.com/nomad-software/triggerapi/internal/rest"
)

// formatButton is a button which sets out the JSON in an editor with each
// member and element on a line of its own. It is an action button, shown as
// an icon.
type formatButton struct {
	button *qt.QToolButton
	edit   *qt.QPlainTextEdit

	// warn tells the user of a problem.
	warn func(message string)
}

// newFormatButton returns the button which sets out the JSON in an editor.
func newFormatButton(edit *qt.QPlainTextEdit) *formatButton {
	f := &formatButton{edit: edit}
	f.button = newIconButton(colourAction, drawBraces, "Format the JSON, with each value on a line of its own")
	f.button.OnClicked(f.format)

	f.warn = warnBeneath(f.button.QWidget)
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

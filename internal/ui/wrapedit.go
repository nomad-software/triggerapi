package ui

import (
	"math"
	"strings"

	qt "github.com/mappu/miqt/qt6"
)

// wrapEdit is a field for one line of text which, unlike a line edit, wraps
// text too long for its width and grows taller to show all of it. It is used
// for URLs, which can be very long. A line edit cannot wrap, so it is a text
// edit that is kept to one paragraph and to the height of its text.
type wrapEdit struct {
	*qt.QTextEdit

	// text is what the field last held, to tell an edit from the text
	// being recoloured, which a text edit also reports as a change.
	text string

	// onChange is called when the text changes, however it was changed.
	onChange func(string)
	// onReturn is called when Return is pressed in the field.
	onReturn func()
}

func newWrapEdit() *wrapEdit {
	e := &wrapEdit{QTextEdit: qt.NewQTextEdit2()}

	e.SetAcceptRichText(false)
	e.SetTabChangesFocus(true)
	// A URL has nowhere a line would be broken, so it is broken anywhere.
	e.SetWordWrapMode(qt.QTextOption__WrapAnywhere)
	e.SetVerticalScrollBarPolicy(qt.ScrollBarAlwaysOff)
	e.SetHorizontalScrollBarPolicy(qt.ScrollBarAlwaysOff)
	e.SetSizePolicy2(qt.QSizePolicy__Expanding, qt.QSizePolicy__Fixed)

	// The height follows the text, which is laid out again when it changes
	// and when the width of the field does.
	e.Document().DocumentLayout().OnDocumentSizeChanged(func(*qt.QSizeF) { e.fit() })
	e.OnResizeEvent(func(super func(*qt.QResizeEvent), event *qt.QResizeEvent) {
		super(event)
		e.fit()
	})
	e.fit()

	e.OnKeyPressEvent(func(super func(*qt.QKeyEvent), event *qt.QKeyEvent) {
		switch event.Key() {
		case int(qt.Key_Return), int(qt.Key_Enter):
			// There is only the one line.
			if e.onReturn != nil && !e.IsReadOnly() {
				e.onReturn()
			}
		default:
			super(event)
		}
	})

	e.OnTextChanged(func() {
		text := e.ToPlainText()
		if strings.ContainsAny(text, "\r\n") {
			// Something pasted can have more than one line. Setting the
			// text reports this change again, without them.
			e.SetText(text)
			e.MoveCursor(qt.QTextCursor__End)
			return
		}
		if text == e.text {
			return
		}
		e.text = text
		if e.onChange != nil {
			e.onChange(text)
		}
	})

	return e
}

// fit makes the field as tall as its text.
func (e *wrapEdit) fit() {
	height := int(math.Ceil(e.Document().Size().Height())) + 2*e.FrameWidth()
	if height != e.Height() || e.MinimumHeight() != height {
		e.SetFixedHeight(height)
	}
}

// textPiece is a piece of text and the format it is shown in, which is the
// usual one if it is nil.
type textPiece struct {
	text   string
	format *qt.QTextCharFormat
}

// setPieces replaces the text of the field with pieces of text, each in a
// format of its own.
func (e *wrapEdit) setPieces(pieces []textPiece) {
	plain := qt.NewQTextCharFormat()
	defer plain.Delete()

	e.Clear()
	cursor := e.TextCursor()
	for _, piece := range pieces {
		format := piece.format
		if format == nil {
			format = plain
		}
		cursor.InsertText2(strings.NewReplacer("\r", "", "\n", "").Replace(piece.text), format)
	}
}

// Text returns the text of the field.
func (e *wrapEdit) Text() string {
	return e.text
}

// SetText replaces the text of the field, leaving out any line breaks.
func (e *wrapEdit) SetText(text string) {
	text = strings.NewReplacer("\r", "", "\n", "").Replace(text)
	if text != e.ToPlainText() {
		e.SetPlainText(text)
	}
}

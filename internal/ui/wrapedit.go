package ui

import (
	"github.com/nomad-software/qute/qtcore"
	"github.com/nomad-software/qute/qtgui"
	"github.com/nomad-software/qute/qtwidgets"

	"math"
	"strings"
)

// wrapEdit is a field for one line of text which, unlike a line edit, wraps
// text too long for its width and grows taller to show all of it. It is used
// for URLs, which can be very long. A line edit cannot wrap, so it is a text
// edit that is kept to one paragraph and to the height of its text.
type wrapEdit struct {
	qtwidgets.QTextEditBase

	// text is what the field last held, to tell an edit from the text
	// being recoloured, which a text edit also reports as a change.
	text string

	// onChange is called when the text changes. What is typed is reported
	// only once it is committed (commit): when Return is pressed or the
	// field loses the keyboard. Text set from code is reported at once.
	onChange func(string)
	// onReturn is called when Return is pressed in the field, after what
	// was typed is committed.
	onReturn func()
	// onPaste is given text pasted or dropped into the field, and reports
	// if it has taken it, which is then not put in the field.
	onPaste func(text string) bool

	// typing is set while a key is handled, so that a change it makes
	// waits for commit; pending is set while one is waiting.
	typing, pending bool
}

func newWrapEdit() *wrapEdit {
	e := &wrapEdit{}
	qtwidgets.NewQTextEditWith(e, nil)

	e.SetAcceptRichText(false)
	e.SetTabChangesFocus(true)
	// A URL has nowhere a line would be broken, so it is broken anywhere.
	e.SetWordWrapMode(qtgui.QTextOptionWrapAnywhere)
	e.SetVerticalScrollBarPolicy(qtcore.ScrollBarAlwaysOff)
	e.SetHorizontalScrollBarPolicy(qtcore.ScrollBarAlwaysOff)
	e.SetSizePolicy(qtwidgets.NewQSizePolicyWithPolicy(qtwidgets.QSizePolicyExpanding, qtwidgets.QSizePolicyFixed))

	// The height follows the text, which is laid out again when it changes
	// and when the width of the field does (ResizeEvent).
	e.Document().DocumentLayout().OnDocumentSizeChanged(func(qtcore.QSizeF) { e.fit() })
	e.fit()

	e.OnTextChanged(func() {
		text := e.ToPlainText()
		if strings.ContainsAny(text, "\r\n") {
			// Something pasted can have more than one line. Setting the
			// text reports this change again, without them.
			e.SetText(text)
			e.MoveCursor(qtgui.QTextCursorEnd)
			return
		}
		if text == e.text {
			return
		}
		e.text = text
		if e.typing {
			e.pending = true
			return
		}
		e.pending = false
		if e.onChange != nil {
			e.onChange(text)
		}
	})

	return e
}

// ResizeEvent fits the height of the field to its text at its new width.
func (e *wrapEdit) ResizeEvent(event *qtgui.QResizeEvent) {
	e.Super().ResizeEvent(event)
	e.fit()
}

// KeyPressEvent commits what was typed on Return, which does not break the
// one line, and has what other keys change wait for commit.
func (e *wrapEdit) KeyPressEvent(event *qtgui.QKeyEvent) {
	switch event.Key() {
	case qtcore.Key_Return, qtcore.Key_Enter:
		// There is only the one line.
		e.commit()
		if e.onReturn != nil && !e.IsReadOnly() {
			e.onReturn()
		}
	default:
		e.typing = true
		e.Super().KeyPressEvent(event)
		e.typing = false
	}
}

// InsertFromMimeData gives what is pasted or dropped to onPaste, and puts
// it in the field unless onPaste takes it.
func (e *wrapEdit) InsertFromMimeData(source *qtcore.QMimeData) {
	if e.onPaste != nil && !e.IsReadOnly() && source.HasText() && e.onPaste(source.Text()) {
		return
	}
	e.Super().InsertFromMimeData(source)
}

// FocusOutEvent commits what was typed when the field loses the keyboard.
func (e *wrapEdit) FocusOutEvent(event *qtgui.QFocusEvent) {
	e.commit()
	e.Super().FocusOutEvent(event)
}

// commit reports what has been typed since it was last reported, if anything.
func (e *wrapEdit) commit() {
	if e.pending {
		e.pending = false
		if e.onChange != nil {
			e.onChange(e.text)
		}
	}
}

// fit makes the field as tall as its text.
func (e *wrapEdit) fit() {
	height := int(math.Ceil(e.Document().Size().Height)) + 2*e.FrameWidth()
	if height != e.Height() || e.MinimumHeight() != height {
		e.SetFixedHeight(height)
	}
}

// textPiece is a piece of text and the format it is shown in, which is the
// usual one if it is nil.
type textPiece struct {
	text   string
	format *qtgui.QTextCharFormat
}

// setPieces replaces the text of the field with pieces of text, each in a
// format of its own.
func (e *wrapEdit) setPieces(pieces []textPiece) {
	plain := qtgui.NewQTextCharFormat()
	defer plain.Delete()

	e.Clear()
	cursor := e.TextCursor()
	defer cursor.Delete()
	for _, piece := range pieces {
		format := piece.format
		if format == nil {
			format = plain
		}
		cursor.InsertTextWithTextCharFormat(strings.NewReplacer("\r", "", "\n", "").Replace(piece.text), format)
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

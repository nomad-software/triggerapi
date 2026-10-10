package ui

import (
	"runtime"
	"unicode/utf16"

	qt "github.com/mappu/miqt/qt6"

	"github.com/nomad-software/postie/internal/env"
)

// placeholderRange is the position of a {{key}} placeholder in a piece of
// text, in the UTF-16 code units Qt counts in.
type placeholderRange struct {
	start, length int
	// known is false if the active environment does not define it.
	known bool
}

// placeholderRanges finds the placeholders in text. expand substitutes the
// variables of the active environment, and is used to tell which are defined.
func placeholderRanges(text string, expand func(string) string) []placeholderRange {
	var ranges []placeholderRange

	units, last := 0, 0
	for _, loc := range env.FindPlaceholders(text) {
		units += utf16Len(text[last:loc[0]])
		match := text[loc[0]:loc[1]]
		length := utf16Len(match)

		ranges = append(ranges, placeholderRange{start: units, length: length, known: expand(match) != match})

		units += length
		last = loc[1]
	}

	return ranges
}

func utf16Len(s string) int {
	n := 0
	for _, r := range s {
		n += utf16.RuneLen(r)
	}
	return n
}

// placeholderHighlighter colours the placeholders in line edits and text
// documents: in the colour of a successful response if the active environment
// defines them, and in the warning colour if not.
type placeholderHighlighter struct {
	// expand substitutes the variables of the active environment.
	expand func(string) string

	known   *qt.QTextCharFormat
	unknown *qt.QTextCharFormat
	json    jsonFormats

	edits     []*qt.QLineEdit
	documents []*qt.QSyntaxHighlighter
}

func newPlaceholderHighlighter() *placeholderHighlighter {
	format := func(colour string) *qt.QTextCharFormat {
		f := qt.NewQTextCharFormat()
		f.SetForeground(qt.NewQBrush3(qt.NewQColor6(colour)))
		return f
	}

	return &placeholderHighlighter{
		expand:  func(s string) string { return s },
		known:   format(statusColour(200)),
		unknown: format(statusColour(400)),
		json:    newJSONFormats(),
	}
}

func (h *placeholderHighlighter) format(r placeholderRange) *qt.QTextCharFormat {
	if r.known {
		return h.known
	}
	return h.unknown
}

// document colours the placeholders in a text document. While isJSON reports
// true the text is coloured as JSON as well, beneath the placeholders. A
// document can only have one highlighter, so both are done here.
func (h *placeholderHighlighter) document(doc *qt.QTextDocument, isJSON func() bool) {
	highlighter := qt.NewQSyntaxHighlighter2(doc)
	highlighter.OnHighlightBlock(func(text string) {
		if isJSON() {
			h.json.apply(highlighter, text)
		}
		for _, r := range placeholderRanges(text, h.expand) {
			highlighter.SetFormat(r.start, r.length, h.format(r))
		}
	})
	h.documents = append(h.documents, highlighter)
}

// lineEdit colours the placeholders in a line edit.
func (h *placeholderHighlighter) lineEdit(edit *qt.QLineEdit) {
	edit.OnTextChanged(func(string) { h.apply(edit) })
	// The formats are positioned relative to the cursor.
	edit.OnCursorPositionChanged(func(int, int) { h.apply(edit) })
	h.edits = append(h.edits, edit)
}

// apply sets the colours in a line edit. QLineEdit has no API for formatting
// part of its text, but it does apply the formats carried by an input method
// event, so an event with no text is used to deliver them.
func (h *placeholderHighlighter) apply(edit *qt.QLineEdit) {
	cursor := edit.CursorPosition()

	var attributes []qt.QInputMethodEvent__Attribute
	for _, r := range placeholderRanges(edit.Text(), h.expand) {
		// The variant is freed by a finalizer miqt attaches to it, so it
		// must not be deleted here as well, only kept alive while in use.
		value := h.format(r).ToQVariant()
		attribute := qt.NewQInputMethodEvent__Attribute(qt.QInputMethodEvent__TextFormat, r.start-cursor, r.length, *value)
		runtime.KeepAlive(value)
		defer attribute.Delete()

		attributes = append(attributes, *attribute)
	}

	event := qt.NewQInputMethodEvent2("", attributes)
	defer event.Delete()

	qt.QCoreApplication_SendEvent(edit.QObject, event.QEvent)
}

// refresh recolours everything, for when the active environment or its
// variables change.
func (h *placeholderHighlighter) refresh() {
	for _, edit := range h.edits {
		h.apply(edit)
	}
	for _, doc := range h.documents {
		doc.Rehighlight()
	}
}

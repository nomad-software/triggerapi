package ui

import (
	"runtime"
	"unicode/utf16"

	qt "github.com/mappu/miqt/qt6"

	"github.com/nomad-software/triggerapi/internal/env"
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
	tables    []*qt.QTableWidget

	// knownColour and unknownColour are the colours of the two formats, for
	// text that is painted directly. The selected ones are paler, to be
	// read against the colour of a selected cell.
	knownColour     *qt.QColor
	unknownColour   *qt.QColor
	knownSelected   *qt.QColor
	unknownSelected *qt.QColor
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

		knownColour:     qt.NewQColor6(statusColour(200)),
		unknownColour:   qt.NewQColor6(statusColour(400)),
		knownSelected:   qt.NewQColor6("#b9f6ca"),
		unknownSelected: qt.NewQColor6("#ffd180"),
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
	for _, table := range h.tables {
		table.Viewport().Update()
	}
}

// table colours the placeholders in the cells of a table. A cell has no way
// of colouring part of its text, so the cells that have placeholders in them
// are drawn without their text, which is then painted over them a piece at a
// time. A cell is not coloured while it is being edited.
func (h *placeholderHighlighter) table(table *qt.QTableWidget) {
	delegate := qt.NewQStyledItemDelegate2(table.QObject)

	// hideText makes the next cell be drawn with no text.
	hideText := false
	delegate.OnInitStyleOption(func(super func(*qt.QStyleOptionViewItem, *qt.QModelIndex), option *qt.QStyleOptionViewItem, index *qt.QModelIndex) {
		super(option, index)
		if hideText {
			option.SetText("")
		}
	})

	delegate.OnPaint(func(super func(*qt.QPainter, *qt.QStyleOptionViewItem, *qt.QModelIndex), painter *qt.QPainter, option *qt.QStyleOptionViewItem, index *qt.QModelIndex) {
		text := index.Data().ToString()
		ranges := placeholderRanges(text, h.expand)
		if len(ranges) == 0 {
			super(painter, option, index)
			return
		}

		// The background, and the selection if there is one.
		hideText = true
		super(painter, option, index)
		hideText = false

		// The text starts where the style would have put it, and is in the
		// colour it would have had.
		rect := option.Rect()
		margin := table.Style().PixelMetric(qt.QStyle__PM_FocusFrameHMargin, nil, table.QWidget) + 1
		role := qt.QPalette__Text
		known, unknown := h.knownColour, h.unknownColour
		if option.State()&qt.QStyle__State_Selected != 0 {
			role = qt.QPalette__HighlightedText
			known, unknown = h.knownSelected, h.unknownSelected
		}
		plain := option.Palette().ColorWithCr(role)
		metrics := option.FontMetrics()

		painter.Save()
		defer painter.Restore()
		painter.SetClipRect2(rect.Left()+margin, rect.Top(), rect.Width()-2*margin, rect.Height())
		painter.SetFont(option.Font())

		// The ranges are counted in UTF-16 code units, as is the text here.
		units := utf16.Encode([]rune(text))
		x := rect.Left() + margin
		draw := func(from, to int, colour *qt.QColor) {
			if to <= from {
				return
			}
			piece := string(utf16.Decode(units[from:to]))
			painter.SetPen(colour)
			painter.DrawText7(x, rect.Top(), rect.Width(), rect.Height(), int(qt.AlignLeft|qt.AlignVCenter), piece)
			x += metrics.HorizontalAdvance(piece)
		}

		at := 0
		for _, r := range ranges {
			draw(at, r.start, plain)
			colour := unknown
			if r.known {
				colour = known
			}
			draw(r.start, r.start+r.length, colour)
			at = r.start + r.length
		}
		draw(at, len(units), plain)
	})

	table.SetItemDelegate(delegate.QAbstractItemDelegate)
	h.tables = append(h.tables, table)
}

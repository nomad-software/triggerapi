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
	// known is false if the active environment does not define it, or
	// defines it as nothing.
	known bool
}

// placeholderRanges finds the placeholders in text. expand substitutes the
// variables of the active environment, and is used to tell which are defined.
// One that is defined as nothing counts as not defined, as it leaves as much
// of a gap in what is sent.
func placeholderRanges(text string, expand func(string) string) []placeholderRange {
	var ranges []placeholderRange

	units, last := 0, 0
	for _, loc := range env.FindPlaceholders(text) {
		units += utf16Len(text[last:loc[0]])
		match := text[loc[0]:loc[1]]
		length := utf16Len(match)

		value := expand(match)
		ranges = append(ranges, placeholderRange{start: units, length: length, known: value != match && value != ""})

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
// documents: in one of the colours of the settings if the active environment
// defines them (colourFound), and in another if it does not, or defines them
// as nothing (colourNotFound).
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
	h := &placeholderHighlighter{
		expand:  func(s string) string { return s },
		known:   qt.NewQTextCharFormat(),
		unknown: qt.NewQTextCharFormat(),
		json:    newJSONFormats(),
	}
	h.setColours()
	return h
}

// setColours takes the colours of placeholders from the settings. Text that
// has already been coloured keeps its colours until refresh is called.
func (h *placeholderHighlighter) setColours() {
	// pale returns a colour mixed with white.
	pale := func(colour *qt.QColor) *qt.QColor {
		mix := func(c int) int { return c + (255-c)*3/5 }
		return qt.NewQColor3(mix(colour.Red()), mix(colour.Green()), mix(colour.Blue()))
	}

	// The colours there were are freed, as they are replaced.
	for _, colour := range []*qt.QColor{h.knownColour, h.unknownColour, h.knownSelected, h.unknownSelected} {
		if colour != nil {
			colour.Delete()
		}
	}
	h.knownColour = qt.NewQColor6(themeColour(colourFound))
	h.unknownColour = qt.NewQColor6(themeColour(colourNotFound))
	h.knownSelected = pale(h.knownColour)
	h.unknownSelected = pale(h.unknownColour)

	// The formats keep copies of the brushes.
	known, unknown := qt.NewQBrush3(h.knownColour), qt.NewQBrush3(h.unknownColour)
	defer known.Delete()
	defer unknown.Delete()
	h.known.SetForeground(known)
	h.unknown.SetForeground(unknown)
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
	h.code(doc, func(text string, _ *qt.QSyntaxHighlighter) []jsonToken {
		if isJSON() {
			return jsonTokens(text)
		}
		return nil
	})
}

// graphql colours a text document as GraphQL, in the colours of JSON, and
// its placeholders. The state of a line is whether it ends inside a block
// string, which the next carries on with.
func (h *placeholderHighlighter) graphql(doc *qt.QTextDocument) {
	h.code(doc, func(text string, highlighter *qt.QSyntaxHighlighter) []jsonToken {
		tokens, inBlock := graphqlTokens(text, highlighter.PreviousBlockState() == 1)
		if inBlock {
			highlighter.SetCurrentBlockState(1)
		} else {
			highlighter.SetCurrentBlockState(0)
		}
		return tokens
	})
}

// code colours the tokens tokenize finds in each line of a text document in
// the colours of JSON, and the placeholders over them.
func (h *placeholderHighlighter) code(doc *qt.QTextDocument, tokenize func(string, *qt.QSyntaxHighlighter) []jsonToken) {
	highlighter := qt.NewQSyntaxHighlighter2(doc)
	highlighter.OnHighlightBlock(func(text string) {
		h.json.apply(highlighter, tokenize(text, highlighter))
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

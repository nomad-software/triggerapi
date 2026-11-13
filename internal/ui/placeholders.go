package ui

import (
	"github.com/nomad-software/qute/qtcore"
	"github.com/nomad-software/qute/qtgui"
	"github.com/nomad-software/qute/qtwidgets"

	"unicode/utf16"

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

	known   *qtgui.QTextCharFormat
	unknown *qtgui.QTextCharFormat
	json    jsonFormats

	edits     []*qtwidgets.QLineEdit
	documents []*highlighter
	tables    []*qtwidgets.QTableWidget

	// knownColour and unknownColour are the colours of the two formats, for
	// text that is painted directly. The selected ones are paler, to be
	// read against the colour of a selected cell.
	knownColour     qtgui.QColor
	unknownColour   qtgui.QColor
	knownSelected   qtgui.QColor
	unknownSelected qtgui.QColor
}

func newPlaceholderHighlighter() *placeholderHighlighter {
	h := &placeholderHighlighter{
		expand:  func(s string) string { return s },
		known:   qtgui.NewQTextCharFormat(),
		unknown: qtgui.NewQTextCharFormat(),
		json:    newJSONFormats(),
	}
	h.setColours()
	return h
}

// setColours takes the colours of placeholders from the settings. Text that
// has already been coloured keeps its colours until refresh is called.
func (h *placeholderHighlighter) setColours() {
	// pale returns a colour mixed with white.
	pale := func(colour qtgui.QColor) qtgui.QColor {
		mix := func(c int) int { return c + (255-c)*3/5 }
		return qtgui.RGB(uint8(mix(int(colour.R))), uint8(mix(int(colour.G))), uint8(mix(int(colour.B))))
	}

	h.knownColour = qcolour(themeColour(colourFound))
	h.unknownColour = qcolour(themeColour(colourNotFound))
	h.knownSelected = pale(h.knownColour)
	h.unknownSelected = pale(h.unknownColour)

	// The formats keep copies of the brushes.
	known, unknown := qtgui.NewQBrushWithColor(h.knownColour), qtgui.NewQBrushWithColor(h.unknownColour)
	defer known.Delete()
	defer unknown.Delete()
	h.known.SetForeground(known)
	h.unknown.SetForeground(unknown)
}

func (h *placeholderHighlighter) format(r placeholderRange) *qtgui.QTextCharFormat {
	if r.known {
		return h.known
	}
	return h.unknown
}

// document colours the placeholders in a text document. While isJSON reports
// true the text is coloured as JSON as well, beneath the placeholders. A
// document can only have one highlighter, so both are done here.
func (h *placeholderHighlighter) document(doc *qtgui.QTextDocument, isJSON func() bool) {
	h.code(doc, func(text string, _ *highlighter) []jsonToken {
		if isJSON() {
			return jsonTokens(text)
		}
		return nil
	})
}

// graphql colours a text document as GraphQL, in the colours of JSON, and
// its placeholders. The state of a line is whether it ends inside a block
// string, which the next carries on with.
func (h *placeholderHighlighter) graphql(doc *qtgui.QTextDocument) {
	h.code(doc, func(text string, highlighter *highlighter) []jsonToken {
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
func (h *placeholderHighlighter) code(doc *qtgui.QTextDocument, tokenize func(string, *highlighter) []jsonToken) {
	lines := newHighlighter(doc)
	lines.block = func(text string) {
		h.json.apply(lines, tokenize(text, lines))
		for _, r := range placeholderRanges(text, h.expand) {
			lines.SetFormat(r.start, r.length, h.format(r))
		}
	}
	h.documents = append(h.documents, lines)
}

// lineEdit colours the placeholders in a line edit.
func (h *placeholderHighlighter) lineEdit(edit *qtwidgets.QLineEdit) {
	edit.OnTextChanged(func(string) { h.apply(edit) })
	// The formats are positioned relative to the cursor.
	edit.OnCursorPositionChanged(func(int, int) { h.apply(edit) })
	h.edits = append(h.edits, edit)
}

// apply sets the colours in a line edit. QLineEdit has no API for formatting
// part of its text, but it does apply the formats carried by an input method
// event, so an event with no text is used to deliver them.
func (h *placeholderHighlighter) apply(edit *qtwidgets.QLineEdit) {
	cursor := edit.CursorPosition()

	var attributes []*qtgui.QInputMethodEventAttribute
	for _, r := range placeholderRanges(edit.Text(), h.expand) {
		attributes = append(attributes, qtgui.NewQInputMethodEventAttributeWithValue(
			qtgui.QInputMethodEventTextFormat, r.start-cursor, r.length, h.format(r).ToQVariant()))
	}

	event := qtgui.NewQInputMethodEventWithPreeditText("", attributes)
	defer event.Delete()

	qtcore.QCoreApplicationSendEvent(edit.AsQObject(), event.AsQEvent())
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
func (h *placeholderHighlighter) table(table *qtwidgets.QTableWidget) {
	delegate := &placeholderDelegate{highlighter: h, table: table}
	qtwidgets.NewQStyledItemDelegateWith(delegate, table)
	h.tables = append(h.tables, table)
	table.SetItemDelegate(delegate)
}

// placeholderDelegate draws the cells of a table with their placeholders
// coloured. See placeholderHighlighter.table.
type placeholderDelegate struct {
	qtwidgets.QStyledItemDelegateBase
	highlighter *placeholderHighlighter
	table       *qtwidgets.QTableWidget
	// hideText makes the next cell be drawn with no text.
	hideText bool
}

// InitStyleOption leaves the text out of a cell the text of which is
// painted over it.
func (d *placeholderDelegate) InitStyleOption(option *qtwidgets.QStyleOptionViewItem, index *qtcore.QModelIndex) {
	d.Super().InitStyleOption(option, index)
	if d.hideText {
		option.SetText("")
	}
}

// Paint draws a cell, painting the text of one with placeholders in pieces.
func (d *placeholderDelegate) Paint(painter *qtgui.QPainter, option *qtwidgets.QStyleOptionViewItem, index *qtcore.QModelIndex) {
	h, table := d.highlighter, d.table
	text := index.Data().ToString()
	ranges := placeholderRanges(text, h.expand)
	if len(ranges) == 0 {
		d.Super().Paint(painter, option, index)
		return
	}

	// The background, and the selection if there is one.
	d.hideText = true
	d.Super().Paint(painter, option, index)
	d.hideText = false

	// The text starts where the style would have put it, and is in the
	// colour it would have had.
	rect := option.Rect()
	margin := table.Style().PixelMetricFull(qtwidgets.QStylePM_FocusFrameHMargin, nil, table.AsQWidget()) + 1
	role := qtgui.QPaletteText
	known, unknown := h.knownColour, h.unknownColour
	if option.State()&qtwidgets.QStyleState_Selected != 0 {
		role = qtgui.QPaletteHighlightedText
		known, unknown = h.knownSelected, h.unknownSelected
	}
	plain := option.Palette().Color(role)
	metrics := option.FontMetrics()

	painter.Save()
	defer painter.Restore()
	painter.SetClipRect(qtcore.QRect{X: rect.Left() + margin, Y: rect.Top(), Width: rect.Width - 2*margin, Height: rect.Height})
	painter.SetFont(option.Font())

	// The text is cut into pieces at the placeholders, each in its colour.
	// The ranges are counted in UTF-16 code units, as is the text here.
	type piece struct {
		from, to int
		colour   qtgui.QColor
	}
	var pieces []piece
	at := 0
	for _, r := range ranges {
		colour := unknown
		if r.known {
			colour = known
		}
		pieces = append(pieces, piece{at, r.start, plain}, piece{r.start, r.start + r.length, colour})
		at = r.start + r.length
	}
	units := utf16.Encode([]rune(text))
	pieces = append(pieces, piece{at, len(units), plain})

	x := rect.Left() + margin
	for _, p := range pieces {
		if p.to <= p.from {
			continue
		}
		text := string(utf16.Decode(units[p.from:p.to]))
		painter.SetPenColor(p.colour)
		painter.DrawTextIn(qtcore.QRect{X: x, Y: rect.Top(), Width: rect.Width, Height: rect.Height}, int(qtcore.AlignLeft|qtcore.AlignVCenter), text)
		x += metrics.HorizontalAdvance(text)
	}
}

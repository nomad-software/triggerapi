package ui

import (
	"github.com/nomad-software/qute/qtcore"
	"github.com/nomad-software/qute/qtgui"
	"github.com/nomad-software/qute/qtwidgets"

	"fmt"
	"runtime"
	"slices"
	"sort"
	"strings"
	"unicode"
	"unicode/utf16"
)

// span is a run of text: where it starts and how long it is, in the UTF-16
// code units that Qt counts positions in.
type span struct {
	start, length int
}

// findAll returns each place the query is in the text, from the start. The
// places do not overlap. Without matchCase, capitals and small letters are
// taken to be the same.
func findAll(text, query string, matchCase bool) []span {
	if query == "" {
		return nil
	}

	hay, needle := []rune(text), []rune(query)
	if !matchCase {
		for i, r := range hay {
			hay[i] = unicode.ToLower(r)
		}
		for i, r := range needle {
			needle[i] = unicode.ToLower(r)
		}
	}

	width := 0
	for _, r := range needle {
		width += utf16.RuneLen(r)
	}

	var found []span
	// position is that of hay[i] in UTF-16 code units.
	position := 0
	for i := 0; i+len(needle) <= len(hay); {
		if slices.Equal(hay[i:i+len(needle)], needle) {
			found = append(found, span{position, width})
			position += width
			i += len(needle)
		} else {
			position += utf16.RuneLen(hay[i])
			i++
		}
	}
	return found
}

// findBar is the bar above the body of a response in which text to find in
// it is typed. Every match is marked in the body by its highlighter, and the
// one that has been gone to is the selection.
type findBar struct {
	root *qtwidgets.QWidget

	// button shows and hides the bar. It is beside the tabs of the
	// response panel, and is down while the bar is on show.
	button *qtwidgets.QToolButton

	field *qtwidgets.QLineEdit
	// count says which of the matches is selected, and how many there are.
	count    *qtwidgets.QLabel
	previous *qtwidgets.QToolButton
	next     *qtwidgets.QToolButton
	// matchCase has capitals and small letters told apart.
	matchCase *qtwidgets.QToolButton
	hide      *qtwidgets.QToolButton

	edit        *qtwidgets.QPlainTextEdit
	highlighter *highlighter
	fold        *folder

	// query is what is being looked for, which is nothing while the bar is
	// hidden, and exact if capitals matter. They are kept for the
	// highlighter, which marks the matches a line at a time.
	query string
	exact bool
	// format is how a match is marked.
	format *qtgui.QTextCharFormat
	// matches are the places the query is in the body, and current the one
	// that is selected, or -1.
	matches []span
	current int

	// onShow is called when the bar is shown.
	onShow func()
}

// newFindBar returns the bar which finds text in an editor. The highlighter
// of the editor must call highlight for each line.
func newFindBar(edit *qtwidgets.QPlainTextEdit, highlighter *highlighter, fold *folder) *findBar {
	f := &findBar{
		root:        qtwidgets.NewQWidget(nil),
		field:       qtwidgets.NewQLineEdit(nil),
		count:       qtwidgets.NewQLabel(nil),
		previous:    qtwidgets.NewQToolButton(nil),
		next:        qtwidgets.NewQToolButton(nil),
		matchCase:   qtwidgets.NewQToolButton(nil),
		hide:        qtwidgets.NewQToolButton(nil),
		edit:        edit,
		highlighter: highlighter,
		fold:        fold,
		format:      qtgui.NewQTextCharFormat(),
		current:     -1,
	}

	f.button = newIconButton(colourAction, drawMagnifier, "Find in the response (Ctrl+F)")
	f.button.SetCheckable(true)
	f.button.OnToggled(f.toggled)

	f.field.SetPlaceholderText("Find in the response")
	f.field.SetClearButtonEnabled(true)
	f.field.OnTextChanged(func(string) { f.search() })
	f.field.OverrideKeyPressEvent(func(super func(*qtgui.QKeyEvent), event *qtgui.QKeyEvent) {
		switch event.Key() {
		case qtcore.Key_Return, qtcore.Key_Enter:
			if event.Modifiers()&qtcore.ShiftModifier != 0 {
				f.step(-1)
			} else {
				f.step(1)
			}
		case qtcore.Key_Escape:
			f.close()
		default:
			super(event)
		}
	})

	f.previous.SetArrowType(qtcore.UpArrow)
	f.previous.SetToolTip("Previous match (Shift+Enter)")
	f.previous.OnClicked(func(_ bool) { f.step(-1) })
	f.next.SetArrowType(qtcore.DownArrow)
	f.next.SetToolTip("Next match (Enter)")
	f.next.OnClicked(func(_ bool) { f.step(1) })

	f.matchCase.SetText("Aa")
	f.matchCase.SetToolTip("Match case")
	f.matchCase.SetCheckable(true)
	f.matchCase.OnToggled(func(bool) { f.search() })

	f.hide.SetText("✕")
	f.hide.SetToolTip("Close (Escape)")
	f.hide.OnClicked(func(_ bool) { f.close() })

	for _, button := range []*qtwidgets.QToolButton{f.previous, f.next, f.matchCase, f.hide} {
		button.SetAutoRaise(true)
	}

	layout := qtwidgets.NewQHBoxLayout(f.root)
	layout.SetContentsMargins(qtcore.QMargins{Left: 0, Top: 0, Right: 0, Bottom: 0})
	layout.SetSpacing(2)
	layout.AddWidgetFull(f.field.AsQWidget(), 1, 0)
	layout.AddSpacing(6)
	layout.AddWidget(f.count.AsQWidget())
	layout.AddSpacing(6)
	layout.AddWidget(f.previous.AsQWidget())
	layout.AddWidget(f.next.AsQWidget())
	layout.AddWidget(f.matchCase.AsQWidget())
	layout.AddWidget(f.hide.AsQWidget())
	f.root.SetVisible(false)

	f.recolour()
	return f
}

// isOpen reports if the bar is on show.
func (f *findBar) isOpen() bool {
	return f.button.IsChecked()
}

// open shows the bar, if it is not on show already, with the keyboard in it
// and what was last looked for selected, to be typed over.
func (f *findBar) open() {
	f.button.SetChecked(true)
	f.field.SetFocus()
	f.field.SelectAll()
}

// close hides the bar and reports if it was on show.
func (f *findBar) close() bool {
	if !f.isOpen() {
		return false
	}
	f.button.SetChecked(false)
	return true
}

// toggled shows or hides the bar, as its button goes down or comes up.
func (f *findBar) toggled(open bool) {
	f.root.SetVisible(open)
	if !open {
		// The matches are no longer marked, though the one that was
		// selected still is.
		f.search()
		f.edit.SetFocus()
		return
	}

	if f.onShow != nil {
		f.onShow()
	}
	// Text selected in the response is what to look for.
	cursor := f.edit.TextCursor()
	selected := cursor.SelectedText()
	cursor.Delete()
	// A selection of more than one line has U+2029 between its lines.
	if selected != "" && !strings.ContainsAny(selected, "\n\u2029") {
		f.field.SetText(selected)
	}
	f.search()
	f.field.SetFocus()
	f.field.SelectAll()
}

// search looks in the body for what is typed in the bar, and selects the
// first match at or after the one that was selected, so that typing more of
// a word stays where it is. It is called when what to look for changes, and
// when the body does.
func (f *findBar) search() {
	query := ""
	if f.isOpen() {
		query = f.field.Text()
	}
	exact := f.matchCase.IsChecked()
	changed := query != f.query || exact != f.exact
	f.query, f.exact = query, exact

	f.matches, f.current = nil, -1
	if query != "" {
		f.matches = findAll(f.edit.ToPlainText(), query, exact)
	}
	// A new body has been marked already, as it was highlighted.
	if changed {
		f.highlighter.Rehighlight()
	}

	cursor := f.edit.TextCursor()
	defer cursor.Delete()
	if len(f.matches) > 0 {
		from := cursor.SelectionStart()
		first := sort.Search(len(f.matches), func(i int) bool { return f.matches[i].start >= from })
		f.goTo(first % len(f.matches))
	} else if f.isOpen() {
		cursor.ClearSelection()
		f.edit.SetTextCursor(cursor)
	}
	f.report()
}

// step goes to the match that many after the selected one, going round from
// the last to the first.
func (f *findBar) step(by int) {
	if count := len(f.matches); count > 0 {
		f.goTo((f.current + by + count) % count)
		f.report()
	}
}

// goTo selects a match and brings it into view, unfolding what it is
// folded away in.
func (f *findBar) goTo(index int) {
	f.current = index
	match := f.matches[index]

	f.fold.reveal(f.edit.Document().FindBlock(match.start).BlockNumber())

	cursor := f.edit.TextCursor()
	defer cursor.Delete()
	cursor.SetPosition(match.start)
	cursor.SetPositionFull(match.start+match.length, qtgui.QTextCursorKeepAnchor)
	f.edit.SetTextCursor(cursor)
}

// report shows which match is selected and how many there are. What was
// typed is in the colour of errors if it is nowhere in the body.
func (f *findBar) report() {
	style := ""
	switch {
	case f.query == "":
		f.count.SetText("")
	case len(f.matches) == 0:
		f.count.SetText("No matches")
		style = "color: " + themeColour(colourError)
	default:
		f.count.SetText(fmt.Sprintf("%d of %d", f.current+1, len(f.matches)))
	}
	f.field.SetStyleSheet(style)

	f.previous.SetEnabled(len(f.matches) > 0)
	f.next.SetEnabled(len(f.matches) > 0)
}

// highlight marks the matches in a line of the body that is being
// highlighted.
func (f *findBar) highlight(line string) {
	for _, match := range findAll(line, f.query, f.exact) {
		f.highlighter.SetFormat(match.start, match.length, f.format)
	}
}

// recolour marks the matches in the colour of the settings, with their text
// in black or white, whichever can be read against it.
func (f *findBar) recolour() {
	// The selected match is the selection of the editor, which is to look
	// the same while the keyboard is in the bar as when it is in the
	// editor, in the palette of the theme in effect. The colours are those
	// of the application, which a change of theme changes at once, and
	// those of widgets only a turn of the event loop later; the palette
	// sets only these, so the editor has the others of the theme.
	theme := qtgui.QGuiApplicationPalette()
	palette := qtgui.NewQPalette()
	defer palette.Delete()
	for _, role := range []qtgui.QPaletteColorRole{qtgui.QPaletteHighlight, qtgui.QPaletteHighlightedText} {
		palette.SetGroupColor(qtgui.QPaletteInactive, role, theme.GroupColor(qtgui.QPaletteActive, role))
	}
	runtime.KeepAlive(theme)
	f.edit.SetPalette(palette)

	colour := qcolour(themeColour(colourMatch))
	text := qcolour(readableOn(colour))

	// The format keeps copies of the brushes.
	background, foreground := qtgui.NewQBrushWithColor(colour), qtgui.NewQBrushWithColor(text)
	defer background.Delete()
	defer foreground.Delete()
	f.format.SetBackground(background)
	f.format.SetForeground(foreground)

	if f.query != "" {
		f.highlighter.Rehighlight()
	}
	f.report()
}

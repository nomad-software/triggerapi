package ui

import (
	"github.com/nomad-software/qute/qtcore"
	"github.com/nomad-software/qute/qtgui"
	"github.com/nomad-software/qute/qtwidgets"

	"fmt"
)

// primaryStyle returns the style sheet which makes a button stand out as the
// main action of its page, in the colour of the settings (colourSend). A
// style sheet replaces the native look of the button entirely, so the
// padding and each of its states are given here: lighter with the pointer
// over it and darker while pressed. Its text is black or white, whichever
// can be read against the colour.
func primaryStyle() string {
	colour := qcolour(themeColour(colourSend))
	text := readableOn(colour)

	return fmt.Sprintf(`
QPushButton {
	background-color: %s;
	color: %s;
	font-weight: bold;
	border: none;
	border-radius: 4px;
	padding: 5px 18px;
	min-width: 56px;
}
QPushButton:hover { background-color: %s; }
QPushButton:pressed { background-color: %s; }
QPushButton:focus { border: 1px solid %s; }
QPushButton:disabled { background-color: #8a9aab; }
`, colour.Name(), text, colour.Lighter(112).Name(), colour.Darker(112).Name(), colour.Lighter(160).Name())
}

// readableOn returns the colour of text that can be read against a colour:
// black on a light one, and white on a dark one.
func readableOn(colour qtgui.QColor) string {
	if colour.Lightness() > 140 {
		return "#000000"
	}
	return "#ffffff"
}

// makePrimary styles a button as the main action of its page. It is called
// again when the colour changes.
func makePrimary(button *qtwidgets.QPushButton) {
	button.SetStyleSheet(primaryStyle())
}

// shadeHeadings has the rows of a tree which are headings, as headingAt
// reports of an index, drawn on a background of the colour of text at 3%
// opacity, the whole width of the tree.
func shadeHeadings(tree *qtwidgets.QTreeWidget, headingAt func(index *qtcore.QModelIndex) bool) {
	tree.OverrideDrawRow(func(super func(*qtgui.QPainter, *qtwidgets.QStyleOptionViewItem, *qtcore.QModelIndex), painter *qtgui.QPainter, options *qtwidgets.QStyleOptionViewItem, index *qtcore.QModelIndex) {
		if headingAt(index) {
			colour := tree.Palette().Color(qtgui.QPaletteText).WithAlphaF(0.03)
			rect := tree.VisualRect(index)
			row := qtcore.QRect{X: 0, Y: rect.Top(), Width: tree.Viewport().Width(), Height: rect.Height}
			painter.FillRect(row, colour)
		}
		super(painter, options, index)
	})
}

// fadedTextColour returns the colour of text that is to stand out less than
// the text beside it: the colour of text mixed half and half with the colour
// of the window behind it, as if it were half transparent.
func fadedTextColour() string {
	palette := qtgui.QGuiApplicationPalette()
	window := palette.Color(qtgui.QPaletteWindow)
	text := palette.Color(qtgui.QPaletteWindowText)

	mix := func(a, b int) int { return (a + b) / 2 }
	return fmt.Sprintf("#%02x%02x%02x", mix(int(text.R), int(window.R)), mix(int(text.G), int(window.G)), mix(int(text.B), int(window.B)))
}

// plainPage gives the page of a tab widget the background of the window.
// Some styles, such as the one Qt falls back on where it has none of the
// desktop's own, draw the pages of a tab widget lighter than the window,
// which in a dark theme leaves dimmed text hard to read and an empty page a
// slab of grey. The page is returned.
func plainPage(page *qtwidgets.QWidget) *qtwidgets.QWidget {
	page.SetBackgroundRole(qtgui.QPaletteWindow)
	page.SetAutoFillBackground(true)
	return page
}

// newPanel returns a sunken frame with the background of a text field, as
// an editor or a list has, with a widget filling it. It is what stands in
// for an editor where there is nothing to edit.
func newPanel(content *qtwidgets.QWidget) *qtwidgets.QWidget {
	panel := qtwidgets.NewQFrame(nil)
	panel.SetFrameStyle(int(qtwidgets.QFrameStyledPanel) | int(qtwidgets.QFrameSunken))
	panel.SetBackgroundRole(qtgui.QPaletteBase)
	panel.SetAutoFillBackground(true)

	layout := qtwidgets.NewQVBoxLayout(panel.AsQWidget())
	layout.SetContentsMargins(qtcore.QMargins{Left: 0, Top: 0, Right: 0, Bottom: 0})
	layout.AddWidget(content)
	return panel.AsQWidget()
}

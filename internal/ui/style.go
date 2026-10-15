package ui

import (
	"fmt"

	qt "github.com/mappu/miqt/qt6"
)

// primaryColour is the colour, until another is chosen in the settings, of
// the buttons that are the main action of their page, such as Send.
const primaryColour = "#e8590c"

// primaryStyle returns the style sheet which makes a button stand out as the
// main action of its page, in the colour of the settings (colourSend). A
// style sheet replaces the native look of the button entirely, so the
// padding and each of its states are given here: lighter with the pointer
// over it and darker while pressed. Its text is black or white, whichever
// can be read against the colour.
func primaryStyle() string {
	colour := qt.NewQColor6(themeColour(colourSend))
	defer colour.Delete()

	text := "#ffffff"
	if colour.Lightness() > 140 {
		text = "#000000"
	}

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
`, colour.Name(), text, colour.LighterWithInt(112).Name(), colour.DarkerWithInt(112).Name(), colour.LighterWithInt(160).Name())
}

// makePrimary styles a button as the main action of its page. It is called
// again when the colour changes.
func makePrimary(button *qt.QPushButton) {
	button.SetStyleSheet(primaryStyle())
}

// fadedTextColour returns the colour of text that is to stand out less than
// the text beside it: the colour of text mixed half and half with the colour
// of the window behind it, as if it were half transparent.
func fadedTextColour() string {
	palette := qt.QGuiApplication_Palette()
	window := palette.ColorWithCr(qt.QPalette__Window)
	text := palette.ColorWithCr(qt.QPalette__WindowText)

	mix := func(a, b int) int { return (a + b) / 2 }
	return fmt.Sprintf("#%02x%02x%02x", mix(text.Red(), window.Red()), mix(text.Green(), window.Green()), mix(text.Blue(), window.Blue()))
}

// plainPage gives the page of a tab widget the background of the window.
// Some styles, such as the one Qt falls back on where it has none of the
// desktop's own, draw the pages of a tab widget lighter than the window,
// which in a dark theme leaves dimmed text hard to read and an empty page a
// slab of grey. The page is returned.
func plainPage(page *qt.QWidget) *qt.QWidget {
	page.SetBackgroundRole(qt.QPalette__Window)
	page.SetAutoFillBackground(true)
	return page
}

// newPanel returns a sunken frame with the background of a text field, as
// an editor or a list has, with a widget filling it. It is what stands in
// for an editor where there is nothing to edit.
func newPanel(content *qt.QWidget) *qt.QWidget {
	panel := qt.NewQFrame2()
	panel.SetFrameStyle(int(qt.QFrame__StyledPanel) | int(qt.QFrame__Sunken))
	panel.SetBackgroundRole(qt.QPalette__Base)
	panel.SetAutoFillBackground(true)

	layout := qt.NewQVBoxLayout(panel.QWidget)
	layout.SetContentsMargins(0, 0, 0, 0)
	layout.AddWidget(content)
	return panel.QWidget
}

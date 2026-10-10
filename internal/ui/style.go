package ui

import (
	"fmt"

	qt "github.com/mappu/miqt/qt6"
)

// primaryStyle makes a button stand out as the main action of its page. A
// style sheet replaces the native look of the button entirely, so the
// padding and each of its states are given here.
const primaryStyle = `
QPushButton {
	background-color: #e8590c;
	color: #ffffff;
	font-weight: bold;
	border: none;
	border-radius: 4px;
	padding: 5px 18px;
	min-width: 56px;
}
QPushButton:hover { background-color: #f76707; }
QPushButton:pressed { background-color: #d9480f; }
QPushButton:focus { border: 1px solid #ffc078; }
QPushButton:disabled { background-color: #8a9aab; }
`

// makePrimary styles a button as the main action of its page.
func makePrimary(button *qt.QPushButton) {
	button.SetStyleSheet(primaryStyle)
}

// borderColour returns a colour for a border drawn around part of a window:
// that of the window with some of the colour of its text mixed in, so that
// it shows on a light theme and on a dark one.
func borderColour() string {
	palette := qt.QGuiApplication_Palette()
	window := palette.ColorWithCr(qt.QPalette__Window)
	text := palette.ColorWithCr(qt.QPalette__WindowText)

	mix := func(a, b int) int { return (a*65 + b*35) / 100 }
	return fmt.Sprintf("#%02x%02x%02x", mix(window.Red(), text.Red()), mix(window.Green(), text.Green()), mix(window.Blue(), text.Blue()))
}

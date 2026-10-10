package ui

import (
	qt "github.com/mappu/miqt/qt6"
)

// primaryColour is the colour of the buttons that are the main action of
// their page, such as Send.
const primaryColour = "#e8590c"

// primaryStyle makes a button stand out as the main action of its page. A
// style sheet replaces the native look of the button entirely, so the
// padding and each of its states are given here.
const primaryStyle = `
QPushButton {
	background-color: ` + primaryColour + `;
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

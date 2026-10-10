package ui

import (
	qt "github.com/mappu/miqt/qt6"
)

// primaryStyle makes a button stand out as the main action of its page. A
// style sheet replaces the native look of the button entirely, so the
// padding and each of its states are given here.
const primaryStyle = `
QPushButton {
	background-color: #2f7fd1;
	color: #ffffff;
	font-weight: bold;
	border: none;
	border-radius: 4px;
	padding: 5px 18px;
	min-width: 56px;
}
QPushButton:hover { background-color: #428fdd; }
QPushButton:pressed { background-color: #2569b0; }
QPushButton:focus { border: 1px solid #9cc7f2; }
QPushButton:disabled { background-color: #8a9aab; }
`

// makePrimary styles a button as the main action of its page.
func makePrimary(button *qt.QPushButton) {
	button.SetStyleSheet(primaryStyle)
}

package ui

import (
	qt "github.com/mappu/miqt/qt6"
)

// choice is one of the things a choiceDialog offers.
type choice[K comparable] struct {
	key   K
	label string
	icon  *qt.QIcon
}

// choiceMinWidth is the narrowest a choiceDialog is, so that one offering
// one or two things is not squeezed around them.
const choiceMinWidth = 460

// choiceDialog asks the user to pick one of a few things, offering each as
// an icon with its name beneath.
type choiceDialog[K comparable] struct {
	*qt.QDialog

	buttons map[K]*qt.QToolButton
	// chosen is the key of what was picked, if the dialog was accepted.
	chosen K
}

func newChoiceDialog[K comparable](parent *qt.QWidget, title, prompt string, choices []choice[K]) *choiceDialog[K] {
	d := &choiceDialog[K]{
		QDialog: qt.NewQDialog(parent),
		buttons: map[K]*qt.QToolButton{},
	}
	d.SetWindowTitle(title)

	// The choices are in the middle of the row, which a dialog of only a
	// few is wider than.
	row := qt.NewQHBoxLayout2()
	row.AddStretch()
	for _, c := range choices {
		button := qt.NewQToolButton2()
		button.SetToolButtonStyle(qt.ToolButtonTextUnderIcon)
		button.SetAutoRaise(true)
		button.SetIcon(c.icon)
		button.SetIconSize(qt.NewQSize2(iconSize, iconSize))
		button.SetText(c.label)
		button.SetMinimumSize2(iconSize+48, iconSize+56)
		button.OnClicked(func() {
			d.chosen = c.key
			d.Accept()
		})

		d.buttons[c.key] = button
		row.AddWidget(button.QWidget)
	}
	row.AddStretch()

	cancel := qt.NewQPushButton3("Cancel")
	cancel.OnClicked(d.Reject)

	bottom := qt.NewQHBoxLayout2()
	bottom.AddStretch()
	bottom.AddWidget(cancel.QWidget)
	bottom.AddStretch()

	layout := qt.NewQVBoxLayout(d.QWidget)
	// Leave room around the edge of the window and between its parts, and
	// make a dialog of few choices as wide as one of the request types.
	layout.SetContentsMargins(24, 24, 24, 24)
	d.SetMinimumWidth(choiceMinWidth)
	layout.SetSpacing(18)
	label := qt.NewQLabel3(prompt)
	label.SetAlignment(qt.AlignCenter)
	layout.AddWidget(label.QWidget)
	layout.AddLayout(row.QLayout)
	layout.AddLayout(bottom.QLayout)

	return d
}

// ask shows the dialog and waits for the user. It returns what was picked,
// and false if the dialog was cancelled.
func (d *choiceDialog[K]) ask() (K, bool) {
	// A dialog is not destroyed by being closed.
	defer d.DeleteLater()

	accepted := d.Exec() == int(qt.QDialog__Accepted)
	return d.chosen, accepted
}

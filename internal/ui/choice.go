package ui

import (
	"github.com/nomad-software/qute/qtcore"
	"github.com/nomad-software/qute/qtgui"
	"github.com/nomad-software/qute/qtwidgets"
)

// choice is one of the things a choiceDialog offers.
type choice[K comparable] struct {
	key   K
	label string
	icon  *qtgui.QIcon
}

// choiceMinWidth is the narrowest a choiceDialog is, so that one offering
// one or two things is not squeezed around them.
const choiceMinWidth = 460

// choiceDialog asks the user to pick one of a few things, offering each as
// an icon with its name beneath.
type choiceDialog[K comparable] struct {
	*qtwidgets.QDialog

	buttons map[K]*qtwidgets.QToolButton
	// chosen is the key of what was picked, if the dialog was accepted.
	chosen K
}

func newChoiceDialog[K comparable](parent *qtwidgets.QWidget, title, prompt string, choices []choice[K]) *choiceDialog[K] {
	d := &choiceDialog[K]{
		QDialog: qtwidgets.NewQDialog(parent),
		buttons: map[K]*qtwidgets.QToolButton{},
	}
	d.SetWindowTitle(title)

	// The choices are in the middle of the row, which a dialog of only a
	// few is wider than.
	row := qtwidgets.NewQHBoxLayout(nil)
	row.AddStretch()
	for _, c := range choices {
		button := qtwidgets.NewQToolButton(nil)
		button.SetToolButtonStyle(qtcore.ToolButtonTextUnderIcon)
		button.SetAutoRaise(true)
		button.SetIcon(c.icon)
		button.SetIconSize(qtcore.QSize{Width: iconSize, Height: iconSize})
		button.SetText(c.label)
		button.SetMinimumSize(iconSize+48, iconSize+56)
		button.OnClicked(func(_ bool) {
			d.chosen = c.key
			d.Accept()
		})

		d.buttons[c.key] = button
		row.AddWidget(button.AsQWidget())
	}
	row.AddStretch()

	cancel := qtwidgets.NewQPushButtonWithText("Cancel", nil)
	cancel.OnClicked(func(_ bool) { d.Reject() })

	bottom := qtwidgets.NewQHBoxLayout(nil)
	bottom.AddStretch()
	bottom.AddWidget(cancel.AsQWidget())
	bottom.AddStretch()

	layout := qtwidgets.NewQVBoxLayout(d.AsQWidget())
	// Leave room around the edge of the window and between its parts, and
	// make a dialog of few choices as wide as one of the request types.
	layout.SetContentsMargins(qtcore.QMargins{Left: 24, Top: 24, Right: 24, Bottom: 24})
	d.SetMinimumWidth(choiceMinWidth)
	layout.SetSpacing(18)
	label := qtwidgets.NewQLabelWithText(prompt, nil)
	label.SetAlignment(qtcore.AlignCenter)
	layout.AddWidget(label.AsQWidget())
	layout.AddLayout(row.AsQLayout())
	layout.AddLayout(bottom.AsQLayout())

	return d
}

// ask shows the dialog and waits for the user. It returns what was picked,
// and false if the dialog was cancelled.
func (d *choiceDialog[K]) ask() (K, bool) {
	// A dialog is not destroyed by being closed.
	defer d.DeleteLater()

	accepted := d.Exec() == int(qtwidgets.QDialogAccepted)
	return d.chosen, accepted
}

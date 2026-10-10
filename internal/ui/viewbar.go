package ui

import (
	qt "github.com/mappu/miqt/qt6"
)

// viewIconSize is the size of the icons in the slim bars at the right of the
// request and response panels.
const viewIconSize = 32

// newViewButton returns a button for a slim bar, showing a symbol on a
// colour. See drawIcon.
func newViewButton(colour string, glyph func(*qt.QPainter), tip string) *qt.QToolButton {
	button := qt.NewQToolButton2()
	button.SetIcon(drawIcon(viewIconSize*2, colour, glyph))
	button.SetIconSize(qt.NewQSize2(viewIconSize, viewIconSize))
	button.SetToolTip(tip)
	button.SetAutoRaise(true)
	return button
}

// newViewBar returns a slim bar of buttons, to go down the right edge of a
// panel.
func newViewBar(buttons ...*qt.QToolButton) *qt.QWidget {
	// The bar is framed, like the pane of the sidebar at the left. The
	// border is given outright, as not every theme draws one around a frame.
	bar := qt.NewQFrame2()
	bar.SetStyleSheet(".QFrame { border: 1px solid " + borderColour() + "; border-radius: 3px; }")
	layout := qt.NewQVBoxLayout(bar.QWidget)
	layout.SetContentsMargins(4, 4, 4, 4)
	for _, button := range buttons {
		layout.AddWidget(button.QWidget)
	}
	layout.AddStretch()

	return bar.QWidget
}

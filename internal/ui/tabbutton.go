package ui

import (
	qt "github.com/mappu/miqt/qt6"
)

// tabButtonSize is the size of the icon of a button beside the tabs of a
// panel.
const tabButtonSize = 32

// newTabButton returns a button to go beside the tabs of a panel, showing a
// symbol on a colour. See drawIcon.
func newTabButton(colour string, glyph func(*qt.QPainter), tip string) *qt.QToolButton {
	button := qt.NewQToolButton2()
	button.SetIcon(drawIcon(tabButtonSize*2, colour, glyph))
	button.SetIconSize(qt.NewQSize2(tabButtonSize, tabButtonSize))
	button.SetToolTip(tip)
	button.SetAutoRaise(true)
	return button
}

// newButtonTabs returns a tab widget with a button at the right of the line
// its tabs are on.
func newButtonTabs(button *qt.QToolButton) *qt.QTabWidget {
	// Something put beside the tabs is given no more than their height, so
	// they are made tall enough for the button. The size is kept and
	// returned each time, as one made for each call would never be freed.
	height := button.SizeHint().Height()
	tabSize := qt.NewQSize()
	bar := qt.NewQTabBar2()
	bar.OnTabSizeHint(func(super func(int) *qt.QSize, index int) *qt.QSize {
		size := super(index)
		tabSize.SetWidth(size.Width())
		tabSize.SetHeight(max(size.Height(), height))
		return tabSize
	})

	tabs := qt.NewQTabWidget2()
	tabs.SetTabBar(bar)
	tabs.SetCornerWidget2(button.QWidget, qt.TopRightCorner)
	return tabs
}

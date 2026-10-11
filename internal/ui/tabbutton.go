package ui

import (
	qt "github.com/mappu/miqt/qt6"
)

// iconButtonSize is the size of the icon of a button that is shown as an
// icon alone: those beside the tabs of a panel, and along the bottom of the
// sidebar.
const iconButtonSize = 32

// newIconButton returns a button shown as an icon, a symbol on one of the
// colours of the settings, with what it does as its tooltip. See drawIcon.
func newIconButton(colour colourKind, glyph func(*qt.QPainter), tip string) *qt.QToolButton {
	button := qt.NewQToolButton2()
	followColours(button, func() { button.SetIcon(drawIcon(iconButtonSize*2, themeColour(colour), glyph)) })
	button.SetIconSize(qt.NewQSize2(iconButtonSize, iconButtonSize))
	button.SetToolTip(tip)
	button.SetAutoRaise(true)
	return button
}

// newButtonTabs returns a tab widget with buttons at the right of the line
// its tabs are on.
func newButtonTabs(buttons ...*qt.QToolButton) *qt.QTabWidget {
	// More than one button is put in a row.
	corner := buttons[0].QWidget
	if len(buttons) > 1 {
		corner = qt.NewQWidget2()
		row := qt.NewQHBoxLayout(corner)
		row.SetContentsMargins(0, 0, 0, 0)
		row.SetSpacing(2)
		for _, button := range buttons {
			row.AddWidget(button.QWidget)
		}
	}

	// Something put beside the tabs is given no more than their height, so
	// they are made tall enough for the buttons. The size is kept and
	// returned each time, as one made for each call would never be freed.
	height := buttons[0].SizeHint().Height()
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
	tabs.SetCornerWidget2(corner, qt.TopRightCorner)
	return tabs
}

// newButtonRow lays out icon buttons in a row from the left, to go along the
// bottom of a tab of the sidebar.
func newButtonRow(buttons ...*qt.QToolButton) *qt.QHBoxLayout {
	row := qt.NewQHBoxLayout2()
	row.SetSpacing(2)
	for _, button := range buttons {
		row.AddWidget(button.QWidget)
	}
	row.AddStretch()
	return row
}

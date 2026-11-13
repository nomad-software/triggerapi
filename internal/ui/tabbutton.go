package ui

import (
	"github.com/nomad-software/qute/qtcore"
	"github.com/nomad-software/qute/qtgui"
	"github.com/nomad-software/qute/qtwidgets"
)

// iconButtonSize is the size of the icon of a button that is shown as an
// icon alone: those beside the tabs of a panel, and along the bottom of the
// sidebar.
const iconButtonSize = 32

// newIconButton returns a button shown as an icon, a symbol on one of the
// colours of the settings, with what it does as its tooltip. See drawIcon.
func newIconButton(colour colourKind, glyph func(*qtgui.QPainter), tip string) *qtwidgets.QToolButton {
	button := qtwidgets.NewQToolButton(nil)
	followColours(button, func() { button.SetIcon(drawIcon(iconButtonSize*2, themeColour(colour), glyph)) })
	button.SetIconSize(qtcore.QSize{Width: iconButtonSize, Height: iconButtonSize})
	button.SetToolTip(tip)
	button.SetAutoRaise(true)
	return button
}

// newButtonTabs returns a tab widget with buttons at the right of the line
// its tabs are on.
func newButtonTabs(buttons ...*qtwidgets.QToolButton) *qtwidgets.QTabWidget {
	// More than one button is put in a row.
	corner := buttons[0].AsQWidget()
	if len(buttons) > 1 {
		corner = qtwidgets.NewQWidget(nil)
		row := qtwidgets.NewQHBoxLayout(corner)
		row.SetContentsMargins(qtcore.QMargins{Left: 0, Top: 0, Right: 0, Bottom: 0})
		row.SetSpacing(2)
		for _, button := range buttons {
			row.AddWidget(button.AsQWidget())
		}
	}

	// Something put beside the tabs is given no more than their height, so
	// they are made tall enough for the buttons.
	height := buttons[0].SizeHint().Height
	bar := qtwidgets.NewQTabBar(nil)
	bar.OverrideTabSizeHint(func(super func(int) qtcore.QSize, index int) qtcore.QSize {
		size := super(index)
		return qtcore.QSize{Width: size.Width, Height: max(size.Height, height)}
	})

	// A tab widget is given its own tab bar through its protected
	// setTabBar, which only a subclass can call.
	tabs := &buttonTabs{}
	qtwidgets.NewQTabWidgetWith(tabs, nil)
	tabs.SetTabBar(bar)
	tabs.SetCornerWidgetFull(corner, qtcore.TopRightCorner)
	return tabs.QTabWidget
}

// buttonTabs is the tab widget of newButtonTabs.
type buttonTabs struct {
	qtwidgets.QTabWidgetBase
}

// newButtonRow lays out icon buttons in a row from the left, to go along the
// bottom of a tab of the sidebar.
func newButtonRow(buttons ...*qtwidgets.QToolButton) *qtwidgets.QHBoxLayout {
	row := qtwidgets.NewQHBoxLayout(nil)
	row.SetSpacing(2)
	for _, button := range buttons {
		row.AddWidget(button.AsQWidget())
	}
	row.AddStretch()
	return row
}

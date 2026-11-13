package ui

import (
	"github.com/nomad-software/qute/qtcore"
	"github.com/nomad-software/qute/qtwidgets"
)

// warnDialog returns what tells the user of a problem in a dialog over a
// widget, which they close.
func warnDialog(parent *qtwidgets.QWidget) func(message string) {
	return func(message string) {
		qtwidgets.QMessageBoxWarning(parent, "TriggerAPI", message)
	}
}

// warnBeneath returns what tells the user of a problem in a tooltip beneath
// a widget, which goes away by itself.
func warnBeneath(widget *qtwidgets.QWidget) func(message string) {
	return func(message string) {
		below := qtcore.QPoint{X: 0, Y: widget.Height()}
		qtwidgets.QToolTipShowTextFull(widget.MapToGlobal(below), message, widget, qtcore.QRect{}, -1)
	}
}

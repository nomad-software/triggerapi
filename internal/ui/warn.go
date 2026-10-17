package ui

import qt "github.com/mappu/miqt/qt6"

// warnDialog returns what tells the user of a problem in a dialog over a
// widget, which they close.
func warnDialog(parent *qt.QWidget) func(message string) {
	return func(message string) {
		qt.QMessageBox_Warning(parent, "TriggerAPI", message)
	}
}

// warnBeneath returns what tells the user of a problem in a tooltip beneath
// a widget, which goes away by itself.
func warnBeneath(widget *qt.QWidget) func(message string) {
	return func(message string) {
		below := qt.NewQPoint2(0, widget.Height())
		defer below.Delete()
		qt.QToolTip_ShowText2(widget.MapToGlobalWithQPoint(below), message, widget)
	}
}

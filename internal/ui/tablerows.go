package ui

import (
	qt "github.com/mappu/miqt/qt6"
)

// What the editable tables of rows share, each row with a checkbox which
// switches it on and off and a button which removes it: the key/value
// tables and the table of assertions.

// removeIconSize is the size of the icon which removes a row.
const removeIconSize = 18

// delegateCheckColumn has a click anywhere in a cell of the column of a
// table, which holds a checkbox, switch it, and not only one on the box
// itself.
func delegateCheckColumn(table *qt.QTableWidget, column int) {
	check := qt.NewQStyledItemDelegate2(table.QObject)
	check.OnEditorEvent(func(super func(*qt.QEvent, *qt.QAbstractItemModel, *qt.QStyleOptionViewItem, *qt.QModelIndex) bool, event *qt.QEvent, model *qt.QAbstractItemModel, option *qt.QStyleOptionViewItem, index *qt.QModelIndex) bool {
		switch event.Type() {
		case qt.QEvent__MouseButtonPress, qt.QEvent__MouseButtonRelease, qt.QEvent__MouseButtonDblClick:
		default:
			// The space bar, as it always has.
			return super(event, model, option, index)
		}
		mouse := qt.UnsafeNewQMouseEvent(event.UnsafePointer())
		if mouse.Button() != qt.LeftButton || !option.Rect().ContainsWithQPoint(mouse.Position().ToPoint()) {
			return super(event, model, option, index)
		}
		// The release switches it, as a click on the box does. The press
		// is left to the table, which selects the row with it, and a double
		// click is taken so that nothing else is done with it.
		switch event.Type() {
		case qt.QEvent__MouseButtonPress:
			return false
		case qt.QEvent__MouseButtonRelease:
			if item := table.Item(index.Row(), column); item != nil {
				item.SetCheckState(checkState(item.CheckState() != qt.Checked))
			}
		}
		return true
	})
	table.SetItemDelegateForColumn(column, check.QAbstractItemDelegate)
}

// delegateRemoveColumn has a cell of the column of a table which holds the
// button that removes its row, and which cannot be selected itself, drawn
// as selected with the rest of its row.
func delegateRemoveColumn(table *qt.QTableWidget, column int) {
	remove := qt.NewQStyledItemDelegate2(table.QObject)
	remove.OnPaint(func(super func(*qt.QPainter, *qt.QStyleOptionViewItem, *qt.QModelIndex), painter *qt.QPainter, option *qt.QStyleOptionViewItem, index *qt.QModelIndex) {
		if table.SelectionModel().IsRowSelected(index.Row()) {
			state := option.State()
			option.SetState(state | qt.QStyle__State_Selected | qt.QStyle__State_Enabled)
			defer option.SetState(state)
		}
		super(painter, option, index)
	})
	table.SetItemDelegateForColumn(column, remove.QAbstractItemDelegate)
}

// removeRowIcon returns the icon of the buttons which remove a row.
func removeRowIcon() *qt.QIcon {
	return cachedIcon("remove row", func() *qt.QIcon { return drawIcon(removeIconSize*2, themeColour(colourRemove), drawBin) })
}

// newRowRemoveButton returns a button, for the column of a table, which
// calls remove with the row it is in.
func newRowRemoveButton(table *qt.QTableWidget, column int, remove func(row int)) *qt.QWidget {
	button := qt.NewQToolButton2()
	followColours(button, func() { button.SetIcon(removeRowIcon()) })
	button.SetIconSize(qt.NewQSize2(removeIconSize, removeIconSize))
	button.SetToolTip("Remove this row")
	button.SetAutoRaise(true)
	// The keyboard moves between the cells that are typed in, and a cell
	// counts as being edited while something inside the table has focus.
	button.SetFocusPolicy(qt.NoFocus)
	// The rows move as others are removed, so the row of the button is
	// found when it is pressed.
	button.OnClicked(func() {
		for row := range table.RowCount() {
			if cell := table.CellWidget(row, column); cell != nil && cell.UnsafePointer() == button.QWidget.UnsafePointer() {
				remove(row)
				return
			}
		}
	})
	return button.QWidget
}

// checkState returns the state of a checkbox which is ticked if enabled is.
func checkState(enabled bool) qt.CheckState {
	if enabled {
		return qt.Checked
	}
	return qt.Unchecked
}

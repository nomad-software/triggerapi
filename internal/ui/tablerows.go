package ui

import (
	"github.com/nomad-software/qute/qtcore"
	"github.com/nomad-software/qute/qtgui"
	"github.com/nomad-software/qute/qtwidgets"
)

// What the editable tables of rows share, each row with a checkbox which
// switches it on and off and a button which removes it: the key/value
// tables and the table of assertions.

// removeIconSize is the size of the icon which removes a row.
const removeIconSize = 18

// delegateCheckColumn has a click anywhere in a cell of the column of a
// table, which holds a checkbox, switch it, and not only one on the box
// itself.
func delegateCheckColumn(table *qtwidgets.QTableWidget, column int) {
	check := &checkColumnDelegate{table: table, column: column}
	qtwidgets.NewQStyledItemDelegateWith(check, table)
	table.SetItemDelegateForColumn(column, check)
}

// checkColumnDelegate is the delegate of delegateCheckColumn.
type checkColumnDelegate struct {
	qtwidgets.QStyledItemDelegateBase
	table  *qtwidgets.QTableWidget
	column int
}

// EditorEvent switches the checkbox of a cell clicked anywhere.
func (d *checkColumnDelegate) EditorEvent(event *qtcore.QEvent, model *qtcore.QAbstractItemModel, option *qtwidgets.QStyleOptionViewItem, index *qtcore.QModelIndex) bool {
	switch event.Type() {
	case qtcore.QEventMouseButtonPress, qtcore.QEventMouseButtonRelease, qtcore.QEventMouseButtonDblClick:
	default:
		// The space bar, as it always has.
		return d.Super().EditorEvent(event, model, option, index)
	}
	mouse := qtgui.QMouseEventCast(event)
	if mouse.Button() != qtcore.LeftButton || !option.Rect().Contains(mouse.Position().ToPoint()) {
		return d.Super().EditorEvent(event, model, option, index)
	}
	// The release switches it, as a click on the box does. The press is
	// left to the table, which selects the row with it, and a double click
	// is taken so that nothing else is done with it.
	switch event.Type() {
	case qtcore.QEventMouseButtonPress:
		return false
	case qtcore.QEventMouseButtonRelease:
		if item := d.table.Item(index.Row(), d.column); item != nil {
			item.SetCheckState(checkState(item.CheckState() != qtcore.Checked))
		}
	}
	return true
}

// delegateRemoveColumn has a cell of the column of a table which holds the
// button that removes its row, and which cannot be selected itself, drawn
// as selected with the rest of its row.
func delegateRemoveColumn(table *qtwidgets.QTableWidget, column int) {
	remove := &removeColumnDelegate{table: table}
	qtwidgets.NewQStyledItemDelegateWith(remove, table)
	table.SetItemDelegateForColumn(column, remove)
}

// removeColumnDelegate is the delegate of delegateRemoveColumn.
type removeColumnDelegate struct {
	qtwidgets.QStyledItemDelegateBase
	table *qtwidgets.QTableWidget
}

// Paint draws the cell selected if its row is.
func (d *removeColumnDelegate) Paint(painter *qtgui.QPainter, option *qtwidgets.QStyleOptionViewItem, index *qtcore.QModelIndex) {
	if d.table.SelectionModel().IsRowSelected(index.Row()) {
		state := option.State()
		option.SetState(state | qtwidgets.QStyleState_Selected | qtwidgets.QStyleState_Enabled)
		defer option.SetState(state)
	}
	d.Super().Paint(painter, option, index)
}

// removeRowIcon returns the icon of the buttons which remove a row.
func removeRowIcon() *qtgui.QIcon {
	return cachedIcon("remove row", func() *qtgui.QIcon { return drawIcon(removeIconSize*2, themeColour(colourRemove), drawBin) })
}

// newRowRemoveButton returns a button, for the column of a table, which
// calls remove with the row it is in.
func newRowRemoveButton(table *qtwidgets.QTableWidget, column int, remove func(row int)) *qtwidgets.QWidget {
	button := qtwidgets.NewQToolButton(nil)
	followColours(button, func() { button.SetIcon(removeRowIcon()) })
	button.SetIconSize(qtcore.QSize{Width: removeIconSize, Height: removeIconSize})
	button.SetToolTip("Remove this row")
	button.SetAutoRaise(true)
	// The keyboard moves between the cells that are typed in, and a cell
	// counts as being edited while something inside the table has focus.
	button.SetFocusPolicy(qtcore.NoFocus)
	// The rows move as others are removed, so the row of the button is
	// found when it is pressed.
	button.OnClicked(func(_ bool) {
		for row := range table.RowCount() {
			if cell := table.CellWidget(row, column); cell != nil && cell.Ptr() == button.Ptr() {
				remove(row)
				return
			}
		}
	})
	return button.AsQWidget()
}

// checkState returns the state of a checkbox which is ticked if enabled is.
func checkState(enabled bool) qtcore.CheckState {
	if enabled {
		return qtcore.Checked
	}
	return qtcore.Unchecked
}

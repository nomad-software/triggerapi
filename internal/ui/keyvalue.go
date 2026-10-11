package ui

import (
	"slices"
	"strings"

	qt "github.com/mappu/miqt/qt6"
	"github.com/mappu/miqt/qt6/mainthread"

	"github.com/nomad-software/triggerapi/internal/rest"
)

const (
	colEnabled = iota
	colKey
	colValue
	// colDescription holds a description of the pair. It is hidden unless
	// the table is one whose pairs have descriptions. See describe.
	colDescription
	// colStatus holds an icon which says something of the row. It is
	// hidden unless the table is one that shows statuses. See showStatuses.
	colStatus
	// colRemove holds the button which removes the row.
	colRemove
)

// describedPair is a pair and the description of it, which is empty in a
// table whose pairs do not have descriptions.
type describedPair struct {
	rest.KeyValue
	Description string
}

// compareDescribed gives the order the rows of a table are kept in: that of
// their keys, and for pairs with the same key, of their descriptions. Both
// ignore case.
func compareDescribed(a, b describedPair) int {
	if c := rest.ComparePairs(a.KeyValue, b.KeyValue); c != 0 {
		return c
	}
	return strings.Compare(strings.ToLower(a.Description), strings.ToLower(b.Description))
}

// rowStatus is what is shown of a row in its status column: an icon, with
// what it means as its tooltip.
type rowStatus struct {
	icon *qt.QIcon
	tip  string
}

// removeIconSize is the size of the icon which removes a row.
const removeIconSize = 18

// keyValueTable is an editable list of key/value pairs, each of which can be
// switched off. An empty row is always kept at the bottom for new entries.
// The pairs are kept in alphabetical order of their keys. Each row but the
// empty one has a button at its right which removes it. A table can also
// have a description of each pair (describe).
type keyValueTable struct {
	table    *qt.QTableWidget
	updating bool

	// onChange is called after the user edits or removes a row.
	onChange func()

	// statuses records that rows have statuses on show, which no longer
	// hold once the rows are changed. onStale is called when they are
	// taken away because of that.
	statuses bool
	onStale  func()

	// faint is the brush descriptions are written with, once made.
	faint *qt.QBrush
}

func newKeyValueTable(keyLabel, valueLabel string) *keyValueTable {
	t := &keyValueTable{table: qt.NewQTableWidget3(0, 6)}

	t.table.SetHorizontalHeaderLabels([]string{"", keyLabel, valueLabel, "", "", ""})
	t.table.SetColumnHidden(colDescription, true)
	t.table.SetColumnHidden(colStatus, true)
	t.table.SetIconSize(qt.NewQSize2(removeIconSize, removeIconSize))
	t.table.VerticalHeader().SetVisible(false)

	header := t.table.HorizontalHeader()
	header.SetSectionResizeMode2(colEnabled, qt.QHeaderView__ResizeToContents)
	header.SetSectionResizeMode2(colKey, qt.QHeaderView__Stretch)
	header.SetSectionResizeMode2(colValue, qt.QHeaderView__Stretch)
	header.SetSectionResizeMode2(colDescription, qt.QHeaderView__Stretch)
	header.SetSectionResizeMode2(colStatus, qt.QHeaderView__ResizeToContents)
	header.SetSectionResizeMode2(colRemove, qt.QHeaderView__ResizeToContents)

	t.table.OnItemChanged(func(item *qt.QTableWidgetItem) {
		if t.updating {
			return
		}
		last := t.table.RowCount() - 1
		if item.Row() == last && (t.text(last, colKey) != "" || t.text(last, colValue) != "" || t.text(last, colDescription) != "") {
			t.addRow("", "", true)
		}
		t.changed()
	})

	// The rows are put in order once a key has been edited, but not while
	// the user is moving on to edit another cell, which would move the row
	// from under them.
	t.table.OnCloseEditor(func(super func(*qt.QWidget, qt.QAbstractItemDelegate__EndEditHint), editor *qt.QWidget, hint qt.QAbstractItemDelegate__EndEditHint) {
		super(editor, hint)
		mainthread.Start(func() {
			if !t.editing() {
				t.sort()
			}
		})
	})

	t.addRow("", "", true)

	return t
}

// editing reports whether a cell is being edited.
func (t *keyValueTable) editing() bool {
	focus := qt.QApplication_FocusWidget()
	return focus != nil && t.table.Viewport().IsAncestorOf(focus)
}

// describe shows the column of descriptions, under a heading.
func (t *keyValueTable) describe(label string) {
	t.table.HorizontalHeaderItem(colDescription).SetText(label)
	t.table.SetColumnHidden(colDescription, false)
}

// sort puts the rows in alphabetical order of their keys, and of their
// descriptions where the keys are the same, keeping the current cell on the
// pair it was on, and calls onChange if any moved.
func (t *keyValueTable) sort() {
	type entry struct {
		describedPair
		row int
	}

	rows := make([]entry, t.table.RowCount()-1)
	for row := range rows {
		rows[row] = entry{t.described(row), row}
	}
	sorted := slices.Clone(rows)
	slices.SortStableFunc(sorted, func(a, b entry) int { return compareDescribed(a.describedPair, b.describedPair) })
	if slices.Equal(rows, sorted) {
		return
	}

	current, column := t.table.CurrentRow(), t.table.CurrentColumn()

	t.updating = true
	for row, e := range sorted {
		t.table.Item(row, colKey).SetText(e.Key)
		t.table.Item(row, colValue).SetText(e.Value)
		t.table.Item(row, colDescription).SetText(e.Description)
		t.table.Item(row, colEnabled).SetCheckState(checkState(e.Enabled))
		if e.row == current {
			t.table.SetCurrentCell(row, column)
		}
	}
	t.updating = false

	t.changed()
}

func checkState(enabled bool) qt.CheckState {
	if enabled {
		return qt.Checked
	}
	return qt.Unchecked
}

// pair returns the contents of a row.
func (t *keyValueTable) pair(row int) rest.KeyValue {
	return rest.KeyValue{
		Key:     t.text(row, colKey),
		Value:   t.text(row, colValue),
		Enabled: t.table.Item(row, colEnabled).CheckState() == qt.Checked,
	}
}

// described returns the contents of a row, with its description.
func (t *keyValueTable) described(row int) describedPair {
	return describedPair{t.pair(row), t.text(row, colDescription)}
}

func (t *keyValueTable) widget() *qt.QWidget {
	return t.table.QWidget
}

// showStatuses shows a status against each row whose pair is enabled and
// has one of the keys, and none against the others. The statuses are taken
// away again when the rows are next changed.
func (t *keyValueTable) showStatuses(statuses map[string]rowStatus) {
	defer func(prev bool) { t.updating = prev }(t.updating)
	t.updating = true

	t.statuses = len(statuses) > 0
	none := qt.NewQIcon()
	defer none.Delete()

	for row := range t.table.RowCount() {
		item := t.table.Item(row, colStatus)
		pair := t.pair(row)
		if status, ok := statuses[strings.TrimSpace(pair.Key)]; ok && pair.Enabled {
			item.SetIcon(status.icon)
			item.SetToolTip(status.tip)
		} else {
			item.SetIcon(none)
			item.SetToolTip("")
		}
	}
}

func (t *keyValueTable) changed() {
	if t.statuses {
		t.showStatuses(nil)
		if t.onStale != nil {
			t.onStale()
		}
	}
	if t.onChange != nil {
		t.onChange()
	}
}

func (t *keyValueTable) addRow(key, value string, enabled bool) {
	defer func(prev bool) { t.updating = prev }(t.updating)
	t.updating = true

	row := t.table.RowCount()
	t.table.InsertRow(row)

	check := qt.NewQTableWidgetItem()
	check.SetFlags(qt.ItemIsEnabled | qt.ItemIsUserCheckable)
	check.SetCheckState(checkState(enabled))

	t.table.SetItem(row, colEnabled, check)
	t.table.SetItem(row, colKey, qt.NewQTableWidgetItem2(key))
	t.table.SetItem(row, colValue, qt.NewQTableWidgetItem2(value))

	// A description is fainter than what it describes: the colour of the
	// text beside it, half transparent.
	description := qt.NewQTableWidgetItem()
	if t.faint == nil {
		// The colour is copied, as the one returned is the palette's own.
		text := t.table.Palette().ColorWithCr(qt.QPalette__Text)
		t.faint = qt.NewQBrush3(qt.NewQColor11(text.Red(), text.Green(), text.Blue(), 127))
	}
	description.SetForeground(t.faint)
	t.table.SetItem(row, colDescription, description)

	// Nothing is typed or selected in the cell of the status, which is
	// left enabled so that its icon is not greyed, or in that of the button.
	status := qt.NewQTableWidgetItem()
	status.SetFlags(qt.ItemIsEnabled)
	t.table.SetItem(row, colStatus, status)
	blank := qt.NewQTableWidgetItem()
	blank.SetFlags(qt.NoItemFlags)
	t.table.SetItem(row, colRemove, blank)

	// The row above is no longer the empty one, so it can now be removed.
	if row > 0 && t.table.CellWidget(row-1, colRemove) == nil {
		t.table.SetCellWidget(row-1, colRemove, t.newRemoveButton())
	}
}

// removeIcon is the icon of the buttons which remove a row, once drawn.
var removeIcon *qt.QIcon

// removeRowIcon returns the icon of the buttons which remove a row.
func removeRowIcon() *qt.QIcon {
	if removeIcon == nil {
		removeIcon = drawIcon(removeIconSize*2, themeColour(colourRemove), drawBin)
	}
	return removeIcon
}

// newRemoveButton returns a button which removes the row it is in.
func (t *keyValueTable) newRemoveButton() *qt.QWidget {
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
		for row := range t.table.RowCount() {
			if cell := t.table.CellWidget(row, colRemove); cell != nil && cell.UnsafePointer() == button.QWidget.UnsafePointer() {
				t.remove(row)
				return
			}
		}
	})
	return button.QWidget
}

// add inserts a pair above the trailing empty row.
func (t *keyValueTable) add(key, value string) {
	last := t.table.RowCount() - 1
	t.updating = true
	t.table.Item(last, colKey).SetText(key)
	t.table.Item(last, colValue).SetText(value)
	t.updating = false
	t.addRow("", "", true)
	t.changed()
	t.sort()
}

// setItems replaces the contents of the table, in alphabetical order,
// without calling onChange.
func (t *keyValueTable) setItems(items []rest.KeyValue) {
	items = slices.Clone(items)
	rest.SortPairs(items)

	defer func(prev bool) { t.updating = prev }(t.updating)
	t.updating = true

	t.statuses = false
	t.table.SetRowCount(0)
	for _, item := range items {
		t.addRow(item.Key, item.Value, item.Enabled)
	}
	t.addRow("", "", true)
}

// setDescribed replaces the contents of the table with pairs that have
// descriptions, in order, without calling onChange.
func (t *keyValueTable) setDescribed(pairs []describedPair) {
	pairs = slices.Clone(pairs)
	slices.SortStableFunc(pairs, compareDescribed)

	items := make([]rest.KeyValue, len(pairs))
	for i, p := range pairs {
		items[i] = p.KeyValue
	}
	// The order is already that of the items, which are not moved.
	t.setItems(items)

	defer func(prev bool) { t.updating = prev }(t.updating)
	t.updating = true
	for row, p := range pairs {
		t.table.Item(row, colDescription).SetText(p.Description)
	}
}

// allDescribed returns every row that has a key, with its description.
func (t *keyValueTable) allDescribed() []describedPair {
	var pairs []describedPair
	for row := range t.table.RowCount() {
		if pair := t.described(row); pair.Key != "" {
			pairs = append(pairs, pair)
		}
	}
	return pairs
}

// remove deletes a row, keeping the trailing empty row.
func (t *keyValueTable) remove(row int) {
	if row < 0 || row >= t.table.RowCount()-1 {
		return
	}
	t.table.RemoveRow(row)
	t.changed()
}

func (t *keyValueTable) text(row, col int) string {
	if item := t.table.Item(row, col); item != nil {
		return item.Text()
	}
	return ""
}

// items returns every row that has a key.
func (t *keyValueTable) items() []rest.KeyValue {
	var items []rest.KeyValue
	for row := range t.table.RowCount() {
		if pair := t.pair(row); pair.Key != "" {
			items = append(items, pair)
		}
	}
	return items
}

package ui

import (
	"cmp"
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

// keyValueTable is an editable list of key/value pairs, each of which can be
// switched off. An empty row is always kept at the bottom for new entries.
// The pairs are kept in alphabetical order of their keys, unless the table
// is sorted by its headings (sortByHeadings). Each row but the empty one has
// a button at its right which removes it. A table can also have a
// description of each pair (describe).
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

	// exclusive keeps to one the pairs with the same key that are switched
	// on. See keepOne.
	exclusive bool

	// plain has descriptions written like the rest of the pair, and not
	// fainter. faint is the brush they are otherwise written with, once
	// made.
	plain bool
	faint *qt.QBrush

	// sortable keeps the pairs in the order they were added, which is the
	// order they are read in, and shows them sorted only as the user
	// chooses with the headings (sortByHeadings). places holds where each
	// row is in that order, which goes with the row wherever it is shown;
	// next is the place of the next row added.
	sortable bool
	places   []int
	next     int
	// sortKeys are the columns the rows are shown sorted by, the first
	// deciding, and none for the order they are kept in. adding records
	// that the heading being clicked is clicked with Shift.
	sortKeys []sortKey
	adding   bool
	// onSort is called after the user sorts by the headings.
	onSort func([]sortKey)
}

// newExclusiveTable returns a table which keeps to one the pairs with the same
// key that are switched on, for what is used by name.
func newExclusiveTable(keyLabel, valueLabel string) *keyValueTable {
	t := newKeyValueTable(keyLabel, valueLabel)
	t.exclusive = true
	return t
}

func newKeyValueTable(keyLabel, valueLabel string) *keyValueTable {
	t := &keyValueTable{table: qt.NewQTableWidget3(0, 6)}

	t.table.SetHorizontalHeaderLabels([]string{"", keyLabel, valueLabel, "", "", ""})
	t.table.SetColumnHidden(colDescription, true)
	t.table.SetColumnHidden(colStatus, true)
	t.table.SetIconSize(qt.NewQSize2(removeIconSize, removeIconSize))
	t.table.VerticalHeader().SetVisible(false)
	// The row being worked on is highlighted as a whole.
	t.table.SetSelectionBehavior(qt.QAbstractItemView__SelectRows)
	t.table.SetSelectionMode(qt.QAbstractItemView__SingleSelection)
	t.delegateColumns()

	t.sizeColumns()

	t.table.OnItemChanged(func(item *qt.QTableWidgetItem) {
		if t.updating {
			return
		}
		last := t.table.RowCount() - 1
		if item.Row() == last && (t.text(last, colKey) != "" || t.text(last, colValue) != "" || t.text(last, colDescription) != "") {
			t.addRow("", "", true)
		}
		if t.exclusive {
			t.keepOne(item)
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

// delegateColumns has a click anywhere in the cell of a checkbox switch it,
// and the cell of the remove button drawn as selected with the rest of its
// row (see delegateCheckColumn and delegateRemoveColumn).
func (t *keyValueTable) delegateColumns() {
	delegateCheckColumn(t.table, colEnabled)
	delegateRemoveColumn(t.table, colRemove)
}

// editing reports whether a cell is being edited.
func (t *keyValueTable) editing() bool {
	focus := qt.QApplication_FocusWidget()
	return focus != nil && t.table.Viewport().IsAncestorOf(focus)
}

// keepOne sees that no more than one of the pairs with the same key is
// switched on, after a cell has been changed. Switching one on switches the
// others off. A pair given the key of one that is on is itself switched off,
// so that typing in another value for a key does not put it into use.
func (t *keyValueTable) keepOne(changed *qt.QTableWidgetItem) {
	row := changed.Row()
	pair := t.pair(row)
	if pair.Key == "" || !pair.Enabled {
		return
	}

	defer func(prev bool) { t.updating = prev }(t.updating)
	t.updating = true

	for other := range t.table.RowCount() {
		if other == row || !t.pair(other).Enabled || t.pair(other).Key != pair.Key {
			continue
		}
		switch changed.Column() {
		case colEnabled:
			t.table.Item(other, colEnabled).SetCheckState(qt.Unchecked)
		case colKey:
			t.table.Item(row, colEnabled).SetCheckState(qt.Unchecked)
			return
		}
	}
}

// sizeColumns has the columns of key, value and description share the width
// the others leave, which are as wide as what is in them.
func (t *keyValueTable) sizeColumns() {
	header := t.table.HorizontalHeader()
	header.SetSectionResizeMode2(colEnabled, qt.QHeaderView__ResizeToContents)
	header.SetSectionResizeMode2(colKey, qt.QHeaderView__Stretch)
	header.SetSectionResizeMode2(colValue, qt.QHeaderView__Stretch)
	header.SetSectionResizeMode2(colDescription, qt.QHeaderView__Stretch)
	header.SetSectionResizeMode2(colStatus, qt.QHeaderView__ResizeToContents)
	header.SetSectionResizeMode2(colRemove, qt.QHeaderView__ResizeToContents)
}

// describe shows the column of descriptions, under a heading.
func (t *keyValueTable) describe(label string) {
	t.table.HorizontalHeaderItem(colDescription).SetText(label)
	t.table.SetColumnHidden(colDescription, false)
}

// compare gives the order rows are shown in, given their pairs and places:
// alphabetical by their keys and then their descriptions, or for a table
// sorted by its headings, as compareSorted says.
func (t *keyValueTable) compare(a describedPair, aPlace int, b describedPair, bPlace int) int {
	if !t.sortable {
		return compareDescribed(a, b)
	}
	return t.compareSorted(a, aPlace, b, bPlace)
}

// sort puts the rows in the order they are shown in (compare), keeping the
// current cell on the pair it was on. Pairs that are kept in order of their
// keys have been changed if any moved, which onChange is told; a sort by the
// headings changes only how they are shown.
func (t *keyValueTable) sort() {
	type entry struct {
		describedPair
		place, row int
	}

	rows := make([]entry, t.table.RowCount()-1)
	for row := range rows {
		rows[row] = entry{t.described(row), t.places[row], row}
	}
	sorted := slices.Clone(rows)
	slices.SortStableFunc(sorted, func(a, b entry) int { return t.compare(a.describedPair, a.place, b.describedPair, b.place) })
	if slices.Equal(rows, sorted) {
		return
	}

	current, column := t.table.CurrentRow(), t.table.CurrentColumn()

	defer func(prev bool) { t.updating = prev }(t.updating)
	t.updating = true
	for row, e := range sorted {
		t.table.Item(row, colKey).SetText(e.Key)
		t.table.Item(row, colValue).SetText(e.Value)
		t.table.Item(row, colDescription).SetText(e.Description)
		t.table.Item(row, colEnabled).SetCheckState(checkState(e.Enabled))
		t.places[row] = e.place
		if e.row == current {
			t.table.SetCurrentCell(row, column)
		}
	}
	t.updating = false

	if !t.sortable {
		t.changed()
	}
}

// kept returns the rows that have a key, in the order their pairs are kept
// in.
func (t *keyValueTable) kept() []int {
	var rows []int
	for row := range t.table.RowCount() {
		if t.text(row, colKey) != "" {
			rows = append(rows, row)
		}
	}
	if t.sortable {
		slices.SortFunc(rows, func(a, b int) int { return cmp.Compare(t.places[a], t.places[b]) })
	}
	return rows
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
	t.places = append(t.places, t.next)
	t.next++

	check := qt.NewQTableWidgetItem()
	check.SetFlags(qt.ItemIsEnabled | qt.ItemIsSelectable | qt.ItemIsUserCheckable)
	check.SetCheckState(checkState(enabled))

	t.table.SetItem(row, colEnabled, check)
	t.table.SetItem(row, colKey, qt.NewQTableWidgetItem2(key))
	t.table.SetItem(row, colValue, qt.NewQTableWidgetItem2(value))

	// A description is fainter than what it describes: the colour of the
	// text beside it, half transparent.
	description := qt.NewQTableWidgetItem()
	if !t.plain {
		if t.faint == nil {
			// The colour is copied, as the one returned is the palette's own.
			text := t.table.Palette().ColorWithCr(qt.QPalette__Text)
			t.faint = qt.NewQBrush3(qt.NewQColor11(text.Red(), text.Green(), text.Blue(), 127))
		}
		description.SetForeground(t.faint)
	}
	t.table.SetItem(row, colDescription, description)

	// Nothing is typed in the cell of the status, which is left enabled so
	// that its icon is not greyed, or in that of the button. The status is
	// highlighted with the rest of its row, and the button is drawn so (see
	// delegateColumns).
	status := qt.NewQTableWidgetItem()
	status.SetFlags(qt.ItemIsEnabled | qt.ItemIsSelectable)
	t.table.SetItem(row, colStatus, status)
	blank := qt.NewQTableWidgetItem()
	blank.SetFlags(qt.NoItemFlags)
	t.table.SetItem(row, colRemove, blank)

	// The row above is no longer the empty one, so it can now be removed.
	if row > 0 && t.table.CellWidget(row-1, colRemove) == nil {
		t.table.SetCellWidget(row-1, colRemove, t.newRemoveButton())
	}
}

// newRemoveButton returns a button which removes the row it is in.
func (t *keyValueTable) newRemoveButton() *qt.QWidget {
	return newRowRemoveButton(t.table, colRemove, t.remove)
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

// addToEdit adds a pair, unless there is one the same already, and opens
// its key for editing, as the caller may only have guessed at it.
func (t *keyValueTable) addToEdit(key, value string) {
	find := func() int {
		for row := range t.table.RowCount() - 1 {
			if pair := t.pair(row); pair.Key == key && pair.Value == value {
				return row
			}
		}
		return -1
	}

	row := find()
	if row < 0 {
		t.add(key, value)
		row = find()
	}
	item := t.table.Item(row, colKey)
	t.table.SetCurrentItem(item)
	t.table.EditItem(item)
}

// setItems replaces the contents of the table, in alphabetical order or, for
// a table sorted by its headings, in the order given and shown as sorted,
// without calling onChange.
func (t *keyValueTable) setItems(items []rest.KeyValue) {
	t.fill(items)
	if t.sortable {
		t.sort()
	}
}

// fill replaces the contents of the table, in alphabetical order or, for a
// table sorted by its headings, in the order given, without calling
// onChange.
func (t *keyValueTable) fill(items []rest.KeyValue) {
	if !t.sortable {
		items = slices.Clone(items)
		rest.SortPairs(items)
	}

	defer func(prev bool) { t.updating = prev }(t.updating)
	t.updating = true

	t.statuses = false
	t.table.SetRowCount(0)
	t.places, t.next = nil, 0
	for _, item := range items {
		t.addRow(item.Key, item.Value, item.Enabled)
	}
	t.addRow("", "", true)
}

// setDescribed replaces the contents of the table with pairs that have
// descriptions, in order, without calling onChange.
func (t *keyValueTable) setDescribed(pairs []describedPair) {
	if !t.sortable {
		pairs = slices.Clone(pairs)
		slices.SortStableFunc(pairs, compareDescribed)
	}

	items := make([]rest.KeyValue, len(pairs))
	for i, p := range pairs {
		items[i] = p.KeyValue
	}
	// The order is already that of the items, which are not moved.
	t.fill(items)

	defer func(prev bool) { t.updating = prev }(t.updating)
	t.updating = true
	for row, p := range pairs {
		t.table.Item(row, colDescription).SetText(p.Description)
	}
	if t.sortable {
		t.sort()
	}
}

// allDescribed returns every row that has a key, with its description, in
// the order they are kept in.
func (t *keyValueTable) allDescribed() []describedPair {
	var pairs []describedPair
	for _, row := range t.kept() {
		pairs = append(pairs, t.described(row))
	}
	return pairs
}

// remove deletes a row, keeping the trailing empty row.
func (t *keyValueTable) remove(row int) {
	if row < 0 || row >= t.table.RowCount()-1 {
		return
	}
	t.table.RemoveRow(row)
	t.places = slices.Delete(t.places, row, row+1)
	t.changed()
}

func (t *keyValueTable) text(row, col int) string {
	if item := t.table.Item(row, col); item != nil {
		return item.Text()
	}
	return ""
}

// items returns every row that has a key, in the order they are kept in.
func (t *keyValueTable) items() []rest.KeyValue {
	var items []rest.KeyValue
	for _, row := range t.kept() {
		items = append(items, t.pair(row))
	}
	return items
}

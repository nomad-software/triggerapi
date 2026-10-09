package ui

import (
	qt "github.com/mappu/miqt/qt6"

	"github.com/nomad-software/postie/internal/rest"
)

const (
	colEnabled = iota
	colKey
	colValue
)

// keyValueTable is an editable list of key/value pairs, each of which can be
// switched off. An empty row is always kept at the bottom for new entries.
type keyValueTable struct {
	table    *qt.QTableWidget
	updating bool

	// onChange is called after the user edits or removes a row.
	onChange func()
}

func newKeyValueTable(keyLabel, valueLabel string) *keyValueTable {
	t := &keyValueTable{table: qt.NewQTableWidget3(0, 3)}

	t.table.SetHorizontalHeaderLabels([]string{"", keyLabel, valueLabel})
	t.table.VerticalHeader().SetVisible(false)

	header := t.table.HorizontalHeader()
	header.SetSectionResizeMode2(colEnabled, qt.QHeaderView__ResizeToContents)
	header.SetSectionResizeMode2(colKey, qt.QHeaderView__Stretch)
	header.SetSectionResizeMode2(colValue, qt.QHeaderView__Stretch)

	t.table.OnItemChanged(func(item *qt.QTableWidgetItem) {
		if t.updating {
			return
		}
		last := t.table.RowCount() - 1
		if item.Row() == last && (t.text(last, colKey) != "" || t.text(last, colValue) != "") {
			t.addRow("", "", true)
		}
		t.changed()
	})

	t.addRow("", "", true)

	return t
}

func (t *keyValueTable) widget() *qt.QWidget {
	return t.table.QWidget
}

func (t *keyValueTable) changed() {
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
	if enabled {
		check.SetCheckState(qt.Checked)
	} else {
		check.SetCheckState(qt.Unchecked)
	}

	t.table.SetItem(row, colEnabled, check)
	t.table.SetItem(row, colKey, qt.NewQTableWidgetItem2(key))
	t.table.SetItem(row, colValue, qt.NewQTableWidgetItem2(value))
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
}

// setItems replaces the contents of the table without calling onChange.
func (t *keyValueTable) setItems(items []rest.KeyValue) {
	defer func(prev bool) { t.updating = prev }(t.updating)
	t.updating = true

	t.table.SetRowCount(0)
	for _, item := range items {
		t.addRow(item.Key, item.Value, item.Enabled)
	}
	t.addRow("", "", true)
}

// removeCurrent deletes the selected row, keeping the trailing empty row.
func (t *keyValueTable) removeCurrent() {
	row := t.table.CurrentRow()
	if row < 0 || row == t.table.RowCount()-1 {
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
		key := t.text(row, colKey)
		if key == "" {
			continue
		}
		items = append(items, rest.KeyValue{
			Key:     key,
			Value:   t.text(row, colValue),
			Enabled: t.table.Item(row, colEnabled).CheckState() == qt.Checked,
		})
	}
	return items
}

// panel returns the table with a remove button beneath it.
func (t *keyValueTable) panel() *qt.QWidget {
	panel := qt.NewQWidget2()
	layout := qt.NewQVBoxLayout(panel)
	layout.SetContentsMargins(0, 0, 0, 0)

	remove := qt.NewQPushButton3("Remove")
	remove.OnClicked(t.removeCurrent)

	buttons := qt.NewQHBoxLayout2()
	buttons.AddStretch()
	buttons.AddWidget(remove.QWidget)

	layout.AddWidget(t.widget())
	layout.AddLayout(buttons.QLayout)

	return panel
}

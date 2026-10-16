package ui

import (
	"reflect"
	"testing"

	qt "github.com/mappu/miqt/qt6"
	"github.com/mappu/miqt/qt6/mainthread"

	"github.com/nomad-software/triggerapi/internal/rest"
)

func TestKeyValueTable(t *testing.T) {
	mainthread.Wait(func() {
		table := newKeyValueTable("Key", "Value")

		// Typing into the trailing row adds a new empty one.
		table.table.Item(0, colKey).SetText("a")
		if table.table.RowCount() != 2 {
			t.Fatalf("rows = %d, want 2", table.table.RowCount())
		}

		table.add("b", "2")
		table.table.Item(1, colEnabled).SetCheckState(qt.Unchecked)

		items := table.items()
		if len(items) != 2 || !items[0].Enabled || items[1].Enabled || items[1].Value != "2" {
			t.Errorf("items = %+v", items)
		}

		table.remove(0)
		if items := table.items(); len(items) != 1 || items[0].Key != "b" {
			t.Errorf("items after remove = %+v", items)
		}

		// The empty row cannot be removed.
		table.remove(1)
		if table.table.RowCount() != 2 {
			t.Errorf("rows = %d, want 2", table.table.RowCount())
		}
	})
}

func TestKeyValueOrder(t *testing.T) {
	mainthread.Wait(func() {
		table := newKeyValueTable("Key", "Value")
		changes := 0
		table.onChange = func() { changes++ }

		keys := func() []string {
			var keys []string
			for _, item := range table.items() {
				keys = append(keys, item.Key)
			}
			return keys
		}

		// Pairs are shown in order, ignoring case, and those with the same
		// key stay as they were.
		table.setItems([]rest.KeyValue{
			{Key: "zeta", Value: "1", Enabled: true},
			{Key: "Beta", Value: "first"},
			{Key: "alpha", Value: "3", Enabled: true},
			{Key: "beta", Value: "second", Enabled: true},
		})
		want := []rest.KeyValue{
			{Key: "alpha", Value: "3", Enabled: true},
			{Key: "Beta", Value: "first"},
			{Key: "beta", Value: "second", Enabled: true},
			{Key: "zeta", Value: "1", Enabled: true},
		}
		if got := table.items(); !reflect.DeepEqual(got, want) || changes != 0 {
			t.Fatalf("items = %+v, %d changes", got, changes)
		}

		// A row whose key is edited moves to its place, taking the rest of
		// the row and the current cell with it.
		table.table.SetCurrentCell(0, colValue)
		table.table.Item(0, colKey).SetText("gamma")
		changes = 0
		table.sort()
		if got, want := keys(), []string{"Beta", "beta", "gamma", "zeta"}; !reflect.DeepEqual(got, want) {
			t.Errorf("keys = %q, want %q", got, want)
		}
		if got := table.pair(2); got != (rest.KeyValue{Key: "gamma", Value: "3", Enabled: true}) || table.pair(0).Enabled {
			t.Errorf("moved row = %+v", got)
		}
		if table.table.CurrentRow() != 2 || table.table.CurrentColumn() != colValue || changes != 1 {
			t.Errorf("current cell = %d,%d with %d changes", table.table.CurrentRow(), table.table.CurrentColumn(), changes)
		}

		// Nothing is reported when the rows are already in order.
		table.sort()
		if changes != 1 {
			t.Errorf("%d changes", changes)
		}

		// A new pair goes to its place, leaving the empty row at the bottom.
		table.add("delta", "4")
		if got, want := keys(), []string{"Beta", "beta", "delta", "gamma", "zeta"}; !reflect.DeepEqual(got, want) {
			t.Errorf("keys = %q, want %q", got, want)
		}
		if last := table.table.RowCount() - 1; last != 5 || table.pair(last).Key != "" {
			t.Errorf("last row = %+v of %d", table.pair(last), last+1)
		}
	})
}

func TestKeyValueOrderAfterEditing(t *testing.T) {
	var table *keyValueTable
	mainthread.Wait(func() {
		table = newKeyValueTable("Key", "Value")
		table.setItems([]rest.KeyValue{{Key: "a", Value: "1", Enabled: true}, {Key: "b", Value: "2", Enabled: true}})
		table.table.Show()

		// The key of the first row is edited and the editor closed.
		item := table.table.Item(0, colKey)
		table.table.SetCurrentItem(item)
		table.table.EditItem(item)
		item.SetText("c")
		table.table.ClosePersistentEditor(item)
		table.table.SetCurrentItem(table.table.Item(0, colKey))
	})

	waitFor(t, "the rows to be put in order", func() bool {
		return table.pair(0).Key == "b" && table.pair(1) == rest.KeyValue{Key: "c", Value: "1", Enabled: true}
	})
}

func TestKeyValueRemoveButtons(t *testing.T) {
	var table *keyValueTable
	changes := 0
	mainthread.Wait(func() {
		table = newKeyValueTable("Key", "Value")
		table.onChange = func() { changes++ }
		table.setItems([]rest.KeyValue{{Key: "a", Value: "1", Enabled: true}, {Key: "b", Value: "2", Enabled: true}, {Key: "c", Value: "3", Enabled: true}})
		table.table.Resize(500, 300)
		table.table.Show()
	})

	mainthread.Wait(func() {
		keys := func() []string {
			var keys []string
			for _, item := range table.items() {
				keys = append(keys, item.Key)
			}
			return keys
		}
		click := func(row int) {
			qt.UnsafeNewQAbstractButton(table.table.CellWidget(row, colRemove).UnsafePointer()).Click()
		}

		// Every row but the empty one has a button.
		for row, want := range []bool{true, true, true, false} {
			if got := table.table.CellWidget(row, colRemove) != nil; got != want {
				t.Errorf("row %d has a button: %v", row, got)
			}
		}

		// A button removes its own row, without asking.
		click(1)
		if got := keys(); !reflect.DeepEqual(got, []string{"a", "c"}) || changes != 1 {
			t.Errorf("keys = %q with %d changes", got, changes)
		}

		// The buttons move with their rows.
		click(1)
		if got := keys(); !reflect.DeepEqual(got, []string{"a"}) {
			t.Errorf("keys = %q", got)
		}

		// A row that is filled in gets a button.
		table.table.Item(1, colKey).SetText("z")
		if table.table.CellWidget(1, colRemove) == nil || table.table.CellWidget(2, colRemove) != nil {
			t.Error("the new row has no button, or the empty one has")
		}
	})
}

func TestKeyValueRowClicks(t *testing.T) {
	var table *keyValueTable
	mainthread.Wait(func() {
		table = newKeyValueTable("Key", "Value")
		table.setItems([]rest.KeyValue{{Key: "a", Value: "1", Enabled: true}, {Key: "b", Value: "2", Enabled: true}})
		table.table.Resize(500, 300)
		table.table.Show()
	})

	mainthread.Wait(func() {
		// click clicks a cell at its right edge, away from a checkbox.
		click := func(row, col int) {
			rect := table.table.VisualItemRect(table.table.Item(row, col))
			at := qt.NewQPointF3(float64(rect.Right()-2), float64(rect.Center().Y()))
			defer at.Delete()
			for _, kind := range []qt.QEvent__Type{qt.QEvent__MouseButtonPress, qt.QEvent__MouseButtonRelease} {
				buttons := qt.LeftButton
				if kind == qt.QEvent__MouseButtonRelease {
					buttons = qt.NoButton
				}
				event := qt.NewQMouseEvent(kind, at, qt.LeftButton, buttons, qt.NoModifier)
				qt.QCoreApplication_SendEvent(table.table.Viewport().QObject, event.QEvent)
				event.Delete()
			}
		}
		enabled := func() []bool {
			var got []bool
			for _, item := range table.items() {
				got = append(got, item.Enabled)
			}
			return got
		}

		// A click anywhere in the cell of the checkbox switches it, once.
		click(1, colEnabled)
		if got := enabled(); !reflect.DeepEqual(got, []bool{true, false}) {
			t.Errorf("after one click: %v", got)
		}
		click(1, colEnabled)
		if got := enabled(); !reflect.DeepEqual(got, []bool{true, true}) {
			t.Errorf("after two clicks: %v", got)
		}

		// The row clicked in is highlighted as a whole, and only that one.
		click(0, colValue)
		if !table.table.SelectionModel().IsRowSelected(0) || table.table.SelectionModel().IsRowSelected(1) {
			t.Error("row 0 is not the one highlighted")
		}
		click(1, colEnabled)
		if !table.table.SelectionModel().IsRowSelected(1) || table.table.SelectionModel().IsRowSelected(0) {
			t.Error("row 1 is not the one highlighted")
		}
	})
}

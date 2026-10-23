package ui

import (
	"reflect"
	"slices"
	"strconv"
	"testing"

	qt "github.com/mappu/miqt/qt6"
	"github.com/mappu/miqt/qt6/mainthread"

	"github.com/nomad-software/triggerapi/internal/env"
)

// clickHeading clicks the heading of a column of a table as the pointer
// does, with the keyboard modifiers given.
func clickHeading(table *qt.QTableWidget, column int, modifiers qt.KeyboardModifier) {
	header := table.HorizontalHeader()
	at := qt.NewQPointF3(float64(header.SectionViewportPosition(column)+header.SectionSize(column)/2), float64(header.Height()/2))
	defer at.Delete()
	for _, kind := range []qt.QEvent__Type{qt.QEvent__MouseButtonPress, qt.QEvent__MouseButtonRelease} {
		buttons := qt.LeftButton
		if kind == qt.QEvent__MouseButtonRelease {
			buttons = qt.NoButton
		}
		event := qt.NewQMouseEvent(kind, at, qt.LeftButton, buttons, modifiers)
		qt.QCoreApplication_SendEvent(header.Viewport().QObject, event.QEvent)
		event.Delete()
	}
}

// shownRows returns the key, value and description of the rows of a table as
// they are shown.
func shownRows(t *keyValueTable) []string {
	var rows []string
	for row := range t.table.RowCount() - 1 {
		rows = append(rows, t.text(row, colKey)+"="+t.text(row, colValue)+"/"+t.text(row, colDescription))
	}
	return rows
}

// headings returns the headings of the columns a table can be sorted by, with
// the marks they are painted with: ^ or v, and the place among several.
func headings(t *keyValueTable) []string {
	var got []string
	for _, column := range sortColumns {
		heading := t.table.HorizontalHeaderItem(column).Text()
		if at := slices.IndexFunc(t.sortKeys, func(k sortKey) bool { return k.column == column }); at >= 0 {
			heading += " ^"
			if t.sortKeys[at].descending {
				heading = heading[:len(heading)-1] + "v"
			}
			if len(t.sortKeys) > 1 {
				heading += strconv.Itoa(at + 1)
			}
		}
		got = append(got, heading)
	}
	return got
}

// The variables are shown in the order they are kept in, and sorted only as
// the headings are clicked: by one column, or with Shift by several. Each
// environment has its own sort, kept in the file of environments, and the
// order of the variables never changes with it.
func TestSortByHeadings(t *testing.T) {
	files := testFiles(t)
	kept := []env.Variable{
		{Key: "host", Value: "2", Enabled: true, Description: "b"},
		{Key: "a", Value: "9", Enabled: true, Description: "c"},
		{Key: "Host", Value: "1", Description: "a"},
	}
	store := env.Store{Environments: []env.Environment{
		{Name: "Local", Variables: kept},
		{Name: "Live", Variables: kept},
	}}
	if err := store.Save(files.Environments); err != nil {
		t.Fatal(err)
	}
	unsorted := []string{"host=2/b", "a=9/c", "Host=1/a"}

	var w *Window
	mainthread.Wait(func() {
		w = NewWindow(files.Files)
		w.Show()
		w.environments.list.SetCurrentRow(1) // Local
		w.environments.edit()
		table := w.editor.variables

		if got := shownRows(table); !slices.Equal(got, unsorted) {
			t.Errorf("shown %q", got)
		}
		if got := headings(table); !slices.Equal(got, []string{"Variable", "Value", "Description"}) {
			t.Errorf("headings %q", got)
		}

		for _, step := range []struct {
			column   int
			shift    bool
			shown    []string
			headings []string
			sort     []env.SortKey
		}{
			{colKey, false, []string{"a=9/c", "host=2/b", "Host=1/a"},
				[]string{"Variable ^", "Value", "Description"}, []env.SortKey{{Column: env.SortVariable}}},
			// The values of a variable decide between those of a name.
			{colValue, true, []string{"a=9/c", "Host=1/a", "host=2/b"},
				[]string{"Variable ^1", "Value ^2", "Description"}, []env.SortKey{{Column: env.SortVariable}, {Column: env.SortValue}}},
			{colValue, true, []string{"a=9/c", "host=2/b", "Host=1/a"},
				[]string{"Variable ^1", "Value v2", "Description"}, []env.SortKey{{Column: env.SortVariable}, {Column: env.SortValue, Descending: true}}},
			{colValue, true, []string{"a=9/c", "host=2/b", "Host=1/a"},
				[]string{"Variable ^", "Value", "Description"}, []env.SortKey{{Column: env.SortVariable}}},
			// The descriptions instead.
			{colDescription, true, []string{"a=9/c", "Host=1/a", "host=2/b"},
				[]string{"Variable ^1", "Value", "Description ^2"}, []env.SortKey{{Column: env.SortVariable}, {Column: env.SortDescription}}},
			// A click without Shift sorts by its column alone.
			{colValue, false, []string{"Host=1/a", "host=2/b", "a=9/c"},
				[]string{"Variable", "Value ^", "Description"}, []env.SortKey{{Column: env.SortValue}}},
			{colValue, false, []string{"a=9/c", "host=2/b", "Host=1/a"},
				[]string{"Variable", "Value v", "Description"}, []env.SortKey{{Column: env.SortValue, Descending: true}}},
			{colValue, false, unsorted,
				[]string{"Variable", "Value", "Description"}, nil},
			// The column of the checkboxes is not sorted by.
			{colEnabled, false, unsorted,
				[]string{"Variable", "Value", "Description"}, nil},
			{colKey, false, []string{"a=9/c", "host=2/b", "Host=1/a"},
				[]string{"Variable ^", "Value", "Description"}, []env.SortKey{{Column: env.SortVariable}}},
			{colDescription, true, []string{"a=9/c", "Host=1/a", "host=2/b"},
				[]string{"Variable ^1", "Value", "Description ^2"}, []env.SortKey{{Column: env.SortVariable}, {Column: env.SortDescription}}},
		} {
			modifiers := qt.NoModifier
			if step.shift {
				modifiers = qt.ShiftModifier
			}
			clickHeading(table.table, step.column, modifiers)
			if got := shownRows(table); !slices.Equal(got, step.shown) {
				t.Errorf("after clicking %d: shown %q, want %q", step.column, got, step.shown)
			}
			if got := headings(table); !slices.Equal(got, step.headings) {
				t.Errorf("after clicking %d: headings %q, want %q", step.column, got, step.headings)
			}
			e, _ := w.environments.store.Current()
			if !reflect.DeepEqual(e.Sort, step.sort) {
				t.Errorf("after clicking %d: sort %+v, want %+v", step.column, e.Sort, step.sort)
			}
			if !slices.Equal(e.Variables, kept) {
				t.Errorf("after clicking %d: variables %+v", step.column, e.Variables)
			}
		}

		// The other environment has its own sort.
		w.environments.list.SetCurrentRow(0) // Live
		if got := shownRows(table); !slices.Equal(got, unsorted) {
			t.Errorf("Live shown %q", got)
		}
		clickHeading(table.table, colValue, qt.NoModifier)
		w.environments.list.SetCurrentRow(1)
		if got := shownRows(table); !slices.Equal(got, []string{"a=9/c", "Host=1/a", "host=2/b"}) {
			t.Errorf("Local shown %q", got)
		}

		// A variable added goes at the end of those kept, and is shown
		// where the sort puts it.
		table.add("b", "0")
		if got := shownRows(table); !slices.Equal(got, []string{"a=9/c", "b=0/", "Host=1/a", "host=2/b"}) {
			t.Errorf("shown %q", got)
		}
	})

	saved, err := loadEnvs(files.Environments)
	if err != nil {
		t.Fatal(err)
	}
	var keys []string
	for _, v := range saved.Environments[1].Variables {
		keys = append(keys, v.Key+"="+v.Value)
	}
	if want := []string{"host=2", "a=9", "Host=1", "b=0"}; !slices.Equal(keys, want) {
		t.Errorf("saved %q, want %q", keys, want)
	}
	if want := []env.SortKey{{Column: env.SortVariable}, {Column: env.SortDescription}}; !reflect.DeepEqual(saved.Environments[1].Sort, want) {
		t.Errorf("saved sort of Local %+v", saved.Environments[1].Sort)
	}
	if want := []env.SortKey{{Column: env.SortValue}}; !reflect.DeepEqual(saved.Environments[0].Sort, want) {
		t.Errorf("saved sort of Live %+v", saved.Environments[0].Sort)
	}

	// The sort is shown again when the environment is next opened.
	mainthread.Wait(func() {
		w.Close()
		w = NewWindow(files.Files)
		w.Show()
		w.environments.list.SetCurrentRow(1)
		w.environments.edit()
		table := w.editor.variables
		if got := shownRows(table); !slices.Equal(got, []string{"a=9/c", "b=0/", "Host=1/a", "host=2/b"}) {
			t.Errorf("shown %q", got)
		}
		if got := headings(table); !slices.Equal(got, []string{"Variable ^1", "Value", "Description ^2"}) {
			t.Errorf("headings %q", got)
		}
	})
}

// The other tables are still kept in order of their keys, and cannot be
// sorted by their headings.
func TestTablesKeptInOrder(t *testing.T) {
	mainthread.Wait(func() {
		table := newKeyValueTable("Key", "Value")
		table.add("b", "2")
		table.add("a", "1")
		if got := shownRows(table); !slices.Equal(got, []string{"a=1/", "b=2/"}) {
			t.Errorf("shown %q", got)
		}
		clickHeading(table.table, colKey, qt.NoModifier)
		clickHeading(table.table, colKey, qt.NoModifier)
		if got := shownRows(table); !slices.Equal(got, []string{"a=1/", "b=2/"}) || table.sortKeys != nil {
			t.Errorf("shown %q after clicking the heading", got)
		}
		table.table.Delete()
	})
}

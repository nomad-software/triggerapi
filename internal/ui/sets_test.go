package ui

import (
	"reflect"
	"slices"
	"strconv"
	"testing"

	qt "github.com/mappu/miqt/qt6"
	"github.com/mappu/miqt/qt6/mainthread"

	"github.com/nomad-software/triggerapi/internal/collection"
	"github.com/nomad-software/triggerapi/internal/rest"
)

func TestSets(t *testing.T) {
	files := testFiles(t)
	store := &collection.Store{}
	one := store.AddFolder("One")
	one.Add(collection.NewREST("A", rest.Request{Method: "GET", URL: "a.test"}))
	one.Add(collection.NewREST("B", rest.Request{Method: "GET", URL: "b.test"}))
	two := store.AddFolder("Two")
	two.Add(collection.NewREST("C", rest.Request{Method: "GET", URL: "c.test"}))
	two.Add(collection.NewREST("D", rest.Request{Method: "GET", URL: "d.test"}))
	if err := store.Save(files.Requests); err != nil {
		t.Fatal(err)
	}

	var w *Window
	check := func(what string, want ...string) {
		t.Helper()
		if got := tabNames(w); !reflect.DeepEqual(got, want) {
			t.Errorf("%s: tabs = %q, want %q", what, got, want)
		}
	}
	setNames := func() []string {
		var names []string
		for _, set := range w.requests.store.Sets {
			name := set.Name + "["
			for i, r := range set.Requests {
				if i > 0 {
					name += " "
				}
				name += strconv.Quote(r.Name)
			}
			names = append(names, name+"]")
		}
		return names
	}
	// drag moves a tab as dragging it does: pressed, moved, and let go.
	drag := func(from, to int) {
		centre := w.tabs.bar.TabRect(from).Center()
		at := qt.NewQPointF3(float64(centre.X()), float64(centre.Y()))
		press := qt.NewQMouseEvent(qt.QEvent__MouseButtonPress, at, qt.LeftButton, qt.LeftButton, qt.NoModifier)
		defer press.Delete()
		qt.QCoreApplication_SendEvent(w.tabs.bar.QObject, press.QEvent)
		w.tabs.bar.MoveTab(from, to)
		release := qt.NewQMouseEvent(qt.QEvent__MouseButtonRelease, at, qt.LeftButton, qt.NoButton, qt.NoModifier)
		defer release.Delete()
		qt.QCoreApplication_SendEvent(w.tabs.bar.QObject, release.QEvent)
	}

	mainthread.Wait(func() {
		w = NewWindow(files.Files)
		w.Show()
		rt := w.requests
		tree := rt.tree
		pick := func(folder, name string) { tree.SetCurrentItem(treeItem(tree, folder, name)) }
		pin := func(folder, name string) {
			pick(folder, name)
			w.tabs.current().button.Click()
		}

		// The views have icons, and the sets are on a page of their own.
		if rt.views.Count() != 2 || rt.views.TabIcon(viewRequests).IsNull() || rt.views.TabIcon(viewSets).CacheKey() != setIcon().CacheKey() {
			t.Error("the views have no icons")
		}
		rt.views.SetCurrentIndex(viewSets)
		if rt.pages.CurrentIndex() != viewSets {
			t.Error("the sets are not shown")
		}

		// A set added holds the pinned tabs, from any folder, and is the
		// one chosen.
		pin("One", "A")
		pin("Two", "C")
		check("pinned", "A*", "C*")
		rt.sets.add()
		if got := setNames(); !reflect.DeepEqual(got, []string{`New Set["A" "C"]`}) || rt.sets.active != rt.store.Sets[0] {
			t.Errorf("sets = %q", got)
		}
		rt.sets.list.CurrentItem().SetText("Work")
	})
	waitFor(t, "the set to be renamed", func() bool {
		return mainthread.Wait2(func() bool {
			return w.requests.sets.list.Item(0).Text() == "Work" && w.requests.store.Sets[0].Name == "Work"
		})
	})

	mainthread.Wait(func() {
		rt := w.requests
		tree := rt.tree
		pick := func(folder, name string) { tree.SetCurrentItem(treeItem(tree, folder, name)) }

		// A second set, made while the first is chosen, starts with its
		// tabs, and the tabs pinned and unpinned are kept in it.
		rt.sets.add()
		doubleClick(w.tabs.tabs[0].button) // A, which is not shown
		pick("Two", "D")
		w.tabs.current().button.Click()
		check("second set", "C*", "D*")
		if got := setNames(); !reflect.DeepEqual(got, []string{`New Set["C" "D"]`, `Work["A" "C"]`}) {
			t.Errorf("sets = %q", got)
		}

		// Choosing the first replaces the tabs with its own.
		rt.sets.list.SetCurrentItem(rt.sets.list.Item(1))
		check("first set", "A*", "C*")
		if w.open == nil || w.open.Name != "A" || rt.sets.active.Name != "Work" {
			t.Errorf("open %+v", w.open)
		}

		// Deselecting the set takes its tabs away, leaving one empty tab,
		// which belongs to no set: pinning changes none.
		rt.sets.list.ClearSelection()
		if rt.sets.active != nil {
			t.Fatal("a set is chosen with none selected")
		}
		check("deselected", "Untitled")
		if w.open != nil {
			t.Errorf("open after deselecting: %+v", w.open)
		}
		pick("One", "B")
		w.tabs.current().button.Click()
		check("pinned with none chosen", "B*")
		if got := setNames(); !reflect.DeepEqual(got, []string{`New Set["C" "D"]`, `Work["A" "C"]`}) {
			t.Errorf("sets changed while none was chosen: %q", got)
		}
		rt.sets.list.SetCurrentItem(rt.sets.list.Item(1))
		check("first set again", "A*", "C*")

		// Tabs are dragged into another order, which the set keeps.
		drag(0, 1)
		check("moved", "C*", "A*")
		if got := setNames()[1]; got != `Work["C" "A"]` {
			t.Errorf("after the move: %q", got)
		}

		// A tab that is not pinned is pinned by being moved.
		pick("One", "B")
		check("B", "C*", "A*", "B")
		drag(2, 0)
		check("B moved", "B*", "C*", "A*")
		if got := setNames()[1]; got != `Work["B" "C" "A"]` {
			t.Errorf("after pinning by moving: %q", got)
		}

		// A tab dragged past New leaves New at the end.
		drag(0, 3)
		check("B moved to the end", "C*", "A*", "B*")
		if got := setNames()[1]; got != `Work["C" "A" "B"]` || w.tabs.bar.TabText(3) != "New" || w.tabs.newAt != 3 {
			t.Errorf("after moving past New: %q, last tab %q", got, w.tabs.bar.TabText(3))
		}
		drag(2, 0)

		// Renaming a request keeps it in the set, and removing one takes
		// it out of every set.
		treeItem(tree, "Two", "C").SetText(colName, "Charlie")
	})
	waitFor(t, "the rename to be sorted", func() bool {
		return mainthread.Wait2(func() bool { return treeItem(w.requests.tree, "Two", "Charlie") != nil })
	})
	mainthread.Wait(func() {
		rt := w.requests
		rt.confirm = func(string) bool { return true }
		if got := setNames(); !reflect.DeepEqual(got, []string{`New Set["Charlie" "D"]`, `Work["B" "Charlie" "A"]`}) {
			t.Errorf("after the rename: %q", got)
		}
		rt.tree.SetCurrentItem(treeItem(rt.tree, "Two", "Charlie"))
		rt.remove()
		if got := setNames(); !reflect.DeepEqual(got, []string{`New Set["D"]`, `Work["B" "A"]`}) {
			t.Errorf("after the removal: %q", got)
		}
		// What was beside it is opened in the tab that is not pinned.
		check("after the removal", "B*", "A*", "D")

		// The search narrows the sets while they are shown.
		rt.search.SetText("wor")
		if !rt.sets.list.Item(0).IsHidden() || rt.sets.list.Item(1).IsHidden() {
			t.Error("the search does not narrow the sets")
		}
		rt.search.SetText("")
	})

	// The set chosen is chosen again when its workspace is gone back to.
	mainthread.Wait(func() {
		w.workspaces.warn = func(message string) { t.Error(message) }
		w.workspaces.add()
		if w.requests.sets.active != nil {
			t.Errorf("chosen in a new workspace: %+v", w.requests.sets.active)
		}
		w.workspaces.list.SetCurrentRow(0)
		if w.requests.sets.active == nil || w.requests.sets.active.Name != "Work" || w.requests.sets.selected() != w.requests.sets.active {
			t.Errorf("chosen back in the first workspace: %+v", w.requests.sets.active)
		}
		check("back in the first workspace", "B*", "A*")
	})

	// The sets are saved, but none is chosen at the next run, and nothing is
	// selected.
	saved, err := loadRequests(files.Requests)
	if err != nil || len(saved.Sets) != 2 || saved.Sets[1].Name != "Work" || len(saved.Sets[1].Requests) != 2 {
		t.Fatalf("saved %+v", saved.Sets)
	}
	mainthread.Wait(func() {
		w.Close()
		w = NewWindow(files.Files)
		if w.requests.sets.active != nil || w.requests.sets.selected() != nil || w.requests.tree.CurrentItem() != nil || w.open != nil {
			t.Errorf("at the next run: set %+v, open %+v", w.requests.sets.active, w.open)
		}
		check("next run", "Untitled")

		// Removing a set leaves the tabs alone, and none is chosen.
		w.requests.sets.list.SetCurrentItem(w.requests.sets.list.Item(1))
		check("Work chosen", "B*", "A*")
		w.requests.confirm = func(string) bool { return true }
		w.requests.sets.remove()
		if len(w.requests.store.Sets) != 1 || w.requests.sets.active != nil {
			t.Errorf("after removing: %q", setNames())
		}
		check("after removing", "B*", "A*")
	})
}

func TestSetButtons(t *testing.T) {
	files := testFiles(t)
	store := &collection.Store{}
	folder := store.AddFolder("One")
	store.AddSet("Work", []*collection.Request{
		folder.Add(collection.NewREST("A", rest.Request{Method: "GET", URL: "a.test"})),
		folder.Add(collection.NewREST("B", rest.Request{Method: "GET", URL: "b.test"})),
	})
	if err := store.Save(files.Requests); err != nil {
		t.Fatal(err)
	}

	mainthread.Wait(func() {
		w := NewWindow(files.Files)
		w.Show()
		rt := w.requests
		sets := rt.sets
		list := sets.list
		rt.showView(viewSets)

		// Duplicating the set chosen makes a copy holding the same
		// requests, which is chosen, with its name open for editing.
		list.SetCurrentItem(list.Item(0))
		sets.duplicate()
		if len(rt.store.Sets) != 2 || sets.active == nil || sets.active.Name != "Work copy" || !slices.Equal(sets.active.Requests, rt.store.FindSet("Work").Requests) {
			t.Errorf("after duplicating: active %+v, sets %d", sets.active, len(rt.store.Sets))
			return
		}
		if list.State() != qt.QAbstractItemView__EditingState {
			t.Error("the name of the copy is not open for editing")
		}
		if names := tabNames(w); !slices.Equal(names, []string{"A*", "B*"}) {
			t.Errorf("tabs %q", names)
		}
		// Resetting the view closes the editor.
		list.Reset()

		// Rename opens the name of the selected set; with none selected it
		// does nothing.
		list.SetCurrentItem(list.Item(0))
		sets.rename()
		if list.State() != qt.QAbstractItemView__EditingState {
			t.Error("rename did not open the name")
		}
		list.Reset()

		// Clicking where there is no set chooses none, leaving one empty
		// tab.
		viewport := list.Viewport()
		at := qt.NewQPointF3(5, float64(viewport.Height()-5))
		defer at.Delete()
		press := qt.NewQMouseEvent(qt.QEvent__MouseButtonPress, at, qt.LeftButton, qt.LeftButton, qt.NoModifier)
		defer press.Delete()
		qt.QCoreApplication_SendEvent(viewport.QObject, press.QEvent)
		if sets.active != nil || sets.selected() != nil || len(w.tabs.tabs) != 1 || w.open != nil {
			t.Errorf("after clicking empty space: set %+v, %d tabs", sets.active, len(w.tabs.tabs))
		}
		sets.rename()
		if list.State() == qt.QAbstractItemView__EditingState {
			t.Error("rename opened a name with no set selected")
		}

		// Return in the search goes to the first set found, without
		// choosing it.
		rt.search.SetText("copy")
		rt.browse()
		// A window that is not on a screen is not the active one, so it is
		// asked which of its widgets the keyboard goes to.
		focused := w.FocusWidget()
		if item := list.CurrentItem(); item == nil || item.Text() != "Work copy" || sets.active != nil || focused == nil || focused.UnsafePointer() != list.QWidget.UnsafePointer() {
			t.Errorf("browsing: current %v, active %+v", item, sets.active)
		}
	})
}

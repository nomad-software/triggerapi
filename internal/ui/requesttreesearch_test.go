package ui

import (
	"reflect"
	"testing"

	qt "github.com/mappu/miqt/qt6"
	"github.com/mappu/miqt/qt6/mainthread"

	"github.com/nomad-software/triggerapi/internal/collection"
	"github.com/nomad-software/triggerapi/internal/rest"
	"github.com/nomad-software/triggerapi/internal/rpc"
)

func TestSearchRequests(t *testing.T) {
	files := testFiles(t)

	store := &collection.Store{}
	hotel := store.AddFolder("Hotel")
	hotel.Add(collection.NewREST("Ping", rest.Request{Method: "GET"}))
	rooms := hotel.AddFolder("Rooms")
	rooms.Add(collection.NewREST("Book room", rest.Request{Method: "POST"}))
	rooms.Add(collection.NewREST("List rooms", rest.Request{Method: "GET"}))
	users := store.AddFolder("Users")
	users.Add(collection.NewREST("List users", rest.Request{Method: "GET"}))
	store.AddFolder("Empty")
	if err := store.Save(files.Requests); err != nil {
		t.Fatal(err)
	}

	mainthread.Wait(func() {
		w := NewWindow(files.Files)
		w.Show()
		rt := w.requests
		tree := rt.tree

		// shown lists the paths of the items that are not hidden.
		shown := func() []string {
			var paths []string
			var walk func(item *qt.QTreeWidgetItem, path string)
			walk = func(item *qt.QTreeWidgetItem, path string) {
				if item.IsHidden() {
					return
				}
				path += "/" + item.Text(colName)
				paths = append(paths, path)
				for i := range item.ChildCount() {
					walk(item.Child(i), path)
				}
			}
			for i := range topCount(tree) {
				walk(topFolder(tree, i), "")
			}
			return paths
		}
		check := func(query string, want ...string) {
			t.Helper()
			rt.search.SetText(query)
			if got := shown(); !reflect.DeepEqual(got, want) {
				t.Errorf("search %q shows %q, want %q", query, got, want)
			}
		}

		// Requests that match are shown with the folders they are in, which
		// are opened, whatever the case of the letters.
		check("LIST", "/Hotel", "/Hotel/Rooms", "/Hotel/Rooms/List rooms", "/Users", "/Users/List users")
		if !treeItem(tree, "Hotel", "Rooms").IsExpanded() || !treeItem(tree, "Users").IsExpanded() {
			t.Error("the folders of what was found are not open")
		}

		// Every word must be in the name of the request, in any order.
		check("list room", "/Hotel", "/Hotel/Rooms", "/Hotel/Rooms/List rooms")
		check("users list", "/Users", "/Users/List users")
		check("rooms", "/Hotel", "/Hotel/Rooms", "/Hotel/Rooms/List rooms")

		// The names of folders are not searched, so a folder is only shown
		// to hold what was found.
		check("hotel")
		check("rooms ping")
		check("empty")
		check("nothing like this")

		// What is found is still the request it was.
		rt.search.SetText("book")
		tree.SetCurrentItem(treeItem(tree, "Hotel", "Rooms", "Book room"))
		if w.open == nil || w.open.Name != "Book room" || w.request.method.CurrentText() != "POST" {
			t.Errorf("opened %+v", w.open)
		}

		// Clearing the search shows everything, with the folders open or
		// closed as they were, and the request still selected.
		check("", "/Hotel", "/Hotel/Rooms", "/Hotel/Rooms/Book room", "/Hotel/Rooms/List rooms", "/Hotel/Ping", "/Users", "/Users/List users", "/Empty")
		// The folders were closed, but those of the selected request are
		// left open to show it.
		if !treeItem(tree, "Hotel", "Rooms").IsExpanded() || treeItem(tree, "Users").IsExpanded() || !treeItem(tree, "Hotel").IsExpanded() {
			t.Error("the folders are not as they were")
		}
		if item := tree.CurrentItem(); item == nil || item.Text(colName) != "Book room" {
			t.Error("the request is no longer selected")
		}
		if users := rt.store.Folders[2]; users.Name != "Users" || users.Open {
			t.Error("the search changed which folders are closed")
		}

		// Adding something clears the search, so that it can be seen.
		rt.search.SetText("users")
		tree.SetCurrentItem(treeItem(tree, "Users"))
		rt.addRequest()
		if rt.search.Text() != "" || len(shown()) != 9 || tree.CurrentItem().Text(colName) != "New Request" {
			t.Errorf("after adding: search %q, shown %q", rt.search.Text(), shown())
		}
	})
}

// Ctrl+P goes to the search of the requests, Return goes on from it to the
// tree, where the keyboard moves about without opening anything, and Return
// there opens the request it has got to.
func TestQuickOpen(t *testing.T) {
	files := testFiles(t)
	store := collection.Store{Folders: []*collection.Folder{
		{Name: "Health", Requests: []*collection.Request{collection.NewREST("Ping", rest.Request{Method: "GET"})}},
		{Name: "Hotel",
			Folders: []*collection.Folder{{Name: "Rooms", Requests: []*collection.Request{
				collection.NewREST("Book room", rest.Request{Method: "POST"}),
				collection.NewREST("List rooms", rest.Request{Method: "GET"}),
			}}},
			Requests: []*collection.Request{collection.NewGRPC("Availability", rpc.Request{})},
		},
	}}
	if err := store.Save(files.Requests); err != nil {
		t.Fatal(err)
	}

	mainthread.Wait(func() {
		w := NewWindow(files.Files)
		w.Show()
		tree := w.requests.tree

		press := func(key qt.Key) {
			event := qt.NewQKeyEvent(qt.QEvent__KeyPress, int(key), qt.NoModifier)
			defer event.Delete()
			qt.QCoreApplication_SendEvent(tree.QObject, event.QEvent)
		}
		current := func() string {
			if item := tree.CurrentItem(); item != nil {
				return item.Text(colName)
			}
			return ""
		}
		opened := func() string {
			if w.open != nil {
				return w.open.Name
			}
			return ""
		}

		// The search is gone to from another tab of the sidebar.
		w.sidebar.SetCurrentIndex(tabEnvironments)
		w.quickOpen()
		if w.sidebar.CurrentIndex() != tabRequests {
			t.Error("the requests are not shown")
		}

		// Return goes to the first request that was found, which is not
		// opened by that.
		w.requests.search.SetText("room")
		w.requests.search.ReturnPressed()
		if current() != "Book room" || opened() != "" {
			t.Errorf("after the search: at %q, with %q open", current(), opened())
		}

		// Nor is anything opened by moving about.
		press(qt.Key_Down)
		if current() != "List rooms" || opened() != "" {
			t.Errorf("after down: at %q, with %q open", current(), opened())
		}
		press(qt.Key_Up)
		press(qt.Key_Up)
		if current() != "Rooms" || opened() != "" {
			t.Errorf("after up twice: at %q, with %q open", current(), opened())
		}

		// Left closes a folder and right opens it, as does Return.
		folder := tree.CurrentItem()
		press(qt.Key_Left)
		if folder.IsExpanded() {
			t.Error("left did not close the folder")
		}
		press(qt.Key_Right)
		if !folder.IsExpanded() {
			t.Error("right did not open the folder")
		}
		press(qt.Key_Return)
		if folder.IsExpanded() || opened() != "" {
			t.Errorf("return on a folder: open %v, with %q open", folder.IsExpanded(), opened())
		}
		press(qt.Key_Return)

		// Return opens the request, in the tab that is not pinned.
		press(qt.Key_Down)
		press(qt.Key_Down)
		press(qt.Key_Return)
		if current() != "List rooms" || opened() != "List rooms" || w.tabs.bar.TabText(w.tabs.bar.CurrentIndex()) != "List rooms" {
			t.Errorf("after return: at %q, with %q open", current(), opened())
		}
		w.tabs.toggle(w.tabs.current())

		// One that is pinned is gone to in its own tab.
		press(qt.Key_Up)
		press(qt.Key_Return)
		if opened() != "Book room" || len(w.tabs.tabs) != 2 {
			t.Errorf("another: %q open in %d tabs", opened(), len(w.tabs.tabs))
		}
		press(qt.Key_Down)
		if opened() != "Book room" {
			t.Errorf("moving back opened %q", opened())
		}
		press(qt.Key_Return)
		if opened() != "List rooms" || len(w.tabs.tabs) != 2 || w.tabs.bar.CurrentIndex() != 0 {
			t.Errorf("the pinned one: %q open in %d tabs, tab %d shown", opened(), len(w.tabs.tabs), w.tabs.bar.CurrentIndex())
		}

		// Without a search, Return in the box starts where the selection
		// is, or at the first item if there is none.
		w.requests.search.SetText("")
		w.requests.search.ReturnPressed()
		if current() != "List rooms" {
			t.Errorf("without a search: at %q", current())
		}
		w.requests.deselect()
		w.requests.search.ReturnPressed()
		if current() != "Health" {
			t.Errorf("with nothing selected: at %q", current())
		}

		// A click still opens a request.
		tree.SetCurrentItem(topFolder(tree, 0).Child(0))
		if opened() != "Ping" {
			t.Errorf("selected with the pointer: %q open", opened())
		}
	})
}

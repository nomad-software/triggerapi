package ui

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"testing"

	qt "github.com/mappu/miqt/qt6"
	"github.com/mappu/miqt/qt6/mainthread"

	"github.com/nomad-software/triggerapi/internal/collection"
	"github.com/nomad-software/triggerapi/internal/rest"
	"github.com/nomad-software/triggerapi/internal/rpc"
)

func TestPinnedRequests(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("pong"))
	}))
	defer srv.Close()

	files := testFiles(t)
	store := &collection.Store{}
	folder := store.AddFolder("Folder")
	folder.Add(collection.NewREST("A", rest.Request{Method: "GET", URL: "a.test"}))
	folder.Add(collection.NewREST("B", rest.Request{Method: "GET", URL: srv.URL}))
	folder.Add(collection.NewGRPC("C", rpc.Request{URL: "c.test:1"}))
	if err := store.Save(files.Requests); err != nil {
		t.Fatal(err)
	}

	var w *Window
	check := func(what string, current int, want ...string) {
		t.Helper()
		if got := tabNames(w); !reflect.DeepEqual(got, want) || w.tabs.bar.CurrentIndex() != current || w.tabs.bar.Count() != len(want)+1 {
			t.Errorf("%s: tabs = %q with %d shown, want %q with %d shown", what, got, w.tabs.bar.CurrentIndex(), want, current)
		}
	}

	mainthread.Wait(func() {
		w = NewWindow(files.Files)
		w.Show()
		tree := w.requests.tree
		pick := func(name string) { tree.SetCurrentItem(treeItem(tree, "Folder", name)) }

		// There is one tab to begin with, which has nothing to pin.
		check("at first", 0, "Untitled")
		if w.tabs.current().button.IsEnabled() {
			t.Error("a request that is not saved can be pinned")
		}

		// It shows whichever request is selected.
		pick("A")
		check("after selecting A", 0, "A")
		pick("B")
		check("after selecting B", 0, "B")
		if w.open == nil || w.open.Name != "B" || !w.tabs.current().button.IsEnabled() {
			t.Fatalf("open = %+v", w.open)
		}

		// Pinning it keeps B there, and the next request selected has a
		// new tab.
		w.tabs.current().button.Click()
		check("after pinning B", 0, "B*")
		pick("C")
		check("after selecting C", 1, "B*", "C")
		if w.open.Name != "C" || w.stack.CurrentIndex() != pageGRPC {
			t.Errorf("open = %+v on page %d", w.open, w.stack.CurrentIndex())
		}
		pick("A")
		check("after selecting A", 1, "B*", "A")

		// Selecting a pinned request goes to its tab.
		pick("B")
		check("after selecting B", 0, "B*", "A")
		if w.open.Name != "B" || w.request.url.Text() != srv.URL {
			t.Errorf("open = %+v", w.open)
		}

		// Choosing a tab shows its request, and selects it in the sidebar.
		w.tabs.bar.SetCurrentIndex(1)
		if w.open.Name != "A" || w.request.url.Text() != "a.test" || tree.CurrentItem().Text(colName) != "A" {
			t.Errorf("open = %+v, selected %q", w.open, tree.CurrentItem().Text(colName))
		}
		w.tabs.current().button.Click()
		pick("C")
		check("after pinning A and selecting C", 2, "B*", "A*", "C")

		// The button of a tab is a pin until the tab is pinned, and then
		// shows the type of its request, but for while the pointer is over
		// the tab, when it is a pin again to unpin it with.
		key := func(tab *requestTab) int64 { return tab.button.Icon().CacheKey() }
		pinned, preview := w.tabs.tabs[0], w.tabs.tabs[2]
		if key(preview) != w.tabs.unpinnedIcon.CacheKey() || key(pinned) != requestTypeIcon(collection.TypeREST).CacheKey() {
			t.Error("the buttons do not show a pin and the type of the request")
		}
		w.tabs.hover(pinned)
		if key(pinned) != w.tabs.pinnedIcon.CacheKey() || pinned.button.ToolTip() != "" || w.tabs.bar.TabToolTip(0) != "" {
			t.Error("the button of a pinned tab is not a pin under the pointer")
		}
		w.tabs.hover(preview)
		if key(pinned) != requestTypeIcon(collection.TypeREST).CacheKey() || key(preview) != w.tabs.unpinnedIcon.CacheKey() {
			t.Error("the button did not go back to the type of the request")
		}
		w.tabs.hover(nil)
		// The tabs have no icon of their own, and no line names the request
		// beneath them.
		if !w.tabs.bar.TabIcon(0).IsNull() {
			t.Error("a tab has an icon beside its button")
		}

		// Renaming a request renames its tab.
		treeItem(tree, "Folder", "A").SetText(colName, "Alpha")
		check("after renaming A", 2, "B*", "Alpha*", "C")

		// A request sent from a tab has its response there when the tab is
		// gone back to.
		w.tabs.bar.SetCurrentIndex(0)
		w.send()
	})

	waitFor(t, "the response", func() bool { return fieldValue(w.response, "Status") == "200 OK" })

	mainthread.Wait(func() {
		tree := w.requests.tree

		w.tabs.bar.SetCurrentIndex(1)
		if fieldValue(w.response, "Status") != "" || w.response.body.ToPlainText() != "" {
			t.Error("the response of one tab is shown in another")
		}
		w.tabs.bar.SetCurrentIndex(0)
		if fieldValue(w.response, "Status") != "200 OK" || w.response.body.ToPlainText() != "pong" {
			t.Errorf("response = %q %q", fieldValue(w.response, "Status"), w.response.body.ToPlainText())
		}

		// A click does not unpin a tab; it takes a double click, which
		// closes a tab that is not shown.
		w.tabs.tabs[1].button.Click()
		check("after clicking Alpha", 0, "B*", "Alpha*", "C")
		doubleClick(w.tabs.tabs[1].button)
		check("after unpinning Alpha", 0, "B*", "C")

		// Unpinning the tab that is shown makes it the one that follows
		// the selection, in place of the other.
		doubleClick(w.tabs.current().button)
		check("after unpinning B", 0, "B")
		if w.open.Name != "B" {
			t.Errorf("open = %+v", w.open)
		}
		tree.SetCurrentItem(treeItem(tree, "Folder", "C"))
		check("after selecting C", 0, "C")

		// B and C are left pinned. Double clicking a tab that is not
		// pinned pins it, and the second click does not unpin it again.
		doubleClick(w.tabs.current().button)
		tree.SetCurrentItem(treeItem(tree, "Folder", "B"))
		w.tabs.current().button.Click()
		check("after pinning C and B", 1, "C*", "B*")

		w.Close()
	})

	// Tabs that are not in a set are not opened again by the next run.
	mainthread.Wait(func() {
		w = NewWindow(files.Files)
		w.Show()
		tree := w.requests.tree
		w.requests.confirm = func(string) bool { return true }

		check("on the next run", 0, "Untitled")
		if w.open != nil {
			t.Errorf("open on the next run: %+v", w.open)
		}
		tree.SetCurrentItem(treeItem(tree, "Folder", "C"))
		w.tabs.current().button.Click()
		tree.SetCurrentItem(treeItem(tree, "Folder", "B"))
		w.tabs.current().button.Click()
		w.tabs.bar.SetCurrentIndex(0)
		check("pinned again", 0, "C*", "B*")
		if w.open == nil || w.open.Name != "C" || w.stack.CurrentIndex() != pageGRPC || tree.CurrentItem() == nil || tree.CurrentItem().Text(colName) != "C" {
			t.Fatalf("open = %+v on page %d", w.open, w.stack.CurrentIndex())
		}

		// The tabs are put away while an environment is edited.
		w.environments.add()
		if w.stack.CurrentIndex() != pageEditor || !w.tabs.bar.IsHidden() {
			t.Error("the tabs are shown above the environment editor")
		}
		w.closeEditor()
		if w.tabs.bar.IsHidden() {
			t.Error("the tabs did not come back")
		}

		// A request being composed is kept while another tab is shown.
		w.stash(w.tabs.current())
		w.showTab(w.tabs.open(nil), false)
		check("with a request that is not saved", 2, "C*", "B*", "Untitled")
		w.request.url.SetText("draft.test")
		w.tabs.bar.SetCurrentIndex(1)
		if w.open == nil || w.open.Name != "B" {
			t.Fatalf("open = %+v", w.open)
		}
		w.tabs.bar.SetCurrentIndex(2)
		if w.open != nil || w.request.url.Text() != "draft.test" {
			t.Errorf("open = %+v, url %q", w.open, w.request.url.Text())
		}

		// Removing a pinned request closes its tab.
		tree.SetCurrentItem(treeItem(tree, "Folder", "B"))
		check("after selecting B", 1, "C*", "B*", "Untitled")
		w.requests.remove()
		if got := tabNames(w); len(got) < 1 || got[0] != "C*" || slices.Contains(got, "B*") {
			t.Errorf("tabs after removing B = %q", got)
		}

		// Pinned tabs that are not in a set are not kept when the workspace
		// is left.
		w.workspaces.warn = func(message string) { t.Error(message) }
		w.workspaces.add()
		check("in a new workspace", 0, "Untitled")
		if w.open != nil {
			t.Errorf("open = %+v", w.open)
		}
		w.workspaces.list.SetCurrentRow(0)
		if got := tabNames(w); !reflect.DeepEqual(got, []string{"Untitled"}) || w.open != nil {
			t.Errorf("tabs back in the first workspace = %q, open %+v", got, w.open)
		}
	})
}

func TestNewTabAndSave(t *testing.T) {
	files := testFiles(t)

	mainthread.Wait(func() {
		w := NewWindow(files.Files)
		w.Show()
		click := func(index int) {
			centre := w.tabs.bar.TabRect(index).Center()
			at := qt.NewQPointF3(float64(centre.X()), float64(centre.Y()))
			for _, kind := range []qt.QEvent__Type{qt.QEvent__MouseButtonPress, qt.QEvent__MouseButtonRelease} {
				buttons := qt.LeftButton
				if kind == qt.QEvent__MouseButtonRelease {
					buttons = qt.NoButton
				}
				event := qt.NewQMouseEvent(kind, at, qt.LeftButton, buttons, qt.NoModifier)
				qt.QCoreApplication_SendEvent(w.tabs.bar.QObject, event.QEvent)
				event.Delete()
			}
		}

		// Save is beside Send, for the request that is not saved.
		if w.request.save.IsHidden() || w.request.save.Text() != "Save" {
			t.Error("Save is not there for the request that is not saved")
		}
		w.request.url.SetText("example.test/users?page=2")
		w.request.save.Click()
		if w.open == nil || w.open.Name != "New Request" || w.open.URL != "example.test/users" || len(w.open.Params) != 1 {
			t.Fatalf("saved %+v", w.open)
		}
		if got := tabNames(w); !reflect.DeepEqual(got, []string{"New Request"}) || !w.request.save.IsHidden() {
			t.Errorf("after saving: tabs %q, Save shown %t", got, !w.request.save.IsHidden())
		}

		// New is the last tab, and is never shown: clicking it shows a new
		// request of the type chosen, which is not saved until Save is
		// pressed.
		saved := w.open
		w.tabs.current().button.Click()
		if w.tabs.bar.Count() != 2 || w.tabs.bar.TabText(1) != "New" {
			t.Fatalf("tabs %d, last %q", w.tabs.bar.Count(), w.tabs.bar.TabText(1))
		}
		defer func(prev collection.Type) { testRequestType = prev }(testRequestType)
		testRequestType = ""
		click(w.tabs.newAt)
		if w.open != saved || w.tabs.bar.CurrentIndex() != 0 {
			t.Errorf("after deciding against a request: open %+v, tab %d", w.open, w.tabs.bar.CurrentIndex())
		}
		count := func() int {
			n := 0
			for _, f := range w.requests.store.Folders {
				n += len(f.All())
			}
			return n
		}
		testRequestType = collection.TypeGRPC
		click(w.tabs.newAt)
		if w.open != nil || w.stack.CurrentIndex() != pageGRPC || w.grpc.save.IsHidden() || count() != 1 {
			t.Errorf("after New: open %+v, page %d, %d saved", w.open, w.stack.CurrentIndex(), count())
			return
		}
		if got := tabNames(w); !reflect.DeepEqual(got, []string{"New Request*", "Untitled"}) || w.tabs.bar.TabText(2) != "New" {
			t.Errorf("after New: tabs %q", got)
		}

		// It is kept while another tab is shown.
		w.grpc.url.SetText("localhost:1")
		w.tabs.bar.SetCurrentIndex(0)
		if w.open != saved || w.stack.CurrentIndex() != pageRequest {
			t.Errorf("back on the saved tab: open %+v, page %d", w.open, w.stack.CurrentIndex())
		}
		w.tabs.bar.SetCurrentIndex(1)
		if w.open != nil || w.stack.CurrentIndex() != pageGRPC || w.grpc.url.Text() != "localhost:1" || count() != 1 {
			t.Errorf("back on the new tab: open %+v, page %d, address %q", w.open, w.stack.CurrentIndex(), w.grpc.url.Text())
		}

		// Save beside Invoke adds it to the sidebar.
		w.grpc.save.Click()
		if w.open == nil || w.open.Type != collection.TypeGRPC || w.open.GRPC.URL != "localhost:1" || count() != 2 || !w.grpc.save.IsHidden() {
			t.Errorf("after saving: open %+v, %d saved", w.open, count())
		}

		// A REST request from New is not saved either.
		testRequestType = collection.TypeREST
		click(w.tabs.newAt)
		if w.open != nil || w.stack.CurrentIndex() != pageRequest || w.request.url.Text() != "" || count() != 2 {
			t.Errorf("after New: open %+v, page %d, %d saved", w.open, w.stack.CurrentIndex(), count())
		}

		// Choosing New otherwise, as the wheel does, leaves the tab shown.
		w.tabs.bar.SetCurrentIndex(0)
		w.tabs.bar.SetCurrentIndex(w.tabs.newAt)
		if w.tabs.bar.CurrentIndex() != 0 || w.tabs.current() != w.tabs.tabs[0] {
			t.Errorf("New was shown: tab %d", w.tabs.bar.CurrentIndex())
		}
	})
}

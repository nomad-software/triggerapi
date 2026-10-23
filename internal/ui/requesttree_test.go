package ui

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	qt "github.com/mappu/miqt/qt6"
	"github.com/mappu/miqt/qt6/mainthread"

	"github.com/nomad-software/triggerapi/internal/collection"
	"github.com/nomad-software/triggerapi/internal/env"
	"github.com/nomad-software/triggerapi/internal/rest"
	"github.com/nomad-software/triggerapi/internal/rpc"
	"github.com/nomad-software/triggerapi/internal/workspace"
)

// renameCurrent gives the selected item of the request tree a name, and puts
// the tree back in order straight away, which otherwise happens a moment
// later. Items found before it must be found again.
func renameCurrent(rt *requestTree, name string) {
	rt.tree.CurrentItem().SetText(colName, name)
	rt.sort()
}

// headingGroup reports which heading an item of the top level is, by its
// name, if it is one.
func headingGroup(item *qt.QTreeWidgetItem) (collection.Type, bool) {
	if item.Parent() != nil || !item.Font(colName).Bold() {
		return "", false
	}
	for group, label := range groupLabels {
		if item.Text(colName) == label {
			return group, true
		}
	}
	return "", false
}

func TestRequests(t *testing.T) {
	files := testFiles(t)

	mainthread.Wait(func() {
		w := NewWindow(files.Files)
		w.Show()
		tree := w.requests.tree

		// Adding a request with nothing open saves what is being composed,
		// creating a folder to hold it.
		w.request.url.SetText("{{host}}/users")
		w.requests.addRequest()
		if topCount(tree) != 1 || topFolder(tree, 0).ChildCount() != 1 {
			t.Fatal("folder and request not created")
		}
		list := w.requests.store.Folders[0].Requests[0]
		if w.open != list || list.URL != "{{host}}/users" || tabTitle(w) != "New Request" {
			t.Errorf("open = %+v, title %q", w.open, tabTitle(w))
		}

		// Renaming updates the title.
		topFolder(tree, 0).SetText(0, "Api")
		topFolder(tree, 0).Child(0).SetText(0, "Fetch")
		if list.Name != "Fetch" || tabTitle(w) != "Fetch" || w.requests.store.Folders[0].Name != "Api" {
			t.Errorf("rename: %q, title %q", list.Name, tabTitle(w))
		}

		// Adding with a request open starts from an empty request in the
		// same folder.
		w.requests.addRequest()
		create := w.requests.store.Folders[0].Requests[1]
		if w.open != create || w.request.url.Text() != "" || topFolder(tree, 0).ChildCount() != 2 {
			t.Errorf("second request: open %+v, url %q", w.open, w.request.url.Text())
		}
		topFolder(tree, 0).Child(1).SetText(0, "Post")

		// Edits are written to the open request.
		w.request.method.SetCurrentIndex(1) // POST
		w.request.url.SetText("{{host}}/users")
		w.request.params.add("dry", "1")
		w.request.headers.add("X-Test", "yes")
		w.request.auth.typ.SetCurrentIndex(1) // Basic Auth
		w.request.auth.username.SetText("me")
		w.request.auth.password.SetText("pw")
		w.request.bodyType.SetCurrentIndex(1) // JSON
		w.request.bodyRaw.SetPlainText(`{"name":"a"}`)
		w.request.bodyForm.add("f", "1")

		// Switching between requests swaps the contents of the request UI.
		tree.SetCurrentItem(topFolder(tree, 0).Child(0))
		if w.open != list || w.request.method.CurrentText() != "GET" || w.request.bodyRaw.ToPlainText() != "" || len(w.request.headers.items()) != 0 || w.request.auth.typ.CurrentIndex() != 0 {
			t.Errorf("first request not shown: %+v", w.request.request())
		}
		if list.Method != "GET" || create.Method != "POST" {
			t.Errorf("switching modified a request: %q %q", list.Method, create.Method)
		}

		// Selecting a folder leaves the open request alone. With nothing
		// selected a folder is added at the top level.
		w.requests.deselect()
		w.requests.addFolder()
		if w.open != list || topCount(tree) != 2 {
			t.Errorf("open after adding a folder = %+v", w.open)
		}

		// Selecting a request closes the environment editor.
		w.environments.add()
		if w.stack.CurrentIndex() != pageEditor {
			t.Fatal("editor not open")
		}
		tree.SetCurrentItem(topFolder(tree, 0).Child(1))
		if w.stack.CurrentIndex() != pageRequest || w.open != create {
			t.Error("selecting a request did not return to the request UI")
		}
	})

	want := rest.Request{
		Method:   "POST",
		URL:      "{{host}}/users",
		Params:   []rest.KeyValue{{Key: "dry", Value: "1", Enabled: true}},
		Headers:  []rest.KeyValue{{Key: "X-Test", Value: "yes", Enabled: true}},
		Auth:     rest.Auth{Type: rest.AuthBasic, Username: "me", Password: "pw"},
		BodyType: rest.BodyJSON,
		Body:     `{"name":"a"}`,
		Form:     []rest.KeyValue{{Key: "f", Value: "1", Enabled: true}},
	}

	saved, err := loadRequests(files.Requests)
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.Folders) != 2 || saved.Folders[0].Name != "Api" || len(saved.Folders[0].Requests) != 2 {
		t.Fatalf("saved %+v", saved.Folders)
	}
	if got := saved.Folders[0].Requests[1]; got.Name != "Post" || !reflect.DeepEqual(*got.Request, want) {
		t.Errorf("saved request %+v, want %+v", got, want)
	}

	// A new window restores the tree, and a request loads back unchanged.
	mainthread.Wait(func() {
		w := NewWindow(files.Files)
		tree := w.requests.tree

		if topCount(tree) != 2 || topFolder(tree, 0).Text(0) != "Api" || topFolder(tree, 0).Child(1).Text(0) != "Post" {
			t.Fatal("tree not restored")
		}
		if w.open != nil || tabTitle(w) != untitled {
			t.Error("a request is open at startup")
		}

		tree.SetCurrentItem(topFolder(tree, 0).Child(1))
		if got := w.request.request(); !reflect.DeepEqual(got, want) {
			t.Errorf("loaded %+v, want %+v", got, want)
		}

		// A request is not removed unless the user agrees to it.
		var asked string
		w.requests.confirm = func(question string) bool { asked = question; return false }
		w.requests.remove()
		if folder := w.requests.store.Folders[0]; len(folder.Requests) != 2 || !strings.Contains(asked, "Remove the request") {
			t.Errorf("%d requests after being asked %q", len(folder.Requests), asked)
		}
		w.requests.confirm = func(string) bool { return true }

		// Removing the open request opens its neighbour.
		w.requests.remove()
		if folder := w.requests.store.Folders[0]; len(folder.Requests) != 1 || w.open != folder.Requests[0] || tabTitle(w) != "Fetch" {
			t.Errorf("after remove: open %+v", w.open)
		}

		// Removing a folder that has requests needs confirmation.
		tree.SetCurrentItem(topFolder(tree, 0))
		w.requests.confirm = func(string) bool { return false }
		w.requests.remove()
		if topCount(tree) != 2 {
			t.Error("folder removed without confirmation")
		}

		w.requests.confirm = func(string) bool { return true }
		w.requests.remove()
		if topCount(tree) != 1 || len(w.requests.store.Folders) != 1 {
			t.Error("folder not removed")
		}
		if w.open != nil || w.request.url.Text() != "" || tabTitle(w) != untitled {
			t.Errorf("removed request still open: %+v", w.open)
		}

		// An empty folder is removed without asking.
		w.requests.confirm = func(string) bool { t.Error("asked about an empty folder"); return false }
		tree.SetCurrentItem(topFolder(tree, 0))
		w.requests.remove()
		if len(w.requests.store.Folders) != 0 {
			t.Error("empty folder not removed")
		}
	})

	if saved, err := loadRequests(files.Requests); err != nil || len(saved.Folders) != 0 {
		t.Errorf("removals not saved: %+v, %v", saved, err)
	}
}

func TestRequestsUnreadableFile(t *testing.T) {
	files := testFiles(t)
	os.WriteFile(files.Requests, []byte("{oops"), 0o644)

	mainthread.Wait(func() {
		w := NewWindow(files.Files)
		if !strings.Contains(w.StatusBar().CurrentMessage(), "could not be loaded") {
			t.Error("no error shown")
		}
		w.requests.addRequest()
		w.environments.add()
	})

	writer.Flush()
	if data, _ := os.ReadFile(files.Requests); string(data) != "{oops" {
		t.Errorf("file was overwritten: %q", data)
	}
	if _, err := os.Stat(files.Environments); err != nil {
		t.Errorf("environments not saved: %v", err)
	}
}

func TestRequestReopened(t *testing.T) {
	mainthread.Wait(func() {
		w := NewWindow(testFiles(t).Files)
		w.Show()

		w.requests.addRequest()
		request := w.requests.store.Folders[0].Requests[0]

		// With the environment editor open, opening the request that is
		// already current in the tree returns to it.
		w.environments.add()
		if w.stack.CurrentIndex() != pageEditor {
			t.Fatal("editor not open")
		}
		w.requests.open()
		if w.stack.CurrentIndex() != pageRequest || w.open != request {
			t.Error("request not shown again")
		}
	})
}

func TestNestedFolders(t *testing.T) {
	files := testFiles(t)

	mainthread.Wait(func() {
		w := NewWindow(files.Files)
		w.Show()
		rt := w.requests
		tree := rt.tree

		// A folder is added at the top level when nothing is selected, and
		// inside the selected folder when one is.
		rt.addFolder()
		renameCurrent(rt, "em-ohip-hotel")
		tree.SetCurrentItem(treeItem(tree, "em-ohip-hotel"))

		rt.addFolder()
		renameCurrent(rt, "ohip_hotel")
		service := treeItem(tree, "em-ohip-hotel")
		if topCount(tree) != 1 || service.ChildCount() != 1 || !service.IsExpanded() || treeItem(tree, "em-ohip-hotel", "ohip_hotel") == nil {
			t.Fatalf("subfolder not added: %d top level, %d children", topCount(tree), service.ChildCount())
		}

		// A request goes in the selected folder, however deep.
		tree.SetCurrentItem(treeItem(tree, "em-ohip-hotel", "ohip_hotel"))
		addGRPCRequest(w)
		renameCurrent(rt, "GetHotel")
		folders := rt.store.Folders
		if len(folders) != 1 || len(folders[0].Folders) != 1 || len(folders[0].Folders[0].Requests) != 1 {
			t.Fatalf("store = %+v", folders[0])
		}
		get := folders[0].Folders[0].Requests[0]
		if w.open != get || get.Name != "GetHotel" || get.Type != collection.TypeGRPC {
			t.Errorf("open = %+v", w.open)
		}

		// With a request selected, a folder is added beside it, in the
		// folder that holds it, and listed before the requests.
		tree.SetCurrentItem(treeItem(tree, "em-ohip-hotel", "ohip_hotel", "GetHotel"))
		rt.addFolder()
		renameCurrent(rt, "admin")
		inner := treeItem(tree, "em-ohip-hotel", "ohip_hotel")
		if got, want := treeNames(inner), []string{"admin/", "GetHotel"}; !reflect.DeepEqual(got, want) {
			t.Errorf("children = %q, want %q", got, want)
		}

		// The request is still found now that a folder comes before it.
		tree.SetCurrentItem(treeItem(tree, "em-ohip-hotel", "ohip_hotel", "GetHotel"))
		if w.open != get {
			t.Errorf("open = %+v", w.open)
		}
		w.grpc.url.SetText("localhost:1")
		w.commit()
		if get.GRPC.URL != "localhost:1" {
			t.Errorf("edit went to %+v", get.GRPC)
		}

		// A REST request three folders down.
		tree.SetCurrentItem(treeItem(tree, "em-ohip-hotel", "ohip_hotel", "admin"))
		rt.addRequest()
		renameCurrent(rt, "Report")
		report := folders[0].Folders[0].Folders[0].Requests[0]
		if w.open != report || report.Name != "Report" || report.Type != collection.TypeREST || w.stack.CurrentIndex() != pageRequest {
			t.Errorf("open = %+v", w.open)
		}

		// A second top level folder needs the selection cleared first.
		rt.deselect()
		rt.addFolder()
		renameCurrent(rt, "other")
		if topCount(tree) != 2 || len(rt.store.Folders) != 2 {
			t.Errorf("top level folders = %d", topCount(tree))
		}

		// Renaming a folder inside another renames the right one.
		if folders[0].Folders[0].Name != "ohip_hotel" || folders[0].Folders[0].Folders[0].Name != "admin" {
			t.Errorf("names = %q, %q", folders[0].Folders[0].Name, folders[0].Folders[0].Folders[0].Name)
		}
	})

	saved, err := loadRequests(files.Requests)
	if err != nil {
		t.Fatal(err)
	}
	if got := saved.Folders[0].Folders[0]; got.Name != "ohip_hotel" || got.Requests[0].Name != "GetHotel" || got.Folders[0].Requests[0].Name != "Report" {
		t.Fatalf("saved %+v", got)
	}

	// A new window restores the whole tree.
	mainthread.Wait(func() {
		w := NewWindow(files.Files)
		w.Show()
		rt := w.requests
		tree := rt.tree

		if topCount(tree) != 2 || treeItem(tree, "em-ohip-hotel", "ohip_hotel", "admin", "Report") == nil || treeItem(tree, "em-ohip-hotel", "ohip_hotel", "GetHotel") == nil {
			t.Fatal("tree not restored")
		}

		tree.SetCurrentItem(treeItem(tree, "em-ohip-hotel", "ohip_hotel", "admin", "Report"))
		report := w.open
		if report == nil || report.Name != "Report" {
			t.Fatalf("open = %+v", report)
		}

		// Removing a folder asks first, saying what is in it.
		var asked string
		rt.confirm = func(question string) bool { asked = question; return false }
		tree.SetCurrentItem(treeItem(tree, "em-ohip-hotel", "ohip_hotel"))
		rt.remove()
		if !strings.Contains(asked, `"ohip_hotel"`) || !strings.Contains(asked, "1 folder and 2 requests") {
			t.Errorf("asked %q", asked)
		}
		if treeItem(tree, "em-ohip-hotel").ChildCount() != 1 || w.open != report {
			t.Error("folder removed without confirmation")
		}

		// It takes everything in it, and closes the request that was open.
		rt.confirm = func(string) bool { return true }
		rt.remove()
		if treeItem(tree, "em-ohip-hotel").ChildCount() != 0 || len(rt.store.Folders[0].Folders) != 0 || w.open != nil {
			t.Errorf("after remove: %d children, open %+v", treeItem(tree, "em-ohip-hotel").ChildCount(), w.open)
		}
	})

	if saved, err := loadRequests(files.Requests); err != nil || len(saved.Folders) != 2 || len(saved.Folders[0].Folders) != 0 {
		t.Errorf("removal not saved: %+v, %v", saved, err)
	}
}

func TestSidebarOrder(t *testing.T) {
	files := testFiles(t)

	var w *Window
	mainthread.Wait(func() {
		w = NewWindow(files.Files)
		w.Show()

		// Environments are listed alphabetically, whatever order they were
		// added in, and the one being edited stays selected as it moves.
		for _, name := range []string{"staging", "Local", "dev"} {
			w.environments.add()
			w.editor.name.SetText(name)
		}
		var names []string
		for i := range w.environments.list.Count() {
			names = append(names, w.environments.list.Item(i).Text())
		}
		if want := []string{"dev", "Local", "staging"}; !reflect.DeepEqual(names, want) {
			t.Errorf("environments = %q, want %q", names, want)
		}
		if current, _ := w.environments.store.Current(); current == nil || current.Name != "dev" || w.environments.list.CurrentRow() != 0 || w.editor.name.Text() != "dev" {
			t.Errorf("active = %+v, row %d", current, w.environments.list.CurrentRow())
		}

		// Renaming one moves it, and the header follows.
		w.editor.name.SetText("zulu")
		if w.environments.list.CurrentRow() != 2 || w.environments.list.Item(2).Text() != "zulu" || w.environment.environment.CurrentText() != "zulu" {
			t.Errorf("after rename: row %d, header %q", w.environments.list.CurrentRow(), w.environment.environment.CurrentText())
		}
		w.editor.close.Click()

		// Folders and requests are added in their place in the order.
		rt := w.requests
		rt.addFolder()
		renameCurrent(rt, "zebra")
		rt.deselect()
		rt.addFolder()
		renameCurrent(rt, "Apple")
		if topFolder(rt.tree, 0).Text(colName) != "Apple" || topFolder(rt.tree, 1).Text(colName) != "zebra" {
			t.Errorf("folders = %q, %q", topFolder(rt.tree, 0).Text(colName), topFolder(rt.tree, 1).Text(colName))
		}

		rt.tree.SetCurrentItem(treeItem(rt.tree, "Apple"))
		for _, name := range []string{"update", "Create", "list"} {
			rt.addRequest()
			renameCurrent(rt, name)
			rt.tree.SetCurrentItem(treeItem(rt.tree, "Apple"))
		}
		rt.addFolder()
		renameCurrent(rt, "inner")
		if got, want := treeNames(treeItem(rt.tree, "Apple")), []string{"inner/", "Create", "list", "update"}; !reflect.DeepEqual(got, want) {
			t.Errorf("Apple = %q, want %q", got, want)
		}

		// A rename made in the tree is put in order a moment later, once
		// the tree has finished with the edit.
		rt.tree.SetCurrentItem(treeItem(rt.tree, "Apple", "Create"))
		rt.tree.CurrentItem().SetText(colName, "wipe")
	})

	waitFor(t, "the tree to be put in order", func() bool {
		return reflect.DeepEqual(treeNames(treeItem(w.requests.tree, "Apple")), []string{"inner/", "list", "update", "wipe"})
	})

	mainthread.Wait(func() {
		// The renamed request is still the selected and open one.
		if w.requests.tree.CurrentItem().Text(colName) != "wipe" || w.open == nil || w.open.Name != "wipe" {
			t.Errorf("selected %q, open %+v", w.requests.tree.CurrentItem().Text(colName), w.open)
		}
	})

	// Both files are saved in the order shown.
	envs, _ := loadEnvs(files.Environments)
	if len(envs.Environments) != 3 || envs.Environments[0].Name != "Local" || envs.Environments[2].Name != "zulu" || envs.Active != 2 {
		t.Errorf("saved environments = %+v", envs)
	}
	requests, _ := loadRequests(files.Requests)
	if requests.Folders[0].Name != "Apple" || requests.Folders[0].Requests[2].Name != "wipe" {
		t.Errorf("saved requests = %+v", requests.Folders[0])
	}
}

func TestFoldersStartClosed(t *testing.T) {
	files := testFiles(t)

	// A file from when whether folders were open was saved.
	old := `{"folders":[{"name":"a","folders":[{"name":"inner","requests":[{"name":"r","method":"GET"}]}]},{"name":"b","collapsed":true}]}`
	if err := os.WriteFile(files.Requests, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}

	for run := range 2 {
		mainthread.Wait(func() {
			w := NewWindow(files.Files)
			w.Show()
			tree := w.requests.tree

			// Every folder is closed at startup, however it was left.
			if treeItem(tree, "a").IsExpanded() || treeItem(tree, "a", "inner").IsExpanded() || treeItem(tree, "b").IsExpanded() {
				t.Errorf("run %d: folders do not start closed", run)
			}

			// Folders opened stay open while the tree is drawn again.
			treeItem(tree, "a").SetExpanded(true)
			w.requests.draw(nil)
			if !treeItem(tree, "a").IsExpanded() || treeItem(tree, "a", "inner").IsExpanded() {
				t.Errorf("run %d: folders not kept open within the run", run)
			}

			// Adding to a closed folder opens it.
			tree.SetCurrentItem(treeItem(tree, "a", "inner"))
			w.requests.addRequest()
			if !treeItem(tree, "a", "inner").IsExpanded() {
				t.Errorf("run %d: folder not opened for the request added to it", run)
			}
			w.Close()
		})
	}

	// Whether a folder is open is never saved.
	writer.Flush()
	if data, err := os.ReadFile(files.Requests); err != nil {
		t.Fatal(err)
	} else if saved, err := loadRequests(files.Requests); err != nil || len(saved.Folders) != 2 || strings.Contains(string(data), "collapsed") {
		t.Errorf("saved %+v, %v", saved, err)
	}
}

func TestOpenedForTab(t *testing.T) {
	files := testFiles(t)
	store := &collection.Store{}
	store.AddFolder("a").AddFolder("inner").Add(collection.NewREST("r", rest.Request{Method: "GET"}))
	store.AddFolder("b").Add(collection.NewREST("s", rest.Request{Method: "GET"}))
	if err := store.Save(files.Requests); err != nil {
		t.Fatal(err)
	}

	mainthread.Wait(func() {
		w := NewWindow(files.Files)
		w.Show()
		tree := w.requests.tree

		// Choosing the tab of a request in closed folders opens them, to
		// show it selected.
		treeItem(tree, "a").SetExpanded(true)
		treeItem(tree, "a", "inner").SetExpanded(true)
		tree.SetCurrentItem(treeItem(tree, "a", "inner", "r"))
		w.tabs.current().button.Click()
		treeItem(tree, "a", "inner").SetExpanded(false)
		treeItem(tree, "a").SetExpanded(false)
		treeItem(tree, "b").SetExpanded(true)
		tree.SetCurrentItem(treeItem(tree, "b", "s"))
		w.tabs.bar.SetCurrentIndex(0)
		if !treeItem(tree, "a").IsExpanded() || !treeItem(tree, "a", "inner").IsExpanded() || tree.CurrentItem().Text(colName) != "r" {
			t.Error("the folders of the request of the tab were not opened")
		}
	})
}

func TestRequestHeadings(t *testing.T) {
	files := testFiles(t)

	store := &collection.Store{}
	store.AddFolder("Empty")
	hotel := store.AddFolder("Hotel")
	hotel.Add(collection.NewREST("Ping", rest.Request{Method: "GET"}))
	hotel.AddFolder("Rooms").Add(collection.NewGRPC("Book", rpc.Request{}))
	store.AddFolder("Service").Add(collection.NewGRPC("Get", rpc.Request{}))
	store.AddFolder("Users").Add(collection.NewREST("List", rest.Request{Method: "GET"}))
	if err := store.Save(files.Requests); err != nil {
		t.Fatal(err)
	}

	mainthread.Wait(func() {
		w := NewWindow(files.Files)
		w.Show()
		rt := w.requests
		tree := rt.tree

		// layout lists the top level, with what is under each heading.
		layout := func() []string {
			var got []string
			for i := range tree.TopLevelItemCount() {
				item := tree.TopLevelItem(i)
				if _, ok := headingGroup(item); !ok {
					got = append(got, item.Text(colName))
					continue
				}
				names := make([]string, item.ChildCount())
				for j := range names {
					names[j] = item.Child(j).Text(colName)
				}
				got = append(got, item.Text(colName)+": "+strings.Join(names, ", "))
			}
			return got
		}
		want := []string{"gRPC: Service", "REST: Users", "Mixed: Hotel", "Empty"}
		if got := layout(); !reflect.DeepEqual(got, want) {
			t.Errorf("layout = %q, want %q", got, want)
		}

		// The headings have the icons of their types, and Mixed that of a
		// folder.
		for i, icon := range []*qt.QIcon{requestTypeIcon(collection.TypeGRPC), requestTypeIcon(collection.TypeREST), folderIcon()} {
			if got := tree.TopLevelItem(i).Icon(colName); got.CacheKey() != icon.CacheKey() {
				t.Errorf("heading %d has the wrong icon", i)
			}
		}

		// A heading is not a folder: selecting it opens nothing, and it
		// cannot be renamed, duplicated or removed.
		w.requests.confirm = func(string) bool { t.Error("asked about removing a heading"); return true }
		tree.SetCurrentItem(tree.TopLevelItem(0))
		rt.duplicate()
		rt.remove()
		if w.open != nil || rt.selection() != nil || len(rt.store.Folders) != 4 {
			t.Errorf("heading acted on: open %+v, %d folders", w.open, len(rt.store.Folders))
		}

		// A heading cannot be closed, and has no arrow to close it with.
		heading := tree.TopLevelItem(1)
		heading.SetExpanded(false)
		if !heading.IsExpanded() || tree.RootIsDecorated() {
			t.Error("heading closed like a folder")
		}

		// The keyboard passes over a heading: down from the last request
		// under gRPC goes to the first folder under REST, and up back.
		tree.SetFocus()
		tree.SetCurrentItem(treeItem(tree, "Service", "Get"))
		keyTree := func(key qt.Key) {
			event := qt.NewQKeyEvent(qt.QEvent__KeyPress, int(key), qt.NoModifier)
			defer event.Delete()
			qt.QCoreApplication_SendEvent(tree.QObject, event.QEvent)
		}
		keyTree(qt.Key_Down)
		if item := tree.CurrentItem(); item == nil || item.Text(colName) != "Users" {
			t.Errorf("down went to %q", item.Text(colName))
		}
		keyTree(qt.Key_Up)
		if item := tree.CurrentItem(); item == nil || item.Text(colName) != "Get" {
			t.Errorf("up went to %q", item.Text(colName))
		}

		// Requests have no tooltip.
		if tip := treeItem(tree, "Users", "List").ToolTip(colName); tip != "" {
			t.Errorf("request tooltip %q", tip)
		}

		// Removing the gRPC request from Hotel moves it under REST.
		w.requests.confirm = func(string) bool { return true }
		tree.SetCurrentItem(treeItem(tree, "Hotel", "Rooms", "Book"))
		rt.remove()
		want = []string{"gRPC: Service", "REST: Hotel, Users", "Empty"}
		if got := layout(); !reflect.DeepEqual(got, want) {
			t.Errorf("after remove: layout = %q, want %q", got, want)
		}
		if item := tree.CurrentItem(); item == nil || item.Text(colName) != "Rooms" {
			t.Error("the folder of the removed request is not selected")
		}

		// A request added to the empty folder takes it under a heading.
		testRequestType = collection.TypeGRPC
		defer func() { testRequestType = collection.TypeREST }()
		tree.SetCurrentItem(treeItem(tree, "Empty"))
		rt.addRequest()
		want = []string{"gRPC: Empty, Service", "REST: Hotel, Users"}
		if got := layout(); !reflect.DeepEqual(got, want) {
			t.Errorf("after add: layout = %q, want %q", got, want)
		}
	})

	// The headings are not saved.
	saved, err := loadRequests(files.Requests)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, f := range saved.Folders {
		names = append(names, f.Name)
	}
	if want := []string{"Empty", "Hotel", "Service", "Users"}; !reflect.DeepEqual(names, want) {
		t.Errorf("saved folders %q, want %q", names, want)
	}
}

func TestAddRequestWithNothingSelected(t *testing.T) {
	files := testFiles(t)

	store := &collection.Store{}
	store.AddFolder("Zoo").Add(collection.NewREST("Ping", rest.Request{Method: "GET"}))
	if err := store.Save(files.Requests); err != nil {
		t.Fatal(err)
	}

	mainthread.Wait(func() {
		w := NewWindow(files.Files)
		w.Show()
		rt := w.requests

		// A new folder is made to hold it, even though there is one already.
		rt.deselect()
		rt.addRequest()
		if len(rt.store.Folders) != 2 || rt.store.Folders[0].Name != "New Folder" || len(rt.store.Folders[0].Requests) != 1 ||
			len(rt.store.Folders[1].Requests) != 1 || w.open != rt.store.Folders[0].Requests[0] {
			t.Errorf("folders %+v, open %+v", rt.store.Folders, w.open)
		}
	})
}

func TestDuplicate(t *testing.T) {
	files := testFiles(t)

	envs := &env.Store{Active: 0, Environments: []env.Environment{{Name: "Local", Variables: []env.Variable{{Key: "host", Value: "local.test", Enabled: true}}}}}
	if err := envs.Save(files.Environments); err != nil {
		t.Fatal(err)
	}
	store := &collection.Store{}
	hotel := store.AddFolder("Hotel")
	hotel.Add(collection.NewREST("Book", rest.Request{Method: "POST", URL: "{{host}}/rooms", Headers: []rest.KeyValue{{Key: "X-Key", Value: "abc", Enabled: true}}}))
	hotel.AddFolder("Inner").Add(collection.NewGRPC("Call", rpc.Request{URL: "c.test:1"}))
	if err := store.Save(files.Requests); err != nil {
		t.Fatal(err)
	}

	mainthread.Wait(func() {
		w := NewWindow(files.Files)
		w.Show()
		tree := w.requests.tree

		// A copy of a request is beside it, is opened, and is its own.
		tree.SetCurrentItem(treeItem(tree, "Hotel", "Book"))
		original := w.open
		w.requests.duplicate()
		if got, want := treeNames(treeItem(tree, "Hotel")), []string{"Inner/", "Book", "Book copy"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("hotel = %q, want %q", got, want)
		}
		if w.open == nil || w.open == original || w.open.Name != "Book copy" || w.request.url.Text() != "{{host}}/rooms" || tree.CurrentItem().Text(colName) != "Book copy" {
			t.Fatalf("open = %+v", w.open)
		}
		w.request.url.SetText("{{host}}/changed")
		if original.URL != "{{host}}/rooms" || len(original.Headers) != 1 {
			t.Errorf("the request copied from was changed: %+v", original.Request)
		}

		// A copy of a folder has a copy of everything in it.
		tree.SetCurrentItem(treeItem(tree, "Hotel"))
		w.requests.duplicate()
		if topCount(tree) != 2 || tree.CurrentItem().Text(colName) != "Hotel copy" {
			t.Fatalf("%d folders, selected %q", topCount(tree), tree.CurrentItem().Text(colName))
		}
		if got, want := treeNames(treeItem(tree, "Hotel copy")), []string{"Inner/", "Book", "Book copy"}; !reflect.DeepEqual(got, want) {
			t.Errorf("copy = %q, want %q", got, want)
		}
		if w.requests.store.Folders[1].Folders[0].Requests[0] == w.requests.store.Folders[0].Folders[0].Requests[0] {
			t.Error("the copy shares a request with the folder it is a copy of")
		}

		// Nothing is duplicated with nothing selected.
		w.requests.deselect()
		w.requests.duplicate()
		if topCount(tree) != 2 {
			t.Error("duplicated with nothing selected")
		}

		// A copy of an environment has its variables, and is the one
		// selected and edited.
		w.environments.duplicate()
		list := w.environments.list
		if list.Count() != 2 || list.CurrentRow() != 1 || list.Item(1).Text() != "Local copy" || w.stack.CurrentIndex() != pageEditor || w.editor.name.Text() != "Local copy" {
			t.Fatalf("%d environments, row %d, editing %q", list.Count(), list.CurrentRow(), w.editor.name.Text())
		}
		w.editor.variables.add("token", "t")
		w.closeEditor()
		if all := w.environments.store.Environments; len(all[0].Variables) != 1 || len(all[1].Variables) != 2 || w.expand("{{host}}") != "local.test" {
			t.Errorf("environments = %+v", all)
		}

		// A copy of a workspace has its environments and requests, and is
		// the one in use.
		w.workspaces.warn = func(message string) { t.Error(message) }
		w.workspaces.duplicate()
		if got, want := w.workspaces.names, []string{"Default", "Default copy"}; !reflect.DeepEqual(got, want) || w.workspaces.current() != "Default copy" {
			t.Fatalf("workspaces = %q, current %q", got, w.workspaces.current())
		}
		if w.environments.list.Count() != 2 || topCount(w.requests.tree) != 2 || w.requests.path != workspace.Requests(files.Workspaces, "Default copy") {
			t.Errorf("copy has %d environments and %d folders, saved to %q", w.environments.list.Count(), topCount(w.requests.tree), w.requests.path)
		}

		// Changing the copy leaves the workspace it is a copy of alone.
		w.requests.confirm = func(string) bool { return true }
		w.requests.tree.SetCurrentItem(treeItem(w.requests.tree, "Hotel copy"))
		w.requests.remove()
	})

	if original, err := loadRequests(files.Requests); err != nil || len(original.Folders) != 2 {
		t.Errorf("requests of the first workspace = %+v, %v", original, err)
	}
	if copied, err := loadRequests(workspace.Requests(files.Workspaces, "Default copy")); err != nil || len(copied.Folders) != 1 {
		t.Errorf("requests of the copy = %+v, %v", copied, err)
	}
}

// A request, a folder and an environment cannot be given an empty name: one
// that is emptied keeps the name it had.
func TestNamesCannotBeEmpty(t *testing.T) {
	files := testFiles(t)

	mainthread.Wait(func() {
		w := NewWindow(files.Files)
		w.Show()

		w.requests.addFolder()
		folderItem := topFolder(w.requests.tree, 0)
		folderItem.SetText(colName, "Hotel")
		w.requests.addRequest()
		request := w.open
		requestItem := topFolder(w.requests.tree, 0).Child(0)
		requestItem.SetText(colName, "Book room")

		for _, blank := range []string{"", "   ", "\t"} {
			topFolder(w.requests.tree, 0).SetText(colName, blank)
			topFolder(w.requests.tree, 0).Child(0).SetText(colName, blank)
			folder := w.requests.store.Folders[0]
			if folder.Name != "Hotel" || request.Name != "Book room" {
				t.Errorf("after %q: folder %q, request %q", blank, folder.Name, request.Name)
			}
			if got := topFolder(w.requests.tree, 0).Text(colName); got != "Hotel" {
				t.Errorf("after %q the folder is shown as %q", blank, got)
			}
			if got := topFolder(w.requests.tree, 0).Child(0).Text(colName); got != "Book room" {
				t.Errorf("after %q the request is shown as %q", blank, got)
			}
		}
		if got := w.tabs.bar.TabText(0); got != "Book room" {
			t.Errorf("the tab is named %q", got)
		}
		if saved, err := loadRequests(files.Requests); err != nil || saved.Folders[0].Name != "Hotel" || saved.Folders[0].Requests[0].Name != "Book room" {
			t.Errorf("saved %+v, %v", saved, err)
		}

		// The name of an environment can be emptied on the way to typing
		// another, which leaves it with the name it had.
		w.environments.add()
		w.editor.name.SetText("Local")
		w.editor.variables.add("host", "example.com")
		w.editor.name.SetText("")
		if got := w.environments.store.Environments[0].Name; got != "Local" || w.environments.list.Item(0).Text() != "Local" {
			t.Errorf("emptied: the environment is named %q", got)
		}
		// Its variables are still kept while the field is empty.
		w.editor.variables.add("port", "80")
		if e := w.environments.store.Environments[0]; e.Name != "Local" || len(e.Variables) != 2 {
			t.Errorf("edited while empty: %+v", e)
		}
		w.editor.name.SetText("  ")
		w.editor.name.EditingFinished()
		if got := w.editor.name.Text(); got != "Local" || w.environments.store.Environments[0].Name != "Local" {
			t.Errorf("left empty: the field has %q", got)
		}
		w.editor.name.SetText("L")
		if got := w.environments.store.Environments[0].Name; got != "L" {
			t.Errorf("typed: the environment is named %q", got)
		}
		if saved, err := loadEnvs(files.Environments); err != nil || saved.Environments[0].Name != "L" {
			t.Errorf("saved %+v, %v", saved, err)
		}
	})
}

func TestF2Renames(t *testing.T) {
	files := testFiles(t)
	f2 := func(widget *qt.QWidget) {
		event := qt.NewQKeyEvent(qt.QEvent__KeyPress, int(qt.Key_F2), qt.NoModifier)
		defer event.Delete()
		qt.QCoreApplication_SendEvent(widget.QObject, event.QEvent)
	}

	mainthread.Wait(func() {
		w := NewWindow(files.Files)
		w.Show()

		// A workspace and a folder have their names opened for editing.
		w.sidebar.SetCurrentIndex(tabWorkspaces)
		f2(w.workspaces.list.QWidget)
		if w.workspaces.list.State() != qt.QAbstractItemView__EditingState {
			t.Error("F2 did not start renaming the workspace")
		}

		w.requests.addFolder()
		w.requests.tree.SetFocus()
		f2(w.requests.tree.QWidget)
		if w.requests.tree.State() != qt.QAbstractItemView__EditingState {
			t.Error("F2 did not start renaming the folder")
		}

		// An environment is opened in the editor, with the keyboard in its
		// name and the name selected.
		w.environments.add()
		w.closeEditor()
		w.sidebar.SetCurrentIndex(tabEnvironments)
		w.environments.list.SetFocus()
		f2(w.environments.list.QWidget)
		if w.stack.CurrentIndex() != pageEditor {
			t.Error("F2 did not open the environment editor")
		}
		// A window that is not on a screen is not the active one, so it is
		// asked which of its widgets the keyboard goes to.
		focused := w.FocusWidget()
		if focused == nil || focused.UnsafePointer() != w.editor.name.QWidget.UnsafePointer() {
			t.Error("the name does not have the keyboard")
		}
		if w.editor.name.SelectedText() != w.editor.name.Text() || w.editor.name.Text() == "" {
			t.Errorf("the name is not selected: %q of %q", w.editor.name.SelectedText(), w.editor.name.Text())
		}
		w.Close()
	})
}

func TestCopyPasteAcrossWorkspaces(t *testing.T) {
	files := testFiles(t)
	envs := &env.Store{Active: 0, Environments: []env.Environment{{Name: "Local", Variables: []env.Variable{{Key: "token", Value: "secret", Enabled: true}}}}}
	if err := envs.Save(files.Environments); err != nil {
		t.Fatal(err)
	}
	store := &collection.Store{}
	hotel := store.AddFolder("Hotel")
	hotel.Add(collection.NewREST("Book", rest.Request{Method: "POST", URL: "{{host}}/rooms"}))
	hotel.AddFolder("Inner").Add(collection.NewGRPC("Call", rpc.Request{URL: "c.test:1"}))
	if err := store.Save(files.Requests); err != nil {
		t.Fatal(err)
	}

	mainthread.Wait(func() {
		w := NewWindow(files.Files)
		w.Show()
		rt, el := w.requests, w.environments
		tree := rt.tree
		key := func(widget *qt.QWidget, k qt.Key) {
			event := qt.NewQKeyEvent(qt.QEvent__KeyPress, int(k), qt.ControlModifier)
			defer event.Delete()
			qt.QCoreApplication_SendEvent(widget.QObject, event.QEvent)
		}
		clipboard := qt.QGuiApplication_Clipboard().Text()

		// Copy a request, a folder and an environment in one workspace.
		tree.SetCurrentItem(treeItem(tree, "Hotel", "Book"))
		key(tree.QWidget, qt.Key_C)
		book := rt.copied
		tree.SetCurrentItem(treeItem(tree, "Hotel", "Inner"))
		key(tree.QWidget, qt.Key_C)
		el.list.SetCurrentRow(0)
		key(el.list.QWidget, qt.Key_C)
		if _, ok := rt.copied.(*collection.Folder); !ok || book == nil || el.copied == nil || el.copied.Name != "Local" {
			t.Fatalf("copied %+v and %+v", rt.copied, el.copied)
		}
		if qt.QGuiApplication_Clipboard().Text() != clipboard {
			t.Error("the system clipboard was used")
		}

		// Paste them into another.
		w.workspaces.warn = func(message string) { t.Error(message) }
		w.workspaces.add()
		if topCount(tree) != 0 || el.list.Count() != 0 {
			t.Fatalf("the new workspace is not empty")
		}
		key(tree.QWidget, qt.Key_V)
		if topCount(tree) != 1 || treeItem(tree, "Inner", "Call") == nil {
			t.Errorf("folder not pasted at the top level")
		}
		rt.copied = book
		tree.SetCurrentItem(treeItem(tree, "Inner"))
		key(tree.QWidget, qt.Key_V)
		key(tree.QWidget, qt.Key_V)
		if treeItem(tree, "Inner", "Book") == nil || treeItem(tree, "Inner", "Book copy") == nil || w.open == nil || w.open.Name != "Book copy" {
			t.Errorf("requests not pasted into the folder; open %+v", w.open)
		}
		key(el.list.QWidget, qt.Key_V)
		key(el.list.QWidget, qt.Key_V)
		if el.list.Count() != 2 || el.list.Item(0).Text() != "Local" || el.list.Item(1).Text() != "Local copy" || w.expand("{{token}}") != "secret" {
			t.Errorf("environments %d, token %q", el.list.Count(), w.expand("{{token}}"))
		}
		w.Close()
	})

	// The workspace pasted into has them saved, and the one copied from is
	// as it was.
	saved, err := loadRequests(workspace.Requests(filepath.Dir(filepath.Dir(files.Requests)), "New Workspace"))
	if err != nil || len(saved.Folders) != 1 || len(saved.Folders[0].Requests) != 3 || saved.Folders[0].Requests[0].Name != "Book" || saved.Folders[0].Requests[0].URL != "{{host}}/rooms" {
		t.Errorf("saved %+v, %v", saved, err)
	}
	original, err := loadRequests(files.Requests)
	if err != nil || len(original.Folders) != 1 || len(original.Folders[0].Folders[0].Requests) != 1 {
		t.Errorf("original %+v, %v", original, err)
	}
}

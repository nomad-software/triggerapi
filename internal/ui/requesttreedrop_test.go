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

func TestDragAndDrop(t *testing.T) {
	files := testFiles(t)

	// other/
	// service/
	//   rest/
	//   rpc/
	//     Get
	//     List
	//   Health
	store := &collection.Store{}
	service := store.AddFolder("service")
	rpcFolder := service.AddFolder("rpc")
	rpcFolder.Add(collection.NewGRPC("Get", rpc.Request{}))
	rpcFolder.Add(collection.NewGRPC("List", rpc.Request{}))
	service.AddFolder("rest")
	service.Add(collection.NewREST("Health", rest.Request{Method: "GET"}))
	store.AddFolder("other")
	if err := store.Save(files.Requests); err != nil {
		t.Fatal(err)
	}

	mainthread.Wait(func() {
		w := NewWindow(files.Files)
		w.Show()
		rt := w.requests
		tree := rt.tree

		// The folders and requests of the window are its own copies, kept
		// in alphabetical order.
		other, service := rt.store.Folders[0], rt.store.Folders[1]
		restFolder, rpcFolder := service.Folders[0], service.Folders[1]
		get, list, health := rpcFolder.Requests[0], rpcFolder.Requests[1], service.Requests[0]
		if other.Name != "other" || restFolder.Name != "rest" || get.Name != "Get" || health.Name != "Health" {
			t.Fatalf("store is not as expected: %q %q %q %q", other.Name, restFolder.Name, get.Name, health.Name)
		}

		check := func(what string, item *qt.QTreeWidgetItem, want ...string) {
			t.Helper()
			if item == nil {
				t.Errorf("%s: no such item", what)
				return
			}
			if got := treeNames(item); !reflect.DeepEqual(got, want) {
				t.Errorf("%s: %q, want %q", what, got, want)
			}
		}

		// Nothing can be dropped onto a request, only beside it.
		if treeItem(tree, "service", "Health").Flags()&qt.ItemIsDropEnabled != 0 || treeItem(tree, "service").Flags()&qt.ItemIsDropEnabled == 0 {
			t.Error("drop flags are wrong")
		}

		// Open a request, and close a folder, to see that both survive.
		tree.SetCurrentItem(treeItem(tree, "service", "Health"))
		if w.open != health {
			t.Fatalf("open = %+v", w.open)
		}
		treeItem(tree, "other").SetExpanded(false)

		// A request dropped on a folder goes inside it, in its place in the
		// alphabetical order.
		if !rt.drop(nil, health, rpcFolder, nil, dropOn) {
			t.Fatal("request not moved")
		}
		check("service", treeItem(tree, "service"), "rest/", "rpc/")
		check("rpc", treeItem(tree, "service", "rpc"), "Get", "Health", "List")
		if tree.CurrentItem().Text(colName) != "Health" || w.open != health {
			t.Errorf("selected %q, open %+v", tree.CurrentItem().Text(colName), w.open)
		}
		if treeItem(tree, "other").IsExpanded() || !treeItem(tree, "service").IsExpanded() {
			t.Error("folders did not stay open or closed")
		}

		// Dropping it beside another request in the same folder leaves the
		// order alphabetical.
		rt.drop(nil, health, rpcFolder, get, dropAbove)
		check("rpc", treeItem(tree, "service", "rpc"), "Get", "Health", "List")

		// Dropped beside a folder it goes in the folder that holds that one,
		// with the requests, which come after the folders.
		rt.drop(nil, list, restFolder, nil, dropAbove)
		check("service", treeItem(tree, "service"), "rest/", "rpc/", "List")

		// A request cannot be put at the top level.
		if rt.drop(nil, list, nil, nil, dropNowhere) || rt.drop(nil, list, service, nil, dropBelow) {
			t.Error("request moved to the top level")
		}
		check("service", treeItem(tree, "service"), "rest/", "rpc/", "List")

		// A folder dropped on a closed folder goes inside it, which opens.
		if !rt.drop(rpcFolder, nil, other, nil, dropOn) {
			t.Fatal("folder not moved")
		}
		check("service", treeItem(tree, "service"), "rest/", "List")
		check("other", treeItem(tree, "other"), "rpc/")
		check("rpc", treeItem(tree, "other", "rpc"), "Get", "Health")
		if !treeItem(tree, "other").IsExpanded() || tree.CurrentItem().Text(colName) != "rpc" {
			t.Errorf("other open %t, selected %q", treeItem(tree, "other").IsExpanded(), tree.CurrentItem().Text(colName))
		}

		// The open request is still found where it now is.
		tree.SetCurrentItem(treeItem(tree, "other", "rpc", "Health"))
		w.request.url.SetText("moved.test")
		if health.URL != "moved.test" {
			t.Errorf("edit went to %+v", health.Request)
		}

		// A folder cannot be put inside itself.
		if rt.drop(other, nil, rpcFolder, nil, dropOn) {
			t.Error("folder moved inside itself")
		}

		// A folder dropped in the empty space goes to the top level, in its
		// place in the order.
		rt.drop(restFolder, nil, nil, nil, dropNowhere)
		var top []string
		for _, f := range rt.store.Folders {
			top = append(top, f.Name)
		}
		if want := []string{"other", "rest", "service"}; !reflect.DeepEqual(top, want) {
			t.Errorf("top level = %q, want %q", top, want)
		}

		// A folder dropped beside a request goes with the folders there.
		rt.drop(restFolder, nil, rpcFolder, get, dropBelow)
		check("rpc", treeItem(tree, "other", "rpc"), "rest/", "Get", "Health")
	})

	saved, err := loadRequests(files.Requests)
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.Folders) != 2 || saved.Folders[0].Name != "other" || saved.Folders[0].Folders[0].Folders[0].Name != "rest" || saved.Folders[0].Folders[0].Requests[1].URL != "moved.test" || saved.Folders[1].Requests[0].Name != "List" {
		t.Errorf("saved %+v", saved.Folders)
	}
}

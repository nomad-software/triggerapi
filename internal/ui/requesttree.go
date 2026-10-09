package ui

import (
	"fmt"
	"net/http"

	qt "github.com/mappu/miqt/qt6"

	"github.com/nomad-software/postie/internal/collection"
	"github.com/nomad-software/postie/internal/rest"
)

// requestTree lists the saved requests in their folders. Clicking a request
// or pressing Edit opens it, and every change is saved. Tree items are in the same order as the
// store, so an item is mapped to its folder or request by position.
type requestTree struct {
	root *qt.QWidget
	tree *qt.QTreeWidget

	store *collection.Store
	// path is the file the store is saved to. Nothing is saved if it is empty.
	path     string
	updating bool

	// confirm asks the user a yes/no question.
	confirm func(question string) bool
	// initial returns the contents for a newly added request.
	initial func() rest.Request
	// onOpen is called when a request is selected.
	onOpen func(*collection.Request)
	// onRename is called after a request is renamed.
	onRename func(*collection.Request)
	// onRemove is called after a request is removed.
	onRemove func(*collection.Request)
	onError  func(error)
}

func newRequestTree(store *collection.Store, path string) *requestTree {
	t := &requestTree{
		root:  qt.NewQWidget2(),
		tree:  qt.NewQTreeWidget2(),
		store: store,
		path:  path,
	}

	t.confirm = func(question string) bool {
		return qt.QMessageBox_Question(t.root, "Postie", question) == qt.QMessageBox__Yes
	}
	t.initial = func() rest.Request {
		return rest.Request{Method: http.MethodGet}
	}

	t.tree.SetHeaderHidden(true)
	// Names are only edited through the Rename button.
	t.tree.SetEditTriggers(qt.QAbstractItemView__NoEditTriggers)

	addFolder := qt.NewQPushButton3("Add Folder")
	addFolder.OnClicked(t.addFolder)
	addRequest := qt.NewQPushButton3("Add Request")
	addRequest.OnClicked(t.addRequest)
	edit := qt.NewQPushButton3("Edit")
	edit.OnClicked(t.open)
	rename := qt.NewQPushButton3("Rename")
	rename.OnClicked(t.rename)
	remove := qt.NewQPushButton3("Remove")
	remove.OnClicked(t.remove)

	add := qt.NewQHBoxLayout2()
	add.AddWidget(addFolder.QWidget)
	add.AddWidget(addRequest.QWidget)
	change := qt.NewQHBoxLayout2()
	change.AddWidget(edit.QWidget)
	change.AddWidget(rename.QWidget)
	change.AddWidget(remove.QWidget)

	layout := qt.NewQVBoxLayout(t.root)
	layout.AddWidget2(t.tree.QWidget, 1)
	layout.AddLayout(add.QLayout)
	layout.AddLayout(change.QLayout)

	for _, f := range store.Folders {
		folder := t.newItem(nil, f.Name)
		for _, r := range f.Requests {
			t.newItem(folder, r.Name)
		}
		folder.SetExpanded(true)
	}

	t.tree.OnCurrentItemChanged(func(*qt.QTreeWidgetItem, *qt.QTreeWidgetItem) {
		if !t.updating {
			t.open()
		}
	})

	// A click also opens the request that is already selected, which the
	// selection changing does not cover.
	t.tree.OnItemClicked(func(*qt.QTreeWidgetItem, int) { t.open() })

	t.tree.OnItemChanged(func(item *qt.QTreeWidgetItem, _ int) {
		if t.updating {
			return
		}
		folder, request := t.locate(item)
		if request != nil {
			request.Name = item.Text(0)
			if t.onRename != nil {
				t.onRename(request)
			}
		} else {
			folder.Name = item.Text(0)
		}
		t.save()
	})

	return t
}

// newItem appends an item to the folder item, or to the top level if it is
// nil.
func (t *requestTree) newItem(folder *qt.QTreeWidgetItem, name string) *qt.QTreeWidgetItem {
	defer func(prev bool) { t.updating = prev }(t.updating)
	t.updating = true

	var item *qt.QTreeWidgetItem
	if folder == nil {
		item = qt.NewQTreeWidgetItem3(t.tree)
	} else {
		item = qt.NewQTreeWidgetItem6(folder)
	}
	item.SetText(0, name)
	item.SetFlags(item.Flags() | qt.ItemIsEditable)

	return item
}

// locate maps an item to its folder and, for a request item, its request.
func (t *requestTree) locate(item *qt.QTreeWidgetItem) (*collection.Folder, *collection.Request) {
	parent := item.Parent()
	if parent == nil {
		return t.store.Folders[t.tree.IndexOfTopLevelItem(item)], nil
	}
	folder := t.store.Folders[t.tree.IndexOfTopLevelItem(parent)]
	return folder, folder.Requests[parent.IndexOfChild(item)]
}

// open reports the selected request, if the selection is one.
func (t *requestTree) open() {
	item := t.tree.CurrentItem()
	if item == nil {
		return
	}
	if _, request := t.locate(item); request != nil && t.onOpen != nil {
		t.onOpen(request)
	}
}

// addFolder creates a folder and starts editing its name.
func (t *requestTree) addFolder() {
	t.store.AddFolder("New Folder")
	item := t.newItem(nil, "New Folder")
	t.save()

	t.tree.SetCurrentItem(item)
	t.tree.EditItem(item)
}

// addRequest creates a request in the selected folder, or the folder of the
// selected request, then opens it and starts editing its name. Without a
// selection the last folder is used, which is created if there are none.
func (t *requestTree) addRequest() {
	item := t.tree.CurrentItem()
	switch {
	case item != nil && item.Parent() != nil:
		item = item.Parent()
	case item == nil && len(t.store.Folders) == 0:
		t.store.AddFolder("New Folder")
		item = t.newItem(nil, "New Folder")
	case item == nil:
		item = t.tree.TopLevelItem(len(t.store.Folders) - 1)
	}

	folder, _ := t.locate(item)
	folder.Add("New Request", t.initial())
	request := t.newItem(item, "New Request")
	item.SetExpanded(true)
	t.save()

	t.tree.SetCurrentItem(request)
	t.tree.EditItem(request)
}

// rename starts editing the name of the selected folder or request.
func (t *requestTree) rename() {
	if item := t.tree.CurrentItem(); item != nil {
		t.tree.EditItem(item)
	}
}

// remove deletes the selected request, or the selected folder and everything
// in it.
func (t *requestTree) remove() {
	item := t.tree.CurrentItem()
	if item == nil {
		return
	}

	folder, request := t.locate(item)
	removed := []*collection.Request{request}

	t.updating = true
	if request != nil {
		parent := item.Parent()
		index := parent.IndexOfChild(item)
		folder.Remove(index)
		parent.TakeChild(index).Delete()
	} else {
		if n := len(folder.Requests); n > 0 && !t.confirm(fmt.Sprintf("Remove the folder %q and the %d requests in it?", folder.Name, n)) {
			t.updating = false
			return
		}
		removed = folder.Requests
		index := t.tree.IndexOfTopLevelItem(item)
		t.store.RemoveFolder(index)
		t.tree.TakeTopLevelItem(index).Delete()
	}
	t.updating = false

	t.save()
	if t.onRemove != nil {
		for _, r := range removed {
			t.onRemove(r)
		}
	}

	// The tree has moved the selection to a neighbouring item.
	t.open()
}

func (t *requestTree) save() {
	if t.path == "" {
		return
	}
	if err := t.store.Save(t.path); err != nil && t.onError != nil {
		t.onError(err)
	}
}

package ui

import (
	"fmt"
	"net/http"
	"slices"

	qt "github.com/mappu/miqt/qt6"

	"github.com/nomad-software/postie/internal/collection"
	"github.com/nomad-software/postie/internal/rest"
)

// colName is the column of the request tree, which holds the names.
const colName = 0

// typeLabels are the names the request types are shown by.
var typeLabels = map[collection.Type]string{
	collection.TypeREST: "REST",
	collection.TypeGRPC: "gRPC",
}

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
	// create returns the request to add, or false if the user decided
	// against adding one.
	create func() (*collection.Request, bool)
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
		root: qt.NewQWidget2(),
		tree: qt.NewQTreeWidget2(),
	}

	t.confirm = func(question string) bool {
		return qt.QMessageBox_Question(t.root, "Postie", question) == qt.QMessageBox__Yes
	}
	t.create = func() (*collection.Request, bool) {
		return collection.NewREST("New Request", rest.Request{Method: http.MethodGet}), true
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

	t.load(store, path)

	// Clicking where there is no item clears the selection, which is how a
	// folder is added at the top level once there are others.
	t.tree.OnMousePressEvent(func(super func(*qt.QMouseEvent), event *qt.QMouseEvent) {
		if t.tree.ItemAt(event.Pos()) == nil {
			t.deselect()
		}
		super(event)
	})

	t.tree.OnCurrentItemChanged(func(*qt.QTreeWidgetItem, *qt.QTreeWidgetItem) {
		if !t.updating {
			t.open()
		}
	})

	// A click also opens the request that is already selected, which the
	// selection changing does not cover.
	t.tree.OnItemClicked(func(*qt.QTreeWidgetItem, int) { t.open() })

	t.tree.OnItemChanged(func(item *qt.QTreeWidgetItem, column int) {
		if t.updating || column != colName {
			return
		}
		folder, request := t.locate(item)
		if request != nil {
			request.Name = item.Text(colName)
			if t.onRename != nil {
				t.onRename(request)
			}
		} else {
			folder.Name = item.Text(colName)
		}
		t.save()
	})

	return t
}

// load replaces the requests with those of the store, which is saved to path,
// without calling onOpen. Nothing is selected afterwards.
func (t *requestTree) load(store *collection.Store, path string) {
	defer func(prev bool) { t.updating = prev }(t.updating)
	t.updating = true

	t.store = store
	t.path = path

	t.tree.Clear()
	for _, f := range store.Folders {
		t.fill(t.newItem(nil, -1, f.Name, ""), f)
	}
}

// fill adds the contents of a folder beneath its item: its folders first,
// then its requests.
func (t *requestTree) fill(item *qt.QTreeWidgetItem, folder *collection.Folder) {
	for _, sub := range folder.Folders {
		t.fill(t.newItem(item, -1, sub.Name, ""), sub)
	}
	for _, r := range folder.Requests {
		t.newItem(item, -1, r.Name, r.Type)
	}
	item.SetExpanded(true)
}

// folderIcon is the icon of a folder, once it has been drawn.
var folderIcon *qt.QIcon

// newItem adds an item beneath a parent item, at an index among its
// children, or at their end if the index is -1. With no parent it is added
// at the end of the top level. A request has the icon of its type before its
// name, and an item with no type is a folder.
func (t *requestTree) newItem(parent *qt.QTreeWidgetItem, index int, name string, typ collection.Type) *qt.QTreeWidgetItem {
	defer func(prev bool) { t.updating = prev }(t.updating)
	t.updating = true

	var item *qt.QTreeWidgetItem
	switch {
	case parent == nil:
		item = qt.NewQTreeWidgetItem3(t.tree)
	case index < 0:
		item = qt.NewQTreeWidgetItem6(parent)
	default:
		item = qt.NewQTreeWidgetItem()
		parent.InsertChild(index, item)
	}

	item.SetText(colName, name)
	if icon := requestTypeIcon(typ); icon != nil {
		item.SetIcon(colName, icon)
		item.SetToolTip(colName, typeLabels[typ])
	} else {
		if folderIcon == nil {
			folderIcon = drawIcon(iconSize*2, requestsColour, drawFolder)
		}
		item.SetIcon(colName, folderIcon)
	}
	item.SetFlags(item.Flags() | qt.ItemIsEditable)

	return item
}

// locate maps an item to the folder it is, or to the request it is and the
// folder that holds it. The children of a folder's item are its folders
// followed by its requests, as they are in the store.
func (t *requestTree) locate(item *qt.QTreeWidgetItem) (*collection.Folder, *collection.Request) {
	// The position of the item at each level, from the top down.
	var path []int
	for {
		parent := item.Parent()
		if parent == nil {
			path = append(path, t.tree.IndexOfTopLevelItem(item))
			break
		}
		path = append(path, parent.IndexOfChild(item))
		item = parent
	}
	slices.Reverse(path)

	folder := t.store.Folders[path[0]]
	for _, index := range path[1:] {
		if index >= len(folder.Folders) {
			return folder, folder.Requests[index-len(folder.Folders)]
		}
		folder = folder.Folders[index]
	}
	return folder, nil
}

// folderItem returns the item of the folder that something is to be added
// to: the selected folder, or the folder of the selected request. It is nil
// if nothing is selected.
func (t *requestTree) folderItem() *qt.QTreeWidgetItem {
	item := t.tree.CurrentItem()
	if item == nil {
		return nil
	}
	if _, request := t.locate(item); request != nil {
		return item.Parent()
	}
	return item
}

// deselect leaves nothing selected.
func (t *requestTree) deselect() {
	t.tree.SetCurrentItem(nil)
	t.tree.ClearSelection()
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

// addFolder creates a folder and starts editing its name. It goes inside the
// selected folder, or the folder of the selected request, after the folders
// already there. With nothing selected it goes at the top level.
func (t *requestTree) addFolder() {
	const name = "New Folder"

	var item *qt.QTreeWidgetItem
	if parent := t.folderItem(); parent != nil {
		folder, _ := t.locate(parent)
		folder.AddFolder(name)
		item = t.newItem(parent, len(folder.Folders)-1, name, "")
		parent.SetExpanded(true)
	} else {
		t.store.AddFolder(name)
		item = t.newItem(nil, -1, name, "")
	}
	t.save()

	t.tree.SetCurrentItem(item)
	t.tree.EditItem(item)
}

// addRequest creates a request in the selected folder, or the folder of the
// selected request, then opens it and starts editing its name. Without a
// selection the last top level folder is used, which is created if there are
// none.
func (t *requestTree) addRequest() {
	request, ok := t.create()
	if !ok {
		return
	}

	parent := t.folderItem()
	switch {
	case parent != nil:
	case len(t.store.Folders) == 0:
		t.store.AddFolder("New Folder")
		parent = t.newItem(nil, -1, "New Folder", "")
	default:
		parent = t.tree.TopLevelItem(len(t.store.Folders) - 1)
	}

	folder, _ := t.locate(parent)
	folder.Add(request)
	child := t.newItem(parent, -1, request.Name, request.Type)
	parent.SetExpanded(true)
	t.save()

	t.tree.SetCurrentItem(child)
	t.tree.EditItem(child)
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
	parent := item.Parent()

	// The folder that holds the item, if it is not at the top level, and
	// the position of the item in it.
	var holder *collection.Folder
	index := t.tree.IndexOfTopLevelItem(item)
	if parent != nil {
		holder, _ = t.locate(parent)
		index = parent.IndexOfChild(item)
	}

	var removed []*collection.Request
	switch {
	case request != nil:
		removed = []*collection.Request{request}
		holder.Remove(index - len(holder.Folders))
	default:
		removed = folder.All()
		if folders := folder.Descendants(); (len(removed) > 0 || folders > 0) &&
			!t.confirm(fmt.Sprintf("Remove the folder %q and everything in it? It holds %s and %s.",
				folder.Name, plural(folders, "folder"), plural(len(removed), "request"))) {
			return
		}
		if holder != nil {
			holder.RemoveFolder(index)
		} else {
			t.store.RemoveFolder(index)
		}
	}

	// Deleting an item deletes the items beneath it.
	t.updating = true
	if parent != nil {
		parent.TakeChild(index).Delete()
	} else {
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

// plural returns a count of things, such as "1 folder" or "3 folders".
func plural(n int, thing string) string {
	if n == 1 {
		return "1 " + thing
	}
	return fmt.Sprintf("%d %ss", n, thing)
}

func (t *requestTree) save() {
	if t.path == "" {
		return
	}
	if err := t.store.Save(t.path); err != nil && t.onError != nil {
		t.onError(err)
	}
}

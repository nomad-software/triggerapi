package ui

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"

	qt "github.com/mappu/miqt/qt6"
	"github.com/mappu/miqt/qt6/mainthread"

	"github.com/nomad-software/postie/internal/collection"
	"github.com/nomad-software/postie/internal/openapi"
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
	// collapsed holds the folders that were closed when the tree was last
	// drawn again.
	collapsed map[*collection.Folder]bool

	// confirm asks the user a yes/no question.
	confirm func(question string) bool
	// create returns the request to add, or false if the user decided
	// against adding one.
	create func() (*collection.Request, bool)
	// chooseFile asks the user for an OpenAPI document to import. It reports
	// false if they decide against choosing one.
	chooseFile func() (string, bool)
	// warn tells the user of a problem.
	warn func(message string)
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

	t.chooseFile = func() (string, bool) {
		path := qt.QFileDialog_GetOpenFileName4(t.root, "Import OpenAPI Document", "", "OpenAPI documents (*.json *.yaml *.yml);;All files (*)")
		return path, path != ""
	}
	t.warn = func(message string) {
		qt.QMessageBox_Warning(t.root, "Postie", message)
	}

	t.tree.SetHeaderHidden(true)
	// Names are only edited through the Rename button.
	t.tree.SetEditTriggers(qt.QAbstractItemView__NoEditTriggers)

	addFolder := qt.NewQPushButton3("Add Folder")
	addFolder.OnClicked(t.addFolder)
	addRequest := qt.NewQPushButton3("Add Request")
	addRequest.OnClicked(t.addRequest)
	importButton := qt.NewQPushButton3("Import")
	importButton.SetToolTip("Create a folder of requests from an OpenAPI document")
	importButton.OnClicked(t.importDocument)
	edit := qt.NewQPushButton3("Edit")
	edit.OnClicked(t.open)
	rename := qt.NewQPushButton3("Rename")
	rename.OnClicked(t.rename)
	remove := qt.NewQPushButton3("Remove")
	remove.OnClicked(t.remove)

	add := qt.NewQHBoxLayout2()
	add.AddWidget(addFolder.QWidget)
	add.AddWidget(addRequest.QWidget)
	add.AddWidget(importButton.QWidget)
	change := qt.NewQHBoxLayout2()
	change.AddWidget(edit.QWidget)
	change.AddWidget(rename.QWidget)
	change.AddWidget(remove.QWidget)

	layout := qt.NewQVBoxLayout(t.root)
	layout.AddWidget2(t.tree.QWidget, 1)
	layout.AddLayout(add.QLayout)
	layout.AddLayout(change.QLayout)

	t.load(store, path)

	// Folders and requests are moved by dragging them. The move is made in
	// the store and the tree drawn again from it, instead of letting the
	// tree move its own items, so the two cannot come to differ.
	t.tree.SetDragEnabled(true)
	t.tree.SetAcceptDrops(true)
	t.tree.SetDropIndicatorShown(true)
	t.tree.SetDragDropMode(qt.QAbstractItemView__InternalMove)
	t.tree.OnDropEvent(func(_ func(*qt.QDropEvent), event *qt.QDropEvent) {
		dragged := t.tree.CurrentItem()
		target := t.tree.ItemAt(event.Position().ToPoint())

		where := dropNowhere
		switch t.tree.DropIndicatorPosition() {
		case qt.QAbstractItemView__OnItem:
			where = dropOn
		case qt.QAbstractItemView__AboveItem:
			where = dropAbove
		case qt.QAbstractItemView__BelowItem:
			where = dropBelow
		}

		// Tell the tree that nothing was moved, so that it does not remove
		// what was dragged.
		event.SetDropAction(qt.IgnoreAction)
		event.Accept()

		if dragged == nil {
			return
		}
		draggedFolder, draggedRequest := t.locate(dragged)
		var targetFolder *collection.Folder
		var targetRequest *collection.Request
		if target != nil && where != dropNowhere {
			targetFolder, targetRequest = t.locate(target)
		}

		// The items must not be replaced until the drag has finished.
		mainthread.Start(func() {
			t.drop(draggedFolder, draggedRequest, targetFolder, targetRequest, where)
		})
	})

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

		// The new name may belong somewhere else in the order. The items
		// must not be replaced while one of them is reporting its change.
		mainthread.Start(t.sort)
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

	t.collapsed = map[*collection.Folder]bool{}
	t.draw(nil)
}

// draw fills the tree from the store. Every folder is open except those in
// collapsed. The item of selected, a folder or a request, is made the
// current one if it is given.
func (t *requestTree) draw(selected any) {
	defer func(prev bool) { t.updating = prev }(t.updating)
	t.updating = true

	t.tree.Clear()

	var current *qt.QTreeWidgetItem
	note := func(item *qt.QTreeWidgetItem, of any) {
		if of == selected {
			current = item
		}
	}

	// fill adds the contents of a folder beneath its item: its folders
	// first, then its requests.
	var fill func(item *qt.QTreeWidgetItem, folder *collection.Folder)
	fill = func(item *qt.QTreeWidgetItem, folder *collection.Folder) {
		note(item, folder)
		for _, sub := range folder.Folders {
			fill(t.newItem(item, -1, sub.Name, ""), sub)
		}
		for _, r := range folder.Requests {
			note(t.newItem(item, -1, r.Name, r.Type), r)
		}
		item.SetExpanded(!t.collapsed[folder])
	}

	for _, f := range t.store.Folders {
		fill(t.newItem(nil, -1, f.Name, ""), f)
	}
	if current != nil {
		t.tree.SetCurrentItem(current)
	}
}

// sort puts the folders and requests in alphabetical order and draws the tree
// again if that moved anything, keeping what is selected selected.
func (t *requestTree) sort() {
	var selected any
	if item := t.tree.CurrentItem(); item != nil {
		folder, request := t.locate(item)
		selected = folder
		if request != nil {
			selected = request
		}
	}

	before := t.order()
	t.store.Sort()
	if slices.Equal(before, t.order()) {
		return
	}

	// The tree still shows the order from before.
	t.rememberIn(before)
	t.draw(selected)
}

// order lists every folder and request of the store in the order they are
// shown.
func (t *requestTree) order() []any {
	var all []any
	var walk func(f *collection.Folder)
	walk = func(f *collection.Folder) {
		all = append(all, f)
		for _, sub := range f.Folders {
			walk(sub)
		}
		for _, r := range f.Requests {
			all = append(all, r)
		}
	}
	for _, f := range t.store.Folders {
		walk(f)
	}
	return all
}

// rememberIn notes which folders are closed, as remember does, for a tree
// that shows the folders and requests in the given order, which is no longer
// that of the store.
func (t *requestTree) rememberIn(order []any) {
	clear(t.collapsed)

	// The items of the tree are in the same order, read from the top down.
	i := 0
	var walk func(item *qt.QTreeWidgetItem)
	walk = func(item *qt.QTreeWidgetItem) {
		of := order[i]
		i++
		if folder, ok := of.(*collection.Folder); ok && !item.IsExpanded() {
			t.collapsed[folder] = true
		}
		for child := range item.ChildCount() {
			walk(item.Child(child))
		}
	}
	for top := range t.tree.TopLevelItemCount() {
		walk(t.tree.TopLevelItem(top))
	}
}

// remember notes which folders are closed, so they can be drawn that way
// again. It must be called while the tree still matches the store.
func (t *requestTree) remember() {
	clear(t.collapsed)

	var walk func(item *qt.QTreeWidgetItem, folder *collection.Folder)
	walk = func(item *qt.QTreeWidgetItem, folder *collection.Folder) {
		if !item.IsExpanded() {
			t.collapsed[folder] = true
		}
		for i, sub := range folder.Folders {
			walk(item.Child(i), sub)
		}
	}
	for i, f := range t.store.Folders {
		walk(t.tree.TopLevelItem(i), f)
	}
}

// dropPosition is where something dragged was let go, relative to an item.
type dropPosition int

const (
	// dropNowhere is the empty space beneath the items.
	dropNowhere dropPosition = iota
	dropOn
	dropAbove
	dropBelow
)

// drop moves the folder or request that was dragged to where it was dropped:
// on, above or below the target folder or request, or nowhere. It reports if
// anything was moved.
//
// Dropping on a folder puts it inside, at the end. Dropping above or below
// something puts it beside that, in the same folder, with the one limit that
// folders always come before requests. Dropping in the empty space puts a
// folder at the end of the top level; a request cannot go there, as requests
// are always in a folder.
func (t *requestTree) drop(folder *collection.Folder, request *collection.Request, targetFolder *collection.Folder, targetRequest *collection.Request, where dropPosition) bool {
	after := 0
	if where == dropBelow {
		after = 1
	}

	// to is the folder to move into, which is nil for the top level, and
	// index the position there among the things of the same kind.
	var to *collection.Folder
	index := 0

	switch {
	case where == dropNowhere || targetFolder == nil:
		index = len(t.store.Folders)

	case targetRequest != nil:
		// Beside a request, in the folder that holds it.
		to = targetFolder
		index = len(to.Folders)
		if request != nil {
			index = slices.Index(to.Requests, targetRequest) + after
		}

	case where == dropOn:
		to = targetFolder
		index = len(to.Folders)
		if request != nil {
			index = len(to.Requests)
		}

	default:
		// Beside a folder, in the folder that holds it. A request goes
		// before the requests there, which is as near as it can be.
		parent, ok := t.store.ParentOf(targetFolder)
		if !ok {
			return false
		}
		to = parent
		if request == nil {
			siblings := t.store.Folders
			if to != nil {
				siblings = to.Folders
			}
			index = slices.Index(siblings, targetFolder) + after
		}
	}

	t.remember()

	var moved any
	switch {
	case request != nil:
		if !t.store.MoveRequest(request, to, index) {
			return false
		}
		moved = request
	default:
		if !t.store.MoveFolder(folder, to, index) {
			return false
		}
		moved = folder
	}

	// What something is dropped into is opened to show it.
	if to != nil {
		delete(t.collapsed, to)
	}
	// Everything is kept in alphabetical order, so where exactly in a
	// folder something was dropped makes no difference.
	t.store.Sort()
	t.draw(moved)
	t.save()
	return true
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
	flags := item.Flags() | qt.ItemIsEditable
	if typ != "" {
		// Nothing can be dropped onto a request.
		flags &^= qt.ItemIsDropEnabled
	}
	item.SetFlags(flags)

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

	parent := t.folderItem()
	t.remember()

	var added *collection.Folder
	if parent != nil {
		folder, _ := t.locate(parent)
		added = folder.AddFolder(name)
		delete(t.collapsed, folder)
	} else {
		added = t.store.AddFolder(name)
	}

	t.added(added)
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
	t.remember()

	var folder *collection.Folder
	switch {
	case parent != nil:
		folder, _ = t.locate(parent)
	case len(t.store.Folders) == 0:
		folder = t.store.AddFolder("New Folder")
	default:
		folder = t.store.Folders[len(t.store.Folders)-1]
	}
	folder.Add(request)
	delete(t.collapsed, folder)

	t.added(request)
	if t.onOpen != nil {
		t.onOpen(request)
	}
}

// added shows a folder or request that has just been put in the store, in
// its place in the order, and starts editing its name.
func (t *requestTree) added(what any) {
	t.store.Sort()
	t.draw(what)
	t.save()

	if item := t.tree.CurrentItem(); item != nil {
		t.tree.EditItem(item)
	}
}

// importDocument asks for an OpenAPI document and imports it.
func (t *requestTree) importDocument() {
	path, ok := t.chooseFile()
	if !ok {
		return
	}
	if err := t.importFile(path); err != nil {
		t.warn(fmt.Sprintf("%s could not be imported: %v.", filepath.Base(path), err))
	}
}

// importFile adds a folder of requests made from the OpenAPI document at
// path, at the end of the top level, and selects it.
func (t *requestTree) importFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	folder, err := openapi.Import(data)
	if err != nil {
		return err
	}

	t.remember()
	t.store.Folders = append(t.store.Folders, folder)
	t.store.Sort()
	t.draw(folder)
	t.save()
	return nil
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

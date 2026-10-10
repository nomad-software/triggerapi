package ui

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"

	qt "github.com/mappu/miqt/qt6"
	"github.com/mappu/miqt/qt6/mainthread"

	"github.com/nomad-software/triggerapi/internal/collection"
	"github.com/nomad-software/triggerapi/internal/openapi"
	"github.com/nomad-software/triggerapi/internal/postman"
	"github.com/nomad-software/triggerapi/internal/rest"
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
	// search narrows the tree to the folders and requests that match what
	// is typed in it.
	search *qt.QLineEdit

	store *collection.Store
	// path is the file the store is saved to. Nothing is saved if it is empty.
	path     string
	updating bool

	// confirm asks the user a yes/no question.
	confirm func(question string) bool
	// create returns the request to add, or false if the user decided
	// against adding one.
	create func() (*collection.Request, bool)
	// chooseSource asks the user what kind of file to import. It reports
	// false if they decide against importing.
	chooseSource func() (importSource, bool)
	// chooseFile asks the user for a file of a kind to import. It reports
	// false if they decide against choosing one.
	chooseFile func(source importSource) (string, bool)
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
		root:   qt.NewQWidget2(),
		tree:   qt.NewQTreeWidget2(),
		search: qt.NewQLineEdit2(),
	}

	t.search.SetPlaceholderText("Search requests")
	t.search.SetClearButtonEnabled(true)
	t.search.OnTextChanged(func(string) {
		if t.updating {
			return
		}
		if t.query() == nil {
			// Back to the folders open or closed as they were.
			t.draw(t.selection())
		} else {
			t.applySearch()
		}
	})

	t.confirm = func(question string) bool {
		return qt.QMessageBox_Question(t.root, "TriggerAPI", question) == qt.QMessageBox__Yes
	}
	t.create = func() (*collection.Request, bool) {
		return collection.NewREST("New Request", rest.Request{Method: http.MethodGet}), true
	}

	t.chooseSource = func() (importSource, bool) {
		return newImportDialog(t.root).ask()
	}
	t.chooseFile = func(source importSource) (string, bool) {
		kind := importSources[source]
		path := qt.QFileDialog_GetOpenFileName4(t.root, "Import "+kind.label, "", kind.filter+";;All files (*)")
		return path, path != ""
	}
	t.warn = func(message string) {
		qt.QMessageBox_Warning(t.root, "TriggerAPI", message)
	}

	t.tree.SetHeaderHidden(true)
	// Names are only edited through the Rename button.
	t.tree.SetEditTriggers(qt.QAbstractItemView__NoEditTriggers)

	// The buttons are icons along the bottom, each saying what it does in
	// its tooltip. They are the colour of the icon of the tab, but for the one
	// which removes, and the one which imports is apart at the right. There
	// is none to open a request, as selecting it does that.
	addFolder := newIconButton(requestsColour, drawFolderPlus, "Add a folder")
	addFolder.OnClicked(t.addFolder)
	addRequest := newIconButton(requestsColour, drawPagePlus, "Add a request")
	addRequest.OnClicked(t.addRequest)
	duplicate := newIconButton(requestsColour, drawCopy, "Duplicate the selected folder or request")
	duplicate.OnClicked(t.duplicate)
	rename := newIconButton(requestsColour, drawLetters, "Rename the selected folder or request")
	rename.OnClicked(t.rename)
	remove := newIconButton(removeColour, drawBin, "Remove the selected folder or request")
	remove.OnClicked(t.remove)
	importButton := newIconButton(requestsColour, drawImport, "Import a folder of requests from an OpenAPI document or a Postman collection")
	importButton.OnClicked(t.importDocument)

	buttons := newButtonRow(addFolder, addRequest, duplicate, rename, remove)
	buttons.AddWidget(importButton.QWidget)

	layout := qt.NewQVBoxLayout(t.root)
	layout.AddWidget(t.search.QWidget)
	layout.AddWidget2(t.tree.QWidget, 1)
	layout.AddLayout(buttons.QLayout)

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

	// Whether a folder is open or closed is kept with it.
	t.tree.OnItemExpanded(func(item *qt.QTreeWidgetItem) { t.setCollapsed(item, false) })
	t.tree.OnItemCollapsed(func(item *qt.QTreeWidgetItem) { t.setCollapsed(item, true) })

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

	t.draw(nil)
}

// draw fills the tree from the store, with each folder open or closed as the
// store records. The item of selected, a folder or a request, is made the
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
		item.SetExpanded(!folder.Collapsed)
	}

	for _, f := range t.store.Folders {
		fill(t.newItem(nil, -1, f.Name, ""), f)
	}
	if current != nil {
		t.tree.SetCurrentItem(current)
	}
	t.applySearch()
}

// query returns the words being searched for, in lower case. It is nil when
// there is no search.
func (t *requestTree) query() []string {
	if words := strings.Fields(strings.ToLower(t.search.Text())); len(words) > 0 {
		return words
	}
	return nil
}

// applySearch hides the folders and requests that do not match the search,
// and opens the folders that are left so that what was found can be seen.
// Something matches if every word of the search is in its name or the names
// of the folders it is in, so a folder that matches is shown with all it
// holds. Items are hidden and not removed, as they are matched to the store
// by their position. Which folders are open is not kept: the tree is drawn
// again from the store when the search is cleared.
func (t *requestTree) applySearch() {
	words := t.query()
	if words == nil {
		return
	}

	defer func(prev bool) { t.updating = prev }(t.updating)
	t.updating = true

	// show reports if an item, or anything beneath it, matches.
	var show func(item *qt.QTreeWidgetItem, path string) bool
	show = func(item *qt.QTreeWidgetItem, path string) bool {
		path += " " + strings.ToLower(item.Text(colName))

		found := true
		for _, word := range words {
			found = found && strings.Contains(path, word)
		}
		for i := range item.ChildCount() {
			if show(item.Child(i), path) {
				found = true
			}
		}

		item.SetHidden(!found)
		if found && item.ChildCount() > 0 {
			item.SetExpanded(true)
		}
		return found
	}
	for i := range t.tree.TopLevelItemCount() {
		show(t.tree.TopLevelItem(i), "")
	}
}

// clearSearch empties the search box without drawing the tree again, for
// when it is about to be drawn anyway.
func (t *requestTree) clearSearch() {
	defer func(prev bool) { t.updating = prev }(t.updating)
	t.updating = true
	t.search.Clear()
}

// selection returns the folder or request that is selected, or nil.
func (t *requestTree) selection() any {
	item := t.tree.CurrentItem()
	if item == nil {
		return nil
	}
	if folder, request := t.locate(item); request != nil {
		return request
	} else {
		return folder
	}
}

// sort puts the folders and requests in alphabetical order and draws the tree
// again if that moved anything, keeping what is selected selected.
func (t *requestTree) sort() {
	selected := t.selection()

	before := t.order()
	t.store.Sort()
	if slices.Equal(before, t.order()) {
		// A new name may no longer match the search.
		t.applySearch()
		return
	}

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
		to.Collapsed = false
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

// setCollapsed records that the folder of an item was opened or closed by
// the user.
func (t *requestTree) setCollapsed(item *qt.QTreeWidgetItem, collapsed bool) {
	// Folders opened and closed during a search are not remembered.
	if t.updating || t.query() != nil {
		return
	}
	if folder, request := t.locate(item); request == nil && folder.Collapsed != collapsed {
		folder.Collapsed = collapsed
		t.save()
	}
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

// selectRequest selects the item of a request, if it has one, without
// calling onOpen. Nothing is left selected if there is no request.
func (t *requestTree) selectRequest(r *collection.Request) {
	defer func(prev bool) { t.updating = prev }(t.updating)
	t.updating = true

	if r == nil {
		t.deselect()
		return
	}

	var find func(item *qt.QTreeWidgetItem) *qt.QTreeWidgetItem
	find = func(item *qt.QTreeWidgetItem) *qt.QTreeWidgetItem {
		if _, request := t.locate(item); request == r {
			return item
		}
		for i := range item.ChildCount() {
			if found := find(item.Child(i)); found != nil {
				return found
			}
		}
		return nil
	}

	for i := range t.tree.TopLevelItemCount() {
		if item := find(t.tree.TopLevelItem(i)); item != nil {
			t.tree.SetCurrentItem(item)
			return
		}
	}
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

	var added *collection.Folder
	if parent := t.folderItem(); parent != nil {
		folder, _ := t.locate(parent)
		added = folder.AddFolder(name)
		folder.Collapsed = false
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
	folder.Collapsed = false

	t.added(request)
	if t.onOpen != nil {
		t.onOpen(request)
	}
}

// duplicate makes a copy of the selected request, or of the selected folder
// and everything in it, beside the one it is a copy of, and starts editing
// its name. A copy of a request is opened.
func (t *requestTree) duplicate() {
	item := t.tree.CurrentItem()
	if item == nil {
		return
	}

	folder, request := t.locate(item)
	if request != nil {
		holder := t.store.FolderOf(request)
		if holder == nil {
			return
		}
		clone := request.Clone()
		clone.Name += " copy"
		holder.Add(clone)

		t.added(clone)
		if t.onOpen != nil {
			t.onOpen(clone)
		}
		return
	}

	parent, ok := t.store.ParentOf(folder)
	if !ok {
		return
	}
	clone := folder.Clone()
	clone.Name += " copy"
	if parent != nil {
		parent.Folders = append(parent.Folders, clone)
	} else {
		t.store.Folders = append(t.store.Folders, clone)
	}
	t.added(clone)
}

// added shows a folder or request that has just been put in the store, in
// its place in the order, and starts editing its name.
func (t *requestTree) added(what any) {
	// It may not match a search, and must be seen.
	t.clearSearch()
	t.store.Sort()
	t.draw(what)
	t.save()

	if item := t.tree.CurrentItem(); item != nil {
		t.tree.EditItem(item)
	}
}

// importSource is a kind of file that requests can be imported from.
type importSource string

const (
	importOpenAPI importSource = "openapi"
	importPostman importSource = "postman"
)

// importSources describes the kinds of file that can be imported, in the
// order they are offered.
var importSources = map[importSource]struct {
	label  string
	filter string
	colour string
	glyph  func(*qt.QPainter)
	read   func([]byte) (*collection.Folder, error)
}{
	importOpenAPI: {"OpenAPI", "OpenAPI documents (*.json *.yaml *.yml)", "#6ba539", drawDocument, openapi.Import},
	importPostman: {"Postman", "Postman collections (*.json)", "#ef5b25", drawPlane, postman.Import},
}

// newImportDialog returns a dialog which asks what kind of file to import.
func newImportDialog(parent *qt.QWidget) *choiceDialog[importSource] {
	var choices []choice[importSource]
	for _, source := range []importSource{importOpenAPI, importPostman} {
		kind := importSources[source]
		choices = append(choices, choice[importSource]{source, kind.label, drawIcon(iconSize*2, kind.colour, kind.glyph)})
	}
	return newChoiceDialog(parent, "Import", "Choose what to import requests from.", choices)
}

// importDocument asks what kind of file to import, and for the file, and
// imports it.
func (t *requestTree) importDocument() {
	source, ok := t.chooseSource()
	if !ok {
		return
	}
	path, ok := t.chooseFile(source)
	if !ok {
		return
	}
	if err := t.importFile(source, path); err != nil {
		t.warn(fmt.Sprintf("%s could not be imported: %v.", filepath.Base(path), err))
	}
}

// importFile adds a folder of requests made from the file at path, which is
// of the kind given, to the top level, and selects it. The folders inside it
// start off closed, as there can be a great many.
func (t *requestTree) importFile(source importSource, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	folder, err := importSources[source].read(data)
	if err != nil {
		return err
	}
	folder.Collapse()

	t.store.Folders = append(t.store.Folders, folder)
	t.clearSearch()
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
// in it, once the user has agreed to it. An empty folder is removed without
// asking.
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
		if !t.confirm(fmt.Sprintf("Remove the request %q?", request.Name)) {
			return
		}
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

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
	"github.com/nomad-software/triggerapi/internal/crypt"
	"github.com/nomad-software/triggerapi/internal/openapi"
	"github.com/nomad-software/triggerapi/internal/postman"
	"github.com/nomad-software/triggerapi/internal/rest"
)

// colName is the column of the request tree, which has the names.
const colName = 0

// The views of the Requests tab, which are the tabs beneath the search: the
// requests in their folders, and the sets.
const (
	viewRequests = iota
	viewSets
)

// typeLabels are the names the request types are shown by.
var typeLabels = map[collection.Type]string{
	collection.TypeREST:      "REST",
	collection.TypeGRPC:      "gRPC",
	collection.TypeWebSocket: "WebSocket",
}

// requestTree lists the saved requests in their folders. Clicking a request
// or pressing Edit opens it, and every change is saved. The top level folders
// are listed under headings by the type of their requests (groupFolders), as
// recorded in top when the tree is drawn; beneath them, tree items are in the
// same order as the store, so an item is mapped to its folder or request by
// position.
type requestTree struct {
	root *qt.QWidget
	tree *qt.QTreeWidget
	// search narrows the tree to the folders and requests that match what
	// is typed in it.
	search *qt.QLineEdit
	// views is the tabs beneath the search, which show the requests or the
	// sets, each in a page of pages.
	views *qt.QTabBar
	pages *qt.QStackedWidget
	// sets lists the sets, which are kept in the store with the folders.
	sets *setList

	store *collection.Store
	// path is the file the store is saved to. Nothing is saved if it is empty.
	path     string
	updating bool

	// top holds what each item at the top level stands for, from when the
	// tree was last drawn.
	top []topEntry
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
	// browsing is set while a key moves the selection, which does not open
	// what it moves to.
	browsing bool
	// copied is a copy of the folder or request last copied with Ctrl+C,
	// or nil. It is kept in the tree, which stays when the workspace
	// changes, so that it can be pasted into another; it never goes to
	// the system clipboard, as requests hold tokens and passwords.
	copied any

	// onRename is called after a request is renamed.
	onRename func(*collection.Request)
	// onRemove is called after a request is removed.
	onRemove func(*collection.Request)
	// onSave is called after any change to the store, when it is saved.
	onSave  func()
	onError func(error)
	// keys returns the keys the file is encrypted with, which are nil if
	// it is not to be.
	keys func() *crypt.Keys
}

func newRequestTree(store *collection.Store, path string) *requestTree {
	t := &requestTree{
		root:   qt.NewQWidget2(),
		tree:   qt.NewQTreeWidget2(),
		search: qt.NewQLineEdit2(),
		views:  qt.NewQTabBar2(),
		pages:  qt.NewQStackedWidget2(),
	}
	t.sets = newSetList(t)

	t.views.AddTab("Requests")
	t.views.AddTab("Sets")
	t.drawViewIcons()
	t.views.SetDrawBase(false)
	t.views.SetExpanding(false)
	t.views.OnCurrentChanged(t.showView)

	t.search.SetPlaceholderText("Search requests")
	t.search.SetClearButtonEnabled(true)
	// Return goes on to what was found.
	t.search.OnReturnPressed(t.browse)
	t.search.OnTextChanged(func(string) {
		if t.updating {
			return
		}
		t.sets.applySearch()
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
	// The headings are at the left edge, without the arrow of a folder,
	// and the folders under them have theirs in its place.
	t.tree.SetRootIsDecorated(false)
	// Names are only edited through the Rename button.
	t.tree.SetEditTriggers(qt.QAbstractItemView__NoEditTriggers)

	// The buttons are icons along the bottom, each saying what it does in
	// its tooltip. They are the colour of the icon of the tab, but for the one
	// which removes, and the one which imports is apart at the right. There
	// is none to open a request, as selecting it does that.
	addFolder := newIconButton(colourRequests, drawFolderPlus, "Add a folder")
	addFolder.OnClicked(t.addFolder)
	addRequest := newIconButton(colourRequests, drawPlus, "Add a request")
	addRequest.OnClicked(t.addRequest)
	duplicate := newIconButton(colourRequests, drawCopy, "Duplicate the selected folder or request")
	duplicate.OnClicked(t.duplicate)
	rename := newIconButton(colourRequests, drawLetters, "Rename the selected folder or request")
	rename.OnClicked(t.rename)
	remove := newIconButton(colourRemove, drawBin, "Remove the selected folder or request")
	remove.OnClicked(t.remove)
	importButton := newIconButton(colourRequests, drawImport, "Import a folder of requests from an OpenAPI document or a Postman collection")
	importButton.OnClicked(t.importDocument)

	buttons := newButtonRow(addFolder, addRequest, duplicate, rename, remove)
	buttons.AddWidget(importButton.QWidget)

	requests := qt.NewQWidget2()
	requestsLayout := qt.NewQVBoxLayout(requests)
	requestsLayout.SetContentsMargins(0, 0, 0, 0)
	requestsLayout.AddWidget2(t.tree.QWidget, 1)
	requestsLayout.AddLayout(buttons.QLayout)
	t.pages.AddWidget(requests)
	t.pages.AddWidget(t.sets.root)

	layout := qt.NewQVBoxLayout(t.root)
	layout.AddWidget(t.search.QWidget)
	layout.AddWidget(t.views.QWidget)
	layout.AddWidget2(t.pages.QWidget, 1)

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
		if draggedFolder == nil {
			// A heading.
			return
		}
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
		item := t.tree.ItemAt(event.Pos())
		switch {
		case item == nil:
			t.deselect()
		case t.isHeading(item):
			// A heading cannot be selected.
			return
		}
		super(event)
	})

	// Whether a folder is open or closed is kept with it.
	t.tree.OnItemExpanded(func(item *qt.QTreeWidgetItem) { t.setCollapsed(item, false) })
	t.tree.OnItemCollapsed(func(item *qt.QTreeWidgetItem) { t.setCollapsed(item, true) })

	t.tree.OnCurrentItemChanged(func(*qt.QTreeWidgetItem, *qt.QTreeWidgetItem) {
		if !t.updating && !t.browsing {
			t.open()
		}
	})

	// The keyboard moves through the tree without opening what it passes
	// over: up and down from one item to the next, left to close a folder
	// and right to open one, as a tree does by itself. Return opens the
	// request it has got to.
	t.tree.OnKeyPressEvent(func(super func(*qt.QKeyEvent), event *qt.QKeyEvent) {
		switch event.Key() {
		case int(qt.Key_Return), int(qt.Key_Enter):
			t.activate()
		case int(qt.Key_F2):
			// As the rename button does.
			t.rename()
		default:
			switch {
			case event.Matches(qt.QKeySequence__Copy):
				t.copy()
				return
			case event.Matches(qt.QKeySequence__Paste):
				t.paste()
				return
			}
			defer func(prev bool) { t.browsing = prev }(t.browsing)
			t.browsing = true
			prev := t.tree.CurrentItem()
			super(event)
			// A heading is passed over, or if there is nothing beyond it
			// the selection stays where it was.
			if item := t.tree.CurrentItem(); item != nil && t.isHeading(item) {
				super(event)
				if item := t.tree.CurrentItem(); item != nil && t.isHeading(item) && prev != nil {
					t.tree.SetCurrentItem(prev)
				}
			}
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
		if folder == nil {
			// A heading.
			return
		}

		// Nothing can be left without a name, so one that is emptied is
		// given back the name it had.
		name := item.Text(colName)
		if strings.TrimSpace(name) == "" {
			previous := folder.Name
			if request != nil {
				previous = request.Name
			}
			t.updating = true
			item.SetText(colName, previous)
			t.updating = false
			return
		}

		if request != nil {
			request.Name = name
			if t.onRename != nil {
				t.onRename(request)
			}
		} else {
			folder.Name = name
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
	t.sets.load(nil)
}

// drawViewIcons gives the tabs of the views their icons, in the colours of
// the settings: the folder of the Requests tab of the sidebar, and the
// checklist of a set.
func (t *requestTree) drawViewIcons() {
	t.views.SetTabIcon(viewRequests, drawIcon(iconSize*2, themeColour(colourRequests), drawFolder))
	t.views.SetTabIcon(viewSets, setIcon())
}

// showView shows the requests or the sets.
func (t *requestTree) showView(view int) {
	if t.views.CurrentIndex() != view {
		// This is called again as the tab changes.
		t.views.SetCurrentIndex(view)
		return
	}
	t.pages.SetCurrentIndex(view)
}

// focusList puts the keyboard in the tree, or the list of sets if they are
// shown.
func (t *requestTree) focusList() {
	if t.views.CurrentIndex() == viewSets {
		t.sets.list.SetFocus()
	} else {
		t.tree.SetFocus()
	}
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
		item.SetExpanded(folder.Open)
	}

	t.top = groupFolders(t.store.Folders)
	for _, entry := range t.top {
		if entry.group == "" {
			fill(t.newItem(nil, -1, entry.folders[0].Name, ""), entry.folders[0])
			continue
		}
		heading := t.newHeading(entry.group)
		for _, f := range entry.folders {
			fill(t.newItem(heading, -1, f.Name, ""), f)
		}
		heading.SetExpanded(true)
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

// applySearch hides the requests that do not match the search, and the
// folders and headings with none that do, and opens the folders that are
// left so that what was found can be seen. A request matches if every word
// of the search is in its own name; the names of folders are not searched.
// Items are hidden and not removed, as they are matched to the store by
// their position. Which folders are open is not kept: the tree is drawn
// again from the store when the search is cleared.
func (t *requestTree) applySearch() {
	words := t.query()
	if words == nil {
		return
	}

	defer func(prev bool) { t.updating = prev }(t.updating)
	t.updating = true

	// show reports if an item is a request that matches, or holds one.
	var show func(item *qt.QTreeWidgetItem) bool
	show = func(item *qt.QTreeWidgetItem) bool {
		found := false
		if _, request := t.locate(item); request != nil {
			name := strings.ToLower(request.Name)
			found = true
			for _, word := range words {
				found = found && strings.Contains(name, word)
			}
		} else {
			for i := range item.ChildCount() {
				if show(item.Child(i)) {
					found = true
				}
			}
		}

		item.SetHidden(!found)
		if found && item.ChildCount() > 0 {
			item.SetExpanded(true)
		}
		return found
	}
	for i := range t.tree.TopLevelItemCount() {
		show(t.tree.TopLevelItem(i))
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
	if item := t.tree.CurrentItem(); item != nil {
		return t.subject(item)
	}
	return nil
}

// subject returns the folder or request of an item, or nil for a heading.
func (t *requestTree) subject(item *qt.QTreeWidgetItem) any {
	switch folder, request := t.locate(item); {
	case request != nil:
		return request
	case folder != nil:
		return folder
	}
	return nil
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
// folders always come before requests. Dropping in the empty space, or by a
// heading, puts a folder at the end of the top level; a request cannot go
// there, as requests are always in a folder.
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
		// Nowhere, or by a heading.
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
		to.Open = true
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

// folderIcon returns the icon of a folder, drawing it if need be.
func (t *requestTree) folderIcon() *qt.QIcon {
	if folderIcon == nil {
		folderIcon = drawIcon(iconSize*2, themeColour(colourRequests), drawFolder)
	}
	return folderIcon
}

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
	} else {
		item.SetIcon(colName, t.folderIcon())
	}
	flags := item.Flags() | qt.ItemIsEditable
	if typ != "" {
		// Nothing can be dropped onto a request.
		flags &^= qt.ItemIsDropEnabled
	}
	item.SetFlags(flags)

	return item
}

// newHeading adds the item of a heading at the end of the top level, with the
// icon of the type of request it lists, or for mixed ones that of a folder.
// It is not a folder: it is always open, has no arrow, and cannot be
// selected, renamed, dragged or dropped onto.
func (t *requestTree) newHeading(group collection.Type) *qt.QTreeWidgetItem {
	defer func(prev bool) { t.updating = prev }(t.updating)
	t.updating = true

	item := qt.NewQTreeWidgetItem3(t.tree)
	item.SetText(colName, groupLabels[group])
	icon := requestTypeIcon(group)
	if icon == nil {
		icon = t.folderIcon()
	}
	item.SetIcon(colName, icon)
	font := item.Font(colName)
	font.SetBold(true)
	item.SetFont(colName, font)
	item.SetFlags(qt.ItemIsEnabled)
	return item
}

// isHeading reports if an item is that of a heading.
func (t *requestTree) isHeading(item *qt.QTreeWidgetItem) bool {
	if item.Parent() != nil {
		return false
	}
	index := t.tree.IndexOfTopLevelItem(item)
	return index >= 0 && index < len(t.top) && t.top[index].group != ""
}

// locate maps an item to the folder it is, or to the request it is and the
// folder that holds it. The item of a heading is neither, and gives nil for
// both. The children of a folder's item are its folders followed by its
// requests, as they are in the store.
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

	entry := t.top[path[0]]
	if entry.group != "" {
		if len(path) == 1 {
			return nil, nil
		}
		entry.folders = entry.folders[path[1]:]
		path = path[1:]
	}
	folder := entry.folders[0]
	for _, index := range path[1:] {
		if index >= len(folder.Folders) {
			return folder, folder.Requests[index-len(folder.Folders)]
		}
		folder = folder.Folders[index]
	}
	return folder, nil
}

// setCollapsed records that the folder of an item was opened or closed by
// the user, for the tree to be drawn again with it so. It is not saved.
func (t *requestTree) setCollapsed(item *qt.QTreeWidgetItem, collapsed bool) {
	// Folders opened and closed during a search are not remembered.
	if t.updating || t.query() != nil {
		return
	}
	if t.isHeading(item) {
		// A heading is not a folder, and is always open.
		if collapsed {
			item.SetExpanded(true)
		}
		return
	}
	if folder, request := t.locate(item); request == nil {
		folder.Open = !collapsed
	}
}

// folderItem returns the item of the folder that something is to be added
// to: the selected folder, or the folder of the selected request. It is nil
// if nothing is selected, or a heading is.
func (t *requestTree) folderItem() *qt.QTreeWidgetItem {
	item := t.tree.CurrentItem()
	if item == nil || t.isHeading(item) {
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

// focusSearch puts the keyboard in the search box, with what was last
// searched for selected, to be typed over.
func (t *requestTree) focusSearch() {
	t.search.SetFocus()
	t.search.SelectAll()
}

// browse puts the keyboard in the tree, to move through it with. A search
// starts it at the first request that was found. Otherwise it starts where
// the selection is, or at the first folder if nothing is selected. Nothing is
// opened by it. With the sets shown, it is their list the keyboard goes to.
func (t *requestTree) browse() {
	if t.views.CurrentIndex() == viewSets {
		t.sets.browse()
		return
	}

	// first returns the first folder or request on show beneath those
	// given, which is a request if only is set.
	var first func(count int, child func(int) *qt.QTreeWidgetItem, only bool) *qt.QTreeWidgetItem
	first = func(count int, child func(int) *qt.QTreeWidgetItem, only bool) *qt.QTreeWidgetItem {
		for i := range count {
			item := child(i)
			if item.IsHidden() {
				continue
			}
			// A heading is passed over for what is under it.
			if folder, request := t.locate(item); request != nil || (folder != nil && !only) {
				return item
			}
			if found := first(item.ChildCount(), item.Child, only); found != nil {
				return found
			}
		}
		return nil
	}

	start := t.tree.CurrentItem()
	if t.query() != nil {
		start = first(t.tree.TopLevelItemCount(), t.tree.TopLevelItem, true)
	}
	if start == nil || start.IsHidden() {
		start = first(t.tree.TopLevelItemCount(), t.tree.TopLevelItem, false)
	}

	if start != nil {
		t.updating = true
		t.tree.SetCurrentItem(start)
		t.updating = false
	}
	t.tree.SetFocus()
}

// activate opens the selected request, or if a folder is selected opens or
// closes that.
func (t *requestTree) activate() {
	item := t.tree.CurrentItem()
	if item == nil {
		return
	}
	if _, request := t.locate(item); request != nil {
		t.open()
	} else {
		item.SetExpanded(!item.IsExpanded())
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
		folder.Open = true
	} else {
		added = t.store.AddFolder(name)
	}

	t.added(added)
}

// addRequest creates a request in the selected folder, or the folder of the
// selected request, then opens it and starts editing its name. Without a
// selection a new folder is made at the top level to hold it, as requests
// are always in a folder.
func (t *requestTree) addRequest() {
	if request, ok := t.create(); ok {
		t.insert(request)
	}
}

// insert puts a request in the selected folder, or the folder of the selected
// request, or else in a new folder at the top level, and opens it with its
// name open for editing.
func (t *requestTree) insert(request *collection.Request) {
	var folder *collection.Folder
	if parent := t.folderItem(); parent != nil {
		folder, _ = t.locate(parent)
	} else {
		folder = t.store.AddFolder("New Folder")
	}
	folder.Add(request)
	folder.Open = true

	t.added(request)
	if t.onOpen != nil {
		t.onOpen(request)
	}
}

// copy keeps a copy of the selected request, or of the selected folder and
// everything in it, to be pasted. Nothing is copied from a heading.
func (t *requestTree) copy() {
	item := t.tree.CurrentItem()
	if item == nil {
		return
	}
	switch folder, request := t.locate(item); {
	case request != nil:
		t.copied = request.Clone()
	case folder != nil:
		t.copied = folder.Clone()
	}
}

// paste adds a copy of what was copied: into the selected folder, or the
// folder of the selected request, or else at the top level, a request in a
// new folder of its own there as requests are always in a folder. Something
// with the name of another beside it has " copy" added to its name. A
// request pasted is opened.
func (t *requestTree) paste() {
	var into *collection.Folder
	if parent := t.folderItem(); parent != nil {
		into, _ = t.locate(parent)
	}

	switch copied := t.copied.(type) {
	case *collection.Request:
		request := copied.Clone()
		if into == nil {
			into = t.store.AddFolder("New Folder")
		}
		for slices.ContainsFunc(into.Requests, func(r *collection.Request) bool { return r.Name == request.Name }) {
			request.Name += " copy"
		}
		into.Add(request)
		into.Open = true
		t.pasted(request)
		if t.onOpen != nil {
			t.onOpen(request)
		}
	case *collection.Folder:
		folder := copied.Clone()
		siblings := &t.store.Folders
		if into != nil {
			siblings = &into.Folders
			into.Open = true
		}
		for slices.ContainsFunc(*siblings, func(f *collection.Folder) bool { return f.Name == folder.Name }) {
			folder.Name += " copy"
		}
		*siblings = append(*siblings, folder)
		t.pasted(folder)
	}
}

// pasted shows and saves a folder or request that has just been pasted,
// selected, in its place in the order.
func (t *requestTree) pasted(what any) {
	t.showView(viewRequests)
	t.clearSearch()
	t.store.Sort()
	t.draw(what)
	t.save()
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
	if folder == nil {
		return
	}
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
	t.adopt(folder)
	return nil
}

// extended shows and saves what has been added to the store from outside
// the tree, keeping a request selected.
func (t *requestTree) extended(selected *collection.Request) {
	t.showView(viewRequests)
	t.clearSearch()
	t.store.Sort()
	t.draw(selected)
	t.save()
}

// adopt adds a folder, with everything in it, to the top level, and selects
// it.
func (t *requestTree) adopt(folder *collection.Folder) {
	// It is shown open, with the folders inside it closed.
	folder.Open = true
	t.showView(viewRequests)
	t.store.Folders = append(t.store.Folders, folder)
	t.clearSearch()
	t.store.Sort()
	t.draw(folder)
	t.save()
}

// rename starts editing the name of the selected folder or request.
func (t *requestTree) rename() {
	if item := t.tree.CurrentItem(); item != nil && !t.isHeading(item) {
		t.tree.EditItem(item)
	}
}

// remove deletes the selected request, or the selected folder and everything
// in it, once the user has agreed to it. An empty folder is removed without
// asking. What was beside it is selected in its place, and opened if it is a
// request.
func (t *requestTree) remove() {
	item := t.tree.CurrentItem()
	if item == nil {
		return
	}

	folder, request := t.locate(item)
	if folder == nil {
		// A heading.
		return
	}
	next := t.neighbour(item)

	var removed []*collection.Request
	switch {
	case request != nil:
		if !t.confirm(fmt.Sprintf("Remove the request %q?", request.Name)) {
			return
		}
		removed = []*collection.Request{request}
		holder := t.store.FolderOf(request)
		holder.Remove(slices.Index(holder.Requests, request))
	default:
		removed = folder.All()
		if folders := folder.Descendants(); (len(removed) > 0 || folders > 0) &&
			!t.confirm(fmt.Sprintf("Remove the folder %q and everything in it? It holds %s and %s.",
				folder.Name, plural(folders, "folder"), plural(len(removed), "request"))) {
			return
		}
		parent, ok := t.store.ParentOf(folder)
		if !ok {
			return
		}
		if parent != nil {
			parent.RemoveFolder(slices.Index(parent.Folders, folder))
		} else {
			t.store.RemoveFolder(slices.Index(t.store.Folders, folder))
		}
	}

	// What is left may belong under another heading, so the tree is drawn
	// again rather than the item taken out of it.
	t.draw(next)
	// The sets no longer hold what was removed.
	t.store.PruneSets()
	t.sets.draw(t.sets.selected())

	t.save()
	if t.onRemove != nil {
		for _, r := range removed {
			t.onRemove(r)
		}
	}
	t.open()
}

// neighbour returns the folder or request to select once that of an item is
// removed: the one after it beside it, or else the one before it, or else
// the folder that holds it. It is nil if there is none of these.
func (t *requestTree) neighbour(item *qt.QTreeWidgetItem) any {
	parent := item.Parent()
	count := t.tree.TopLevelItemCount()
	child := t.tree.TopLevelItem
	if parent != nil {
		count = parent.ChildCount()
		child = parent.Child
	}

	index := t.tree.IndexOfTopLevelItem(item)
	if parent != nil {
		index = parent.IndexOfChild(item)
	}

	for _, i := range []int{index + 1, index - 1} {
		if i < 0 || i >= count {
			continue
		}
		if sibling := child(i); !sibling.IsHidden() && !t.isHeading(sibling) {
			return t.subject(sibling)
		}
	}
	if parent != nil {
		return t.subject(parent)
	}
	return nil
}

// plural returns a count of things, such as "1 folder" or "3 folders".
func plural(n int, thing string) string {
	if n == 1 {
		return "1 " + thing
	}
	return fmt.Sprintf("%d %ss", n, thing)
}

func (t *requestTree) save() {
	if t.onSave != nil {
		defer t.onSave()
	}
	if t.path == "" {
		return
	}
	var keys *crypt.Keys
	if t.keys != nil {
		keys = t.keys()
	}
	if err := t.store.SaveWith(t.path, keys); err != nil && t.onError != nil {
		t.onError(err)
	}
}

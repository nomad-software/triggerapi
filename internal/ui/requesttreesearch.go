package ui

import (
	"strings"

	qt "github.com/mappu/miqt/qt6"
)

// Searching the request tree by the names of requests, and going from the
// search to what it found.

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

package ui

import (
	"fmt"
	"slices"
	"strings"

	qt "github.com/mappu/miqt/qt6"
	"github.com/mappu/miqt/qt6/mainthread"

	"github.com/nomad-software/triggerapi/internal/collection"
)

// setList is the Sets view of the Requests tab: the sets of the store,
// each a set of requests kept open together in pinned tabs. The set chosen
// (active) is the one whose requests are the pinned tabs: choosing another
// replaces them with its own, and pinning, unpinning and moving tabs changes
// the active one. Sets are not in folders, and are listed in alphabetical
// order, ignoring case, in the same order as the store, so an item is mapped
// to its set by its row.
type setList struct {
	root *qt.QWidget
	list *qt.QListWidget
	// owner is the request tree, whose store holds the sets and which
	// saves them.
	owner *requestTree

	// active is the set chosen, or nil if there is none.
	active   *collection.Set
	updating bool
	// browsing is set while a key moves the selection, which does not
	// choose what it moves to.
	browsing bool

	// pinned returns the requests that are pinned in tabs, which a new set
	// is given.
	pinned func() []*collection.Request
	// onChoose is called when the user chooses a set, or none.
	onChoose func(*collection.Set)
}

func newSetList(owner *requestTree) *setList {
	l := &setList{root: qt.NewQWidget2(), list: qt.NewQListWidget2(), owner: owner}

	// Names are only edited through the Rename button, or F2.
	l.list.SetEditTriggers(qt.QAbstractItemView__NoEditTriggers)

	// The buttons are the colour of the sets, but for the one which
	// removes.
	add := newIconButton(colourSets, drawPlus, "Add a set holding the pinned tabs")
	add.OnClicked(l.add)
	duplicate := newIconButton(colourSets, drawCopy, "Duplicate the selected set")
	duplicate.OnClicked(l.duplicate)
	rename := newIconButton(colourSets, drawLetters, "Rename the selected set")
	rename.OnClicked(l.rename)
	remove := newIconButton(colourRemove, drawBin, "Remove the selected set")
	remove.OnClicked(l.remove)

	layout := qt.NewQVBoxLayout(l.root)
	layout.SetContentsMargins(0, 0, 0, 0)
	layout.AddWidget2(l.list.QWidget, 1)
	layout.AddLayout(newButtonRow(add, duplicate, rename, remove).QLayout)

	// Clicking where there is no set chooses none.
	l.list.OnMousePressEvent(func(super func(*qt.QMouseEvent), event *qt.QMouseEvent) {
		if l.list.ItemAt(event.Pos()) == nil {
			l.deselect()
			l.choose(nil)
			return
		}
		super(event)
	})

	// The set chosen is the one selected. Deselecting it, however that is
	// done, chooses none, which takes its tabs away and leaves one empty
	// tab, belonging to no set.
	l.list.OnItemSelectionChanged(func() {
		if !l.updating && !l.browsing {
			l.choose(l.selected())
		}
	})

	// The keyboard moves through the sets without choosing them, and
	// Return chooses the one it has got to.
	l.list.OnKeyPressEvent(func(super func(*qt.QKeyEvent), event *qt.QKeyEvent) {
		switch event.Key() {
		case int(qt.Key_Return), int(qt.Key_Enter):
			l.choose(l.selected())
		case int(qt.Key_F2):
			l.rename()
		default:
			defer func(prev bool) { l.browsing = prev }(l.browsing)
			l.browsing = true
			super(event)
		}
	})

	l.list.OnItemChanged(func(item *qt.QListWidgetItem) {
		if l.updating {
			return
		}
		set := l.setOf(item)
		if set == nil {
			return
		}
		// Nothing can be left without a name.
		name := item.Text()
		if strings.TrimSpace(name) == "" {
			l.quietly(func() { item.SetText(set.Name) })
			return
		}
		set.Name = name
		l.owner.save()
		// The new name may belong elsewhere in the order. The items must
		// not be replaced while one of them is reporting its change.
		mainthread.Start(func() {
			l.owner.store.SortSets()
			l.draw(l.selected())
		})
	})

	return l
}

// quietly makes changes to the list without reacting to them.
func (l *setList) quietly(change func()) {
	defer func(prev bool) { l.updating = prev }(l.updating)
	l.updating = true
	change()
}

// draw fills the list from the store, with the item of selected, if it is
// given, made the current one.
func (l *setList) draw(selected *collection.Set) {
	l.quietly(func() {
		l.list.Clear()
		icon := setIcon()
		for _, set := range l.owner.store.Sets {
			item := qt.NewQListWidgetItem7(set.Name, l.list)
			item.SetIcon(icon)
			item.SetFlags(item.Flags() | qt.ItemIsEditable)
			item.SetToolTip(fmt.Sprintf("%s: %s", set.Name, plural(len(set.Requests), "request")))
			if set == selected {
				l.list.SetCurrentItem(item)
			}
		}
	})
	l.applySearch()
}

// load shows the sets of the store, of which active is the one chosen.
func (l *setList) load(active *collection.Set) {
	l.active = active
	l.draw(active)
}

// setOf returns the set of an item, or nil.
func (l *setList) setOf(item *qt.QListWidgetItem) *collection.Set {
	if item == nil {
		return nil
	}
	if row := l.list.Row(item); row >= 0 && row < len(l.owner.store.Sets) {
		return l.owner.store.Sets[row]
	}
	return nil
}

// selected returns the set that is selected, or nil.
func (l *setList) selected() *collection.Set {
	if items := l.list.SelectedItems(); len(items) == 0 {
		return nil
	}
	return l.setOf(l.list.CurrentItem())
}

// deselect leaves no set selected.
func (l *setList) deselect() {
	l.quietly(func() {
		l.list.SetCurrentItem(nil)
		l.list.ClearSelection()
	})
}

// choose makes a set the active one, and has its tabs shown.
func (l *setList) choose(set *collection.Set) {
	if set == l.active {
		return
	}
	l.active = set
	if l.onChoose != nil {
		l.onChoose(set)
	}
}

// keep records the requests pinned in tabs as those of the active set, and
// saves them.
func (l *setList) keep(pinned []*collection.Request) {
	if l.active == nil || slices.Equal(l.active.Requests, pinned) {
		return
	}
	l.active.Requests = slices.Clone(pinned)
	l.owner.save()
	l.draw(l.selected())
}

// added shows a set that has just been put in the store as the active one,
// selected, with its name open for editing. Its requests are those pinned
// already, so the tabs stay as they are.
func (l *setList) added(set *collection.Set) {
	l.owner.clearSearch()
	l.owner.store.SortSets()
	l.active = set
	l.draw(set)
	l.owner.save()
	if item := l.list.CurrentItem(); item != nil {
		l.list.EditItem(item)
	}
}

// add makes a set of the requests that are pinned.
func (l *setList) add() {
	var pinned []*collection.Request
	if l.pinned != nil {
		pinned = l.pinned()
	}
	l.added(l.owner.store.AddSet("New Set", pinned))
}

// duplicate copies the selected set, which holds the same requests.
func (l *setList) duplicate() {
	set := l.selected()
	if set == nil {
		return
	}
	if l.active != set && l.onChoose != nil {
		// Its tabs are shown, as they are those of the copy.
		l.active = set
		l.onChoose(set)
	}
	l.added(l.owner.store.AddSet(set.Name+" copy", set.Requests))
}

// rename opens the name of the selected set for editing.
func (l *setList) rename() {
	if item := l.list.CurrentItem(); item != nil && l.selected() != nil {
		l.list.EditItem(item)
	}
}

// remove deletes the selected set, once the user has agreed to it if it
// holds any requests. The requests themselves, and the tabs, are left as
// they are.
func (l *setList) remove() {
	set := l.selected()
	if set == nil {
		return
	}
	if len(set.Requests) > 0 && !l.owner.confirm(fmt.Sprintf("Remove the set %q? Its %s are not removed.", set.Name, plural(len(set.Requests), "request"))) {
		return
	}
	l.owner.store.RemoveSet(set)
	if l.active == set {
		l.active = nil
	}
	l.draw(nil)
	l.owner.save()
}

// applySearch hides the sets that do not have every word of the search in
// their name, ignoring case.
func (l *setList) applySearch() {
	words := l.owner.query()
	l.quietly(func() {
		for i := range l.list.Count() {
			item := l.list.Item(i)
			name := strings.ToLower(item.Text())
			shown := true
			for _, word := range words {
				shown = shown && strings.Contains(name, word)
			}
			item.SetHidden(!shown)
		}
	})
}

// browse puts the keyboard in the list, at the first set found if there is
// a search, without choosing it.
func (l *setList) browse() {
	if l.owner.query() != nil {
		for i := range l.list.Count() {
			if item := l.list.Item(i); !item.IsHidden() {
				l.quietly(func() { l.list.SetCurrentItem(item) })
				break
			}
		}
	}
	l.list.SetFocus()
}

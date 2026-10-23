package ui

import (
	"slices"

	"github.com/nomad-software/triggerapi/internal/collection"
)

// How the window shows requests in its tabs: the tab of the request that is
// not saved, saving it, going between tabs, and the sets of pinned tabs.

// newRequest asks for the type of a request, and shows an empty request of
// that type in the tab that is not pinned, without saving it: it is added to
// the sidebar only once it is saved.
func (w *Window) newRequest() {
	typ, ok := askRequestType(w.QWidget)
	if !ok {
		return
	}
	draft := collection.New(typ, "")

	w.stash(w.tabs.current())
	tab := w.tabs.open(nil)
	tab.draft, tab.replay = draft, nil
	w.showTab(tab, true)
	w.closeEditor()
}

// saveRequest saves the request being composed in the tab that holds no
// saved request, which it shows in that tab, and adds it to the sidebar.
func (w *Window) saveRequest() {
	if w.open != nil {
		return
	}
	w.requests.insert(w.composed("New Request"))
}

// composed returns the request that is not saved, as it is in the request
// UI, with a name. It is not in the store.
func (w *Window) composed(name string) *collection.Request {
	w.commit()
	return w.requestUI(w.unsaved).editor.compose(name)
}

// stash keeps the request being composed in a tab, if it is one that is not
// saved, so that it is still there when the tab is shown again.
func (w *Window) stash(tab *requestTab) {
	if tab != nil && tab.request == nil {
		tab.draft = w.composed("")
	} else {
		w.commit()
	}
}

// showTab shows the request of a tab in the request UI, with the response
// it was last given. The request is selected in the sidebar too if sync is
// set.
func (w *Window) showTab(tab *requestTab, sync bool) {
	w.show(tab.request)
	if tab.request == nil && tab.draft != nil {
		w.showDraft(tab.draft)
	}
	if tab.replay != nil {
		tab.replay()
	}
	if sync {
		w.requests.selectRequest(tab.request)
	}
}

// showDraft shows a request that is not saved, which show(nil) has made way
// for, in the request UI of its type.
func (w *Window) showDraft(draft *collection.Request) {
	w.unsaved = draft.Type
	w.load(draft)
	if w.stack.CurrentIndex() != pageEditor {
		w.stack.SetCurrentIndex(w.requestPage())
	}
}

// chooseSet replaces the tabs with those of a set, and shows the first.
// Choosing no set, by deselecting the one chosen, takes the tabs away and
// leaves one empty tab.
func (w *Window) chooseSet(set *collection.Set) {
	w.closeEditor()
	w.stash(w.tabs.current())
	var requests []*collection.Request
	if set != nil {
		requests = set.Requests
	}
	w.tabs.reset(requests)
	w.showTab(w.tabs.current(), true)
}

// restoreTabs opens a tab for each request of the set that was chosen in
// the workspace in use when it was left, and shows the first. With no set,
// as at startup, there is only a request that is not saved and nothing is
// selected: tabs are only remembered in sets.
func (w *Window) restoreTabs() {
	if name, ok := w.chosenSets[w.config.Workspace]; ok {
		if set := w.requests.store.FindSet(name); set != nil {
			w.requests.sets.load(set)
			w.tabs.reset(set.Requests)
			w.showTab(w.tabs.current(), true)
			return
		}
	}

	w.tabs.reset(nil)
	w.showTab(w.tabs.current(), true)
}

// showSets shows the sets in the sidebar, with the keyboard in their list,
// where the one chosen is selected.
func (w *Window) showSets() {
	w.sidebar.SetCurrentIndex(tabRequests)
	w.requests.showView(viewSets)
	w.requests.sets.list.SetFocus()
}

// updateSets lists the sets of requests in the selector above the tabs, with
// the one chosen selected.
func (w *Window) updateSets() {
	store := w.requests.store
	names := make([]string, len(store.Sets))
	for i, set := range store.Sets {
		names[i] = set.Name
	}
	w.environment.setSets(names, slices.Index(store.Sets, w.requests.sets.active))
}

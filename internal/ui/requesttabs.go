package ui

import (
	"slices"

	qt "github.com/mappu/miqt/qt6"

	"github.com/nomad-software/triggerapi/internal/collection"
	"github.com/nomad-software/triggerapi/internal/rest"
)

// pinIconSize is the size of the icon of the button that pins a tab.
const pinIconSize = 16

// untitled is the name on the tab of a request that has not been saved.
const untitled = "Untitled"

// requestTab is a tab above the request UI.
type requestTab struct {
	// request is the saved request the tab shows. It is nil in the tab
	// that is not pinned while that holds a request which is not saved.
	request *collection.Request
	// pinned tabs keep their request. The one tab that is not pinned shows
	// whichever request was last selected in the sidebar.
	pinned bool
	button *qt.QToolButton

	// draft is the request that was being composed in the tab, without
	// being saved, when another tab was shown.
	draft *rest.Request
	// replay shows the response the request of the tab was last given
	// again. It is nil if there has been none.
	replay func()
}

// requestTabs is the row of tabs above the request UI. There is a tab for
// each pinned request, and at most one tab that is not pinned. All of them
// show their request in the one request UI, so a tab is a way of getting
// back to a request and not a copy of the UI.
//
// Each tab has a button before its name. On the tab that is not pinned it is
// a pin, which pins the tab. On a pinned tab it shows the type of the
// request, and turns into a pin, which unpins the tab, while the pointer is
// over the tab.
type requestTabs struct {
	bar *qt.QTabBar
	// tabs are the tabs in the order they are shown.
	tabs     []*requestTab
	updating bool
	// last is the tab that was shown before the one that is now.
	last *requestTab
	// hovered is the tab the pointer is over, or nil.
	hovered *requestTab

	pinnedIcon   *qt.QIcon
	unpinnedIcon *qt.QIcon

	// onSelect is called when the user picks a tab, with the tab that was
	// shown until then, which is nil if it has gone.
	onSelect func(from, to *requestTab)
}

func newRequestTabs() *requestTabs {
	s := &requestTabs{
		bar: qt.NewQTabBar2(),
		// A pinned tab is unpinned with the icon of the buttons which
		// remove.
		pinnedIcon: drawIcon(pinIconSize*2, themeColour(colourRemove), drawBin),
		// The pin is one of the action buttons.
		unpinnedIcon: drawIcon(pinIconSize*2, themeColour(colourAction), drawPin),
	}

	s.bar.SetExpanding(false)
	s.bar.SetDrawBase(false)
	s.bar.SetUsesScrollButtons(true)
	s.bar.SetElideMode(qt.ElideRight)

	// The tab under the pointer is followed, to show the pin on it.
	s.bar.SetMouseTracking(true)
	s.bar.OnMouseMoveEvent(func(super func(*qt.QMouseEvent), event *qt.QMouseEvent) {
		super(event)
		if index := s.bar.TabAt(event.Pos()); index >= 0 && index < len(s.tabs) {
			s.hover(s.tabs[index])
		} else {
			s.hover(nil)
		}
	})
	s.bar.OnLeaveEvent(func(super func(*qt.QEvent), event *qt.QEvent) {
		super(event)
		s.hover(nil)
	})

	s.bar.OnCurrentChanged(func(index int) {
		if s.updating || index < 0 || index >= len(s.tabs) {
			return
		}
		from, to := s.last, s.tabs[index]
		s.last = to
		if s.onSelect != nil && from != to {
			s.onSelect(from, to)
		}
	})

	s.reset(nil)
	return s
}

// current returns the tab that is shown. There always is one.
func (s *requestTabs) current() *requestTab {
	return s.tabs[max(0, s.bar.CurrentIndex())]
}

// pinned returns the requests that are pinned, in the order of their tabs.
func (s *requestTabs) pinned() []*collection.Request {
	var requests []*collection.Request
	for _, tab := range s.tabs {
		if tab.pinned {
			requests = append(requests, tab.request)
		}
	}
	return requests
}

// find returns the tab a request is pinned in, or nil if it is not pinned.
func (s *requestTabs) find(r *collection.Request) *requestTab {
	for _, tab := range s.tabs {
		if tab.pinned && tab.request == r {
			return tab
		}
	}
	return nil
}

// preview returns the tab that is not pinned, or nil if every tab is.
func (s *requestTabs) preview() *requestTab {
	for _, tab := range s.tabs {
		if !tab.pinned {
			return tab
		}
	}
	return nil
}

// quietly makes changes to the tabs without calling onSelect.
func (s *requestTabs) quietly(change func()) {
	defer func(prev bool) { s.updating = prev }(s.updating)
	s.updating = true
	change()
}

// add puts a tab at the end of the row.
func (s *requestTabs) add(tab *requestTab) {
	tab.button = qt.NewQToolButton2()
	tab.button.SetIconSize(qt.NewQSize2(pinIconSize, pinIconSize))
	tab.button.SetAutoRaise(true)
	tab.button.SetFocusPolicy(qt.NoFocus)
	tab.button.OnClicked(func() { s.toggle(tab) })
	// The tab bar is not told of the pointer moving over the button.
	tab.button.OnEnterEvent(func(super func(*qt.QEnterEvent), event *qt.QEnterEvent) {
		super(event)
		s.hover(tab)
	})

	s.quietly(func() {
		s.tabs = append(s.tabs, tab)
		index := s.bar.AddTab("")
		s.bar.SetTabButton(index, qt.QTabBar__LeftSide, tab.button.QWidget)
	})
	s.refresh(tab)
}

// take removes a tab from the row.
func (s *requestTabs) take(tab *requestTab) {
	index := slices.Index(s.tabs, tab)
	if index < 0 {
		return
	}
	s.quietly(func() {
		s.tabs = slices.Delete(s.tabs, index, index+1)
		// The tab bar deletes the button of the tab.
		s.bar.RemoveTab(index)
	})
	if s.last == tab {
		s.last = nil
	}
	if s.hovered == tab {
		s.hovered = nil
	}
}

// hover notes the tab the pointer is over, which is nil when it is over
// none, and shows the pin on it.
func (s *requestTabs) hover(tab *requestTab) {
	if s.hovered == tab {
		return
	}
	previous := s.hovered
	s.hovered = tab
	for _, changed := range []*requestTab{previous, tab} {
		if changed != nil {
			s.refresh(changed)
		}
	}
}

// show makes a tab the one that is shown, without calling onSelect.
func (s *requestTabs) show(tab *requestTab) {
	s.quietly(func() { s.bar.SetCurrentIndex(slices.Index(s.tabs, tab)) })
	s.last = tab
}

// refresh puts the name of its request on a tab, and the pin or the type of
// the request on its button.
func (s *requestTabs) refresh(tab *requestTab) {
	index := slices.Index(s.tabs, tab)
	if index < 0 {
		return
	}

	name, tip := untitled, "A request that has not been saved"
	if tab.request != nil {
		name, tip = tab.request.Name, tab.request.Name
	}
	s.bar.SetTabText(index, name)
	s.bar.SetTabToolTip(index, tip)

	// A request has to be saved before it can be pinned.
	tab.button.SetEnabled(tab.request != nil)
	switch {
	case !tab.pinned:
		tab.button.SetIcon(s.unpinnedIcon)
		tab.button.SetToolTip("Pin this tab, to keep this request open in it")
	case tab == s.hovered:
		tab.button.SetIcon(s.pinnedIcon)
		tab.button.SetToolTip("Unpin this tab")
	default:
		if icon := requestTypeIcon(tab.request.Type); icon != nil {
			tab.button.SetIcon(icon)
		}
		tab.button.SetToolTip(typeLabels[tab.request.Type] + " request, pinned")
	}
}

// recolour gives the buttons of the tabs their icons again, after the colours
// have changed.
func (s *requestTabs) recolour() {
	s.pinnedIcon = drawIcon(pinIconSize*2, themeColour(colourRemove), drawBin)
	s.unpinnedIcon = drawIcon(pinIconSize*2, themeColour(colourAction), drawPin)
	for _, tab := range s.tabs {
		s.refresh(tab)
	}
}

// open shows a request that was selected in the sidebar, or one that is not
// saved if it is nil, and returns its tab: the one it is pinned in, or else
// the tab that is not pinned, which is added if there is none. It does not
// call onSelect.
func (s *requestTabs) open(r *collection.Request) *requestTab {
	tab := s.find(r)
	if r == nil || tab == nil {
		if tab = s.preview(); tab == nil {
			tab = &requestTab{}
			s.add(tab)
		}
		if tab.request != r {
			tab.request, tab.draft, tab.replay = r, nil, nil
			s.refresh(tab)
		}
	}
	s.show(tab)
	return tab
}

// toggle pins a tab, or unpins it. A tab that is unpinned while it is shown
// becomes the one tab that is not pinned, in place of any other. One that is
// unpinned while another is shown is closed.
func (s *requestTabs) toggle(tab *requestTab) {
	switch {
	case !tab.pinned:
		if tab.request == nil {
			return
		}
		tab.pinned = true
	case tab == s.current():
		if other := s.preview(); other != nil {
			s.take(other)
		}
		tab.pinned = false
		s.show(tab)
	default:
		s.take(tab)
		return
	}
	s.refresh(tab)
}

// rename shows the new name of a request on its tabs.
func (s *requestTabs) rename(r *collection.Request) {
	for _, tab := range s.tabs {
		if tab.request == r {
			s.refresh(tab)
		}
	}
}

// remove closes the tab a request is pinned in, and empties the tab that is
// not pinned if it shows the request. It does not call onSelect, though the
// tab that is shown may be another afterwards.
func (s *requestTabs) remove(r *collection.Request) {
	for _, tab := range slices.Clone(s.tabs) {
		switch {
		case tab.request != r:
		case tab.pinned:
			s.take(tab)
		default:
			tab.request, tab.draft, tab.replay = nil, nil, nil
			s.refresh(tab)
		}
	}
	s.settle()
}

// settle makes sure there is a tab, and that the row agrees with which one
// is shown.
func (s *requestTabs) settle() {
	if len(s.tabs) == 0 {
		s.add(&requestTab{})
	}
	s.show(s.current())
}

// reset replaces the tabs with one for each of the pinned requests given,
// or a single tab that is not pinned if there are none. The first is the
// one shown. It does not call onSelect.
func (s *requestTabs) reset(pinned []*collection.Request) {
	for _, tab := range slices.Clone(s.tabs) {
		s.take(tab)
	}
	for _, r := range pinned {
		s.add(&requestTab{request: r, pinned: true})
	}
	s.settle()
	s.show(s.tabs[0])
}

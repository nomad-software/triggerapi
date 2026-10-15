package ui

import (
	"slices"

	qt "github.com/mappu/miqt/qt6"

	"github.com/nomad-software/triggerapi/internal/collection"
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
	// pinnedByClick is set when the click just before pinned the tab, so
	// that the double click it may be the start of does not unpin it.
	pinnedByClick bool
	// doubleClicked is set from a double click until the release that ends
	// it, which is not taken for another click.
	doubleClicked bool

	// draft is the request that was being composed in the tab, without
	// being saved, when another tab was shown.
	draft *collection.Request
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
//
// Tabs are reordered by dragging them, and a tab that is not pinned is
// pinned by being dragged to another place.
//
// After the last tab is one more, New, which is not a tab of a request: it
// is never shown, and clicking it calls onNew. The bar holds it at newAt,
// which is the end but for while a tab is dragged past it; s.tabs leaves it
// out, so a position in the bar is turned into a tab by tabAt and back by
// barIndex.
type requestTabs struct {
	bar *qt.QTabBar
	// tabs are the tabs of requests in the order they are shown.
	tabs []*requestTab
	// newAt is the position of New in the bar.
	newAt    int
	updating bool
	// last is the tab that was shown before the one that is now.
	last *requestTab
	// hovered is the tab the pointer is over, or nil.
	hovered *requestTab
	// pressed is the tab the pointer was pressed on, which may be dragged,
	// and moved is set once it has been dragged to another place, until
	// the drag ends.
	pressed *requestTab
	moved   bool

	pinnedIcon   *qt.QIcon
	unpinnedIcon *qt.QIcon

	// onSelect is called when the user picks a tab, with the tab that was
	// shown until then, which is nil if it has gone.
	onSelect func(from, to *requestTab)
	// onPins is called when the pinned tabs change, or their order does,
	// by the user's doing.
	onPins func()
	// onNew is called when New is clicked.
	onNew func()
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
	s.bar.AddTab("New")
	// New is drawn at half strength, background and text alike, by
	// painting the background of the window over it at half opacity.
	s.bar.OnPaintEvent(func(super func(*qt.QPaintEvent), event *qt.QPaintEvent) {
		super(event)
		painter := qt.NewQPainter2(s.bar.QPaintDevice)
		defer painter.Delete()
		// The palette's own colour is copied, not changed.
		veil := qt.NewQColor10(s.bar.Palette().Color(qt.QPalette__Active, qt.QPalette__Window))
		defer veil.Delete()
		veil.SetAlpha(128)
		painter.FillRect6(s.bar.TabRect(s.newAt), veil)
		painter.End()
	})

	// Tabs are dragged into another order. The tab bar reports each step
	// of the way, as the tab dragged swaps places with the one beside it,
	// and the tabs are kept in the same order as it.
	s.bar.SetMovable(true)
	s.bar.OnTabMoved(func(from, to int) {
		order := s.order()
		if from < 0 || from >= len(order) || to < 0 || to >= len(order) {
			return
		}
		moving := order[from]
		order = slices.Insert(slices.Delete(order, from, from+1), to, moving)
		// DeleteFunc changes the slice it is given, so New is found first.
		s.newAt = slices.Index(order, nil)
		s.tabs = slices.DeleteFunc(order, func(tab *requestTab) bool { return tab == nil })
		if s.pressed != nil {
			s.moved = true
		}
	})
	s.bar.OnMousePressEvent(func(super func(*qt.QMouseEvent), event *qt.QMouseEvent) {
		index := s.bar.TabAt(event.Pos())
		if index >= 0 && index == s.newAt {
			// New is not chosen or dragged.
			if event.Button() == qt.LeftButton && s.onNew != nil {
				s.onNew()
			}
			return
		}
		s.pressed, s.moved = s.tabAt(index), false
		super(event)
	})
	s.bar.OnMouseReleaseEvent(func(super func(*qt.QMouseEvent), event *qt.QMouseEvent) {
		super(event)
		dragged, moved := s.pressed, s.moved
		s.pressed, s.moved = nil, false
		// A tab dragged past New leaves it at the end again.
		if s.newAt != len(s.tabs) {
			s.quietly(func() { s.bar.MoveTab(s.newAt, len(s.tabs)) })
		}
		if !moved {
			return
		}
		// A tab dragged to another place is pinned there.
		if dragged != nil && !dragged.pinned && dragged.request != nil {
			dragged.pinned = true
			s.refresh(dragged)
		}
		s.changedPins()
	})

	// The tab under the pointer is followed, to show the pin on it.
	s.bar.SetMouseTracking(true)
	s.bar.OnMouseMoveEvent(func(super func(*qt.QMouseEvent), event *qt.QMouseEvent) {
		super(event)
		s.hover(s.tabAt(s.bar.TabAt(event.Pos())))
	})
	s.bar.OnLeaveEvent(func(super func(*qt.QEvent), event *qt.QEvent) {
		super(event)
		s.hover(nil)
	})

	s.bar.OnCurrentChanged(func(index int) {
		if s.updating {
			return
		}
		to := s.tabAt(index)
		if to == nil {
			// New, as by the wheel: the tab shown stays shown.
			if s.last != nil {
				s.show(s.last)
			}
			return
		}
		from := s.last
		s.last = to
		if s.onSelect != nil && from != to {
			s.onSelect(from, to)
		}
	})

	s.reset(nil)
	return s
}

// order returns what is in the bar, in its order, with nil for New.
func (s *requestTabs) order() []*requestTab {
	return slices.Insert(slices.Clone(s.tabs), s.newAt, nil)
}

// tabAt returns the tab at a position in the bar, or nil for New or a
// position where there is none.
func (s *requestTabs) tabAt(index int) *requestTab {
	if order := s.order(); index >= 0 && index < len(order) {
		return order[index]
	}
	return nil
}

// barIndex returns the position of a tab in the bar, or -1 if it is not
// there.
func (s *requestTabs) barIndex(tab *requestTab) int {
	if tab == nil {
		return -1
	}
	return slices.Index(s.order(), tab)
}

// changedPins reports that the pinned tabs have changed.
func (s *requestTabs) changedPins() {
	if s.onPins != nil {
		s.onPins()
	}
}

// current returns the tab that is shown. There always is one.
func (s *requestTabs) current() *requestTab {
	if tab := s.tabAt(s.bar.CurrentIndex()); tab != nil {
		return tab
	}
	return s.tabs[0]
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

// add puts a tab at the end of the row, before New.
func (s *requestTabs) add(tab *requestTab) {
	tab.button = qt.NewQToolButton2()
	tab.button.SetIconSize(qt.NewQSize2(pinIconSize, pinIconSize))
	tab.button.SetAutoRaise(true)
	tab.button.SetFocusPolicy(qt.NoFocus)
	// A click pins the tab, and it takes a double click to unpin it, so
	// that a pinned tab is not unpinned by a stray click.
	tab.button.OnMousePressEvent(func(super func(*qt.QMouseEvent), event *qt.QMouseEvent) {
		tab.pinnedByClick, tab.doubleClicked = false, false
		super(event)
	})
	tab.button.OnClicked(func() {
		if tab.doubleClicked {
			tab.doubleClicked = false
			return
		}
		if !tab.pinned {
			s.toggle(tab)
			tab.pinnedByClick = tab.pinned
		}
	})
	tab.button.OnMouseDoubleClickEvent(func(super func(*qt.QMouseEvent), event *qt.QMouseEvent) {
		// The button handles a double click as a press, which clears the
		// flags, so they are read first.
		justPinned := tab.pinnedByClick
		super(event)
		tab.doubleClicked = true
		if event.Button() == qt.LeftButton && !justPinned && tab.pinned {
			s.toggle(tab)
		}
	})
	// The tab bar is not told of the pointer moving over the button.
	tab.button.OnEnterEvent(func(super func(*qt.QEnterEvent), event *qt.QEnterEvent) {
		super(event)
		s.hover(tab)
	})

	s.quietly(func() {
		s.tabs = append(s.tabs, tab)
		index := s.bar.InsertTab(s.newAt, "")
		s.newAt++
		s.bar.SetTabButton(index, qt.QTabBar__LeftSide, tab.button.QWidget)
	})
	s.refresh(tab)
}

// take removes a tab from the row.
func (s *requestTabs) take(tab *requestTab) {
	index := s.barIndex(tab)
	if index < 0 {
		return
	}
	s.quietly(func() {
		s.tabs = slices.DeleteFunc(s.tabs, func(t *requestTab) bool { return t == tab })
		if index < s.newAt {
			s.newAt--
		}
		// The tab bar deletes the button of the tab.
		s.bar.RemoveTab(index)
	})
	for _, gone := range []**requestTab{&s.last, &s.hovered, &s.pressed} {
		if *gone == tab {
			*gone = nil
		}
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
	s.quietly(func() { s.bar.SetCurrentIndex(s.barIndex(tab)) })
	s.last = tab
}

// refresh puts the name of its request on a tab, and the pin or the type of
// the request on its button.
func (s *requestTabs) refresh(tab *requestTab) {
	index := s.barIndex(tab)
	if index < 0 {
		return
	}

	// Neither the tabs nor their buttons have tooltips.
	name := untitled
	if tab.request != nil {
		name = tab.request.Name
	}
	s.bar.SetTabText(index, name)

	// A request has to be saved before it can be pinned.
	tab.button.SetEnabled(tab.request != nil)
	switch {
	case !tab.pinned:
		tab.button.SetIcon(s.unpinnedIcon)
	case tab == s.hovered:
		tab.button.SetIcon(s.pinnedIcon)
	default:
		if icon := requestTypeIcon(tab.request.Type); icon != nil {
			tab.button.SetIcon(icon)
		}
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
		s.changedPins()
		return
	}
	s.refresh(tab)
	s.changedPins()
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
	unpinned := false
	for _, tab := range slices.Clone(s.tabs) {
		switch {
		case tab.request != r:
		case tab.pinned:
			s.take(tab)
			unpinned = true
		default:
			tab.request, tab.draft, tab.replay = nil, nil, nil
			s.refresh(tab)
		}
	}
	s.settle()
	if unpinned {
		s.changedPins()
	}
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

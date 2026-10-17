package ui

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	qt "github.com/mappu/miqt/qt6"
	"github.com/mappu/miqt/qt6/mainthread"

	"github.com/nomad-software/triggerapi/internal/crypt"
	"github.com/nomad-software/triggerapi/internal/history"
)

// The columns of the history: the box that marks an entry to be removed,
// when the request was sent, with the icon of its type, how it went, and
// what it was. A heading is its box and its day, across them all.
const (
	colHistoryCheck = iota
	colHistoryTime
	colHistoryStatus
	colHistoryTitle
)

// historyList lists the requests sent from the workspace in use, newest
// first, under a heading for each day. Selecting one opens a copy of it, with
// its response, as a request that is not saved, and double-clicking it opens
// the saved request it was sent from. Each entry has a box to tick it, and a
// click on a heading ticks every entry of its day, or none if they all are,
// for the remove button to remove them.
//
// The file is written only when flush is called, as it can be large and is
// encrypted as a whole: when the workspace is left, the keys or the folder of
// the workspaces change, and the window closes.
type historyList struct {
	root   *qt.QWidget
	search *qt.QLineEdit
	tree   *qt.QTreeWidget
	// diff compares the responses of the ticked entries, and is enabled
	// while at least two are.
	diff *qt.QToolButton

	store *history.Store
	// path is the file the store is saved to. Nothing is saved if it is
	// empty. dirty is set when the store has changed since it was saved.
	path  string
	dirty bool
	// days are the entries listed under each heading, in the order of the
	// tree, which items are mapped to by their position.
	days [][]*history.Entry
	// ticked are the entries whose boxes are ticked, which stay so while a
	// search hides them.
	ticked   map[*history.Entry]bool
	updating bool
	// toggled is set when a press of the mouse has ticked or unticked, so
	// that its release is not taken for a click.
	toggled bool
	// browsing is set while a key moves the selection, which does not open
	// what it moves to.
	browsing bool
	// opening is set while an entry is being opened, which happens in the
	// middle of the tree's handling of a click or a key.
	opening bool

	// limit returns how many entries are kept.
	limit func() int
	// now returns the time, which decides which day is today.
	now func() time.Time
	// onOpen is called when the user picks an entry, and onGoTo when they
	// double-click one, to show the saved request it was sent from.
	onOpen  func(e *history.Entry)
	onGoTo  func(e *history.Entry)
	onError func(error)
	// onDiff compares the responses of entries, and warn tells the user
	// why it could not.
	onDiff func(entries []*history.Entry) error
	warn   func(message string)
	// keys returns the keys the file is encrypted with, which are nil if
	// it is not to be.
	keys func() *crypt.Keys
}

func newHistoryList(store *history.Store, path string) *historyList {
	h := &historyList{
		root:   qt.NewQWidget2(),
		search: qt.NewQLineEdit2(),
		tree:   qt.NewQTreeWidget2(),
		limit:  func() int { return 0 },
		now:    time.Now,
		ticked: map[*history.Entry]bool{},
	}
	h.warn = func(message string) {
		qt.QMessageBox_Warning(h.root, "TriggerAPI", message)
	}

	h.search.SetPlaceholderText("Search history")
	h.search.SetClearButtonEnabled(true)
	h.search.OnTextChanged(func(string) { h.draw(h.selected()) })
	// Return goes on to the first entry found.
	h.search.OnReturnPressed(h.browse)

	h.tree.SetColumnCount(4)
	h.tree.SetHeaderHidden(true)
	// The headings are at the left edge, with the boxes of the entries
	// beneath theirs. They are always open.
	h.tree.SetRootIsDecorated(false)
	h.tree.SetIndentation(0)
	h.tree.SetItemsExpandable(false)
	h.tree.SetExpandsOnDoubleClick(false)
	h.tree.SetEditTriggers(qt.QAbstractItemView__NoEditTriggers)
	// The headings of the days are shaded, across the whole tree.
	shadeHeadings(h.tree, func(index *qt.QModelIndex) bool { return !index.Parent().IsValid() })
	// The title has the width that is left, and is cut short to fit it.
	header := h.tree.Header()
	header.SetStretchLastSection(false)
	// The column of the boxes is as wide as a box and the margins either
	// side of it.
	style := h.tree.Style()
	margin := style.PixelMetric(qt.QStyle__PM_FocusFrameHMargin, nil, h.tree.QWidget) + 1
	width := style.PixelMetric(qt.QStyle__PM_IndicatorWidth, nil, h.tree.QWidget) + 3*margin
	header.SetMinimumSectionSize(width)
	header.SetSectionResizeMode2(colHistoryCheck, qt.QHeaderView__Fixed)
	header.ResizeSection(colHistoryCheck, width)
	header.SetSectionResizeMode2(colHistoryTime, qt.QHeaderView__ResizeToContents)
	header.SetSectionResizeMode2(colHistoryStatus, qt.QHeaderView__ResizeToContents)
	header.SetSectionResizeMode2(colHistoryTitle, qt.QHeaderView__Stretch)

	// The buttons are icons along the bottom, each saying what it does in
	// its tooltip, in the colour of the tab but for those which remove.
	// There is none to open an entry, as selecting it does that.
	h.diff = newIconButton(colourHistory, drawDiff, "Compare the responses of the ticked entries with the diff tool of the settings")
	h.diff.OnClicked(h.compare)
	remove := newIconButton(colourRemove, drawBin, "Remove the ticked entries from the history, or the selected one if none is ticked")
	remove.OnClicked(h.remove)

	layout := qt.NewQVBoxLayout(h.root)
	layout.AddWidget(h.search.QWidget)
	layout.AddWidget2(h.tree.QWidget, 1)
	layout.AddLayout(newButtonRow(h.diff, remove).QLayout)

	// Clicking where there is no entry clears the selection. A heading
	// cannot be selected: a click on it ticks its day, as one on the box of
	// an entry ticks the entry, without selecting it. A double click on
	// either is two clicks.
	toggle := func(event *qt.QMouseEvent) bool {
		item := h.tree.ItemAt(event.Pos())
		if item == nil || (item.Parent() != nil && h.tree.ColumnAt(event.Pos().X()) != colHistoryCheck) {
			return false
		}
		h.toggle(item)
		h.toggled = true
		return true
	}
	h.tree.OnMousePressEvent(func(super func(*qt.QMouseEvent), event *qt.QMouseEvent) {
		h.toggled = false
		if h.tree.ItemAt(event.Pos()) == nil {
			h.tree.SetCurrentItem(nil)
			h.tree.ClearSelection()
		}
		if !toggle(event) {
			super(event)
		}
	})
	h.tree.OnMouseReleaseEvent(func(super func(*qt.QMouseEvent), event *qt.QMouseEvent) {
		if !h.toggled {
			super(event)
		}
		h.toggled = false
	})
	h.tree.OnMouseDoubleClickEvent(func(super func(*qt.QMouseEvent), event *qt.QMouseEvent) {
		if !toggle(event) {
			super(event)
		}
	})
	h.tree.OnCurrentItemChanged(func(*qt.QTreeWidgetItem, *qt.QTreeWidgetItem) {
		if h.updating || h.browsing {
			return
		}
		h.open()
	})
	// A click also opens the entry that is already selected, and a double
	// click goes on to the saved request it was sent from.
	h.tree.OnItemClicked(func(*qt.QTreeWidgetItem, int) { h.open() })
	h.tree.OnItemDoubleClicked(func(item *qt.QTreeWidgetItem, _ int) {
		if e := h.entryOf(item); e != nil && h.onGoTo != nil {
			h.onGoTo(e)
		}
	})
	// The keyboard moves through the entries without opening them, passing
	// over the headings. Return opens the entry it has got to, Space ticks
	// it, and Delete removes the ticked entries or else it.
	h.tree.OnKeyPressEvent(func(super func(*qt.QKeyEvent), event *qt.QKeyEvent) {
		switch event.Key() {
		case int(qt.Key_Return), int(qt.Key_Enter):
			h.open()
		case int(qt.Key_Space):
			if item := h.tree.CurrentItem(); item != nil {
				h.toggle(item)
			}
		case int(qt.Key_Delete):
			h.remove()
		default:
			defer func(prev bool) { h.browsing = prev }(h.browsing)
			h.browsing = true
			prev := h.tree.CurrentItem()
			super(event)
			if item := h.tree.CurrentItem(); item != nil && item.Parent() == nil {
				super(event)
				if item := h.tree.CurrentItem(); item != nil && item.Parent() == nil && prev != nil {
					h.tree.SetCurrentItem(prev)
				}
			}
		}
	})

	h.load(store, path)
	return h
}

// load replaces the history with that of the store, which is saved to path,
// with nothing ticked. Call flush first to keep the changes to the one
// before.
func (h *historyList) load(store *history.Store, path string) {
	h.store = store
	h.path = path
	h.dirty = false
	h.ticked = map[*history.Entry]bool{}
	h.draw(nil)
}

// query returns the words being searched for, in lower case. It is nil when
// there is no search.
func (h *historyList) query() []string {
	if words := strings.Fields(strings.ToLower(h.search.Text())); len(words) > 0 {
		return words
	}
	return nil
}

// draw lists the entries that match the search, under a heading for each
// day, with an entry selected if it is listed.
func (h *historyList) draw(selected *history.Entry) {
	defer func(prev bool) { h.updating = prev }(h.updating)
	h.updating = true

	h.tree.Clear()
	h.days = nil
	words := h.query()
	today := dayOf(h.now())
	var heading *qt.QTreeWidgetItem
	var day time.Time
	for _, e := range h.store.Entries {
		if words != nil && !e.Matches(words) {
			continue
		}
		if d := dayOf(e.Time); heading == nil || !d.Equal(day) {
			heading, day = h.newHeading(dayLabel(d, today)), d
			h.days = append(h.days, nil)
		}
		item := h.newEntry(heading, e)
		h.days[len(h.days)-1] = append(h.days[len(h.days)-1], e)
		if e == selected {
			h.tree.SetCurrentItem(item)
		}
	}
	h.refresh()
}

// refresh enables the button which compares responses while at least two
// entries are ticked.
func (h *historyList) refresh() {
	h.diff.SetEnabled(len(h.ticked) >= 2)
}

// tickedEntries returns the ticked entries, newest first.
func (h *historyList) tickedEntries() []*history.Entry {
	var entries []*history.Entry
	for _, e := range h.store.Entries {
		if h.ticked[e] {
			entries = append(entries, e)
		}
	}
	return entries
}

// compare has the responses of the ticked entries compared, warning of a
// failure.
func (h *historyList) compare() {
	entries := h.tickedEntries()
	if len(entries) < 2 || h.onDiff == nil {
		return
	}
	if err := h.onDiff(entries); err != nil {
		message := err.Error()
		h.warn(strings.ToUpper(message[:1]) + message[1:] + ".")
	}
}

// dayOf returns the start of the day of a time, where the user is.
func dayOf(t time.Time) time.Time {
	year, month, day := t.Local().Date()
	return time.Date(year, month, day, 0, 0, 0, 0, time.Local)
}

// dayLabel returns the heading of the entries of a day: Today, Yesterday, or
// the date.
func dayLabel(day, today time.Time) string {
	switch {
	case day.Equal(today):
		return "Today"
	case day.Equal(today.AddDate(0, 0, -1)):
		return "Yesterday"
	}
	return day.Format("Monday 2 January 2006")
}

// newHeading adds the heading of a day at the end of the top level. It is
// always open and cannot be selected. Its box is ticked when every entry
// beneath it is, and partly when some are.
func (h *historyList) newHeading(label string) *qt.QTreeWidgetItem {
	item := qt.NewQTreeWidgetItem3(h.tree)
	item.SetText(colHistoryCheck, label)
	font := item.Font(colHistoryCheck)
	font.SetBold(true)
	item.SetFont(colHistoryCheck, font)
	item.SetFlags(qt.ItemIsEnabled | qt.ItemIsUserCheckable | qt.ItemIsAutoTristate)
	item.SetCheckState(colHistoryCheck, qt.Unchecked)
	item.SetFirstColumnSpanned(true)
	item.SetExpanded(true)
	return item
}

// newEntry adds the item of an entry under a heading: its box, its time,
// with the icon of the type of its request, how it went, in the colour of
// errors if it failed, and what it was.
func (h *historyList) newEntry(heading *qt.QTreeWidgetItem, e *history.Entry) *qt.QTreeWidgetItem {
	item := qt.NewQTreeWidgetItem6(heading)
	item.SetFlags(item.Flags() | qt.ItemIsUserCheckable)
	item.SetCheckState(colHistoryCheck, checkState(h.ticked[e]))
	item.SetText(colHistoryTime, e.Time.Local().Format("15:04:05"))
	if icon := requestTypeIcon(e.Request.Type); icon != nil {
		item.SetIcon(colHistoryTime, icon)
	}
	status, failed := e.Status()
	item.SetText(colHistoryStatus, status)
	if failed {
		colour := qt.NewQColor6(themeColour(colourError))
		defer colour.Delete()
		brush := qt.NewQBrush3(colour)
		defer brush.Delete()
		item.SetForeground(colHistoryStatus, brush)
	}
	item.SetText(colHistoryTitle, e.Title())
	return item
}

// recolour lists the entries again, after the colours have changed.
func (h *historyList) recolour() {
	h.draw(h.selected())
}

// entryOf returns the entry of an item, or nil for a heading.
func (h *historyList) entryOf(item *qt.QTreeWidgetItem) *history.Entry {
	if item == nil || item.Parent() == nil {
		return nil
	}
	day := h.tree.IndexOfTopLevelItem(item.Parent())
	index := item.Parent().IndexOfChild(item)
	if day < 0 || day >= len(h.days) || index < 0 || index >= len(h.days[day]) {
		return nil
	}
	return h.days[day][index]
}

// selected returns the selected entry, or nil if there is none.
func (h *historyList) selected() *history.Entry {
	return h.entryOf(h.tree.CurrentItem())
}

// toggle ticks an entry, or unticks it if it is ticked. For a heading, it
// ticks every entry of its day, or unticks them if they all are.
func (h *historyList) toggle(item *qt.QTreeWidgetItem) {
	defer func(prev bool) { h.updating = prev }(h.updating)
	h.updating = true

	state := checkState(item.CheckState(colHistoryCheck) != qt.Checked)
	// The heading passes its state on to its entries, and an entry's is
	// passed up to its heading.
	item.SetCheckState(colHistoryCheck, state)
	heading := item
	if item.Parent() != nil {
		heading = item.Parent()
	}
	for i := range heading.ChildCount() {
		child := heading.Child(i)
		if e := h.entryOf(child); e != nil {
			if child.CheckState(colHistoryCheck) == qt.Checked {
				h.ticked[e] = true
			} else {
				delete(h.ticked, e)
			}
		}
	}
	h.refresh()
}

// open reports the selected entry, if there is one.
func (h *historyList) open() {
	if e := h.selected(); e != nil && h.onOpen != nil {
		defer func(prev bool) { h.opening = prev }(h.opening)
		h.opening = true
		h.onOpen(e)
	}
}

// browse moves the keyboard from the search box to the entries, at the
// first one found, without opening it.
func (h *historyList) browse() {
	h.tree.SetFocus()
	if h.tree.TopLevelItemCount() == 0 || h.tree.TopLevelItem(0).ChildCount() == 0 {
		return
	}
	defer func(prev bool) { h.browsing = prev }(h.browsing)
	h.browsing = true
	h.tree.SetCurrentItem(h.tree.TopLevelItem(0).Child(0))
}

// add puts an entry at the top of the history.
func (h *historyList) add(e *history.Entry) {
	h.store.Add(e, h.limit())
	h.changed()
	// Opening an entry can end a connection, which is then added. The tree
	// is not cleared from under its own handling of the click or key that
	// opened it, but once that is done.
	if h.opening {
		mainthread.Start(func() { h.draw(h.selected()) })
		return
	}
	h.draw(h.selected())
}

// keep drops the oldest entries beyond the limit, after it has changed.
func (h *historyList) keep() {
	count := len(h.store.Entries)
	h.store.Keep(h.limit())
	if len(h.store.Entries) != count {
		h.forget()
		h.draw(h.selected())
		h.changed()
	}
}

// remove takes the ticked entries out of the history, or the selected one
// if none is ticked, including those a search hides. The selection stays
// where it is if that is left, and otherwise goes to the entry listed after
// the first removed that is left, or else the last before it.
func (h *historyList) remove() {
	removed := map[*history.Entry]bool{}
	for e := range h.ticked {
		removed[e] = true
	}
	if len(removed) == 0 {
		if e := h.selected(); e != nil {
			removed[e] = true
		}
	}
	if len(removed) == 0 {
		return
	}

	shown := slices.Concat(h.days...)
	first := slices.IndexFunc(shown, func(e *history.Entry) bool { return removed[e] })
	left := slices.DeleteFunc(slices.Clone(shown), func(e *history.Entry) bool { return removed[e] })
	next := h.selected()
	if next == nil || removed[next] {
		switch {
		case first >= 0 && first < len(left):
			next = left[first]
		case len(left) > 0:
			next = left[len(left)-1]
		default:
			next = nil
		}
	}

	h.store.Remove(func(e *history.Entry) bool { return removed[e] })
	h.forget()
	h.draw(next)
	h.changed()
}

// forget unticks the entries that are no longer in the history.
func (h *historyList) forget() {
	for e := range h.ticked {
		if !slices.Contains(h.store.Entries, e) {
			delete(h.ticked, e)
		}
	}
}

// changed records that the store has changed, to be saved by flush.
func (h *historyList) changed() {
	if h.path != "" {
		h.dirty = true
	}
}

// flush writes the store to its file if it has changed since it was last
// written. A workspace whose folder has gone, as one that was removed, has
// nothing written.
func (h *historyList) flush() {
	if !h.dirty {
		return
	}
	h.dirty = false
	if _, err := os.Stat(filepath.Dir(h.path)); errors.Is(err, fs.ErrNotExist) {
		return
	}
	saveStore(h.path, h.keys, h.store.SaveWith, h.onError)
}

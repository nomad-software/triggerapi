package ui

import (
	"os"
	"slices"
	"testing"
	"time"

	qt "github.com/mappu/miqt/qt6"
	"github.com/mappu/miqt/qt6/mainthread"

	"github.com/nomad-software/triggerapi/internal/collection"
	"github.com/nomad-software/triggerapi/internal/crypt"
	"github.com/nomad-software/triggerapi/internal/history"
	"github.com/nomad-software/triggerapi/internal/rest"
)

// testHistory returns a history of REST requests with the names given, sent
// a minute apart going back from at.
func testHistory(at time.Time, names ...string) *history.Store {
	store := &history.Store{}
	for i, name := range names {
		store.Entries = append(store.Entries, &history.Entry{
			Time:    at.Add(-time.Duration(i) * time.Minute),
			Request: collection.NewREST(name, rest.Request{Method: "GET", URL: name + ".test"}),
			URL:     "http://" + name + ".test",
			HTTP:    &rest.Response{StatusCode: 200},
		})
	}
	return store
}

// historyRows returns what each row of the history shows: the heading of a
// day, or the title of an entry after a dash.
func historyRows(h *historyList) []string {
	var rows []string
	for i := range h.tree.TopLevelItemCount() {
		heading := h.tree.TopLevelItem(i)
		rows = append(rows, heading.Text(colHistoryCheck))
		for j := range heading.ChildCount() {
			rows = append(rows, "- "+heading.Child(j).Text(colHistoryTitle))
		}
	}
	return rows
}

func TestHistoryDays(t *testing.T) {
	now := time.Date(2026, 10, 17, 12, 0, 0, 0, time.Local)
	store := testHistory(now, "a", "b")
	store.Entries[1].Time = now.AddDate(0, 0, -1)
	older := testHistory(time.Date(2026, 10, 12, 8, 5, 9, 0, time.Local), "c")
	store.Entries = append(store.Entries, older.Entries...)

	mainthread.Wait(func() {
		h := newHistoryList(&history.Store{}, "")
		h.now = func() time.Time { return now }
		h.load(store, "")
		want := []string{"Today", "- a", "Yesterday", "- b", "Monday 12 October 2026", "- c"}
		if got := historyRows(h); !slices.Equal(got, want) {
			t.Errorf("rows = %q, want %q", got, want)
		}
		if got := h.tree.TopLevelItem(2).Child(0).Text(colHistoryTime); got != "08:05:09" {
			t.Errorf("time = %q", got)
		}
		// A heading cannot be selected.
		if h.tree.TopLevelItem(0).Flags()&qt.ItemIsSelectable != 0 {
			t.Error("a heading can be selected")
		}
	})
}

func TestHistorySearch(t *testing.T) {
	now := time.Now()
	mainthread.Wait(func() {
		h := newHistoryList(testHistory(now, "list rooms", "book room", "cancel"), "")
		opened := 0
		h.onOpen = func(*history.Entry) { opened++ }

		// The title and the URL are searched, every word typed.
		h.search.SetText("room")
		if got, want := historyRows(h), []string{"Today", "- list rooms", "- book room"}; !slices.Equal(got, want) {
			t.Errorf("rows = %q, want %q", got, want)
		}
		h.search.SetText("cancel.test")
		if got, want := historyRows(h), []string{"Today", "- cancel"}; !slices.Equal(got, want) {
			t.Errorf("rows = %q, want %q", got, want)
		}
		h.search.SetText("room nothing")
		if h.tree.TopLevelItemCount() != 0 {
			t.Errorf("rows = %q", historyRows(h))
		}

		// Return goes to the first found without opening it.
		h.search.SetText("room")
		h.browse()
		if e := h.selected(); e == nil || e.Title() != "list rooms" || opened != 0 {
			t.Errorf("selected %v, opened %d", e, opened)
		}
		h.search.Clear()
		if len(historyRows(h)) != 4 || h.selected() == nil || h.selected().Title() != "list rooms" {
			t.Errorf("rows = %q", historyRows(h))
		}
	})
}

// clickHistory clicks an item of the history in a column, or twice for a
// double click. The first column of a heading is clicked on its box, past
// its arrow.
func clickHistory(h *historyList, item *qt.QTreeWidgetItem, column int, double bool) {
	x := h.tree.Header().SectionViewportPosition(column) + 3
	if item.Parent() == nil && column == colHistoryCheck {
		x += h.tree.Indentation()
	}
	clickHistoryAt(h, item, x, double)
}

// clickDate clicks the date of a heading of the history, past its box, or
// twice for a double click.
func clickDate(h *historyList, item *qt.QTreeWidgetItem, double bool) {
	clickHistoryAt(h, item, h.tree.Header().SectionViewportPosition(colHistoryTitle), double)
}

// clickHistoryAt clicks the row of an item of the history at a place across
// it, or twice for a double click.
func clickHistoryAt(h *historyList, item *qt.QTreeWidgetItem, x int, double bool) {
	rect := h.tree.VisualItemRect(item)
	at := qt.NewQPointF3(float64(x), float64(rect.Center().Y()))
	defer at.Delete()
	kinds := []qt.QEvent__Type{qt.QEvent__MouseButtonPress, qt.QEvent__MouseButtonRelease}
	if double {
		kinds = append(kinds, qt.QEvent__MouseButtonDblClick, qt.QEvent__MouseButtonRelease)
	}
	for _, kind := range kinds {
		buttons := qt.LeftButton
		if kind == qt.QEvent__MouseButtonRelease {
			buttons = qt.NoButton
		}
		event := qt.NewQMouseEvent(kind, at, qt.LeftButton, buttons, qt.NoModifier)
		qt.QCoreApplication_SendEvent(h.tree.Viewport().QObject, event.QEvent)
		event.Delete()
	}
}

// tickedTitles returns the titles of the ticked entries, newest first.
func tickedTitles(h *historyList) []string {
	var titles []string
	for _, e := range h.store.Entries {
		if h.ticked[e] {
			titles = append(titles, e.Title())
		}
	}
	return titles
}

func TestHistoryRemove(t *testing.T) {
	files := testFiles(t)
	now := time.Now()
	mainthread.Wait(func() {
		h := newHistoryList(testHistory(now, "a", "b", "c"), files.History)
		h.keys = func() *crypt.Keys { return testKeys }

		// With nothing ticked, the selected entry is removed, and the one
		// after it selected, or else the one before.
		h.tree.SetCurrentItem(h.tree.TopLevelItem(0).Child(1))
		h.remove()
		if got := historyRows(h); !slices.Equal(got, []string{"Today", "- a", "- c"}) || h.selected().Title() != "c" {
			t.Errorf("rows = %q, selected %v", got, h.selected())
		}
		h.remove()
		if h.selected() == nil || h.selected().Title() != "a" {
			t.Errorf("selected %v", h.selected())
		}

		// Nothing is written until the history is flushed.
		writer.Flush()
		if _, err := os.Stat(files.History); err == nil {
			t.Error("the history was written before it was flushed")
		}
		h.flush()
	})

	saved, err := loadHistory(files.History)
	if err != nil || len(saved.Entries) != 1 || saved.Entries[0].Title() != "a" {
		t.Errorf("saved %+v, %v", saved, err)
	}
}

func TestHistoryTicking(t *testing.T) {
	now := time.Date(2026, 10, 17, 12, 0, 0, 0, time.Local)
	store := testHistory(now, "a", "b", "c", "d")
	store.Entries[2].Time = now.AddDate(0, 0, -1)
	store.Entries[3].Time = now.AddDate(0, 0, -1)
	store.Entries[3].Path = []string{"Folder", "d"}

	mainthread.Wait(func() {
		h := newHistoryList(store, "")
		h.now = func() time.Time { return now }
		h.draw(nil)
		var opened, goneTo []string
		h.onOpen = func(e *history.Entry) { opened = append(opened, e.Title()) }
		h.onGoTo = func(e *history.Entry) { goneTo = append(goneTo, e.Title()) }
		h.root.Resize(400, 400)
		h.root.Show()
		defer h.root.Hide()
		today, yesterday := h.tree.TopLevelItem(0), h.tree.TopLevelItem(1)

		// A click on the box of an entry ticks it without opening it, and
		// its heading shows that some of its day are.
		clickHistory(h, today.Child(0), colHistoryCheck, false)
		if got := tickedTitles(h); !slices.Equal(got, []string{"a"}) || len(opened) != 0 || h.selected() != nil {
			t.Errorf("ticked %q, opened %q", got, opened)
		}
		if today.CheckState(colHistoryCheck) != qt.PartiallyChecked {
			t.Errorf("heading %v", today.CheckState(colHistoryCheck))
		}

		// A click on the date of a heading ticks nothing, and selects
		// nothing.
		clickDate(h, yesterday, false)
		if got := tickedTitles(h); !slices.Equal(got, []string{"a"}) || h.selected() != nil || len(opened) != 0 {
			t.Errorf("ticked %q", got)
		}

		// A click on the box of a heading ticks every entry of its day, and
		// again unticks them.
		clickHistory(h, yesterday, colHistoryCheck, false)
		if got := tickedTitles(h); !slices.Equal(got, []string{"a", "c", "d"}) || yesterday.CheckState(colHistoryCheck) != qt.Checked {
			t.Errorf("ticked %q", got)
		}
		clickHistory(h, yesterday, colHistoryCheck, false)
		if got := tickedTitles(h); !slices.Equal(got, []string{"a"}) {
			t.Errorf("ticked %q", got)
		}
		// A day partly ticked is ticked whole.
		clickHistory(h, today, colHistoryCheck, false)
		if got := tickedTitles(h); !slices.Equal(got, []string{"a", "b"}) || today.CheckState(colHistoryCheck) != qt.Checked {
			t.Errorf("ticked %q", got)
		}

		// A click elsewhere on an entry opens it, and a double click goes to
		// its saved request. One on its box ticks it twice.
		clickHistory(h, yesterday.Child(1), colHistoryTitle, true)
		if !slices.Equal(opened, []string{"d", "d"}) || !slices.Equal(goneTo, []string{"d"}) {
			t.Errorf("opened %q, gone to %q", opened, goneTo)
		}
		clickHistory(h, yesterday.Child(0), colHistoryCheck, true)
		if got := tickedTitles(h); !slices.Equal(got, []string{"a", "b"}) || len(goneTo) != 1 {
			t.Errorf("ticked %q, gone to %q", got, goneTo)
		}

		// The ticks stay while a search hides them.
		h.search.SetText("c")
		h.search.Clear()
		if got := tickedTitles(h); !slices.Equal(got, []string{"a", "b"}) || h.tree.TopLevelItem(0).CheckState(colHistoryCheck) != qt.Checked {
			t.Errorf("ticked %q", got)
		}

		// Remove takes the ticked entries, not the selected one, which stays
		// selected.
		h.tree.SetCurrentItem(h.tree.TopLevelItem(1).Child(1))
		h.remove()
		if got := historyRows(h); !slices.Equal(got, []string{"Yesterday", "- c", "- d"}) || len(h.ticked) != 0 || h.selected().Title() != "d" {
			t.Errorf("rows = %q, selected %v", got, h.selected())
		}
	})
}

func TestHistoryKeyboard(t *testing.T) {
	now := time.Date(2026, 10, 17, 12, 0, 0, 0, time.Local)
	store := testHistory(now, "a", "b")
	store.Entries[1].Time = now.AddDate(0, 0, -1)
	mainthread.Wait(func() {
		h := newHistoryList(store, "")
		h.now = func() time.Time { return now }
		h.draw(nil)
		var opened []string
		h.onOpen = func(e *history.Entry) { opened = append(opened, e.Title()) }
		h.root.Show()
		defer h.root.Hide()

		press := func(key qt.Key) {
			event := qt.NewQKeyEvent(qt.QEvent__KeyPress, int(key), qt.NoModifier)
			defer event.Delete()
			qt.QCoreApplication_SendEvent(h.tree.QObject, event.QEvent)
		}

		// Down goes over the heading of yesterday to its entry, without
		// opening it, and Return opens it.
		h.tree.SetCurrentItem(h.tree.TopLevelItem(0).Child(0))
		opened = nil
		press(qt.Key_Down)
		if e := h.selected(); e == nil || e.Title() != "b" || len(opened) != 0 {
			t.Errorf("selected %v, opened %q", e, opened)
		}
		press(qt.Key_Return)
		if !slices.Equal(opened, []string{"b"}) {
			t.Errorf("opened %q", opened)
		}
		// Space ticks it, and Delete removes what is ticked.
		press(qt.Key_Space)
		if got := tickedTitles(h); !slices.Equal(got, []string{"b"}) {
			t.Errorf("ticked %q", got)
		}
		press(qt.Key_Delete)
		if len(h.store.Entries) != 1 || h.selected().Title() != "a" {
			t.Errorf("%d entries, selected %v", len(h.store.Entries), h.selected())
		}
	})
}

func TestHistoryHeadingsShaded(t *testing.T) {
	mainthread.Wait(func() {
		h := newHistoryList(testHistory(time.Now(), "a", "b"), "")
		h.root.Resize(400, 300)
		h.root.Show()
		defer h.root.Hide()

		heading := h.tree.TopLevelItem(0)
		// The heading is drawn as other text is, on a shaded background,
		// which its entries are not.
		if heading.Foreground(colHistoryCheck).Style() != qt.NoBrush {
			t.Error("the heading has a colour of its own")
		}
		if rowColour(h.tree, heading) == rowColour(h.tree, heading.Child(0)) {
			t.Error("the heading is not shaded")
		}
		if rowColour(h.tree, heading.Child(0)) != rowColour(h.tree, heading.Child(1)) {
			t.Error("an entry is shaded")
		}
	})
}

func TestHistoryDaysOpenAndClose(t *testing.T) {
	now := time.Date(2026, 10, 17, 12, 0, 0, 0, time.Local)
	store := testHistory(now, "a", "b", "c")
	store.Entries[2].Time = now.AddDate(0, 0, -1)
	mainthread.Wait(func() {
		h := newHistoryList(store, "")
		h.now = func() time.Time { return now }
		h.limit = func() int { return 10 }
		h.draw(nil)
		h.root.Resize(400, 400)
		h.root.Show()
		defer h.root.Hide()
		today := func() *qt.QTreeWidgetItem { return h.tree.TopLevelItem(0) }
		yesterday := func() *qt.QTreeWidgetItem { return h.tree.TopLevelItem(1) }

		// Every day is open to begin with.
		if !today().IsExpanded() || !yesterday().IsExpanded() || !h.tree.RootIsDecorated() {
			t.Fatal("the days are not open folders")
		}

		// The arrow of a heading closes it, without ticking anything, and
		// opens it again.
		arrow := h.tree.Header().SectionViewportPosition(colHistoryCheck) + h.tree.Indentation()/2
		clickHistoryAt(h, today(), arrow, false)
		if today().IsExpanded() || len(h.ticked) != 0 {
			t.Errorf("open %v, ticked %q", today().IsExpanded(), tickedTitles(h))
		}
		clickHistoryAt(h, today(), arrow, false)
		if !today().IsExpanded() {
			t.Error("the day did not open again")
		}

		// A double click on the date closes it, and another opens it,
		// without ticking anything.
		clickDate(h, today(), true)
		if today().IsExpanded() || len(h.ticked) != 0 {
			t.Errorf("open %v, ticked %q", today().IsExpanded(), tickedTitles(h))
		}
		clickDate(h, today(), true)
		if !today().IsExpanded() || len(h.ticked) != 0 {
			t.Errorf("open %v, ticked %q", today().IsExpanded(), tickedTitles(h))
		}

		// A click on its box ticks the day, leaving it open.
		clickHistory(h, today(), colHistoryCheck, false)
		if got := tickedTitles(h); !slices.Equal(got, []string{"a", "b"}) || !today().IsExpanded() {
			t.Errorf("ticked %q", got)
		}

		// A closed day stays closed when the history is drawn again, and a
		// new entry is seen in a day that is open.
		clickHistoryAt(h, yesterday(), arrow, false)
		h.add(&history.Entry{Time: now.Add(time.Minute), Request: collection.New(collection.TypeREST, "new"), Error: "refused"})
		if yesterday().IsExpanded() || !today().IsExpanded() || today().ChildCount() != 3 {
			t.Errorf("yesterday open %v, today open %v", yesterday().IsExpanded(), today().IsExpanded())
		}

		// A search opens every day, and clearing it closes them again.
		h.search.SetText("c")
		if !h.tree.TopLevelItem(0).IsExpanded() {
			t.Error("the search did not open the day it found something in")
		}
		h.search.Clear()
		if yesterday().IsExpanded() {
			t.Error("the closed day was opened by the search")
		}

		// Left and Right on an entry close and open its day.
		h.tree.SetFocus()
		h.tree.SetCurrentItem(today().Child(0))
		press := func(key qt.Key) {
			event := qt.NewQKeyEvent(qt.QEvent__KeyPress, int(key), qt.NoModifier)
			defer event.Delete()
			qt.QCoreApplication_SendEvent(h.tree.QObject, event.QEvent)
		}
		press(qt.Key_Left)
		if today().IsExpanded() {
			t.Error("Left did not close the day")
		}
		press(qt.Key_Right)
		if !today().IsExpanded() {
			t.Error("Right did not open the day")
		}

		// Another workspace's history starts with every day open.
		h.load(testHistory(now.AddDate(0, 0, -1), "x"), "")
		if !h.tree.TopLevelItem(0).IsExpanded() {
			t.Error("a day of another history is closed")
		}
	})
}

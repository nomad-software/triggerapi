package ui

import (
	"net/http"
	"strconv"

	qt "github.com/mappu/miqt/qt6"

	"github.com/nomad-software/triggerapi/internal/assert"
	"github.com/nomad-software/triggerapi/internal/mcp"
	"github.com/nomad-software/triggerapi/internal/rest"
	"github.com/nomad-software/triggerapi/internal/rpc"
)

// subjectEntry is what an assertion can look at, as it is offered.
type subjectEntry struct {
	subject assert.Subject
	label   string
}

// assertSubjects are what an assertion can look at, in the order they are
// offered.
var assertSubjects = []subjectEntry{
	{assert.Status, "Status"},
	{assert.Time, "Time (ms)"},
	{assert.Header, "Header"},
	{assert.Body, "Body"},
	{assert.Size, "Size (bytes)"},
}

// comparisonEntry is how an assertion can compare, as it is offered.
type comparisonEntry struct {
	comparison assert.Comparison
	label      string
}

// assertComparisons are how an assertion can compare, in the order they are
// offered.
var assertComparisons = []comparisonEntry{
	{assert.Equals, "equals"},
	{assert.NotEquals, "does not equal"},
	{assert.Contains, "contains"},
	{assert.NotContains, "does not contain"},
	{assert.Matches, "matches regex"},
	{assert.Exists, "exists"},
	{assert.NotExists, "does not exist"},
	{assert.Less, "is less than"},
	{assert.Greater, "is greater than"},
	{assert.Length, "has length"},
	{assert.Empty, "is empty"},
}

// subjectIndex and comparisonIndex return the place of a subject and a
// comparison in the lists they are offered in.
func subjectIndex(s assert.Subject) int {
	return selectorIndex(assertSubjects, func(e subjectEntry) bool { return e.subject == s })
}

func comparisonIndex(c assert.Comparison) int {
	return selectorIndex(assertComparisons, func(e comparisonEntry) bool { return e.comparison == c })
}

// assertIcon returns a tick for an assertion that passed, and a cross for
// one that failed.
func assertIcon(passed bool) *qt.QIcon {
	if passed {
		return cachedIcon("passed", func() *qt.QIcon { return drawIcon(iconSize*2, themeColour(colourPassed), drawTick) })
	}
	return cachedIcon("failed", func() *qt.QIcon { return drawIcon(iconSize*2, themeColour(colourFailed), drawCross) })
}

// minAssertColumn is the least width of the columns of an assertion that
// are typed in, so that there is room to type in one that is empty.
const minAssertColumn = 120

// comboPadding is the room the lists of the table of assertions leave
// before their text, which the style puts against their edge.
const comboPadding = 6

// paddedStyle is the style of the lists of the table of assertions, once
// made: that of the application, with room before the text.
var paddedStyle *qt.QProxyStyle

// paddedComboStyle returns the style of the lists of the table of
// assertions.
func paddedComboStyle() *qt.QStyle {
	if paddedStyle == nil {
		paddedStyle = qt.NewQProxyStyle()
		paddedStyle.OnSubControlRect(func(super func(qt.QStyle__ComplexControl, *qt.QStyleOptionComplex, qt.QStyle__SubControl, *qt.QWidget) *qt.QRect, control qt.QStyle__ComplexControl, option *qt.QStyleOptionComplex, sub qt.QStyle__SubControl, widget *qt.QWidget) *qt.QRect {
			rect := super(control, option, sub, widget)
			if control == qt.QStyle__CC_ComboBox && sub == qt.QStyle__SC_ComboBoxEditField {
				rect.SetLeft(rect.Left() + comboPadding)
			}
			return rect
		})
		paddedStyle.OnSizeFromContents(func(super func(qt.QStyle__ContentsType, *qt.QStyleOption, *qt.QSize, *qt.QWidget) *qt.QSize, kind qt.QStyle__ContentsType, option *qt.QStyleOption, size *qt.QSize, widget *qt.QWidget) *qt.QSize {
			hint := super(kind, option, size, widget)
			if kind == qt.QStyle__CT_ComboBox {
				hint.SetWidth(hint.Width() + comboPadding)
			}
			return hint
		})
	}
	return paddedStyle.QStyle
}

// The columns of the table of assertions.
const (
	colAssertEnabled = iota
	colAssertSubject
	colAssertPath
	colAssertComparison
	colAssertValue
	colAssertStatus
	colAssertRemove
)

// assertTable is the table of the Assertions tab of a response panel: what
// the response should be, a check to a row, each of which can be switched
// off. Each row has what it looks at and how it compares chosen from a list,
// and the path and the value typed in, which are not typed where the check
// has none. An empty row is always kept at the bottom for a new check, and
// each row but that has a button at its right which removes it. The rows are
// kept in the order they were added, as that is the order they are read in.
// After a response each row is marked with how its check went, and why it
// failed if it did.
type assertTable struct {
	table    *qt.QTableWidget
	updating bool

	// onChange is called after the user edits or removes a row.
	onChange func()

	// statuses records that rows have statuses on show, which no longer
	// hold once the rows are changed. onStale is called when they are
	// taken away because of that.
	statuses bool
	onStale  func()
}

func newAssertTable(highlight *placeholderHighlighter) *assertTable {
	t := &assertTable{table: qt.NewQTableWidget3(0, 7)}

	t.table.SetHorizontalHeaderLabels([]string{"", "Check", "Name", "Comparison", "Value", "Result", ""})
	t.table.SetIconSize(qt.NewQSize2(removeIconSize, removeIconSize))
	t.table.VerticalHeader().SetVisible(false)
	t.table.SetSelectionBehavior(qt.QAbstractItemView__SelectRows)
	t.table.SetSelectionMode(qt.QAbstractItemView__SingleSelection)
	// Placeholders in the values are replaced when they are checked.
	highlight.table(t.table)
	delegateCheckColumn(t.table, colAssertEnabled)
	delegateRemoveColumn(t.table, colAssertRemove)

	// The columns are fitted to what is in them (fitColumns).
	t.table.SetHorizontalScrollMode(qt.QAbstractItemView__ScrollPerPixel)
	t.table.OnResizeEvent(func(super func(*qt.QResizeEvent), event *qt.QResizeEvent) {
		super(event)
		t.fitColumns()
	})

	t.table.OnItemChanged(func(item *qt.QTableWidgetItem) {
		if !t.updating {
			t.edited(item.Row())
		}
		t.fitColumns()
	})

	t.addRow(assert.Check{Subject: assert.Status, Comparison: assert.Equals, Enabled: true})
	return t
}

func (t *assertTable) widget() *qt.QWidget {
	return t.table.QWidget
}

// edited is called after a row has been changed by the user. A change to
// the empty row makes it a check, and another empty row is added.
func (t *assertTable) edited(row int) {
	if row == t.table.RowCount()-1 {
		t.addRow(assert.Check{Subject: assert.Status, Comparison: assert.Equals, Enabled: true})
	}
	t.changed()
}

func (t *assertTable) changed() {
	if t.statuses {
		t.showResults(nil)
		if t.onStale != nil {
			t.onStale()
		}
	}
	if t.onChange != nil {
		t.onChange()
	}
}

// addRow adds a row holding a check at the bottom of the table.
func (t *assertTable) addRow(c assert.Check) {
	defer func(prev bool) { t.updating = prev }(t.updating)
	t.updating = true

	row := t.table.RowCount()
	t.table.InsertRow(row)

	check := qt.NewQTableWidgetItem()
	check.SetFlags(qt.ItemIsEnabled | qt.ItemIsSelectable | qt.ItemIsUserCheckable)
	check.SetCheckState(checkState(c.Enabled))
	t.table.SetItem(row, colAssertEnabled, check)

	t.table.SetItem(row, colAssertPath, qt.NewQTableWidgetItem2(c.Path))
	t.table.SetItem(row, colAssertValue, qt.NewQTableWidgetItem2(c.Value))

	// The cells of the lists hold nothing of their own, and are drawn
	// selected with the rest of the row.
	for _, column := range []int{colAssertSubject, colAssertComparison} {
		blank := qt.NewQTableWidgetItem()
		blank.SetFlags(qt.ItemIsEnabled | qt.ItemIsSelectable)
		t.table.SetItem(row, column, blank)
	}
	subjects := qt.NewQComboBox2()
	subjects.SetStyle(paddedComboStyle())
	for _, s := range assertSubjects {
		subjects.AddItem(s.label)
	}
	subjects.SetCurrentIndex(subjectIndex(c.Subject))
	comparisons := qt.NewQComboBox2()
	comparisons.SetStyle(paddedComboStyle())
	for _, e := range assertComparisons {
		comparisons.AddItem(e.label)
	}
	comparisons.SetCurrentIndex(comparisonIndex(c.Comparison))
	t.table.SetCellWidget(row, colAssertSubject, subjects.QWidget)
	t.table.SetCellWidget(row, colAssertComparison, comparisons.QWidget)
	// The rows move as others are removed, so the row of a list is found
	// when it is changed.
	for _, combo := range []*qt.QComboBox{subjects, comparisons} {
		combo.OnCurrentIndexChanged(func(int) {
			if t.updating {
				return
			}
			if row := t.rowOf(combo.QWidget); row >= 0 {
				t.fitCells(row)
				t.edited(row)
				t.fitColumns()
			}
		})
	}

	// Nothing is typed in the cell of the result, which is left enabled so
	// that its icon is not greyed, or in that of the button.
	status := qt.NewQTableWidgetItem()
	status.SetFlags(qt.ItemIsEnabled | qt.ItemIsSelectable)
	t.table.SetItem(row, colAssertStatus, status)
	blank := qt.NewQTableWidgetItem()
	blank.SetFlags(qt.NoItemFlags)
	t.table.SetItem(row, colAssertRemove, blank)

	t.fitCells(row)

	// The row above is no longer the empty one, so it can now be removed.
	if row > 0 && t.table.CellWidget(row-1, colAssertRemove) == nil {
		t.table.SetCellWidget(row-1, colAssertRemove, newRowRemoveButton(t.table, colAssertRemove, t.remove))
	}
	t.fitColumns()
}

// fitColumns makes each column of the table as wide as what is in it, and
// no narrower than its heading, or than minAssertColumn for the path and
// the value, which share the width left over. If there is not enough for
// everything, the table scrolls sideways.
func (t *assertTable) fitColumns() {
	header := t.table.HorizontalHeader()
	total := 0
	for column := range t.table.ColumnCount() {
		header.SetSectionResizeMode2(column, qt.QHeaderView__Interactive)
		width := max(t.table.SizeHintForColumn(column), header.SectionSizeHint(column))
		if t.flows(column) {
			width = max(width, minAssertColumn)
		}
		t.table.SetColumnWidth(column, width)
		total += width
	}

	spare := t.table.Viewport().Width() - total
	if spare <= 0 {
		return
	}
	flowing := []int{colAssertPath, colAssertValue}
	for i, column := range flowing {
		share := spare / len(flowing)
		if i == len(flowing)-1 {
			share = spare - share*(len(flowing)-1)
		}
		t.table.SetColumnWidth(column, header.SectionSize(column)+share)
	}
}

// flows reports whether a column is one of those that are given the width
// left over: the path and the value.
func (t *assertTable) flows(column int) bool {
	return column == colAssertPath || column == colAssertValue
}

// rowOf returns the row a widget of a cell is in, or -1 if it is in none.
func (t *assertTable) rowOf(widget *qt.QWidget) int {
	for row := range t.table.RowCount() {
		for _, column := range []int{colAssertSubject, colAssertComparison} {
			if cell := t.table.CellWidget(row, column); cell != nil && cell.UnsafePointer() == widget.UnsafePointer() {
				return row
			}
		}
	}
	return -1
}

// fitCells has the path and the value of a row typed in only where its
// check has them. What is in them is kept, faded, in case it is wanted
// again.
func (t *assertTable) fitCells(row int) {
	defer func(prev bool) { t.updating = prev }(t.updating)
	t.updating = true

	c := t.check(row)
	for column, used := range map[int]bool{colAssertPath: c.Subject.HasPath(), colAssertValue: c.Comparison.HasValue()} {
		flags := qt.ItemIsSelectable
		if used {
			flags |= qt.ItemIsEnabled | qt.ItemIsEditable
		}
		t.table.Item(row, column).SetFlags(flags)
	}
}

// combo returns the list in a cell.
func (t *assertTable) combo(row, column int) *qt.QComboBox {
	return qt.UnsafeNewQComboBox(t.table.CellWidget(row, column).UnsafePointer())
}

// check returns the check of a row.
func (t *assertTable) check(row int) assert.Check {
	return assert.Check{
		Subject:    assertSubjects[max(t.combo(row, colAssertSubject).CurrentIndex(), 0)].subject,
		Path:       t.table.Item(row, colAssertPath).Text(),
		Comparison: assertComparisons[max(t.combo(row, colAssertComparison).CurrentIndex(), 0)].comparison,
		Value:      t.table.Item(row, colAssertValue).Text(),
		Enabled:    t.table.Item(row, colAssertEnabled).CheckState() == qt.Checked,
	}
}

// checks returns the checks of every row but the empty one.
func (t *assertTable) checks() []assert.Check {
	var checks []assert.Check
	for row := range t.table.RowCount() - 1 {
		checks = append(checks, t.check(row))
	}
	return checks
}

// setChecks replaces the rows of the table with checks, in their order,
// without calling onChange.
func (t *assertTable) setChecks(checks []assert.Check) {
	defer func(prev bool) { t.updating = prev }(t.updating)
	t.updating = true

	t.statuses = false
	t.table.SetRowCount(0)
	for _, c := range checks {
		t.addRow(c)
	}
	t.addRow(assert.Check{Subject: assert.Status, Comparison: assert.Equals, Enabled: true})
}

// remove deletes a row, keeping the empty row at the bottom.
func (t *assertTable) remove(row int) {
	if row < 0 || row >= t.table.RowCount()-1 {
		return
	}
	t.table.RemoveRow(row)
	t.changed()
	t.fitColumns()
}

// add makes the empty row a check, adds another empty row, and opens the
// value of the check for editing, as the caller may only have guessed at it.
func (t *assertTable) add(c assert.Check) {
	row := t.table.RowCount() - 1
	t.updating = true
	t.combo(row, colAssertSubject).SetCurrentIndex(subjectIndex(c.Subject))
	t.combo(row, colAssertComparison).SetCurrentIndex(comparisonIndex(c.Comparison))
	t.table.Item(row, colAssertPath).SetText(c.Path)
	t.table.Item(row, colAssertValue).SetText(c.Value)
	t.table.Item(row, colAssertEnabled).SetCheckState(checkState(c.Enabled))
	t.updating = false
	t.fitCells(row)
	t.edited(row)

	item := t.table.Item(row, colAssertValue)
	t.table.SetCurrentItem(item)
	if c.Comparison.HasValue() {
		t.table.EditItem(item)
	}
}

// showResults marks each row with how its check went: a tick if it passed
// and a cross and why if it failed. A result goes to the
// first row of the same check that has none, so that it is against the
// right row even if rows have since been added or removed. With no results
// the marks are taken away. They are taken away again when the rows are
// next changed.
func (t *assertTable) showResults(results []assert.Result) {
	defer func(prev bool) { t.updating = prev }(t.updating)
	t.updating = true

	t.statuses = len(results) > 0
	none := qt.NewQIcon()
	defer none.Delete()

	used := make([]bool, len(results))
	for row := range t.table.RowCount() {
		item := t.table.Item(row, colAssertStatus)
		item.SetIcon(none)
		item.SetText("")
		if row == t.table.RowCount()-1 {
			continue
		}
		c := t.check(row)
		for i, r := range results {
			if used[i] || r.Check != c {
				continue
			}
			used[i] = true
			item.SetIcon(assertIcon(r.Passed))
			if r.Passed {
				item.SetText("Passed")
			} else {
				item.SetText(r.Reason)
			}
			break
		}
	}
}

// httpFacts returns what is checked of an HTTP response: the status as its
// code, then its status line, and the body values are captured from, which
// for a stream of events is the data of the last.
func httpFacts(res *rest.Response) assert.Facts {
	names := []string{strconv.Itoa(res.StatusCode)}
	if res.Status != "" && res.Status != names[0] {
		names = append(names, res.Status)
	}
	return assert.Facts{Status: names, Time: res.Duration, Headers: res.Headers, Body: res.Document(), Size: len(res.Body)}
}

// rpcFacts returns what is checked of the response to a gRPC call: the name
// of its code then its number, its headers and trailers, and the last
// message as the body. The size is that of all the messages.
func rpcFacts(res *rpc.Response) assert.Facts {
	headers := http.Header{}
	for _, h := range []http.Header{res.Headers, res.Trailers} {
		for key, values := range h {
			headers[key] = append(headers[key], values...)
		}
	}
	f := assert.Facts{Status: []string{res.CodeName, strconv.Itoa(res.Code)}, Time: res.Duration, Headers: headers}
	for _, m := range res.Messages {
		f.Size += len(m)
	}
	if len(res.Messages) > 0 {
		f.Body = res.Messages[len(res.Messages)-1]
	}
	return f
}

// mcpFacts returns what is checked of the answer to a call to an MCP server:
// its outcome, and the result, or the error the server answered with, as
// the body. The status of a JSON-RPC error is its code and name, and each
// of those alone.
func mcpFacts(res *mcp.Response) assert.Facts {
	status, _, _ := res.Outcome()
	names := []string{status}
	if res.Error != nil {
		names = append(names, strconv.FormatInt(res.Error.Code, 10), mcp.CodeName(res.Error.Code))
	}
	body := res.Text()
	return assert.Facts{Status: names, Time: res.Duration, Headers: res.Headers, Body: body, Size: len(body)}
}

// runAssertions checks the facts of a response with the checks of its
// request, with the placeholders of their values replaced from the active
// environment, as it is once any values have been captured.
func (w *Window) runAssertions(checks []assert.Check, facts assert.Facts) []assert.Result {
	return assert.Run(checks, facts, w.expand)
}

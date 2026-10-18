package ui

import (
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"unicode/utf16"

	qt "github.com/mappu/miqt/qt6"
	"github.com/mappu/miqt/qt6/mainthread"

	"github.com/nomad-software/triggerapi/internal/assert"
	"github.com/nomad-software/triggerapi/internal/collection"
	"github.com/nomad-software/triggerapi/internal/env"
	"github.com/nomad-software/triggerapi/internal/mcp/mcptest"
	"github.com/nomad-software/triggerapi/internal/rest"
	"github.com/nomad-software/triggerapi/internal/rpc/rpctest"
)

// assertMarks returns how the checks of a response are shown to have gone on
// a response panel: "tick", "cross" or "" for the tab, and the same for each
// row of the table, in order, with what the Result column of each says.
func assertMarks(p *responsePanel) (tab string, rows, results []string) {
	name := func(icon *qt.QIcon) string {
		switch {
		case icon.IsNull():
			return ""
		case icon.CacheKey() == assertIcon(true).CacheKey():
			return "tick"
		case icon.CacheKey() == assertIcon(false).CacheKey():
			return "cross"
		}
		return "unknown"
	}

	table := p.assertions.table
	for row := range table.RowCount() - 1 {
		rows = append(rows, name(table.Item(row, colAssertStatus).Icon()))
		results = append(results, table.Item(row, colAssertStatus).Text())
	}
	return name(p.tabs.TabIcon(tabAssertions)), rows, results
}

func TestAssertions(t *testing.T) {
	status := http.StatusOK
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		io.WriteString(w, `{"data": {"id": 7, "items": [1, 2, 3]}}`)
	}))
	defer srv.Close()

	files := testFiles(t)
	store := env.Store{Environments: []env.Environment{
		{Name: "Local", Variables: []env.Variable{{Key: "host", Value: srv.URL, Enabled: true}, {Key: "id", Value: "7", Enabled: true}}},
	}}
	if err := store.Save(files.Environments); err != nil {
		t.Fatal(err)
	}

	checks := []assert.Check{
		{Subject: assert.Status, Comparison: assert.Equals, Value: "200", Enabled: true},
		{Subject: assert.Body, Path: "data.id", Comparison: assert.Equals, Value: "{{id}}", Enabled: true},
		{Subject: assert.Body, Path: "data.items", Comparison: assert.Length, Value: "4", Enabled: true},
		{Subject: assert.Header, Path: "Content-Type", Comparison: assert.Contains, Value: "json"},
	}

	var w *Window
	var request *collection.Request
	mainthread.Wait(func() {
		w = NewWindow(files.Files)
		w.Show()
		w.requests.addFolder()
		w.requests.addRequest()
		request = w.open

		// The assertions are on a tab of every response but that of a
		// WebSocket, after the Capture tab.
		for name, p := range map[string]*responsePanel{"REST": w.response, "gRPC": w.grpcResponse, "GraphQL": w.graphqlResponse, "MCP": w.mcpResponse} {
			if p.tabs.TabText(tabAssertions) != "Assertions" || p.assertions == nil {
				t.Errorf("%s response has %q where the Assertions tab should be", name, p.tabs.TabText(tabAssertions))
			}
		}
		if w.wsResponse.assertions != nil {
			t.Error("a WebSocket response has assertions")
		}
		if !w.response.tabs.Widget(tabAssertions).IsAncestorOf(w.request.assertions.table.QWidget) {
			t.Error("the assertions are not on the tab")
		}

		w.request.url.SetText("{{host}}/item")
		w.request.assertions.setChecks(checks)
		w.request.assertions.onChange()

		// They are part of the request, in the order they were given,
		// and are saved with it.
		if !slices.Equal(request.Request.Assertions, checks) {
			t.Errorf("assertions = %+v", request.Request.Assertions)
		}
		if saved, err := loadRequests(files.Requests); err != nil || !slices.Equal(saved.Folders[0].Requests[0].Request.Assertions, checks) {
			t.Errorf("saved %+v, %v", saved, err)
		}

		w.request.send.Click()
	})
	waitFor(t, "the response", func() bool { return strings.Contains(w.response.result.Text(), "200 OK") })

	mainthread.Wait(func() {
		// Each check is marked with how it went and why it failed, the
		// placeholder of a value replaced, and the tab with a cross as one
		// failed. One that is switched off is not checked.
		tab, rows, results := assertMarks(w.response)
		if tab != "cross" || !slices.Equal(rows, []string{"tick", "tick", "cross", ""}) {
			t.Errorf("tab %q, rows %v", tab, rows)
		}
		if !slices.Equal(results, []string{"Passed", "Passed", "the length of data.items is 3, not 4", ""}) {
			t.Errorf("results %q", results)
		}
		// Nothing is said of it in the bar, and nothing has a tooltip.
		if strings.Contains(w.response.result.Text()+w.response.details.Text(), "ssert") || w.response.tabs.TabToolTip(tabAssertions) != "" {
			t.Errorf("bar = %q, %q", w.response.result.Text(), w.response.details.Text())
		}

		// The results are kept in the history.
		if e := w.history.store.Entries[0]; len(e.Assertions) != 3 || assert.Failed(e.Assertions) != 1 {
			t.Errorf("history has %+v", e.Assertions)
		}

		// The marks follow the colours of the settings.
		s := w.config
		s.Colours.Passed, s.Colours.Failed = "#002200", "#220000"
		if err := w.applySettings(s); err != nil {
			t.Error(err)
		}
		if got := iconColour(w.response.tabs.TabIcon(tabAssertions)); got != "#220000" {
			t.Errorf("the cross is %s", got)
		}
		if got := iconColour(w.request.assertions.table.Item(0, colAssertStatus).Icon()); got != "#002200" {
			t.Errorf("the tick is %s", got)
		}

		// Changing the assertions takes the marks away, as they are of the
		// assertions as they were.
		w.request.assertions.remove(2)
		if tab, rows, _ := assertMarks(w.response); tab != "" || slices.Contains(rows, "tick") {
			t.Errorf("after a change: tab %q, rows %v", tab, rows)
		}
		w.request.send.Click()
	})
	waitFor(t, "the marks", func() bool { tab, _, _ := assertMarks(w.response); return tab != "" })

	mainthread.Wait(func() {
		if tab, rows, _ := assertMarks(w.response); tab != "tick" || !slices.Equal(rows, []string{"tick", "tick", ""}) {
			t.Errorf("tab %q, rows %v", tab, rows)
		}

		// A failed status is checked too.
		status = http.StatusNotFound
		w.request.send.Click()
	})
	waitFor(t, "the failure", func() bool { return strings.Contains(w.response.result.Text(), "404") })

	mainthread.Wait(func() {
		if tab, rows, results := assertMarks(w.response); tab != "cross" || rows[0] != "cross" || results[0] != "the status is 404, not 200" {
			t.Errorf("tab %q, rows %v, results %q", tab, rows, results)
		}

		// Opening the entry of the history shows how its checks went.
		w.openEntry(w.history.store.Entries[1])
		if tab, rows, _ := assertMarks(w.response); tab != "tick" || !slices.Equal(rows, []string{"tick", "tick", ""}) {
			t.Errorf("from the history: tab %q, rows %v", tab, rows)
		}
	})
}

// TestAssertionTable checks that rows are added by changing the empty one,
// and that the path and value are typed only where a check has them.
func TestAssertionTable(t *testing.T) {
	mainthread.Wait(func() {
		w := NewWindow(testFiles(t).Files)
		w.Show()
		table := w.request.assertions
		changes := 0
		table.onChange = func() { changes++ }

		editable := func(row, column int) bool {
			return table.table.Item(row, column).Flags()&qt.ItemIsEditable != 0
		}
		if table.table.RowCount() != 1 || len(table.checks()) != 0 {
			t.Fatalf("%d rows, checks %+v", table.table.RowCount(), table.checks())
		}
		// A status has no path, but equals has a value.
		if editable(0, colAssertPath) || !editable(0, colAssertValue) {
			t.Error("the empty row has a path to type, or no value")
		}

		// Choosing what the empty row checks makes it a check.
		table.combo(0, colAssertSubject).SetCurrentIndex(subjectIndex(assert.Body))
		if table.table.RowCount() != 2 || changes != 1 || !editable(0, colAssertPath) {
			t.Errorf("%d rows after %d changes", table.table.RowCount(), changes)
		}
		table.table.Item(0, colAssertPath).SetText("data.id")
		table.combo(0, colAssertComparison).SetCurrentIndex(comparisonIndex(assert.Exists))
		if editable(0, colAssertValue) {
			t.Error("exists has a value to type")
		}
		want := []assert.Check{{Subject: assert.Body, Path: "data.id", Comparison: assert.Exists, Enabled: true}}
		if !slices.Equal(table.checks(), want) || table.table.RowCount() != 2 {
			t.Errorf("checks %+v in %d rows", table.checks(), table.table.RowCount())
		}

		// Typing a value in the empty row makes another check.
		table.table.Item(1, colAssertValue).SetText("200")
		want = append(want, assert.Check{Subject: assert.Status, Comparison: assert.Equals, Value: "200", Enabled: true})
		if !slices.Equal(table.checks(), want) || table.table.RowCount() != 3 {
			t.Errorf("checks %+v in %d rows", table.checks(), table.table.RowCount())
		}

		// The rows keep their order, and each but the empty one can be
		// removed.
		if table.table.CellWidget(1, colAssertRemove) == nil || table.table.CellWidget(2, colAssertRemove) != nil {
			t.Error("the bins are not where they should be")
		}
		table.remove(0)
		if !slices.Equal(table.checks(), want[1:]) {
			t.Errorf("checks %+v", table.checks())
		}
		// The list of a row is found once rows have moved.
		table.combo(0, colAssertComparison).SetCurrentIndex(comparisonIndex(assert.Less))
		if got := table.checks(); len(got) != 1 || got[0].Comparison != assert.Less {
			t.Errorf("checks %+v", got)
		}

		// Every subject and comparison there is can be chosen.
		if len(assertSubjects) != len(assert.Subjects) || len(assertComparisons) != len(assert.Comparisons) {
			t.Error("not every subject and comparison is offered")
		}
		for _, s := range assert.Subjects {
			if assertSubjects[subjectIndex(s)].subject != s {
				t.Errorf("%s is not offered", s)
			}
		}
		for _, c := range assert.Comparisons {
			if assertComparisons[comparisonIndex(c)].comparison != c {
				t.Errorf("%s is not offered", c)
			}
		}
	})
}

// The menu of the body of a response offers to assert that the value it is
// opened on is what it is.
func TestClickToAssert(t *testing.T) {
	mainthread.Wait(func() {
		w := NewWindow(testFiles(t).Files)
		w.Show()

		p := w.response
		p.show(&rest.Response{Status: "200 OK", StatusCode: 200, Headers: http.Header{"Content-Type": {"application/json"}}, Body: []byte(`{"data":{"items":[{"id":7,"name":"Ann"}]}}`)})
		body := p.body.ToPlainText()
		menu := p.contextMenu(len(utf16.Encode([]rune(body[:strings.Index(body, `"Ann"`)]))) + 1)
		actions := menu.Actions()
		last := actions[len(actions)-1]
		if last.Text() != "Assert data.items[0].name" {
			t.Fatalf("the menu offers %q", last.Text())
		}
		last.Trigger()

		want := []assert.Check{{Subject: assert.Body, Path: "data.items[0].name", Comparison: assert.Equals, Value: "Ann", Enabled: true}}
		table := w.request.assertions
		if !slices.Equal(table.checks(), want) {
			t.Errorf("checks %+v", table.checks())
		}
		if p.tabs.CurrentIndex() != tabAssertions || table.table.CurrentRow() != 0 || table.table.CurrentColumn() != colAssertValue {
			t.Errorf("tab %d, cell %d, %d", p.tabs.CurrentIndex(), table.table.CurrentRow(), table.table.CurrentColumn())
		}
	})
}

func TestAssertionsGRPC(t *testing.T) {
	srv := rpctest.NewServer(rpctest.ReflectionAll)
	defer srv.Close()

	var w *Window
	mainthread.Wait(func() {
		w = NewWindow(testFiles(t).Files)
		w.Show()
		w.requests.addFolder()
		addGRPCRequest(w)
		w.grpc.url.SetText(srv.URL)
		w.grpc.targetChanged()
	})
	waitFor(t, "the methods", func() bool { return w.grpc.method.count() == 4 })

	mainthread.Wait(func() {
		w.grpc.method.selectMethod("test.v1.Echo/Repeat")
		w.grpc.message.SetPlainText(`{"text": "hello", "count": 3}`)
		w.grpc.assertions.setChecks([]assert.Check{
			{Subject: assert.Status, Comparison: assert.Equals, Value: "ok", Enabled: true},
			{Subject: assert.Body, Path: "index", Comparison: assert.Equals, Value: "2", Enabled: true},
		})
		w.grpc.assertions.onChange()
		w.grpc.invoke.Click()
	})
	waitFor(t, "the response", func() bool { return strings.Contains(w.grpcResponse.result.Text(), "0 OK") })

	mainthread.Wait(func() {
		// The body is the last message.
		if tab, rows, _ := assertMarks(w.grpcResponse); tab != "tick" || !slices.Equal(rows, []string{"tick", "tick"}) {
			t.Errorf("tab %q, rows %v", tab, rows)
		}

		w.grpc.method.selectMethod("test.v1.Echo/Fail")
		w.grpc.invoke.Click()
	})
	waitFor(t, "the failure", func() bool { return strings.Contains(w.grpcResponse.result.Text(), "NOT_FOUND") })

	mainthread.Wait(func() {
		if tab, _, results := assertMarks(w.grpcResponse); tab != "cross" || results[0] != "the status is NOT_FOUND, not ok" || results[1] != "the response has no body" {
			t.Errorf("tab %q, results %q", tab, results)
		}
	})
}

func TestAssertionsMCP(t *testing.T) {
	srv := mcptest.NewServer(t)
	files := testFiles(t)
	envs := &env.Store{Environments: []env.Environment{{Name: "Local", Variables: []env.Variable{{Key: "host", Value: srv.URL, Enabled: true}}}}}
	if err := envs.Save(files.Environments); err != nil {
		t.Fatal(err)
	}

	var w *Window
	mainthread.Wait(func() {
		w = NewWindow(files.Files)
		w.Show()
		defer func(prev collection.Type) { testRequestType = prev }(testRequestType)
		testRequestType = collection.TypeMCP
		w.requests.addRequest()
		w.mcp.url.SetText("{{host}}/mcp")
		w.mcp.reload.Click()
	})
	waitFor(t, "the capabilities", func() bool { return mainthread.Wait2(w.mcp.endpoint.count) == 7 })

	mainthread.Wait(func() {
		w.mcp.endpoint.selectMethod("Tools/fail")
		w.mcp.assertions.setChecks([]assert.Check{
			{Subject: assert.Status, Comparison: assert.Equals, Value: "OK", Enabled: true},
			{Subject: assert.Body, Path: "isError", Comparison: assert.Equals, Value: "true", Enabled: true},
		})
		w.mcp.assertions.onChange()
		w.mcp.call.Click()
	})
	waitFor(t, "the marks", func() bool {
		return mainthread.Wait2(func() bool { tab, _, _ := assertMarks(w.mcpResponse); return tab != "" })
	})

	mainthread.Wait(func() {
		if tab, rows, results := assertMarks(w.mcpResponse); tab != "cross" || !slices.Equal(rows, []string{"cross", "tick"}) || results[0] != "the status is Tool error, not OK" {
			t.Errorf("tab %q, rows %v, results %q", tab, rows, results)
		}
	})
}

package ui

import (
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"unicode/utf16"

	qt "github.com/mappu/miqt/qt6"
	"github.com/mappu/miqt/qt6/mainthread"

	"github.com/nomad-software/triggerapi/internal/capture"
	"github.com/nomad-software/triggerapi/internal/collection"
	"github.com/nomad-software/triggerapi/internal/env"
	"github.com/nomad-software/triggerapi/internal/rest"
	"github.com/nomad-software/triggerapi/internal/rpc"
	"github.com/nomad-software/triggerapi/internal/rpc/rpctest"
)

// captureMarks returns how capturing is shown to have gone on a response
// panel: "tick", "cross" or "" for the tab, and the same for each row of the
// table by its key, with the tooltips of the rows.
func captureMarks(p *responsePanel) (tab string, rows, tips map[string]string) {
	name := func(icon *qt.QIcon) string {
		switch {
		case icon.IsNull():
			return ""
		case icon.CacheKey() == captureIcon(true).CacheKey():
			return "tick"
		case icon.CacheKey() == captureIcon(false).CacheKey():
			return "cross"
		}
		return "unknown"
	}

	rows, tips = map[string]string{}, map[string]string{}
	table := p.captures.table
	for row := range table.RowCount() - 1 {
		key := table.Item(row, colKey).Text()
		rows[key] = name(table.Item(row, colStatus).Icon())
		tips[key] = table.Item(row, colStatus).ToolTip()
	}
	return name(p.tabs.TabIcon(tabCapture)), rows, tips
}

func TestCapture(t *testing.T) {
	status := http.StatusOK
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		io.WriteString(w, `{"data": {"token": "abc123", "items": [{"id": 7}, {"id": 8}]}}`)
	}))
	defer srv.Close()

	files := testFiles(t)
	store := env.Store{Environments: []env.Environment{
		{Name: "Local", Variables: []env.Variable{{Key: "host", Value: srv.URL, Enabled: true}, {Key: "token", Value: "old", Enabled: false}}},
	}}
	if err := store.Save(files.Environments); err != nil {
		t.Fatal(err)
	}

	var w *Window
	var request *collection.Request
	mainthread.Wait(func() {
		w = NewWindow(files.Files)
		w.Show()
		w.requests.addFolder()
		w.requests.addRequest()
		request = w.open

		// The captures are set on a tab of the response, which the Timing
		// tab of a REST response comes after.
		for name, tabs := range map[string]*qt.QTabWidget{"REST": w.response.tabs, "gRPC": w.grpcResponse.tabs} {
			if tabs.TabText(tabCapture) != "Capture" {
				t.Errorf("%s response has %q where the Capture tab should be", name, tabs.TabText(tabCapture))
			}
		}
		if w.response.tabs.Widget(tabCapture).IsAncestorOf(w.request.captures.table.QWidget) == false {
			t.Error("the captures are not on the tab")
		}

		w.request.url.SetText("{{host}}/login")
		w.request.captures.add("token", "data.token")
		w.request.captures.add("last", "data.items[-1].id")
		w.request.captures.add("missing", "data.nothing")
		w.request.captures.add("unused", "data.token")
		rules := captureRules(w.request.captures)
		for i := range rules {
			rules[i].Enabled = rules[i].Variable != "unused"
		}
		// A regex picks out part of the value: what it matches, or what
		// its first group does.
		rules = append(rules,
			capture.Rule{Variable: "tokenDigits", Path: "data.token", Regex: `\d+`, Enabled: true},
			capture.Rule{Variable: "tokenLetters", Path: "data.token", Regex: `^([a-z]+)\d`, Enabled: true},
		)
		setCaptureRules(w.request.captures, rules)
		w.request.captures.onChange()

		// The regex has a column of its own, after the path.
		if table := w.request.captures.table; table.IsColumnHidden(colDescription) || table.Item(3, colDescription).Text() != `\d+` || !strings.HasPrefix(table.HorizontalHeaderItem(colDescription).Text(), "Regex") {
			t.Errorf("the regex of row 3 is %q", table.Item(3, colDescription).Text())
		}

		// The captures are part of the request, and are saved with it.
		want := []capture.Rule{
			{Variable: "last", Path: "data.items[-1].id", Enabled: true},
			{Variable: "missing", Path: "data.nothing", Enabled: true},
			{Variable: "token", Path: "data.token", Enabled: true},
			{Variable: "tokenDigits", Path: "data.token", Regex: `\d+`, Enabled: true},
			{Variable: "tokenLetters", Path: "data.token", Regex: `^([a-z]+)\d`, Enabled: true},
			{Variable: "unused", Path: "data.token"},
		}
		if !slices.Equal(request.Request.Captures, want) {
			t.Errorf("captures = %+v", request.Request.Captures)
		}
		if saved, err := loadRequests(files.Requests); err != nil || !slices.Equal(saved.Folders[0].Requests[0].Request.Captures, want) {
			t.Errorf("saved %+v, %v", saved, err)
		}

		w.request.send.Click()
	})
	waitFor(t, "the response", func() bool { return strings.Contains(w.response.result.Text(), "200 OK") })

	mainthread.Wait(func() {
		// The variables are set in the active environment: one that was
		// there is given the value and enabled, and the others are added.
		got := variables(w)
		if got["token"] != "abc123" || got["last"] != "8" || got["tokenDigits"] != "123" || got["tokenLetters"] != "abc" || len(got) != 5 {
			t.Errorf("variables = %v", got)
		}
		if saved, err := loadEnvs(files.Environments); err != nil || !slices.Equal(saved.Environments[0].Variables, w.environments.store.Environments[0].Variables) {
			t.Errorf("saved %+v, %v", saved, err)
		}

		// Each capture is marked with how it went, and the tab with a
		// cross as one of them failed. Nothing is said of it in the bar.
		tab, rows, tips := captureMarks(w.response)
		if want := map[string]string{"last": "tick", "missing": "cross", "token": "tick", "tokenDigits": "tick", "tokenLetters": "tick", "unused": ""}; tab != "cross" || !maps.Equal(rows, want) {
			t.Errorf("tab %q, rows %v", tab, rows)
		}
		if tips["token"] != "Captured" || tips["missing"] != `Not captured: there is no "nothing" in the response` || tips["unused"] != "" {
			t.Errorf("tips = %v", tips)
		}
		if !strings.Contains(w.response.tabs.TabToolTip(tabCapture), `missing: there is no "nothing"`) {
			t.Errorf("tab tip = %q", w.response.tabs.TabToolTip(tabCapture))
		}
		if strings.Contains(w.response.result.Text()+w.response.details.Text(), "aptured") || isFailure(w.response) {
			t.Errorf("bar = %q, %q", w.response.result.Text(), w.response.details.Text())
		}

		// The marks follow the colours of the settings.
		s := w.config
		s.Colours.Passed, s.Colours.Failed = "#001100", "#110000"
		if err := w.applySettings(s); err != nil {
			t.Error(err)
		}
		if got := iconColour(w.response.tabs.TabIcon(tabCapture)); got != "#110000" {
			t.Errorf("the cross is %s", got)
		}
		if got := iconColour(w.request.captures.table.Item(0, colStatus).Icon()); got != "#001100" {
			t.Errorf("the tick is %s", got)
		}

		// Changing the captures takes the marks away, as they are of the
		// captures as they were.
		w.request.captures.remove(1)
		if tab, rows, _ := captureMarks(w.response); tab != "" || rows["token"] != "" || rows["last"] != "" {
			t.Errorf("after a change: tab %q, rows %v", tab, rows)
		}
		w.request.send.Click()
	})
	waitFor(t, "the marks", func() bool { tab, _, _ := captureMarks(w.response); return tab != "" })

	mainthread.Wait(func() {
		// With every value captured the tab has a tick.
		if tab, rows, _ := captureMarks(w.response); tab != "tick" || rows["token"] != "tick" || rows["last"] != "tick" {
			t.Errorf("tab %q, rows %v", tab, rows)
		}

		// A failed response is not captured from.
		status = http.StatusUnauthorized
		w.environments.store.Environments[0].Set("token", "kept")
		w.request.send.Click()
	})
	waitFor(t, "the failure", func() bool { return strings.Contains(w.response.result.Text(), "401") })

	mainthread.Wait(func() {
		if got := variables(w); got["token"] != "kept" {
			t.Errorf("variables = %v", got)
		}
		if tab, rows, _ := captureMarks(w.response); tab != "" || rows["token"] != "" {
			t.Errorf("after a failure: tab %q, rows %v", tab, rows)
		}

		// With no environment selected there is nowhere to keep a value.
		status = http.StatusOK
		w.request.url.SetText(srv.URL)
		w.environments.confirm = func(string) bool { return true }
		w.environments.remove()
		w.request.send.Click()
	})
	waitFor(t, "the response", func() bool { return strings.Contains(w.response.result.Text(), "200 OK") })

	mainthread.Wait(func() {
		tab, rows, tips := captureMarks(w.response)
		if tab != "cross" || rows["token"] != "cross" || tips["token"] != "Not captured: no environment is selected to keep it in" {
			t.Errorf("tab %q, rows %v, tips %v", tab, rows, tips)
		}
	})
}

func TestCaptureGRPC(t *testing.T) {
	srv := rpctest.NewServer(rpctest.ReflectionAll)
	defer srv.Close()

	files := testFiles(t)
	store := env.Store{Environments: []env.Environment{{Name: "Local"}}}
	if err := store.Save(files.Environments); err != nil {
		t.Fatal(err)
	}

	var w *Window
	var request *collection.Request
	mainthread.Wait(func() {
		w = NewWindow(files.Files)
		w.Show()
		w.requests.addFolder()
		request = addGRPCRequest(w)

		w.grpc.url.SetText(srv.URL)
		w.grpc.targetChanged()
	})
	waitFor(t, "the methods", func() bool { return w.grpc.method.count() == 4 })

	mainthread.Wait(func() {
		w.grpc.method.selectMethod("test.v1.Echo/Repeat")
		w.grpc.message.SetPlainText(`{"text": "hello", "count": 3}`)
		w.grpc.captures.add("said", "text")
		w.grpc.captures.add("index", "index")
		if len(request.GRPC.Captures) != 2 {
			t.Errorf("captures = %+v", request.GRPC.Captures)
		}

		w.grpc.invoke.Click()
	})
	waitFor(t, "the response", func() bool { return strings.Contains(w.grpcResponse.result.Text(), "0 OK") })

	mainthread.Wait(func() {
		// The values are those of the last message.
		if got := variables(w); got["said"] != "hello" || got["index"] != "2" {
			t.Errorf("variables = %v", got)
		}
		if tab, rows, _ := captureMarks(w.grpcResponse); tab != "tick" || rows["said"] != "tick" || rows["index"] != "tick" {
			t.Errorf("tab %q, rows %v", tab, rows)
		}

		// A call that fails is not captured from.
		w.environments.store.Environments[0].Set("said", "kept")
		w.grpc.method.selectMethod("test.v1.Echo/Fail")
		w.grpc.invoke.Click()
	})
	waitFor(t, "the failure", func() bool { return strings.Contains(w.grpcResponse.result.Text(), "NOT_FOUND") })

	mainthread.Wait(func() {
		if got := variables(w); got["said"] != "kept" {
			t.Errorf("variables = %v", got)
		}
		if tab, rows, _ := captureMarks(w.grpcResponse); tab != "" || rows["said"] != "" {
			t.Errorf("after a failure: tab %q, rows %v", tab, rows)
		}
	})
}

// The menu of the body of a response offers to capture the value it is
// opened on, which adds a rule with the path of the value.
func TestClickToCapture(t *testing.T) {
	mainthread.Wait(func() {
		w := NewWindow(testFiles(t).Files)
		w.Show()

		// offers returns what the menu at some text in the body offers to
		// capture, and the action which does it.
		offers := func(p *responsePanel, text string, nth int) (string, *qt.QAction) {
			body := p.body.ToPlainText()
			offset := -1
			for range nth {
				offset += 1 + strings.Index(body[offset+1:], text)
			}
			menu := p.contextMenu(len(utf16.Encode([]rune(body[:offset]))) + 1)
			for _, action := range menu.Actions() {
				if strings.HasPrefix(action.Text(), "Capture") {
					return action.Text(), action
				}
			}
			return "", nil
		}

		// Nothing is offered without a response, or in one that is not
		// JSON.
		p := w.response
		if got, _ := offers(p, "", 1); got != "" {
			t.Errorf("without a response: %q", got)
		}
		p.show(&rest.Response{Status: "200 OK", StatusCode: 200, Headers: http.Header{"Content-Type": {"text/plain"}}, Body: []byte("hello there")})
		if got, _ := offers(p, "there", 1); got != "" {
			t.Errorf("in text: %q", got)
		}

		// A body that is JSON whatever it says it is can be captured
		// from, with the line endings it came with.
		p.show(&rest.Response{Status: "200 OK", StatusCode: 200, Headers: http.Header{"Content-Type": {"text/plain"}}, Body: []byte("{\r\n  \"token\": \"abc\"\r\n}")})
		if got, _ := offers(p, `"abc"`, 1); got != "Capture token" {
			t.Errorf("in JSON sent as text: %q", got)
		}

		p.show(&rest.Response{Status: "200 OK", StatusCode: 200, Headers: http.Header{"Content-Type": {"application/json"}}, Body: []byte(`{"é😀":"x","data":{"items":[{"id":7},{"id":8}],"a&b":1}}`)})
		for text, want := range map[string]string{
			`"data"`: "Capture data",
			`"a&b"`:  "Capture data.a&&b",
			`"x"`:    "Capture é😀",
		} {
			if got, _ := offers(p, text, 1); got != want {
				t.Errorf("at %s: %q, want %q", text, got, want)
			}
		}

		// The rule is added and shown, with its name the current cell.
		got, action := offers(p, `"id"`, 2)
		if got != "Capture data.items[1].id" {
			t.Fatalf("at the second id: %q", got)
		}
		var changes int
		w.request.captures.onChange = func() { changes++ }
		action.Trigger()
		want := []capture.Rule{{Variable: "id", Path: "data.items[1].id", Enabled: true}}
		if rules := captureRules(w.request.captures); !slices.Equal(rules, want) || changes == 0 {
			t.Errorf("rules = %+v after %d changes", rules, changes)
		}
		if table := w.request.captures.table; p.tabs.CurrentIndex() != tabCapture || table.CurrentRow() != 0 || table.CurrentColumn() != colKey {
			t.Errorf("tab %d, cell %d, %d", p.tabs.CurrentIndex(), table.CurrentRow(), table.CurrentColumn())
		}

		// Capturing the same value again adds nothing.
		action.Trigger()
		if rules := captureRules(w.request.captures); len(rules) != 1 {
			t.Errorf("rules = %+v", rules)
		}

		// Of the messages of a gRPC call, values are captured from the
		// last, so they are offered only there.
		p = w.grpcResponse
		p.showRPC(&rpc.Response{Messages: []string{"{\n  \"text\": \"one\"\n}", "{\n  \"text\": \"two\"\n}"}})
		if got, _ := offers(p, `"text"`, 1); got != "" {
			t.Errorf("in the first message: %q", got)
		}
		if got, action := offers(p, `"text"`, 2); got != "Capture text" {
			t.Errorf("in the last message: %q", got)
		} else {
			action.Trigger()
			if rules := captureRules(w.grpc.captures); !slices.Equal(rules, []capture.Rule{{Variable: "text", Path: "text", Enabled: true}}) {
				t.Errorf("rules = %+v", rules)
			}
		}
	})
}

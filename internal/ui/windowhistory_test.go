package ui

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	qt "github.com/mappu/miqt/qt6"
	"github.com/mappu/miqt/qt6/mainthread"

	"github.com/nomad-software/triggerapi/internal/collection"
	"github.com/nomad-software/triggerapi/internal/env"
	"github.com/nomad-software/triggerapi/internal/history"
	"github.com/nomad-software/triggerapi/internal/mcp/mcptest"
	"github.com/nomad-software/triggerapi/internal/rest"
	"github.com/nomad-software/triggerapi/internal/rpc/rpctest"
	"github.com/nomad-software/triggerapi/internal/workspace"
)

// entryCount returns how many requests the history of the window holds.
func entryCount(w *Window) int {
	return mainthread.Wait2(func() int { return len(w.history.store.Entries) })
}

// firstEntryItem returns the item of the newest entry listed in the history.
func firstEntryItem(w *Window) *qt.QTreeWidgetItem {
	return w.history.tree.TopLevelItem(0).Child(0)
}

func TestHistoryRecordsREST(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		io.WriteString(w, `{"id":1}`)
	}))
	defer srv.Close()

	files := testFiles(t)
	envs := &env.Store{Active: 0, Environments: []env.Environment{{Name: "Local", Variables: []env.Variable{
		{Key: "host", Value: strings.TrimPrefix(srv.URL, "http://"), Enabled: true},
		{Key: "token", Value: "secret", Enabled: true},
	}}}}
	if err := envs.Save(files.Environments); err != nil {
		t.Fatal(err)
	}

	var w *Window
	mainthread.Wait(func() {
		w = NewWindow(files.Files)
		w.Show()
		w.requests.addRequest()
		w.request.url.SetText("http://{{host}}/rooms")
		w.request.params.add("page", "2")
		w.request.headers.add("Authorization", "Bearer {{token}}")
		w.request.send.Click()
	})
	waitFor(t, "the entry", func() bool { return entryCount(w) == 1 })

	var e *history.Entry
	mainthread.Wait(func() {
		e = w.history.store.Entries[0]
		// The request is kept with its placeholders, and only where it went
		// with them replaced.
		if e.URL != srv.URL+"/rooms?page=2" || e.Request.URL != "http://{{host}}/rooms" || e.Request.Headers[0].Value != "Bearer {{token}}" {
			t.Errorf("entry sent to %q, request %+v", e.URL, e.Request.Request)
		}
		if e.Request.Name != "New Request" || !reflect.DeepEqual(e.Path, []string{"New Folder", "New Request"}) || e.Environment != "Local" {
			t.Errorf("entry of %q at %q in %q", e.Request.Name, e.Path, e.Environment)
		}
		if e.HTTP == nil || e.HTTP.StatusCode != http.StatusCreated || string(e.HTTP.Body) != `{"id":1}` || e.Duration <= 0 {
			t.Errorf("response %+v after %v", e.HTTP, e.Duration)
		}

		// It is listed under today.
		heading := w.history.tree.TopLevelItem(0)
		item := heading.Child(0)
		if heading.Text(colHistoryCheck) != "Today" || item.Text(colHistoryStatus) != "201" || item.Text(colHistoryTitle) != "New Request" {
			t.Errorf("listed %q: %q %q", heading.Text(colHistoryCheck), item.Text(colHistoryStatus), item.Text(colHistoryTitle))
		}
	})

	mainthread.Wait(func() {
		// Changing the saved request leaves the entry as it was sent.
		w.request.url.SetText("http://changed.test")
		w.request.url.commit()

		// Opening it shows a copy, not saved, with its response.
		w.sidebar.SetCurrentIndex(tabHistory)
		w.history.tree.SetCurrentItem(firstEntryItem(w))
		if w.open != nil || w.stack.CurrentIndex() != pageRequest || w.request.url.Text() != "http://{{host}}/rooms?page=2" {
			t.Errorf("opened %+v, URL %q", w.open, w.request.url.Text())
		}
		if !w.request.saveButton().IsVisibleTo(w.QWidget) || fieldValue(w.response, fieldCode) != "201 Created" || !strings.Contains(w.response.body.ToPlainText(), `"id": 1`) {
			t.Errorf("status %q, body %q", fieldValue(w.response, fieldCode), w.response.body.ToPlainText())
		}

		// The saved request it was sent from can be gone to.
		w.goToEntry(e)
		if w.open == nil || w.open.Name != "New Request" || w.sidebar.CurrentIndex() != tabRequests || w.request.url.Text() != "http://changed.test" {
			t.Errorf("went to %+v, tab %d", w.open, w.sidebar.CurrentIndex())
		}

		// Once it has been moved it cannot.
		r := w.open
		r.Name = "Renamed"
		w.requests.save()
		w.openEntry(e)
		w.goToEntry(e)
		if w.open != nil {
			t.Errorf("went to %+v", w.open)
		}

		// The history is written when the window closes, encrypted like the
		// rest.
		writer.Flush()
		if _, err := os.Stat(files.History); err == nil {
			t.Error("the history was written before the window closed")
		}
		w.Close()
	})
	saved, err := loadHistory(files.History)
	if err != nil || len(saved.Entries) != 1 || saved.Entries[0].URL != srv.URL+"/rooms?page=2" {
		t.Fatalf("saved %+v, %v", saved, err)
	}
}

func TestHistoryFailuresAndSettings(t *testing.T) {
	files := testFiles(t)
	var w *Window
	mainthread.Wait(func() {
		w = NewWindow(files.Files)
		w.Show()
		// A request that is not saved, which cannot be sent.
		w.request.url.SetText("http://127.0.0.1:1/none")
		w.request.send.Click()
	})
	waitFor(t, "the failure", func() bool { return entryCount(w) == 1 })
	mainthread.Wait(func() {
		e := w.history.store.Entries[0]
		if e.Error == "" || e.Path != nil || e.Title() != "GET http://127.0.0.1:1/none" {
			t.Errorf("entry %q, %q, at %q", e.Title(), e.Error, e.Path)
		}
		item := firstEntryItem(w)
		if item.Text(colHistoryStatus) != "Error" || item.Foreground(colHistoryStatus).Color().Name() != themeColour(colourError) {
			t.Errorf("listed as %q", item.Text(colHistoryStatus))
		}

		// It opens with the error it had.
		w.request.url.SetText("http://other.test")
		w.history.tree.SetCurrentItem(item)
		if w.request.url.Text() != "http://127.0.0.1:1/none" || fieldValue(w.response, fieldError) != e.Error || !isFailure(w.response) {
			t.Errorf("URL %q, error %q", w.request.url.Text(), fieldValue(w.response, fieldError))
		}
		w.request.send.Click()
	})
	waitFor(t, "the second failure", func() bool { return entryCount(w) == 2 })

	mainthread.Wait(func() {
		// Keeping fewer drops the oldest at once.
		s := w.config
		s.HistoryLimit = 1
		if err := w.applySettings(s); err != nil {
			t.Fatal(err)
		}
		if len(w.history.store.Entries) != 1 || w.history.tree.TopLevelItem(0).ChildCount() != 1 {
			t.Errorf("%d entries kept", len(w.history.store.Entries))
		}

		// Keeping none sends without adding to it.
		s.NoHistory = true
		if err := w.applySettings(s); err != nil {
			t.Fatal(err)
		}
		w.request.send.Click()
	})
	waitFor(t, "the response", func() bool {
		return mainthread.Wait2(func() bool { return w.cancel == nil && fieldValue(w.response, fieldError) != "" })
	})
	if n := entryCount(w); n != 1 {
		t.Errorf("%d entries with no history kept", n)
	}
	mainthread.Wait(func() { w.Close() })
}

func TestHistoryRecordsGRPC(t *testing.T) {
	srv := rpctest.NewServer(rpctest.ReflectionAll)
	defer srv.Close()

	var w *Window
	mainthread.Wait(func() {
		w = NewWindow(testFiles(t).Files)
		w.Show()
		addGRPCRequest(w)
		w.grpc.url.SetText(srv.URL)
		w.grpc.targetChanged()
	})
	waitFor(t, "the methods", func() bool { return w.grpc.method.count() == 4 })
	mainthread.Wait(func() {
		w.grpc.method.selectMethod("test.v1.Echo/Say")
		w.grpc.message.SetPlainText(`{"text": "hello"}`)
		w.grpc.invoke.Click()
	})
	waitFor(t, "the entry", func() bool { return entryCount(w) == 1 })
	mainthread.Wait(func() {
		e := w.history.store.Entries[0]
		if e.GRPC == nil || !e.GRPC.OK() || len(e.GRPC.Messages) != 1 || e.Request.GRPC.Method != "test.v1.Echo/Say" || e.URL != srv.URL {
			t.Errorf("entry %+v of %+v", e.GRPC, e.Request.GRPC)
		}
		if got := firstEntryItem(w).Text(colHistoryStatus); got != "OK" {
			t.Errorf("listed as %q", got)
		}

		// It opens on the gRPC page, with its messages.
		w.grpcResponse.clear()
		w.history.tree.SetCurrentItem(firstEntryItem(w))
		if w.stack.CurrentIndex() != pageGRPC || fieldValue(w.grpcResponse, fieldCode) != "0 OK" || !strings.Contains(w.grpcResponse.body.ToPlainText(), "hello") {
			t.Errorf("page %d, status %q", w.stack.CurrentIndex(), fieldValue(w.grpcResponse, fieldCode))
		}
		w.Close()
	})
}

func TestHistoryRecordsGraphQLAndMCP(t *testing.T) {
	graph := graphqlServer(t)
	tools := mcptest.NewServer(t)

	var w *Window
	mainthread.Wait(func() {
		w = NewWindow(testFiles(t).Files)
		w.Show()
		defer func(prev collection.Type) { testRequestType = prev }(testRequestType)
		testRequestType = collection.TypeGraphQL
		w.newRequest()
		w.graphql.url.SetText(graph.URL + "/graphql")
		w.graphql.query.SetPlainText("{ fail }")
		w.graphql.send.Click()
	})
	waitFor(t, "the GraphQL entry", func() bool { return entryCount(w) == 1 })
	mainthread.Wait(func() {
		e := w.history.store.Entries[0]
		if e.HTTP == nil || e.Request.Type != collection.TypeGraphQL || e.URL != graph.URL+"/graphql" {
			t.Errorf("entry %+v", e)
		}
		// The errors it reported make it a failure, whatever its status.
		if status, failed := e.Status(); status != "200" || !failed {
			t.Errorf("status %q, failed %v", status, failed)
		}
		w.history.tree.SetCurrentItem(firstEntryItem(w))
		if w.stack.CurrentIndex() != pageGraphQL || w.graphql.query.ToPlainText() != "{ fail }" {
			t.Errorf("page %d, query %q", w.stack.CurrentIndex(), w.graphql.query.ToPlainText())
		}

		defer func(prev collection.Type) { testRequestType = prev }(testRequestType)
		testRequestType = collection.TypeMCP
		w.newRequest()
		w.mcp.url.SetText(tools.URL + "/mcp")
		w.mcp.reload.Click()
	})
	waitFor(t, "the capabilities", func() bool { return mainthread.Wait2(w.mcp.endpoint.count) == 7 })
	mainthread.Wait(func() {
		w.mcp.arguments.SetPlainText(`{"text": "hi", "times": 2}`)
		w.mcp.call.Click()
	})
	waitFor(t, "the MCP entry", func() bool { return entryCount(w) == 2 })
	mainthread.Wait(func() {
		e := w.history.store.Entries[0]
		if e.MCP == nil || !e.MCP.OK() || !strings.Contains(string(e.MCP.Result), "hihi") || e.Title() != tools.URL+"/mcp echo" {
			t.Errorf("entry %q: %+v", e.Title(), e.MCP)
		}
		w.mcpResponse.clear()
		w.history.tree.SetCurrentItem(firstEntryItem(w))
		if w.stack.CurrentIndex() != pageMCP || fieldValue(w.mcpResponse, fieldCode) != "OK" || !strings.Contains(w.mcpResponse.body.ToPlainText(), "hihi") {
			t.Errorf("page %d, status %q", w.stack.CurrentIndex(), fieldValue(w.mcpResponse, fieldCode))
		}
		w.Close()
	})
}

func TestHistoryRecordsWebSocket(t *testing.T) {
	srv := wsEchoServer(t)
	var w *Window
	log := func() string { return mainthread.Wait2(func() string { return w.wsResponse.body.ToPlainText() }) }

	mainthread.Wait(func() {
		w = NewWindow(testFiles(t).Files)
		w.Show()
		defer func(prev collection.Type) { testRequestType = prev }(testRequestType)
		testRequestType = collection.TypeWebSocket
		w.requests.addRequest()
		w.ws.url.SetText(srv.URL)
		w.ws.message.SetPlainText("ping")
		w.ws.connect.Click()
	})
	waitFor(t, "the connection", func() bool { return strings.Contains(log(), "hello") })
	mainthread.Wait(w.ws.send.Click)
	waitFor(t, "the echo", func() bool { return strings.Count(log(), "ping") == 2 })
	// Nothing is kept until the connection is closed.
	if n := entryCount(w); n != 0 {
		t.Errorf("%d entries while connected", n)
	}

	mainthread.Wait(func() {
		w.ws.connect.Click()
		if len(w.history.store.Entries) != 1 {
			t.Fatalf("%d entries", len(w.history.store.Entries))
		}
		e := w.history.store.Entries[0]
		if e.WebSocket == nil || e.WebSocket.Status != "Disconnected" || e.WebSocket.Failed || len(e.WebSocket.Messages) != 3 || e.WebSocket.Handshake <= 0 {
			t.Errorf("entry %+v", e.WebSocket)
		}
		if e.URL != srv.URL || e.Request.WebSocket.Message != "ping" {
			t.Errorf("entry of %q, %+v", e.URL, e.Request.WebSocket)
		}

		// It opens with the messages that went over it.
		w.wsResponse.clear()
		w.history.tree.SetCurrentItem(firstEntryItem(w))
		if !strings.Contains(w.wsResponse.body.ToPlainText(), "→ Sent") || fieldValue(w.wsResponse, "Messages") != "1 sent, 2 received" || fieldValue(w.wsResponse, fieldCode) != "Disconnected" {
			t.Errorf("messages %q, status %q", fieldValue(w.wsResponse, "Messages"), fieldValue(w.wsResponse, fieldCode))
		}

		// A connection that could not be made is kept with its error.
		w.ws.url.SetText("ws://127.0.0.1:1/none")
		w.ws.connect.Click()
	})
	waitFor(t, "the failed connection", func() bool { return entryCount(w) == 2 })
	mainthread.Wait(func() {
		if e := w.history.store.Entries[0]; e.Error == "" || e.WebSocket != nil {
			t.Errorf("entry %+v", e)
		}
		w.Close()
	})
}

func TestHistoryOfEachWorkspace(t *testing.T) {
	files := testFiles(t)
	if err := workspace.Create(files.Workspaces, "Other"); err != nil {
		t.Fatal(err)
	}
	store := &history.Store{}
	store.Add(&history.Entry{
		Time:    time.Now(),
		Request: collection.NewREST("Elsewhere", rest.Request{Method: "GET", URL: "other.test"}),
		HTTP:    &rest.Response{Status: "200 OK", StatusCode: 200, Body: []byte(strings.Repeat("x", 10))},
		Omitted: 5 << 20,
	}, 10)
	if err := store.SaveWith(workspace.History(files.Workspaces, "Other"), testKeys); err != nil {
		t.Fatal(err)
	}

	mainthread.Wait(func() {
		w := NewWindow(files.Files)
		w.Show()
		if len(w.history.store.Entries) != 0 {
			t.Errorf("%d entries in the default workspace", len(w.history.store.Entries))
		}

		w.workspaces.open("Other")
		if len(w.history.store.Entries) != 1 || w.history.tree.TopLevelItemCount() != 1 {
			t.Fatalf("%d entries in the other workspace", len(w.history.store.Entries))
		}
		// A body too large to be kept is said not to be, with its size.
		w.history.tree.SetCurrentItem(firstEntryItem(w))
		if fieldValue(w.response, "Body") != "not kept in the history" || fieldValue(w.response, "Size") != "5.0 MB" {
			t.Errorf("body %q, size %q", fieldValue(w.response, "Body"), fieldValue(w.response, "Size"))
		}

		// Renaming the workspace keeps saving the history in its folder.
		w.renamedWorkspace("Renamed")
		if w.history.path != workspace.History(files.Workspaces, "Renamed") {
			t.Errorf("history saved to %q", w.history.path)
		}

		w.workspaces.open(workspace.Default)
		if len(w.history.store.Entries) != 0 {
			t.Errorf("%d entries back in the default workspace", len(w.history.store.Entries))
		}

		// The history of a workspace is written when it is left.
		w.history.add(&history.Entry{Time: time.Now(), Request: collection.New(collection.TypeREST, "Here"), Error: "refused"})
		w.workspaces.open("Other")
		w.Close()
	})
	saved, err := loadHistory(files.History)
	if err != nil || len(saved.Entries) != 1 || saved.Entries[0].Title() != "Here" {
		t.Errorf("saved %+v, %v", saved, err)
	}
}

// A response opened from the history is saved under the name of its request
// and the time it was received, and one received now under the name of the
// open request, which has none here as it is not saved, and the time it is
// saved.
func TestSaveEntryResponse(t *testing.T) {
	received := time.Date(2026, 3, 4, 5, 6, 7, 0, time.Local)
	e := &history.Entry{
		Time:    received,
		Request: collection.New(collection.TypeREST, "Book room"),
		HTTP:    &rest.Response{Status: "200 OK", StatusCode: 200, Headers: http.Header{"Content-Type": {"application/json"}}, Body: []byte(`{"id": 1}`)},
	}
	mainthread.Wait(func() {
		w := NewWindow(testFiles(t).Files)
		var offered string
		w.response.chooseSave = func(name string) (string, bool) { offered = name; return "", false }
		w.response.now = func() time.Time { return time.Date(2026, 10, 18, 12, 0, 0, 0, time.Local) }

		w.openEntry(e)
		w.response.saveBody()
		if offered != "book_room_20260304_050607_resp.json" {
			t.Errorf("offered %q for the response of the history", offered)
		}

		// Cleared for another response, the time is that of saving.
		w.response.clear()
		w.response.show(e.HTTP)
		w.response.saveBody()
		if offered != "untitled_20261018_120000_resp.json" {
			t.Errorf("offered %q for a response now", offered)
		}
	})
}

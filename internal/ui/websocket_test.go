package ui

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/coder/websocket"
	"github.com/mappu/miqt/qt6/mainthread"

	"github.com/nomad-software/triggerapi/internal/collection"
	"github.com/nomad-software/triggerapi/internal/env"
	"github.com/nomad-software/triggerapi/internal/rest"
	"github.com/nomad-software/triggerapi/internal/rpc"
	"github.com/nomad-software/triggerapi/internal/ws"
)

// wsEchoServer accepts WebSocket connections which send back every message,
// after a first one that says what the X-Test header of the handshake was.
// "bye" has the server close the connection.
func wsEchoServer(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Server", "echo")
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		ctx := r.Context()
		conn.Write(ctx, websocket.MessageText, []byte("hello "+r.Header.Get("X-Test")))
		for {
			typ, data, err := conn.Read(ctx)
			if err != nil {
				return
			}
			if string(data) == "bye" {
				conn.Close(websocket.StatusPolicyViolation, "told to go")
				return
			}
			conn.Write(ctx, typ, data)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestWebSocketRequest(t *testing.T) {
	srv := wsEchoServer(t)
	files := testFiles(t)
	envs := &env.Store{Active: 0, Environments: []env.Environment{{Name: "Local", Variables: []env.Variable{
		{Key: "host", Value: strings.TrimPrefix(srv.URL, "http://"), Enabled: true},
		{Key: "who", Value: "world", Enabled: true},
	}}}}
	if err := envs.Save(files.Environments); err != nil {
		t.Fatal(err)
	}

	var w *Window
	field := func(label string) string { return fieldValue(w.wsResponse, label) }
	log := func() string { return mainthread.Wait2(func() string { return w.wsResponse.body.ToPlainText() }) }

	mainthread.Wait(func() {
		w = NewWindow(files.Files)
		w.Show()

		// A WebSocket request is one of the types to add.
		defer func(prev collection.Type) { testRequestType = prev }(testRequestType)
		testRequestType = collection.TypeWebSocket
		w.requests.addRequest()
		if w.open == nil || w.open.Type != collection.TypeWebSocket || w.stack.CurrentIndex() != pageWS || w.ws.send.IsEnabled() {
			t.Fatalf("open %+v on page %d", w.open, w.stack.CurrentIndex())
		}

		w.ws.url.SetText("ws://{{host}}/socket")
		w.ws.headers.add("X-Test", "{{who}}")
		w.ws.message.SetPlainText(`{"to": "{{who}}"}`)
		w.ws.connect.Click()
	})

	// The connection opens with the environment applied to the handshake.
	waitFor(t, "the connection", func() bool {
		return mainthread.Wait2(func() bool { return w.ws.send.IsEnabled() && strings.Contains(w.wsResponse.body.ToPlainText(), "hello world") })
	})
	mainthread.Wait(func() {
		if w.ws.send.StyleSheet() != primaryStyle() {
			t.Error("Send is not in the colour of sending while connected")
		}
		if w.ws.connect.Text() != "Disconnect" || field(fieldCode) != "Connected" || !strings.Contains(log(), "← Received") {
			t.Errorf("connected: button %q, status %q, log %q", w.ws.connect.Text(), field(fieldCode), log())
		}
		if w.wsResponse.headers.RowCount() == 0 {
			t.Error("the headers of the handshake are not shown")
		}
		w.ws.send.Click()
	})

	// What is sent goes in the log, and so does the echo.
	waitFor(t, "the echo", func() bool { return strings.Count(log(), `{"to": "world"}`) == 2 })
	mainthread.Wait(func() {
		if !strings.Contains(log(), "→ Sent") || field("Messages") != "1 sent, 2 received" {
			t.Errorf("messages %q, log %q", field("Messages"), log())
		}

		// The server closing the connection is shown as a failure.
		w.ws.message.SetPlainText("bye")
		w.ws.send.Click()
	})
	waitFor(t, "the close", func() bool { return mainthread.Wait2(func() bool { return w.socket == nil }) })
	mainthread.Wait(func() {
		if field(fieldCode) != "1008 POLICY_VIOLATION" || field(fieldError) != "told to go" || w.ws.connect.Text() != "Connect" || w.ws.send.IsEnabled() {
			t.Errorf("closed: status %q, error %q", field(fieldCode), field(fieldError))
		}

		// Connecting again, then disconnecting, says so.
		w.ws.connect.Click()
	})
	waitFor(t, "the second connection", func() bool { return mainthread.Wait2(func() bool { return w.ws.send.IsEnabled() }) })
	mainthread.Wait(func() {
		w.ws.connect.Click()
		if w.socket != nil || field(fieldCode) != "Disconnected" || w.ws.send.IsEnabled() || w.ws.send.StyleSheet() != "" {
			t.Errorf("after disconnecting: status %q", field(fieldCode))
		}

		// A connection that cannot be made is reported.
		w.ws.url.SetText("ws://127.0.0.1:1/none")
		w.ws.connect.Click()
	})
	waitFor(t, "the failure", func() bool { return mainthread.Wait2(func() bool { return w.socket == nil }) })
	mainthread.Wait(func() {
		if field(fieldCode) != "Not connected" || field(fieldError) == "" {
			t.Errorf("failure: status %q, error %q", field(fieldCode), field(fieldError))
		}
		w.Close()
	})

	// It is saved with everything but the connection.
	saved, err := loadRequests(files.Requests)
	if err != nil {
		t.Fatal(err)
	}
	r := saved.Folders[0].Requests[0]
	if r.Type != collection.TypeWebSocket || r.WebSocket.URL != "ws://127.0.0.1:1/none" || r.WebSocket.Message != "bye" || len(r.WebSocket.Headers) != 1 {
		t.Errorf("saved %+v %+v", r, r.WebSocket)
	}
}

func TestWebSocketGroupAndDraft(t *testing.T) {
	files := testFiles(t)
	store := &collection.Store{}
	store.AddFolder("Calls").Add(collection.NewGRPC("Ping", rpc.Request{}))
	store.AddFolder("Pages").Add(collection.NewREST("Get", rest.Request{Method: "GET"}))
	sockets := store.AddFolder("Sockets")
	sockets.Add(collection.NewWebSocket("Chat", ws.Request{URL: "ws://chat.test"}))
	mixed := store.AddFolder("Both")
	mixed.Add(collection.NewWebSocket("Feed", ws.Request{}))
	mixed.Add(collection.NewREST("Poll", rest.Request{Method: "GET"}))
	if err := store.Save(files.Requests); err != nil {
		t.Fatal(err)
	}

	mainthread.Wait(func() {
		w := NewWindow(files.Files)
		w.Show()
		tree := w.requests.tree

		// WebSocket requests have a heading of their own, above Mixed.
		var headings []string
		for i := range tree.TopLevelItemCount() {
			headings = append(headings, tree.TopLevelItem(i).Text(colName))
		}
		if want := []string{"gRPC", "REST", "WebSocket", "Mixed"}; !reflect.DeepEqual(headings, want) {
			t.Errorf("headings %q, want %q", headings, want)
		}
		chat := treeItem(tree, "Sockets", "Chat")
		if chat == nil || chat.Icon(colName).CacheKey() != requestTypeIcon(collection.TypeWebSocket).CacheKey() {
			t.Error("the request has not the icon of its type")
		}

		// Opening one shows it in its own UI.
		tree.SetCurrentItem(chat)
		if w.stack.CurrentIndex() != pageWS || w.ws.url.Text() != "ws://chat.test" || !w.ws.save.IsHidden() {
			t.Errorf("page %d, url %q", w.stack.CurrentIndex(), w.ws.url.Text())
		}

		// One made from the New tab is not saved until Save is pressed.
		defer func(prev collection.Type) { testRequestType = prev }(testRequestType)
		testRequestType = collection.TypeWebSocket
		w.newRequest()
		if w.open != nil || w.stack.CurrentIndex() != pageWS || w.ws.save.IsHidden() {
			t.Fatalf("draft: open %+v, page %d", w.open, w.stack.CurrentIndex())
		}
		w.ws.url.SetText("ws://new.test")
		w.ws.save.Click()
		if w.open == nil || w.open.Type != collection.TypeWebSocket || w.open.WebSocket.URL != "ws://new.test" {
			t.Errorf("saved draft %+v", w.open)
		}
	})
}

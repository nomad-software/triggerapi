package ui

import (
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	qt "github.com/mappu/miqt/qt6"
	"github.com/mappu/miqt/qt6/mainthread"

	"github.com/nomad-software/triggerapi/internal/collection"
	"github.com/nomad-software/triggerapi/internal/rest"
	"github.com/nomad-software/triggerapi/internal/rpc"
	"github.com/nomad-software/triggerapi/internal/rpc/rpctest"
)

func TestGRPCRequest(t *testing.T) {
	srv := rpctest.NewServer(rpctest.ReflectionAll)
	defer srv.Close()

	files := testFiles(t)
	var w *Window
	var request *collection.Request

	mainthread.Wait(func() {
		w = NewWindow(files.Files)
		w.Show()

		request = addGRPCRequest(w)
		if request == nil || request.Type != collection.TypeGRPC || request.Request != nil {
			t.Fatalf("request = %+v", request)
		}
		if w.stack.CurrentIndex() != pageGRPC || tabTitle(w) != "New Request" {
			t.Errorf("page = %d, title %q", w.stack.CurrentIndex(), tabTitle(w))
		}
		if got := itemType(topFolder(w.requests.tree, 0).Child(0)); got != collection.TypeGRPC {
			t.Errorf("type shown as %q", got)
		}

		// An example needs the service definition.
		w.grpc.example.Click()
		if w.grpc.status.IsHidden() || w.grpc.message.ToPlainText() != "" {
			t.Errorf("example without a definition: status %q", w.grpc.status.Text())
		}

		// The server address is a variable of the environment.
		w.environments.add()
		w.editor.variables.add("server", srv.URL)
		w.editor.close.Click()
		if w.stack.CurrentIndex() != pageGRPC {
			t.Error("closing the editor did not return to the gRPC request")
		}

		w.grpc.url.SetText("{{server}}")
		w.grpc.targetChanged()
	})

	waitFor(t, "the methods", func() bool { return w.grpc.method.count() == 4 })

	mainthread.Wait(func() {
		// A request with no method takes the first one.
		if request.GRPC.Method != "test.v1.Echo/Fail" || request.GRPC.URL != "{{server}}" {
			t.Errorf("request = %+v", request.GRPC)
		}
		if e := w.grpc.endpoints; e.count() != 4 || !slices.Contains(e.names, "test.v1.Echo/Say") {
			t.Errorf("endpoints = %q", e.names)
		}
		// Loading the definition is only reported if it fails.
		if !w.grpc.status.IsHidden() || w.grpc.status.Text() != "" {
			t.Errorf("status = %q", w.grpc.status.Text())
		}

		w.grpc.method.selectMethod("test.v1.Echo/Say")
		w.grpc.example.Click()
		if got := w.grpc.message.ToPlainText(); !strings.Contains(got, `"text": ""`) || !strings.Contains(got, `"flag": false`) {
			t.Errorf("example = %q", got)
		}

		w.grpc.message.SetPlainText(`{"text": "hello"}`)
		w.grpc.metadata.add("x-trace", "abc")
		w.grpc.auth.typ.SetCurrentIndex(2) // Bearer Token
		w.grpc.auth.token.SetText("secret")
		w.grpc.invoke.Click()
	})

	waitFor(t, "the response", func() bool {
		return fieldValue(w.grpcResponse, "Status") == "0 OK"
	})
	if got := mainthread.Wait2(func() string { return fieldValue(w.grpcResponse, "Protocol") }); got != "gRPC (proto)" {
		t.Errorf("protocol = %q", got)
	}

	mainthread.Wait(func() {
		want := "{\n  \"text\": \"hello\",\n  \"authorization\": \"Bearer secret\",\n  \"trace\": \"abc\"\n}"
		if got := w.grpcResponse.body.ToPlainText(); got != want {
			t.Errorf("body = %q, want %q", got, want)
		}
		if w.grpc.invoke.Text() != "Invoke" {
			t.Errorf("invoke button = %q", w.grpc.invoke.Text())
		}

		// The server's header and trailer are both listed.
		var names []string
		for row := range w.grpcResponse.headers.RowCount() {
			names = append(names, w.grpcResponse.headers.Item(row, 0).Text())
		}
		if !slices.Contains(names, "X-Server") || !slices.Contains(names, "X-Done (trailer)") {
			t.Errorf("headers = %v", names)
		}

		// A status other than OK is a response, not an error.
		w.grpc.method.selectMethod("test.v1.Echo/Fail")
		w.grpc.invoke.Click()
	})

	waitFor(t, "the failure", func() bool { return fieldValue(w.grpcResponse, "Status") == "5 NOT_FOUND" })

	mainthread.Wait(func() {
		// The failure is in the bar beneath the response, not in it.
		if !isFailure(w.grpcResponse) || w.grpcResponse.body.ToPlainText() != "" || fieldValue(w.grpcResponse, "Error") != "no such thing" || fieldValue(w.grpcResponse, "Time") == "" {
			t.Errorf("response = %q, bar = %+v", w.grpcResponse.body.ToPlainText(), w.grpcResponse.fields)
		}
		if got := w.StatusBar().CurrentMessage(); got != "" {
			t.Errorf("status bar = %q", got)
		}
	})

	mainthread.Wait(func() {
		// A message that is not valid is reported as an error.
		w.grpc.method.selectMethod("test.v1.Echo/Say")
		w.grpc.message.SetPlainText("{oops")
		w.grpc.invoke.Click()
	})

	waitFor(t, "the error", func() bool { return strings.Contains(fieldValue(w.grpcResponse, "Error"), "invalid message") })

	mainthread.Wait(func() {
		if got := w.grpcResponse.body.ToPlainText(); got != "" || !isFailure(w.grpcResponse) {
			t.Errorf("response = %q", got)
		}
		w.grpc.message.SetPlainText(`{"text": "saved"}`)
		w.grpc.tls.SetChecked(true)
		w.grpc.tls.SetChecked(false)
	})

	want := rpc.Request{
		URL:      "{{server}}",
		Method:   "test.v1.Echo/Say",
		Message:  `{"text": "saved"}`,
		Metadata: []rest.KeyValue{{Key: "x-trace", Value: "abc", Enabled: true}},
		Auth:     rest.Auth{Type: rest.AuthBearer, Token: "secret"},
	}

	saved, err := loadRequests(files.Requests)
	if err != nil {
		t.Fatal(err)
	}
	if got := saved.Folders[0].Requests[0]; got.Type != collection.TypeGRPC || got.Request != nil || !reflect.DeepEqual(*got.GRPC, want) {
		t.Errorf("saved %+v %+v, want %+v", got, got.GRPC, want)
	}

	// A new window restores the request, and loads the methods when it is
	// opened without changing the one that was saved.
	mainthread.Wait(func() {
		w = NewWindow(files.Files)
		w.Show()

		tree := w.requests.tree
		tree.SetCurrentItem(topFolder(tree, 0).Child(0))
		if w.stack.CurrentIndex() != pageGRPC || w.grpc.method.current() != "test.v1.Echo/Say" {
			t.Errorf("page = %d, method = %q", w.stack.CurrentIndex(), w.grpc.method.current())
		}
	})

	waitFor(t, "the methods", func() bool { return w.grpc.method.count() == 4 })

	mainthread.Wait(func() {
		if got := w.grpc.request(); !reflect.DeepEqual(got, want) {
			t.Errorf("loaded %+v, want %+v", got, want)
		}
	})
}

func TestGRPCAndRESTRequests(t *testing.T) {
	mainthread.Wait(func() {
		w := NewWindow(testFiles(t).Files)
		w.Show()
		tree := w.requests.tree

		w.request.url.SetText("example.com")
		w.requests.addRequest()
		restRequest := w.open

		grpcRequest := addGRPCRequest(w)
		w.grpc.url.SetText("localhost:1")

		if restRequest.Type != collection.TypeREST || restRequest.URL != "example.com" || grpcRequest.GRPC.URL != "localhost:1" {
			t.Errorf("requests = %+v, %+v", restRequest.Request, grpcRequest.GRPC)
		}
		if got := itemType(topFolder(tree, 0).Child(0)); got != collection.TypeREST {
			t.Errorf("type shown as %q", got)
		}
		if tree.ColumnCount() != 1 || itemType(topFolder(tree, 0)) != "" {
			t.Errorf("columns = %d, folder shown as %q", tree.ColumnCount(), itemType(topFolder(tree, 0)))
		}

		// Each type of request opens in its own UI, and leaves the other
		// request alone.
		tree.SetCurrentItem(topFolder(tree, 0).Child(0))
		if w.stack.CurrentIndex() != pageRequest || w.open != restRequest || w.request.url.Text() != "example.com" {
			t.Errorf("REST request not shown: page %d", w.stack.CurrentIndex())
		}
		w.request.url.SetText("changed.test")

		tree.SetCurrentItem(topFolder(tree, 0).Child(1))
		if w.stack.CurrentIndex() != pageGRPC || w.open != grpcRequest || w.grpc.url.Text() != "localhost:1" {
			t.Errorf("gRPC request not shown: page %d", w.stack.CurrentIndex())
		}
		if restRequest.URL != "changed.test" || grpcRequest.GRPC.URL != "localhost:1" || grpcRequest.Request != nil || restRequest.GRPC != nil {
			t.Errorf("requests = %+v, %+v", restRequest.Request, grpcRequest.GRPC)
		}

		// Renaming a gRPC request updates its title.
		topFolder(tree, 0).Child(1).SetText(colName, "Say")
		if grpcRequest.Name != "Say" || tabTitle(w) != "Say" {
			t.Errorf("name = %q, title = %q", grpcRequest.Name, tabTitle(w))
		}

		// Removing the open gRPC request returns to the REST one beside it.
		w.requests.confirm = func(string) bool { return true }
		w.requests.remove()
		if w.stack.CurrentIndex() != pageRequest || w.open != restRequest {
			t.Errorf("after remove: page %d, open %+v", w.stack.CurrentIndex(), w.open)
		}

		// Cancelling the type dialog adds nothing.
		testRequestType = ""
		w.requests.addRequest()
		testRequestType = collection.TypeREST
		if n := len(w.requests.store.Folders[0].Requests); n != 1 {
			t.Errorf("requests = %d, want 1", n)
		}
	})
}

func TestGRPCDefinitionError(t *testing.T) {
	srv := rpctest.NewServer(rpctest.ReflectionNone)
	defer srv.Close()

	var w *Window
	mainthread.Wait(func() {
		w = NewWindow(testFiles(t).Files)
		w.Show()

		addGRPCRequest(w)
		w.grpc.url.SetText(srv.URL)
		w.grpc.reload.Click()
	})

	waitFor(t, "the error", func() bool { return strings.Contains(w.grpc.status.Text(), "does not support reflection") })

	mainthread.Wait(func() {
		if w.grpc.method.count() != 0 {
			t.Errorf("methods = %d", w.grpc.method.count())
		}
	})
}

func TestGRPCCancel(t *testing.T) {
	srv := rpctest.NewServer(rpctest.ReflectionAll)
	defer srv.Close()

	var w *Window
	mainthread.Wait(func() {
		w = NewWindow(testFiles(t).Files)
		w.Show()

		addGRPCRequest(w)
		w.grpc.url.SetText(srv.URL)
		w.grpc.reload.Click()
	})

	waitFor(t, "the methods", func() bool { return w.grpc.method.count() == 4 })

	mainthread.Wait(func() {
		w.grpc.method.selectMethod("test.v1.Echo/Forever")
		w.grpc.message.SetPlainText(`{"text": "one"}`)
		w.grpc.invoke.Click()
		if w.grpc.invoke.Text() != "Cancel" {
			t.Errorf("invoke button = %q", w.grpc.invoke.Text())
		}
	})

	// Give the server time to send its one message before cancelling.
	time.Sleep(300 * time.Millisecond)
	mainthread.Wait(func() { w.grpc.invoke.Click() })

	// The message received before the cancel is still shown.
	waitFor(t, "the cancel", func() bool { return fieldValue(w.grpcResponse, "Status") == "1 CANCELED" })

	mainthread.Wait(func() {
		// The response holds the message alone.
		got := w.grpcResponse.body.ToPlainText()
		if !strings.HasPrefix(got, "{\n  \"text\": \"one\"") || strings.Contains(got, "CANCELED") {
			t.Errorf("body = %q", got)
		}
	})
}

func TestMethodCombo(t *testing.T) {
	mainthread.Wait(func() {
		c := newMethodCombo()
		changes := 0
		c.OnCurrentIndexChanged(func(int) { changes++ })

		// Before the methods are known only the saved one is listed.
		c.setMethods(nil, "a.Users/Get")
		if c.current() != "a.Users/Get" || c.count() != 1 {
			t.Errorf("current = %q, count = %d", c.current(), c.count())
		}

		// Methods are listed by their short names under their services.
		c.setMethods([]string{"a.Orders/List", "a.Users/Get", "a.Users/List"}, "a.Users/Get")
		var rows []string
		for row := range c.Count() {
			rows = append(rows, c.ItemText(row))
		}
		want := []string{"a.Orders", "    List", "a.Users", "    Get", "    List"}
		if !reflect.DeepEqual(rows, want) {
			t.Errorf("rows = %q, want %q", rows, want)
		}
		if c.current() != "a.Users/Get" || c.count() != 3 {
			t.Errorf("current = %q, count = %d", c.current(), c.count())
		}

		// Two methods with the same short name are told apart.
		c.selectMethod("a.Orders/List")
		if c.current() != "a.Orders/List" {
			t.Errorf("current = %q", c.current())
		}
		c.selectMethod("a.Users/List")
		if c.current() != "a.Users/List" {
			t.Errorf("current = %q", c.current())
		}

		// A heading cannot be chosen.
		c.selectMethod("")
		c.selectMethod("a.Users")
		if c.current() != "a.Users/List" {
			t.Errorf("current = %q", c.current())
		}

		// With nothing to keep, the first method is selected, not its heading.
		c.setMethods([]string{"a.Orders/List"}, "")
		if c.current() != "a.Orders/List" {
			t.Errorf("current = %q", c.current())
		}

		// A selected method the server does not have stays, on its own.
		c.setMethods([]string{"a.Orders/List"}, "a.Gone/Old")
		if c.current() != "a.Gone/Old" || c.count() != 2 || c.ItemText(0) != "a.Gone/Old" {
			t.Errorf("current = %q, count = %d", c.current(), c.count())
		}

		c.setMethods(nil, "")
		if c.current() != "" || c.count() != 0 {
			t.Errorf("current = %q, count = %d", c.current(), c.count())
		}
		if changes == 0 {
			t.Error("changes were not reported")
		}
	})
}

func TestGRPCRequestBarOrder(t *testing.T) {
	var w *Window
	mainthread.Wait(func() {
		w = NewWindow(testFiles(t).Files)
		w.Show()
		addGRPCRequest(w)
	})

	// The positions are only known once the page has been laid out.
	waitFor(t, "the layout", func() bool { return w.grpc.url.Width() > 0 && w.grpc.invoke.X() > 0 })

	mainthread.Wait(func() {
		g := w.grpc
		row := []*qt.QWidget{g.url.QWidget, g.method.QWidget, g.reload.QWidget, g.tls.QWidget, g.invoke.QWidget}

		// The method is given three times the width of the address.
		if ratio := float64(g.method.Width()) / float64(g.url.Width()); ratio < 2.8 || ratio > 3.2 {
			t.Errorf("address is %d wide and the method %d", g.url.Width(), g.method.Width())
		}

		// Everything is on one line, in order from the left.
		for i := 1; i < len(row); i++ {
			if row[i].X() <= row[i-1].X() {
				t.Errorf("widget %d is not to the right of widget %d", i, i-1)
			}
			if overlap := row[i].Geometry().Intersects(row[0].Geometry()); overlap || row[i].Geometry().Bottom() < row[0].Y() || row[i].Y() > row[0].Geometry().Bottom() {
				t.Errorf("widget %d is not on the line of the address", i)
			}
		}
	})
}

// TestGRPCClientStream checks that a method which takes a stream of messages
// is said to, given an example of two, and sent each of those given.
func TestGRPCClientStream(t *testing.T) {
	srv := rpctest.NewStreamingServer(rpctest.ReflectionAll)
	defer srv.Close()

	var w *Window
	body := func() string { return w.grpcResponse.body.ToPlainText() }
	mainthread.Wait(func() {
		w = NewWindow(testFiles(t).Files)
		w.Show()
		addGRPCRequest(w)
		if w.grpc.streaming.IsVisibleTo(w.grpc.root) {
			t.Error("a stream is said to be taken before the definition is loaded")
		}
		w.grpc.url.SetText(srv.URL)
		w.grpc.targetChanged()
	})
	waitFor(t, "the methods", func() bool { return w.grpc.method.count() == 6 })

	mainthread.Wait(func() {
		w.grpc.method.selectMethod("test.v1.Echo/Say")
		if w.grpc.streaming.IsVisibleTo(w.grpc.root) {
			t.Error("Say is said to take a stream")
		}
		w.grpc.method.selectMethod("test.v1.Echo/Gather")
		if !w.grpc.streaming.IsVisibleTo(w.grpc.root) {
			t.Error("Gather is not said to take a stream")
		}

		// The example is two messages.
		w.grpc.example.Click()
		if text := w.grpc.message.ToPlainText(); !strings.HasPrefix(text, "[\n  {") || strings.Count(text, `"text"`) != 2 {
			t.Errorf("example %s", text)
		}

		// Messages one after another are formatted each.
		w.grpc.message.SetPlainText(`{"text":"a"} {"text":"b"}`)
		w.grpc.format.format()
		if text := w.grpc.message.ToPlainText(); text != "{\n  \"text\": \"a\"\n}\n{\n  \"text\": \"b\"\n}" {
			t.Errorf("formatted %q", text)
		}
		w.grpc.invoke.Click()
	})
	waitFor(t, "the response", func() bool { return strings.Contains(body(), `"text": "a b"`) })
	mainthread.Wait(func() {
		if fieldValue(w.grpcResponse, fieldCode) != "0 OK" || !strings.Contains(body(), `"index": 2`) {
			t.Errorf("status %q, body %s", fieldValue(w.grpcResponse, fieldCode), body())
		}

		// A method that takes one message is not sent two.
		w.grpc.method.selectMethod("test.v1.Echo/Say")
		w.grpc.invoke.Click()
	})
	waitFor(t, "the error", func() bool {
		return strings.Contains(fieldValue(w.grpcResponse, fieldError), "takes one message, and 2 were given")
	})
}

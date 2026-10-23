package ui

import (
	"testing"

	qt "github.com/mappu/miqt/qt6"
	"github.com/mappu/miqt/qt6/mainthread"

	"github.com/nomad-software/triggerapi/internal/collection"
)

// pressReturn sends Return to a widget.
func pressReturn(widget *qt.QWidget) {
	event := qt.NewQKeyEvent3(qt.QEvent__KeyPress, int(qt.Key_Return), qt.NoModifier, "\r")
	qt.QCoreApplication_SendEvent(widget.QObject, event.QEvent)
	event.Delete()
}

// loseKeyboard tells a widget that it has lost the keyboard.
func loseKeyboard(widget *qt.QWidget) {
	event := qt.NewQFocusEvent(qt.QEvent__FocusOut)
	qt.QCoreApplication_SendEvent(widget.QObject, event.QEvent)
	event.Delete()
}

// typedField is a field of a request UI that is typed into, and where what
// is typed into it is kept in the request.
type typedField struct {
	name string
	typ  collection.Type
	// line is set for a line edit, which is committed by Return; a text
	// editor is committed by losing the keyboard.
	line   bool
	widget func(w *Window) *qt.QWidget
	saved  func(r *collection.Request) string
}

// typedFields are every field typed into of every type of request.
var typedFields = []typedField{
	{"REST body", collection.TypeREST, false,
		func(w *Window) *qt.QWidget { return w.request.bodyRaw.QWidget },
		func(r *collection.Request) string { return r.Request.Body }},
	{"REST username", collection.TypeREST, true,
		func(w *Window) *qt.QWidget { return w.request.auth.username.QWidget },
		func(r *collection.Request) string { return r.Request.Auth.Username }},
	{"gRPC address", collection.TypeGRPC, true,
		func(w *Window) *qt.QWidget { return w.grpc.url.QWidget },
		func(r *collection.Request) string { return r.GRPC.URL }},
	{"gRPC message", collection.TypeGRPC, false,
		func(w *Window) *qt.QWidget { return w.grpc.message.QWidget },
		func(r *collection.Request) string { return r.GRPC.Message }},
	{"gRPC token", collection.TypeGRPC, true,
		func(w *Window) *qt.QWidget { return w.grpc.auth.token.QWidget },
		func(r *collection.Request) string { return r.GRPC.Auth.Token }},
	{"GraphQL endpoint", collection.TypeGraphQL, true,
		func(w *Window) *qt.QWidget { return w.graphql.url.QWidget },
		func(r *collection.Request) string { return r.GraphQL.URL }},
	{"GraphQL query", collection.TypeGraphQL, false,
		func(w *Window) *qt.QWidget { return w.graphql.query.QWidget },
		func(r *collection.Request) string { return r.GraphQL.Query }},
	{"GraphQL variables", collection.TypeGraphQL, false,
		func(w *Window) *qt.QWidget { return w.graphql.variables.QWidget },
		func(r *collection.Request) string { return r.GraphQL.Variables }},
	{"GraphQL password", collection.TypeGraphQL, true,
		func(w *Window) *qt.QWidget { return w.graphql.auth.password.QWidget },
		func(r *collection.Request) string { return r.GraphQL.Auth.Password }},
	{"MCP server", collection.TypeMCP, true,
		func(w *Window) *qt.QWidget { return w.mcp.url.QWidget },
		func(r *collection.Request) string { return r.MCP.URL }},
	{"MCP arguments", collection.TypeMCP, false,
		func(w *Window) *qt.QWidget { return w.mcp.arguments.QWidget },
		func(r *collection.Request) string { return r.MCP.Arguments }},
	{"MCP client ID", collection.TypeMCP, true,
		func(w *Window) *qt.QWidget { return w.mcp.auth.clientID.QWidget },
		func(r *collection.Request) string { return r.MCP.Auth.Settings().ClientID }},
	{"WebSocket address", collection.TypeWebSocket, true,
		func(w *Window) *qt.QWidget { return w.ws.url.QWidget },
		func(r *collection.Request) string { return r.WebSocket.URL }},
	{"WebSocket message", collection.TypeWebSocket, false,
		func(w *Window) *qt.QWidget { return w.ws.message.QWidget },
		func(r *collection.Request) string { return r.WebSocket.Message }},
}

// What is typed into any field of a request is saved once it is committed,
// rather than on every key, as saving writes every request: a line edit on
// Return and a text editor when it loses the keyboard.
func TestTypingSavedOnCommit(t *testing.T) {
	for _, f := range typedFields {
		t.Run(f.name, func(t *testing.T) {
			files := testFiles(t)
			var w *Window
			var r *collection.Request
			mainthread.Wait(func() {
				w = NewWindow(files.Files)
				w.Show()
				r = addRequestOf(w, f.typ)
				typeKeys(f.widget(w), "7")
			})
			if got := mainthread.Wait2(func() string { return f.saved(r) }); got != "" {
				t.Errorf("kept while typing: %q", got)
			}
			if saved, err := loadRequests(files.Requests); err != nil || f.saved(saved.Folders[0].Requests[0]) != "" {
				t.Errorf("saved while typing: %v", err)
			}

			mainthread.Wait(func() {
				if f.line {
					pressReturn(f.widget(w))
				} else {
					loseKeyboard(f.widget(w))
				}
			})
			if got := mainthread.Wait2(func() string { return f.saved(r) }); got != "7" {
				t.Errorf("kept once committed: %q", got)
			}
			saved, err := loadRequests(files.Requests)
			if err != nil {
				t.Fatal(err)
			}
			if got := f.saved(saved.Folders[0].Requests[0]); got != "7" {
				t.Errorf("saved once committed: %q", got)
			}
		})
	}
}

// What is typed and not yet committed is saved before anything that needs
// it: another request being shown, the request being sent, copied or
// duplicated, and the window closing.
func TestTypingCommitted(t *testing.T) {
	type step struct {
		name string
		do   func(w *Window)
		// check reports what of the typing reached where it had to.
		check func(w *Window, r *collection.Request) string
	}
	message := func(r *collection.Request) string { return r.GRPC.Message }
	for _, s := range []step{
		{"show", func(w *Window) { w.show(nil) }, func(_ *Window, r *collection.Request) string { return message(r) }},
		{"invoke", func(w *Window) { w.grpc.invoke.Click() }, func(_ *Window, r *collection.Request) string { return message(r) }},
		{"copy", func(w *Window) { w.requests.copy() }, func(w *Window, _ *collection.Request) string {
			return message(w.requests.copied.(*collection.Request))
		}},
		{"duplicate", func(w *Window) { w.requests.duplicate() }, func(w *Window, _ *collection.Request) string {
			return message(w.open)
		}},
		{"commit", func(w *Window) { w.commit() }, func(_ *Window, r *collection.Request) string { return message(r) }},
	} {
		t.Run(s.name, func(t *testing.T) {
			files := testFiles(t)
			var w *Window
			var r *collection.Request
			mainthread.Wait(func() {
				w = NewWindow(files.Files)
				w.Show()
				r = addGRPCRequest(w)
				typeKeys(w.grpc.message.QWidget, "{}")
				s.do(w)
			})
			if got := mainthread.Wait2(func() string { return s.check(w, r) }); got != "{}" {
				t.Errorf("message = %q", got)
			}
			mainthread.Wait(func() {
				if w.cancel != nil {
					w.cancel()
				}
			})
		})
	}

	// Closing the window writes it to the file, and everything waiting.
	files := testFiles(t)
	mainthread.Wait(func() {
		w := NewWindow(files.Files)
		w.Show()
		addGRPCRequest(w)
		typeKeys(w.grpc.message.QWidget, "{}")
		w.Close()
	})
	saved, err := loadRequests(files.Requests)
	if err != nil {
		t.Fatal(err)
	}
	if got := saved.Folders[0].Requests[0].GRPC.Message; got != "{}" {
		t.Errorf("saved on closing: %q", got)
	}
}

// A change made in one go, rather than typed, is saved at once: an example
// used and JSON formatted.
func TestOneChangeSavedAtOnce(t *testing.T) {
	files := testFiles(t)
	mainthread.Wait(func() {
		w := NewWindow(files.Files)
		w.Show()
		r := addRequestOf(w, collection.TypeWebSocket)
		w.ws.message.SetPlainText(`{"a":1}`)
		w.ws.format.button.Click()
		if r.WebSocket.Message != "{\n  \"a\": 1\n}" {
			t.Errorf("formatted message = %q", r.WebSocket.Message)
		}
	})
}

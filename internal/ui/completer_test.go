package ui

import (
	"strings"
	"testing"

	qt "github.com/mappu/miqt/qt6"
	"github.com/mappu/miqt/qt6/mainthread"

	"github.com/nomad-software/triggerapi/internal/collection"
	"github.com/nomad-software/triggerapi/internal/completion"
	"github.com/nomad-software/triggerapi/internal/rpc/rpctest"
)

// typeKeys types text into a widget a key at a time. It must be called on
// the main thread.
func typeKeys(widget *qt.QWidget, text string) {
	for _, r := range text {
		event := qt.NewQKeyEvent3(qt.QEvent__KeyPress, int(r), qt.NoModifier, string(r))
		qt.QCoreApplication_SendEvent(widget.QObject, event.QEvent)
		event.Delete()
	}
}

// offered returns the labels the list of a completer shows, or nil if it is
// not open.
func offered(c *completer) []string {
	if !c.list.IsVisible() {
		return nil
	}
	var labels []string
	root := qt.NewQModelIndex()
	defer root.Delete()
	for row := range c.model.RowCount(root) {
		labels = append(labels, c.model.Item(row).Text())
	}
	return labels
}

func TestCompleter(t *testing.T) {
	mainthread.Wait(func() {
		edit := qt.NewQPlainTextEdit2()
		defer edit.Delete()
		edit.Show()
		items := []completion.Item{{Label: "name", Detail: "String", Insert: "name"}, {Label: "nick", Detail: "String", Insert: "nick { }"}}
		c := newCompleter(edit, func(text string, offset int) completion.Completion {
			start := completion.NameStart(text, offset)
			return completion.Completion{Start: start, End: offset, Items: completion.Matching(items, text[start:offset])}
		})

		// Nothing is offered until a name is typed.
		typeKeys(edit.QWidget, "é ")
		if got := offered(c); got != nil {
			t.Errorf("before a name: %q", got)
		}
		typeKeys(edit.QWidget, "n")
		if got := offered(c); strings.Join(got, ",") != "name,nick" {
			t.Errorf("after n: %q", got)
		}
		if c.model.Item2(0, 1).Text() != "String" {
			t.Error("the detail is not beside the label")
		}
		typeKeys(edit.QWidget, "i")
		if got := offered(c); strings.Join(got, ",") != "nick" {
			t.Errorf("after ni: %q", got)
		}

		// What is chosen replaces what has been typed of it, after text
		// that is not ASCII.
		c.insert(0)
		if got := edit.ToPlainText(); got != "é nick { }" {
			t.Errorf("inserted: %q", got)
		}

		// Typing something else closes the list.
		typeKeys(edit.QWidget, "n")
		typeKeys(edit.QWidget, " ")
		if got := offered(c); got != nil {
			t.Errorf("after a space: %q", got)
		}

		// While it is open the list has Return, which the editor does not
		// take.
		typeKeys(edit.QWidget, "n")
		before := edit.ToPlainText()
		event := qt.NewQKeyEvent(qt.QEvent__KeyPress, int(qt.Key_Return), qt.NoModifier)
		defer event.Delete()
		qt.QCoreApplication_SendEvent(edit.QObject, event.QEvent)
		if edit.ToPlainText() != before || event.IsAccepted() {
			t.Errorf("Return was taken by the editor: %q", edit.ToPlainText())
		}

		// Ctrl+Space offers what there is without anything typed.
		c.list.Hide()
		edit.SetPlainText("")
		space := qt.NewQKeyEvent3(qt.QEvent__KeyPress, int(qt.Key_Space), qt.ControlModifier, " ")
		defer space.Delete()
		qt.QCoreApplication_SendEvent(edit.QObject, space.QEvent)
		if got := offered(c); strings.Join(got, ",") != "name,nick" || edit.ToPlainText() != "" {
			t.Errorf("Ctrl+Space: %q, text %q", got, edit.ToPlainText())
		}
		c.list.Hide()
	})
}

func TestCompleteGRPCMessage(t *testing.T) {
	srv := rpctest.NewServer(rpctest.ReflectionAll)
	defer srv.Close()

	var w *Window
	var c *completer
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
		c = w.grpc.completer
		w.grpc.message.SetFocus()
		typeKeys(w.grpc.message.QWidget, `{"`)
		if got := offered(c); strings.Join(got, ",") != "count,inner,tags,text" {
			t.Errorf("fields: %q", got)
		}
		c.insert(0)
		if got := w.grpc.message.ToPlainText(); got != `{"count": ` {
			t.Errorf("inserted: %q", got)
		}
		c.list.Hide()
	})
}

func TestCompleteGraphQLQuery(t *testing.T) {
	srv := graphqlServer(t)
	var w *Window
	mainthread.Wait(func() {
		w = NewWindow(testFiles(t).Files)
		w.Show()
		defer func(prev collection.Type) { testRequestType = prev }(testRequestType)
		testRequestType = collection.TypeGraphQL
		w.requests.addRequest()
		w.graphql.url.SetText(srv.URL)
		if w.graphql.loaded != "" {
			t.Error("the schema was asked for before it was wanted")
		}
		// The schema is asked for when the query is given the keyboard,
		// to complete it from.
		w.graphql.query.SetFocus()
	})
	waitFor(t, "the schema", func() bool { return mainthread.Wait2(func() bool { return w.graphql.described != nil }) })
	mainthread.Wait(func() {
		c := w.graphql.completer
		typeKeys(w.graphql.query.QWidget, "{ u")
		if got := offered(c); strings.Join(got, ",") != "user" {
			t.Errorf("fields: %q", got)
		}
		c.insert(0)
		if got := w.graphql.query.ToPlainText(); got != "{ user" {
			t.Errorf("inserted: %q", got)
		}
		c.list.Hide()
	})
}

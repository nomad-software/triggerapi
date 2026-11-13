package ui

import (
	"github.com/nomad-software/qute"
	"github.com/nomad-software/qute/qtcore"
	"github.com/nomad-software/qute/qtgui"
	"github.com/nomad-software/qute/qtwidgets"

	"strings"
	"testing"

	"github.com/nomad-software/triggerapi/internal/collection"
	"github.com/nomad-software/triggerapi/internal/completion"
	"github.com/nomad-software/triggerapi/internal/rpc/rpctest"
)

// typeKeys types text into a widget a key at a time. It must be called on
// the main thread.
func typeKeys(widget *qtwidgets.QWidget, text string) {
	for _, r := range text {
		event := qtgui.NewQKeyEventFull(qtcore.QEventKeyPress, int(r), qtcore.NoModifier, string(r), false, 1)
		qtcore.QCoreApplicationSendEvent(widget.AsQObject(), event.AsQEvent())
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
	for row := range c.model.RowCount() {
		labels = append(labels, c.model.Item(row).Text())
	}
	return labels
}

func TestCompleter(t *testing.T) {
	qute.PostSync(func() {
		edit := qtwidgets.NewQPlainTextEdit(nil)
		defer edit.Delete()
		edit.Show()
		items := []completion.Item{{Label: "name", Detail: "String", Insert: "name"}, {Label: "nick", Detail: "String", Insert: "nick { }"}}
		c := newCompleter(edit, func(text string, offset int) completion.Completion {
			start := completion.NameStart(text, offset)
			return completion.Completion{Start: start, End: offset, Items: completion.Matching(items, text[start:offset])}
		})

		// Nothing is offered until a name is typed.
		typeKeys(edit.AsQWidget(), "é ")
		if got := offered(c); got != nil {
			t.Errorf("before a name: %q", got)
		}
		typeKeys(edit.AsQWidget(), "n")
		if got := offered(c); strings.Join(got, ",") != "name,nick" {
			t.Errorf("after n: %q", got)
		}
		if c.model.ItemFull(0, 1).Text() != "String" {
			t.Error("the detail is not beside the label")
		}
		typeKeys(edit.AsQWidget(), "i")
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
		typeKeys(edit.AsQWidget(), "n")
		typeKeys(edit.AsQWidget(), " ")
		if got := offered(c); got != nil {
			t.Errorf("after a space: %q", got)
		}

		// While it is open the list has Return, which the editor does not
		// take.
		typeKeys(edit.AsQWidget(), "n")
		before := edit.ToPlainText()
		event := qtgui.NewQKeyEvent(qtcore.QEventKeyPress, int(qtcore.Key_Return), qtcore.NoModifier)
		defer event.Delete()
		qtcore.QCoreApplicationSendEvent(edit.AsQObject(), event.AsQEvent())
		if edit.ToPlainText() != before || event.IsAccepted() {
			t.Errorf("Return was taken by the editor: %q", edit.ToPlainText())
		}

		// Ctrl+Space offers what there is without anything typed.
		c.list.Hide()
		edit.SetPlainText("")
		space := qtgui.NewQKeyEventFull(qtcore.QEventKeyPress, int(qtcore.Key_Space), qtcore.ControlModifier, " ", false, 1)
		defer space.Delete()
		qtcore.QCoreApplicationSendEvent(edit.AsQObject(), space.AsQEvent())
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
	qute.PostSync(func() {
		w = NewWindow(testFiles(t).Files)
		w.Show()
		addGRPCRequest(w)
		w.grpc.url.SetText(srv.URL)
		w.grpc.targetChanged()
	})
	waitFor(t, "the methods", func() bool { return w.grpc.method.count() == 4 })
	qute.PostSync(func() {
		w.grpc.method.selectMethod("test.v1.Echo/Say")
		c = w.grpc.completer
		w.grpc.message.SetFocus()
		typeKeys(w.grpc.message.AsQWidget(), `{"`)
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
	qute.PostSync(func() {
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
	waitFor(t, "the schema", func() bool { return onGUI(func() bool { return w.graphql.described != nil }) })
	qute.PostSync(func() {
		c := w.graphql.completer
		typeKeys(w.graphql.query.AsQWidget(), "{ u")
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

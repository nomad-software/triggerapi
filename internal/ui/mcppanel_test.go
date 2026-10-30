package ui

import (
	"reflect"
	"strings"
	"testing"

	qt "github.com/mappu/miqt/qt6"
	"github.com/mappu/miqt/qt6/mainthread"

	"github.com/nomad-software/triggerapi/internal/capture"
	"github.com/nomad-software/triggerapi/internal/collection"
	"github.com/nomad-software/triggerapi/internal/env"
	"github.com/nomad-software/triggerapi/internal/mcp"
	"github.com/nomad-software/triggerapi/internal/mcp/mcptest"
	"github.com/nomad-software/triggerapi/internal/rest"
)

func TestMCPRequest(t *testing.T) {
	srv := mcptest.NewServer(t)
	files := testFiles(t)
	envs := &env.Store{Active: 0, Environments: []env.Environment{{Name: "Local", Variables: []env.Variable{
		{Key: "host", Value: srv.URL, Enabled: true},
		{Key: "who", Value: "world", Enabled: true},
	}}}}
	if err := envs.Save(files.Environments); err != nil {
		t.Fatal(err)
	}

	var w *Window
	field := func(label string) string { return fieldValue(w.mcpResponse, label) }
	body := func() string { return mainthread.Wait2(func() string { return w.mcpResponse.body.ToPlainText() }) }

	mainthread.Wait(func() {
		w = NewWindow(files.Files)
		w.Show()

		// An MCP request is one of the types to add.
		defer func(prev collection.Type) { testRequestType = prev }(testRequestType)
		testRequestType = collection.TypeMCP
		w.requests.addRequest()
		if w.open == nil || w.open.Type != collection.TypeMCP || w.stack.CurrentIndex() != pageMCP {
			t.Fatalf("open %+v on page %d", w.open, w.stack.CurrentIndex())
		}

		w.mcp.url.SetText("{{host}}/mcp")
		w.mcp.headers.add("X-Test", "{{who}}")
		w.mcp.reload.Click()
	})

	// What the server has is offered under headings, the first chosen.
	waitFor(t, "the capabilities", func() bool { return mainthread.Wait2(w.mcp.endpoint.count) == 7 })
	mainthread.Wait(func() {
		if w.mcp.endpoint.current() != "Tools/echo" || w.mcp.endpoints.count() != 7 || w.mcp.description.Text() != "Says the text back." {
			t.Errorf("chosen %q, description %q", w.mcp.endpoint.current(), w.mcp.description.Text())
		}
		if w.open.MCP.Kind != mcp.KindTool || w.open.MCP.Name != "echo" {
			t.Errorf("saved as calling %+v", w.open.MCP)
		}

		// An example is made from what the tool takes.
		w.mcp.example.Click()
		if text := w.mcp.arguments.ToPlainText(); !strings.Contains(text, `"text": ""`) || !strings.Contains(text, `"times": 0`) {
			t.Errorf("example %s", text)
		}

		w.mcp.arguments.SetPlainText(`{"text": "{{who}}", "times": 2}`)
		setCaptureRules(w.mcp.captures, []capture.Rule{{Variable: "said", Path: "structuredContent.said", Enabled: true}})
		w.mcp.call.Click()
	})

	// It is called with the environment applied, and what it captures is
	// kept.
	waitFor(t, "the result", func() bool { return strings.Contains(body(), "worldworld") })
	mainthread.Wait(func() {
		// The server is stateless, as servers are now.
		if field(fieldCode) != "OK" || field(fieldError) != "" || field("Server") != "test-server 1.2.3" || field("Protocol") != "2026-07-28" {
			t.Errorf("status %q, error %q, server %q, protocol %q", field(fieldCode), field(fieldError), field("Server"), field("Protocol"))
		}
		if !strings.Contains(body(), `"header": "world"`) {
			t.Errorf("body %s", body())
		}
		if value := w.expand("{{said}}"); value != "worldworld" {
			t.Errorf("captured %q", value)
		}

		// A tool's error is a failure.
		w.mcp.endpoint.selectMethod("Tools/fail")
		w.mcp.call.Click()
	})
	waitFor(t, "the tool error", func() bool { return mainthread.Wait2(func() bool { return field(fieldCode) == "Tool error" }) })
	mainthread.Wait(func() {
		if field(fieldError) != "it went wrong" {
			t.Errorf("error %q", field(fieldError))
		}

		// A resource is read.
		w.mcp.endpoint.selectMethod("Resources/test://greeting")
		w.mcp.call.Click()
	})
	waitFor(t, "the resource", func() bool { return strings.Contains(body(), `"text": "hello"`) })

	// Requests are made of what is ticked, in a folder named after the
	// server, under the headings.
	mainthread.Wait(func() {
		w.mcp.endpoints.setAll(true)
		w.mcp.endpoints.create.Click()
		var made []string
		for _, f := range w.requests.store.Folders {
			if f.Name != "{{host}}/mcp" {
				continue
			}
			for _, heading := range f.Folders {
				for _, r := range heading.Requests {
					made = append(made, heading.Name+"/"+r.Name+" "+string(r.MCP.Kind))
				}
			}
		}
		if want := []string{"Prompts/greet prompt", "Resources/test://greeting resource", "Tools/echo tool", "Tools/fail tool", "Tools/progress tool", "Tools/sample tool", "Tools/who tool"}; !reflect.DeepEqual(made, want) {
			t.Errorf("made %q", made)
		}
		w.Close()
	})

	saved, err := loadRequests(files.Requests)
	if err != nil {
		t.Fatal(err)
	}
	r := saved.Folders[0].Requests[0]
	want := mcp.Request{
		URL:       "{{host}}/mcp",
		Headers:   []rest.KeyValue{{Key: "X-Test", Value: "{{who}}", Enabled: true}},
		Auth:      rest.Auth{Type: rest.AuthNone},
		Kind:      mcp.KindResource,
		Name:      "test://greeting",
		Arguments: `{"text": "{{who}}", "times": 2}`,
		Captures:  []capture.Rule{{Variable: "said", Path: "structuredContent.said", Enabled: true}},
	}
	if r.Type != collection.TypeMCP || r.MCP == nil || !reflect.DeepEqual(*r.MCP, want) {
		t.Errorf("saved %+v %+v", r, r.MCP)
	}
}

func TestMCPGroupAndDraft(t *testing.T) {
	files := testFiles(t)
	store := &collection.Store{}
	store.AddFolder("Pages").Add(collection.NewREST("Get", rest.Request{Method: "GET"}))
	store.AddFolder("Agents").Add(collection.NewMCP("Echo", mcp.Request{URL: "127.0.0.1:1/mcp", Kind: mcp.KindTool, Name: "echo"}))
	if err := store.Save(files.Requests); err != nil {
		t.Fatal(err)
	}

	mainthread.Wait(func() {
		w := NewWindow(files.Files)
		w.Show()
		tree := w.requests.tree

		var headings []string
		for i := range tree.TopLevelItemCount() {
			headings = append(headings, tree.TopLevelItem(i).Text(colName))
		}
		if want := []string{"MCP", "REST"}; !reflect.DeepEqual(headings, want) {
			t.Errorf("headings %q, want %q", headings, want)
		}
		echo := treeItem(tree, "Agents", "Echo")
		if echo == nil || echo.Icon(colName).CacheKey() != requestTypeIcon(collection.TypeMCP).CacheKey() {
			t.Fatal("the request has not the icon of its type")
		}

		// Opening one shows it in its own UI, calling what it calls until
		// the server says what it has.
		tree.SetCurrentItem(echo)
		if w.stack.CurrentIndex() != pageMCP || w.mcp.url.Text() != "127.0.0.1:1/mcp" || w.mcp.endpoint.current() != "Tools/echo" || !w.mcp.save.IsHidden() {
			t.Errorf("page %d, url %q, endpoint %q", w.stack.CurrentIndex(), w.mcp.url.Text(), w.mcp.endpoint.current())
		}

		// One made from the New tab is not saved until Save is pressed.
		defer func(prev collection.Type) { testRequestType = prev }(testRequestType)
		testRequestType = collection.TypeMCP
		w.newRequest()
		if w.open != nil || w.stack.CurrentIndex() != pageMCP || w.mcp.save.IsHidden() || w.mcp.url.Text() != "" {
			t.Fatalf("draft: open %+v, page %d", w.open, w.stack.CurrentIndex())
		}
		w.mcp.url.SetText("new.test/mcp")
		w.mcp.save.Click()
		if w.open == nil || w.open.Type != collection.TypeMCP || w.open.MCP.URL != "new.test/mcp" {
			t.Errorf("saved draft %+v", w.open)
		}
	})
}

func TestMCPSchemaFailure(t *testing.T) {
	files := testFiles(t)
	var w *Window
	mainthread.Wait(func() {
		w = NewWindow(files.Files)
		w.Show()
		defer func(prev collection.Type) { testRequestType = prev }(testRequestType)
		testRequestType = collection.TypeMCP
		w.newRequest()
		w.mcp.url.SetText("http://127.0.0.1:1/mcp")
		w.mcp.reload.Click()
	})
	waitFor(t, "the failure", func() bool {
		return mainthread.Wait2(func() bool { return strings.Contains(w.mcp.status.Text(), "could not be loaded") })
	})
}

// An example of the arguments needs what the server says the tool takes.
func TestMCPExampleWithoutCapabilities(t *testing.T) {
	files := testFiles(t)
	mainthread.Wait(func() {
		w := NewWindow(files.Files)
		w.Show()
		defer func(prev collection.Type) { testRequestType = prev }(testRequestType)
		testRequestType = collection.TypeMCP
		w.newRequest()
		w.mcp.arguments.SetPlainText(`{"kept": true}`)
		w.mcp.example.Click()
		if !strings.Contains(w.mcp.status.Text(), "capabilities of the server") || w.mcp.status.IsHidden() || w.mcp.arguments.ToPlainText() != `{"kept": true}` {
			t.Errorf("status %q, arguments %q", w.mcp.status.Text(), w.mcp.arguments.ToPlainText())
		}
	})
}

// A description is beside the arguments, as tall as they are, and scrolls.
func TestMCPDescriptionScrolls(t *testing.T) {
	mainthread.Wait(func() {
		p := newMCPPanel()
		defer p.root.Delete()
		p.root.Resize(600, 500)
		p.root.Show()
		p.load(mcp.Request{Kind: mcp.KindTool, Name: "long"})
		qt.QCoreApplication_ProcessEvents()

		if p.descriptionArea.IsVisible() {
			t.Error("shown with no description")
		}
		alone := p.arguments.Width()

		schema := func(description string) *mcp.Schema {
			return &mcp.Schema{Items: []mcp.Item{{Kind: mcp.KindTool, Name: "long", Description: description}}}
		}
		bar := p.descriptionArea.VerticalScrollBar()
		for _, c := range []struct {
			description string
			scrolls     bool
		}{
			{"Short.", false},
			{strings.Repeat("A long line of what the tool does.\n\n", 30), true},
		} {
			p.setSchema(schema(c.description))
			qt.QCoreApplication_ProcessEvents()
			if !p.descriptionArea.IsVisible() || p.descriptionArea.Height() != p.arguments.Height() || p.descriptionArea.Y() != p.arguments.Y() {
				t.Errorf("at %d, %d tall beside arguments at %d, %d tall", p.descriptionArea.Y(), p.descriptionArea.Height(), p.arguments.Y(), p.arguments.Height())
			}
			if p.descriptionArea.X() <= p.arguments.X() || p.arguments.Width() >= alone {
				t.Errorf("at %d beside arguments at %d, %d wide of %d", p.descriptionArea.X(), p.arguments.X(), p.arguments.Width(), alone)
			}
			if (bar.Maximum() > 0) != c.scrolls {
				t.Errorf("%d long, scrolling by %d", len(c.description), bar.Maximum())
			}
		}

		// The two start level, and the separator between them moves.
		sizes := p.argumentsSplit.Sizes()
		if len(sizes) != 2 || sizes[0]-sizes[1] > 2 || sizes[1]-sizes[0] > 2 {
			t.Errorf("sizes %v", sizes)
		}
		p.argumentsSplit.SetSizes([]int{sizes[0] + 50, sizes[1] - 50})
		if got := p.argumentsSplit.Sizes(); got[0] != sizes[0]+50 {
			t.Errorf("moved to %v from %v", got, sizes)
		}

		// The wheel scrolls it.
		pos := qt.NewQPointF2(p.descriptionArea.Viewport().Rect().Center())
		defer pos.Delete()
		global := p.descriptionArea.Viewport().MapToGlobal(pos)
		none, down := qt.NewQPoint(), qt.NewQPoint2(0, -120)
		defer none.Delete()
		defer down.Delete()
		event := qt.NewQWheelEvent(pos, global, *none, *down, qt.NoButton, qt.NoModifier, qt.NoScrollPhase, false)
		defer event.Delete()
		qt.QCoreApplication_SendEvent(p.descriptionArea.Viewport().QObject, event.QEvent)
		if bar.Value() == 0 {
			t.Error("the wheel did not scroll the description")
		}
	})
}

package ui

import (
	"strings"
	"testing"

	qt "github.com/mappu/miqt/qt6"
	"github.com/mappu/miqt/qt6/mainthread"

	"github.com/nomad-software/triggerapi/internal/collection"
	"github.com/nomad-software/triggerapi/internal/mcp"
	"github.com/nomad-software/triggerapi/internal/mcp/mcptest"
)

// TestMCPEvents checks that what the server sends while a call is under way
// is shown in the Events tab, and that what it asks is asked of the user.
func TestMCPEvents(t *testing.T) {
	srv := mcptest.NewServer(t)

	// The dialogs are answered at once.
	var asked *mcp.Elicitation
	var sampled *mcp.Sampling
	var sampleAnswer *mcp.SampleAnswer
	defer func(elicit, sample any) {
		askElicitation = elicit.(func(*qt.QWidget, string, *mcp.Elicitation, func(*mcp.ElicitAnswer)) func())
		askSampling = sample.(func(*qt.QWidget, string, *mcp.Sampling, func(*mcp.SampleAnswer)) func())
	}(askElicitation, askSampling)
	askElicitation = func(_ *qt.QWidget, server string, e *mcp.Elicitation, done func(*mcp.ElicitAnswer)) func() {
		asked = e
		if !strings.HasSuffix(server, "/mcp") {
			t.Errorf("the server is named %q", server)
		}
		done(&mcp.ElicitAnswer{Action: mcp.ActionAccept, Content: map[string]any{"name": "Ada"}})
		return func() {}
	}
	askSampling = func(_ *qt.QWidget, _ string, s *mcp.Sampling, done func(*mcp.SampleAnswer)) func() {
		sampled = s
		done(sampleAnswer)
		return func() {}
	}

	var w *Window
	body := func() string { return w.mcpResponse.body.ToPlainText() }
	events := func() string { return w.mcpResponse.events.log.ToPlainText() }
	tabText := func() string { return w.mcpResponse.tabs.TabText(w.mcpResponse.events.index) }
	call := func(tool, arguments string) {
		mainthread.Wait(func() {
			w.mcp.endpoint.selectMethod("Tools/" + tool)
			w.mcp.arguments.SetPlainText(arguments)
			w.mcp.call.Click()
		})
	}
	mainthread.Wait(func() {
		w = NewWindow(testFiles(t).Files)
		w.Show()
		defer func(prev collection.Type) { testRequestType = prev }(testRequestType)
		testRequestType = collection.TypeMCP
		w.newRequest()
		w.mcp.url.SetText(srv.URL + "/mcp")
		w.mcp.reload.Click()

		// The tab is after the Capture tab, and empty to begin with.
		if w.mcpResponse.events == nil || w.mcpResponse.events.index != tabCapture+1 || tabText() != "Events" {
			t.Error("the panel of MCP requests has no Events tab after Capture")
		}
	})
	waitFor(t, "the capabilities", func() bool { return mainthread.Wait2(w.mcp.endpoint.count) == 7 })

	// Progress and messages logged.
	call("progress", `{"steps": 3}`)
	waitFor(t, "the result", func() bool { return strings.Contains(body(), `"steps": 3`) })
	mainthread.Wait(func() {
		if tabText() != "Events (6)" || !strings.Contains(events(), "Progress 3/3 (100%)\nstep 3") || !strings.Contains(events(), "Log info counter\nstep 1") {
			t.Errorf("tab %q, events %q", tabText(), events())
		}
		if fieldValue(w.mcpResponse, fieldCode) != "OK" {
			t.Errorf("status %q", fieldValue(w.mcpResponse, fieldCode))
		}
		// They are kept in the history.
		if e := w.history.store.Entries[0]; e.MCP == nil || len(e.MCP.Events) != 6 {
			t.Errorf("entry %+v", e.MCP)
		}
	})

	// Elicitation.
	call("who", "")
	waitFor(t, "the answer", func() bool { return strings.Contains(body(), `"name": "Ada"`) })
	mainthread.Wait(func() {
		if asked == nil || asked.Message != "Who are you?" {
			t.Errorf("asked %+v", asked)
		}
		if tabText() != "Events (1)" || !strings.Contains(events(), "Elicitation: accept\nWho are you?\nAnswer: {\n  \"name\": \"Ada\"\n}") {
			t.Errorf("tab %q, events %q", tabText(), events())
		}
	})

	// Sampling, which fails the call if it is declined.
	sampleAnswer = &mcp.SampleAnswer{Text: "Hello there", Model: "by-hand"}
	call("sample", "")
	waitFor(t, "the reply", func() bool { return strings.Contains(body(), `"text": "Hello there"`) })
	mainthread.Wait(func() {
		if sampled == nil || sampled.Messages[0].Text != "Say hello." || !strings.Contains(events(), "Sampling: replied") {
			t.Errorf("sampled %+v, events %q", sampled, events())
		}
	})
	sampleAnswer = nil
	call("sample", "")
	waitFor(t, "the refusal", func() bool { return fieldValue(w.mcpResponse, fieldError) != "" })
}

// TestMCPEventsCancelled checks that a dialog asking the user is closed when
// the call is given up.
func TestMCPEventsCancelled(t *testing.T) {
	srv := mcptest.NewServer(t)
	opened, closed := make(chan struct{}, 1), make(chan struct{}, 1)
	defer func(prev any) {
		askElicitation = prev.(func(*qt.QWidget, string, *mcp.Elicitation, func(*mcp.ElicitAnswer)) func())
	}(askElicitation)
	askElicitation = func(*qt.QWidget, string, *mcp.Elicitation, func(*mcp.ElicitAnswer)) func() {
		opened <- struct{}{}
		return func() { closed <- struct{}{} }
	}

	var w *Window
	mainthread.Wait(func() {
		w = NewWindow(testFiles(t).Files)
		w.Show()
		defer func(prev collection.Type) { testRequestType = prev }(testRequestType)
		testRequestType = collection.TypeMCP
		w.newRequest()
		w.mcp.url.SetText(srv.URL + "/mcp")
		w.mcp.reload.Click()
	})
	waitFor(t, "the capabilities", func() bool { return mainthread.Wait2(w.mcp.endpoint.count) == 7 })
	mainthread.Wait(func() {
		w.mcp.endpoint.selectMethod("Tools/who")
		w.mcp.call.Click()
	})
	<-opened
	mainthread.Wait(func() { w.mcp.call.Click() })
	<-closed
	waitFor(t, "the call to be cancelled", func() bool { return fieldValue(w.mcpResponse, fieldError) == "Cancelled" })
}

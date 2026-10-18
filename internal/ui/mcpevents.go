package ui

import (
	"fmt"
	"strings"

	qt "github.com/mappu/miqt/qt6"

	"github.com/nomad-software/triggerapi/internal/mcp"
)

// eventsTab is the Events tab of the response panel of MCP requests, after
// its Capture tab: what the server sent, and asked and was answered, while a
// call was under way, oldest first.
type eventsTab struct {
	log   *qt.QPlainTextEdit
	index int
	// shown are the events on show.
	shown []mcp.Event
}

// addEventsTab adds the Events tab.
func (p *responsePanel) addEventsTab() {
	log := qt.NewQPlainTextEdit2()
	log.SetReadOnly(true)
	log.SetFont(p.body.Font())
	p.events = &eventsTab{log: log}
	p.events.index = p.tabs.AddTab(log.QWidget, "Events")
}

// showEvents shows the events of a call in the Events tab, if the panel has
// one, in place of any on show.
func (p *responsePanel) showEvents(events []mcp.Event) {
	if p.events == nil {
		return
	}
	p.events.shown = events
	p.events.log.SetPlainText(mcp.EventLog(events))
	p.events.recount(p.tabs)
	scrollToEnd(p.events.log)
}

// noteEvent adds an event of a call under way to the Events tab, and says in
// the bar beneath how far the call has got, if the event says.
func (p *responsePanel) noteEvent(e mcp.Event) {
	if p.events == nil {
		return
	}
	appendEntry(p.events.log, e.Text())
	p.events.shown = append(p.events.shown, e)
	p.events.recount(p.tabs)
	scrollToEnd(p.events.log)

	if e.Kind == mcp.EventProgress {
		p.setFields(field{label: fieldCode, value: "Calling"}, field{label: "Progress", value: strings.TrimSpace(e.Fraction() + "  " + e.Message)})
	}
}

// recount names the tab with how many events it has.
func (t *eventsTab) recount(tabs *qt.QTabWidget) {
	name := "Events"
	if len(t.shown) > 0 {
		name = fmt.Sprintf("Events (%d)", len(t.shown))
	}
	tabs.SetTabText(t.index, name)
}

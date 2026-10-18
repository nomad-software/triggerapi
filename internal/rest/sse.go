package rest

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"
)

// EventStream is the content type of a stream of server-sent events.
const EventStream = "text/event-stream"

// Event is a message of a stream of server-sent events, as the HTML
// standard has them. It is kept in the history with the response.
type Event struct {
	// At is when the event came, from when the request was started. It is
	// zero where that is not known, as for a response that curl read.
	At time.Duration `json:"at,omitempty"`
	// Type is what the event field named it, which is empty for a message.
	Type string `json:"event,omitempty"`
	// ID is the last event ID there was at the event, which an event
	// carries on from the one before if it does not give one.
	ID   string `json:"id,omitempty"`
	Data string `json:"data"`
}

// isEventStream reports if headers declare a stream of server-sent events.
func isEventStream(headers http.Header) bool {
	kind, _, err := mime.ParseMediaType(headers.Get("Content-Type"))
	return err == nil && kind == EventStream
}

// eventParser makes events of the lines of a stream, one line at a time.
type eventParser struct {
	data    strings.Builder
	hasData bool
	typ     string
	// id is the last event ID, which outlives the event it came in.
	id string
}

// line takes a line of the stream, without its line ending, and returns the
// event it ends, if it is the blank line that ends one with data.
func (p *eventParser) line(line string) (Event, bool) {
	if line == "" {
		event, ok := Event{Type: p.typ, ID: p.id, Data: p.data.String()}, p.hasData
		p.data.Reset()
		p.hasData, p.typ = false, ""
		return event, ok
	}
	if strings.HasPrefix(line, ":") {
		// A comment, often sent to keep the connection open.
		return Event{}, false
	}
	field, value, _ := strings.Cut(line, ":")
	value = strings.TrimPrefix(value, " ")
	switch field {
	case "data":
		if p.hasData {
			p.data.WriteByte('\n')
		}
		p.data.WriteString(value)
		p.hasData = true
	case "event":
		p.typ = value
	case "id":
		// An ID with a null in it is ignored, as the standard says.
		if !strings.ContainsRune(value, 0) {
			p.id = value
		}
	}
	// Any other field, retry among them, says nothing of the events.
	return Event{}, false
}

// ParseEvents returns the events of the whole of a stream of server-sent
// events, which have no times. An event the stream ends in the middle of is
// left out, as the standard says.
func ParseEvents(stream []byte) []Event {
	var events []Event
	_ = readEvents(bytes.NewReader(stream), func(e Event) { events = append(events, e) })
	return events
}

// readEvents reads the events of a stream as they come, giving each to
// found, until the stream ends, which is no error, or fails.
func readEvents(stream io.Reader, found func(Event)) error {
	reader := bufio.NewReader(stream)
	var parser eventParser
	first := true
	for {
		line, err := reader.ReadString('\n')
		if line != "" {
			if first {
				line = strings.TrimPrefix(line, "\uFEFF")
				first = false
			}
			line = strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
			if event, ok := parser.line(line); ok {
				found(event)
			}
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

// IsEventStream reports if the response is a stream of server-sent events.
func (r *Response) IsEventStream() bool {
	return isEventStream(r.Headers)
}

// StreamEvents returns the events of a stream of server-sent events. A
// response kept in the history from before they were kept has only its body,
// which they are read from, without their times.
func (r *Response) StreamEvents() []Event {
	if r.Events == nil && len(r.Body) > 0 {
		return ParseEvents(r.Body)
	}
	return r.Events
}

// Document returns what values are captured from: the body, or for a stream
// of events the data of the last of them, which is empty if there are none.
func (r *Response) Document() string {
	if !r.IsEventStream() {
		return string(r.Body)
	}
	events := r.StreamEvents()
	if len(events) == 0 {
		return ""
	}
	return events[len(events)-1].Data
}

// EventLog returns events as text, each under a line saying when it came,
// what type it is and its ID, with a blank line between them.
func EventLog(events []Event) string {
	var log strings.Builder
	for i, e := range events {
		if i > 0 {
			log.WriteString("\n\n")
		}
		log.WriteString(FormatEvent(e))
	}
	return log.String()
}

// FormatEvent returns an event as EventLog has it.
func FormatEvent(e Event) string {
	var head []string
	if e.At > 0 {
		head = append(head, fmt.Sprintf("+%.3f s", e.At.Seconds()))
	}
	typ := e.Type
	if typ == "" {
		typ = "message"
	}
	head = append(head, typ)
	if e.ID != "" {
		head = append(head, "id "+e.ID)
	}
	return "← " + strings.Join(head, "  ") + "\n" + e.Data
}

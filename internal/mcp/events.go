package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// EventKind is the kind of thing a server sends, or asks, while a call is
// under way.
type EventKind string

const (
	// EventProgress is a notification of how far the call has got.
	EventProgress EventKind = "progress"
	// EventLogged is a message the server logs.
	EventLogged EventKind = "log"
	// EventElicitation is the server asking the user for something, and
	// EventSampling it asking for a message from a language model.
	EventElicitation EventKind = "elicitation"
	EventSampling    EventKind = "sampling"
)

// Event is something the server sent, or asked and was answered, while a
// call was under way. It is kept in the history with the response.
type Event struct {
	// At is when it came, from when the call was started, or for what
	// was asked when it was answered.
	At   time.Duration `json:"at"`
	Kind EventKind     `json:"kind"`
	// Message is the message of the progress, the text of the message
	// logged, or the message of what was asked.
	Message string `json:"message,omitempty"`
	// Progress is how far the call has got, out of Total if that is not
	// zero.
	Progress float64 `json:"progress,omitempty"`
	Total    float64 `json:"total,omitempty"`
	// Level and Logger are those of a message logged.
	Level  string `json:"level,omitempty"`
	Logger string `json:"logger,omitempty"`
	// Outcome is how what was asked was answered: the action of an
	// elicitation (accept, decline or cancel), or for sampling replied or
	// declined. Answer is what was given, as JSON.
	Outcome string          `json:"outcome,omitempty"`
	Answer  json.RawMessage `json:"answer,omitempty"`
}

// Size is about how much of the history the event takes, in bytes.
func (e Event) Size() int {
	return len(e.Message) + len(e.Answer)
}

// Text returns the event as EventLog has it: a line saying when it came and
// what it is, with what it says beneath.
func (e Event) Text() string {
	var head strings.Builder
	fmt.Fprintf(&head, "+%.3f s  ", e.At.Seconds())
	var lines []string
	switch e.Kind {
	case EventProgress:
		head.WriteString("Progress " + e.Fraction())
		if e.Message != "" {
			lines = append(lines, e.Message)
		}
	case EventLogged:
		head.WriteString("Log")
		for _, part := range []string{e.Level, e.Logger} {
			if part != "" {
				head.WriteString(" " + part)
			}
		}
		lines = append(lines, e.Message)
	case EventElicitation, EventSampling:
		name := "Elicitation"
		if e.Kind == EventSampling {
			name = "Sampling"
		}
		head.WriteString(name + ": " + e.Outcome)
		if e.Message != "" {
			lines = append(lines, e.Message)
		}
		if len(e.Answer) > 0 {
			var answer bytes.Buffer
			if json.Indent(&answer, e.Answer, "", "  ") != nil {
				answer.Write(e.Answer)
			}
			lines = append(lines, "Answer: "+answer.String())
		}
	default:
		head.WriteString(string(e.Kind))
		lines = append(lines, e.Message)
	}
	return strings.Join(append([]string{head.String()}, lines...), "\n")
}

// Fraction returns how far a call has got as text: the progress, out of
// the total with a percentage if there is one.
func (e Event) Fraction() string {
	if e.Total <= 0 {
		return formatNumber(e.Progress)
	}
	return fmt.Sprintf("%s/%s (%.0f%%)", formatNumber(e.Progress), formatNumber(e.Total), 100*e.Progress/e.Total)
}

// formatNumber returns a number as text, with as many decimal places as it
// needs and no more.
func formatNumber(f float64) string {
	return strconv.FormatFloat(f, 'f', -1, 64)
}

// EventLog returns events as text, oldest first, with a blank line between
// them.
func EventLog(events []Event) string {
	texts := make([]string, len(events))
	for i, e := range events {
		texts[i] = e.Text()
	}
	return strings.Join(texts, "\n\n")
}

// Listener is told what the server of a call sends while it is under way,
// and answers what the server asks. Its functions are called on goroutines
// of the client.
type Listener struct {
	// Event is given each event as it comes, if it is not nil.
	Event func(Event)
	// Elicit asks the user for what the server asks of them. What is asked
	// is declined if it is nil.
	Elicit func(context.Context, *Elicitation) (*ElicitAnswer, error)
	// Sample asks for a message to give the server in place of a language
	// model. What is asked is declined if it is nil, and is if it returns
	// ErrDeclined.
	Sample func(context.Context, *Sampling) (*SampleAnswer, error)
}

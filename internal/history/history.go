// Package history keeps the requests that have been sent from a workspace,
// each with what came back. It has no dependency on the UI.
package history

import (
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/nomad-software/triggerapi/internal/collection"
	"github.com/nomad-software/triggerapi/internal/crypt"
	"github.com/nomad-software/triggerapi/internal/graphql"
	"github.com/nomad-software/triggerapi/internal/jsonfile"
	"github.com/nomad-software/triggerapi/internal/mcp"
	"github.com/nomad-software/triggerapi/internal/rest"
	"github.com/nomad-software/triggerapi/internal/rpc"
	"github.com/nomad-software/triggerapi/internal/ws"
)

// MaxBody is the most of the body of a response that an entry keeps, in
// bytes. A larger body is left out of the history, the rest of the
// response being kept.
const MaxBody = 1 << 20

// MaxTotal is the most of the bodies of responses that the history keeps
// altogether, in bytes. The oldest entries are dropped to keep within it, as
// the whole file is written again each time a request is sent.
const MaxTotal = 32 << 20

// Entry is a request that was sent and what came of it. The request is a
// copy of it as it was then, with its placeholders, which is the record:
// the saved request it was sent from may have changed since, or gone.
type Entry struct {
	Time time.Time `json:"time"`
	// Request is the request as it was composed, with its name if it was
	// a saved one.
	Request *collection.Request `json:"request"`
	// Path is the place of the saved request it was sent from in the
	// requests of the workspace (see collection.Store.Path), or nil if it
	// was not saved. The request may have been moved or removed since.
	Path []string `json:"path,omitempty"`
	// Environment is the name of the environment that was active.
	Environment string `json:"environment,omitempty"`
	// URL is where the request went, with the placeholders replaced. It is
	// the only part kept so, as the values of the others are often secrets.
	URL string `json:"url,omitempty"`
	// Duration is how long the request took, or the connection of a
	// WebSocket was open.
	Duration time.Duration `json:"duration,omitempty"`

	// Error is the error there was instead of a response. Otherwise one of
	// the responses is set: HTTP for a REST or GraphQL request, GRPC,
	// MCP or WebSocket for one of those.
	Error     string         `json:"error,omitempty"`
	HTTP      *rest.Response `json:"http,omitempty"`
	GRPC      *rpc.Response  `json:"grpc,omitempty"`
	MCP       *mcp.Response  `json:"mcp,omitempty"`
	WebSocket *WebSocket     `json:"websocket,omitempty"`

	// Omitted is the size of the body that was left out, as it was larger
	// than MaxBody, or zero if none was.
	Omitted int `json:"omitted,omitempty"`
}

// WebSocket is what went over the connection of a WebSocket request.
type WebSocket struct {
	// Headers are those of the server's answer to the handshake, and
	// Handshake how long it took.
	Headers   http.Header   `json:"headers,omitempty"`
	Handshake time.Duration `json:"handshake,omitempty"`
	// Messages are those sent and received, in order.
	Messages []ws.Message `json:"messages,omitempty"`
	// Status is how the connection ended, such as Disconnected or a close
	// code, and Failed marks one that is a failure. Reason is the reason
	// the server gave for closing it, or the error that ended it.
	Status string `json:"status"`
	Failed bool   `json:"failed,omitempty"`
	Reason string `json:"reason,omitempty"`
}

// Counts returns how many of the messages were sent and how many received.
func (w *WebSocket) Counts() (sent, received int) {
	for _, m := range w.Messages {
		if m.Sent {
			sent++
		} else {
			received++
		}
	}
	return sent, received
}

// Status returns how the request went, short enough to be listed, and
// whether that was a failure: the HTTP status code, the name of the gRPC
// status, the outcome of an MCP call or how a WebSocket connection ended.
func (e *Entry) Status() (string, bool) {
	switch {
	case e.Error != "":
		return "Error", true
	case e.HTTP != nil:
		failed := e.HTTP.StatusCode >= 400
		if e.Request != nil && e.Request.Type == collection.TypeGraphQL && graphql.Errors(e.HTTP.Body) != "" {
			failed = true
		}
		return strconv.Itoa(e.HTTP.StatusCode), failed
	case e.GRPC != nil:
		return e.GRPC.CodeName, !e.GRPC.OK()
	case e.MCP != nil:
		status, _, failed := e.MCP.Outcome()
		return status, failed
	case e.WebSocket != nil:
		return e.WebSocket.Status, e.WebSocket.Failed
	}
	return "", false
}

// Title returns what the entry is listed by: the name of its request, or
// for one that was not saved what it was sent to.
func (e *Entry) Title() string {
	if e.Request == nil {
		return e.URL
	}
	if name := strings.TrimSpace(e.Request.Name); name != "" {
		return name
	}
	switch e.Request.Type {
	case collection.TypeREST:
		return strings.TrimSpace(e.Request.Method + " " + e.URL)
	case collection.TypeGRPC:
		return strings.TrimSpace(e.URL + " " + e.Request.GRPC.Method)
	case collection.TypeMCP:
		return strings.TrimSpace(e.URL + " " + e.Request.MCP.Name)
	}
	return e.URL
}

// Matches reports if every word, in lower case, is in the title or the URL
// of the entry, ignoring case.
func (e *Entry) Matches(words []string) bool {
	text := strings.ToLower(e.Title() + " " + e.URL)
	for _, word := range words {
		if !strings.Contains(text, word) {
			return false
		}
	}
	return true
}

// Text returns the body of the response as it is shown: an HTTP body, with
// JSON indented, the messages of a gRPC call, the result of an MCP call or
// its error, the messages of a WebSocket connection, or the error there was
// instead of a response. It is empty if the body was not kept.
func (e *Entry) Text() string {
	switch {
	case e.Error != "":
		return e.Error
	case e.HTTP != nil:
		return e.HTTP.Text()
	case e.GRPC != nil:
		return strings.Join(e.GRPC.Messages, "\n\n")
	case e.MCP != nil:
		if e.MCP.Result == nil && e.MCP.Error == nil {
			return ""
		}
		return e.MCP.Text()
	case e.WebSocket != nil:
		return ws.Log(e.WebSocket.Messages)
	}
	return ""
}

// size returns the size of the body of the response, in bytes.
func (e *Entry) size() int {
	switch {
	case e.HTTP != nil:
		// A stream of events is kept as it came and as its events.
		n := len(e.HTTP.Body)
		for _, event := range e.HTTP.Events {
			n += len(event.Data)
		}
		return n
	case e.GRPC != nil:
		n := 0
		for _, m := range e.GRPC.Messages {
			n += len(m)
		}
		return n
	case e.MCP != nil:
		n := len(e.MCP.Result)
		for _, event := range e.MCP.Events {
			n += event.Size()
		}
		return n
	case e.WebSocket != nil:
		n := 0
		for _, m := range e.WebSocket.Messages {
			n += len(m.Text)
		}
		return n
	}
	return 0
}

// trim leaves the body of the response out if it is larger than MaxBody.
func (e *Entry) trim() {
	size := e.size()
	if size <= MaxBody {
		return
	}
	// The responses are copied, as whoever recorded them may still be
	// showing them.
	switch {
	case e.HTTP != nil:
		res := *e.HTTP
		res.Body, res.Events = nil, nil
		e.HTTP = &res
	case e.GRPC != nil:
		res := *e.GRPC
		res.Messages = nil
		e.GRPC = &res
	case e.MCP != nil:
		res := *e.MCP
		res.Result, res.Events = nil, nil
		e.MCP = &res
	case e.WebSocket != nil:
		res := *e.WebSocket
		res.Messages = nil
		e.WebSocket = &res
	}
	e.Omitted = size
}

// Store is the history of a workspace, newest first.
type Store struct {
	Entries []*Entry `json:"entries"`
}

// Add puts an entry at the start of the history, leaving out a body larger
// than MaxBody, and keeps the history to limit entries (see Keep).
func (s *Store) Add(e *Entry, limit int) {
	e.trim()
	s.Entries = slices.Insert(s.Entries, 0, e)
	s.Keep(limit)
}

// Keep drops the oldest entries beyond limit, and as many more as it takes
// for the bodies kept to come to no more than MaxTotal. The newest is
// always kept.
func (s *Store) Keep(limit int) {
	limit = max(limit, 1)
	total := 0
	for i, e := range s.Entries {
		total += e.size()
		if i >= limit || (i > 0 && total > MaxTotal) {
			clear(s.Entries[i:])
			s.Entries = s.Entries[:i]
			return
		}
	}
}

// Remove takes the entries out of the history that the function reports
// true for.
func (s *Store) Remove(removed func(*Entry) bool) {
	s.Entries = slices.DeleteFunc(s.Entries, removed)
}

// LoadWith reads the history from the JSON file at path, decrypting it with
// keys. A missing file is an empty history. Entries without a request are
// left out, and the rest put in order, newest first.
func LoadWith(path string, keys *crypt.Keys) (*Store, error) {
	store := &Store{}
	if err := jsonfile.LoadWith(path, store, keys); err != nil {
		return &Store{}, err
	}
	store.Entries = slices.DeleteFunc(store.Entries, func(e *Entry) bool { return e == nil || e.Request == nil })
	for _, e := range store.Entries {
		// The request is given exactly the field for its type.
		e.Request = e.Request.Clone()
	}
	slices.SortStableFunc(store.Entries, func(a, b *Entry) int { return b.Time.Compare(a.Time) })
	return store, nil
}

// SaveWith writes the history to the JSON file at path, encrypted with keys
// unless they are nil.
func (s *Store) SaveWith(path string, keys *crypt.Keys) error {
	return jsonfile.SaveWith(path, s, keys)
}

package ui

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/mappu/miqt/qt6/mainthread"

	"github.com/nomad-software/triggerapi/internal/collection"
	"github.com/nomad-software/triggerapi/internal/history"
	"github.com/nomad-software/triggerapi/internal/network"
	"github.com/nomad-software/triggerapi/internal/ws"
)

// wsSession is the connection the WebSocket request on show has open, and
// what has gone over it. It belongs to the main thread.
type wsSession struct {
	// conn is nil until the handshake is done.
	conn *ws.Conn
	// ctx ends when the connection is closed from this side, which stops
	// the handshake, a send or the reading of messages.
	ctx    context.Context
	cancel context.CancelFunc
	// messages are those sent and received, in order.
	messages       []ws.Message
	sent, received int
	// entry is the entry of the history the connection is kept in once it
	// is closed, or nil if it is not to be, and start when it was asked
	// for.
	entry *history.Entry
	start time.Time
}

// toggleWS opens a connection for the WebSocket request on show, with the
// active environment applied to it, or closes the one that is open. The
// handshake is given the time limit of the settings; the connection, once
// open, has none.
func (w *Window) toggleWS() {
	if w.socket != nil {
		w.disconnectWS()
		return
	}

	request := w.ws.request().Expand(w.expander())
	ctx, cancel := context.WithCancel(context.Background())
	s := &wsSession{ctx: ctx, cancel: cancel, start: time.Now()}
	s.entry = w.historyEntry(w.requestUI(collection.TypeWebSocket), request.URL)
	w.socket = s

	w.ws.connect.SetText("Cancel")
	w.wsResponse.clear()
	w.wsResponse.setFields(field{label: fieldCode, value: "Connecting"})

	limit := w.timeout()
	authorize := w.authorizer(w.requestUI(collection.TypeWebSocket), &request.Auth)
	go func() {
		// The handshake is given the time limit once there is a token,
		// which can take the user signing in.
		var err error
		if authorize != nil {
			err = authorize(ctx)
		}
		var conn *ws.Conn
		if err == nil {
			dial, stop := withLimit(ctx, limit)
			conn, err = ws.Dial(dial, w.client, request)
			stop()
		}
		err = network.Explain(err)
		mainthread.Start(func() {
			if w.socket != s {
				// It was closed, or another request shown, meanwhile.
				if conn != nil {
					go conn.Close()
				}
				return
			}
			if err != nil {
				w.socket = nil
				cancel()
				w.ws.setConnected(false)
				if errors.Is(err, context.DeadlineExceeded) {
					err = timeoutError(w.config.Timeout)
				}
				w.wsResponse.setFields(
					field{fieldCode, "Not connected", true},
					field{fieldError, err.Error(), true},
				)
				if s.entry != nil {
					s.entry.Duration = time.Since(s.start)
					s.entry.Error = err.Error()
					w.history.add(s.entry)
				}
				return
			}

			s.conn = conn
			w.ws.setConnected(true)
			w.wsResponse.setHeaders(headerRows(conn.Headers, ""))
			w.showWSLog(s)
			go w.readWS(s)
		})
	}()
}

// connectAndSendWS sends the message of the WebSocket request on show over
// its connection, which is opened first if it is not, as Ctrl+Return does.
func (w *Window) connectAndSendWS() {
	if w.socket == nil {
		w.toggleWS()
	} else {
		w.sendWS()
	}
}

// readWS receives the messages of a connection until it closes, showing each
// as it comes.
func (w *Window) readWS(s *wsSession) {
	for {
		m, err := s.conn.Receive(s.ctx)
		mainthread.Start(func() {
			if w.socket != s {
				return
			}
			if err != nil {
				w.closedWS(s, err)
				return
			}
			s.messages = append(s.messages, m)
			s.received++
			w.showWSLog(s)
		})
		if err != nil {
			return
		}
	}
}

// sendWS sends the message of the request, with the active environment
// applied to it, over the open connection.
func (w *Window) sendWS() {
	s := w.socket
	if s == nil || s.conn == nil {
		return
	}
	text := w.expander()(w.ws.message.ToPlainText())
	go func() {
		m, err := s.conn.Send(s.ctx, text)
		mainthread.Start(func() {
			if w.socket != s {
				return
			}
			if err != nil {
				w.closedWS(s, err)
				return
			}
			s.messages = append(s.messages, m)
			s.sent++
			w.showWSLog(s)
		})
	}()
}

// disconnectWS closes the open connection, if there is one, leaving what
// went over it on show.
func (w *Window) disconnectWS() {
	s := w.socket
	if s == nil {
		return
	}
	w.socket = nil
	if s.conn != nil {
		// Closing waits for the server to agree, which is not waited for.
		go func() {
			s.conn.Close()
			s.cancel()
		}()
	} else {
		s.cancel()
	}
	w.ws.setConnected(false)
	status := field{fieldCode, "Disconnected", false}
	w.showWSFields(s, status, field{})
	// A connection given up before it was open is not kept.
	if s.conn != nil {
		w.recordWS(s, status, field{})
	}
}

// closedWS shows that the connection ended by itself: the close code and
// reason the server gave, as a failure unless the connection was closed
// normally, or the error that ended it.
func (w *Window) closedWS(s *wsSession, err error) {
	w.socket = nil
	s.cancel()
	w.ws.setConnected(false)

	code, reason := ws.Closed(err)
	status := field{fieldCode, "Closed", true}
	if code >= 0 {
		status = field{fieldCode, strings.TrimSpace(fmt.Sprintf("%d %s", code, ws.CodeName(code))), !ws.Normal(code)}
	}
	problem := field{fieldError, reason, true}
	w.showWSFields(s, status, problem)
	w.recordWS(s, status, problem)
}

// recordWS keeps a connection that has closed in the history: what went
// over it, and how it ended.
func (w *Window) recordWS(s *wsSession, status, problem field) {
	if s.entry == nil {
		return
	}
	record := &history.WebSocket{
		Messages: slices.Clone(s.messages),
		Status:   status.value,
		Failed:   status.failure,
		Reason:   problem.value,
	}
	if s.conn != nil {
		record.Headers, record.Handshake = s.conn.Headers, s.conn.Duration
	}
	s.entry.Duration = time.Since(s.start)
	s.entry.WebSocket = record
	w.history.add(s.entry)
	s.entry = nil
}

// showWSRecord shows a connection kept in the history as it was when it
// closed.
func (w *Window) showWSRecord(record *history.WebSocket) {
	w.showWSMessages(record.Messages)
	w.wsResponse.setHeaders(headerRows(record.Headers, ""))
	sent, received := record.Counts()
	w.wsResponse.setFields(wsFields(field{fieldCode, record.Status, record.Failed}, field{fieldError, record.Reason, true}, sent, received, record.Handshake)...)
}

// showWSLog shows the messages that have gone over a connection, oldest
// first, each under a line saying which way it went and when, and keeps them
// to be saved to a file.
func (w *Window) showWSLog(s *wsSession) {
	w.showWSMessages(s.messages)
	w.showWSFields(s, field{fieldCode, "Connected", false}, field{})
}

// showWSMessages shows messages that have gone over a connection, oldest
// first, each under a line saying which way it went and when, and keeps them
// to be saved to a file.
func (w *Window) showWSMessages(messages []ws.Message) {
	log := ws.Log(messages)
	r := w.wsResponse
	r.setBody(log, false)
	r.setContent([]byte(log), "messages.txt")
	// The newest message is the one wanted.
	scrollToEnd(r.body)
}

// showWSFields fills the bar beneath the messages: the state of the
// connection, any error, how many messages went each way and how long the
// handshake took.
func (w *Window) showWSFields(s *wsSession, status, problem field) {
	var handshake time.Duration
	if s.conn != nil {
		handshake = s.conn.Duration
	}
	w.wsResponse.setFields(wsFields(status, problem, s.sent, s.received, handshake)...)
}

// wsFields returns the fields of the bar beneath the messages of a
// connection. The time of the handshake is left out if it is zero, as it is
// until the handshake is done.
func wsFields(status, problem field, sent, received int, handshake time.Duration) []field {
	fields := []field{status, problem, {label: "Messages", value: fmt.Sprintf("%d sent, %d received", sent, received)}}
	if handshake > 0 {
		fields = append(fields, field{label: "Handshake", value: formatDuration(handshake)})
	}
	return fields
}

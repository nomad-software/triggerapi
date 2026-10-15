package ui

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/mappu/miqt/qt6/mainthread"

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
	s := &wsSession{ctx: ctx, cancel: cancel}
	w.socket = s

	w.ws.connect.SetText("Cancel")
	w.wsResponse.clear()
	w.wsResponse.setFields(field{label: fieldCode, value: "Connecting"})

	limit := w.timeout()
	go func() {
		dial, stop := ctx, context.CancelFunc(func() {})
		if limit > 0 {
			dial, stop = context.WithTimeout(ctx, limit)
		}
		conn, err := ws.Dial(dial, w.client, request)
		stop()
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
					err = fmt.Errorf("timed out after %s, which is the limit in the settings", plural(w.config.Timeout, "second"))
				}
				w.wsResponse.setFields(
					field{fieldCode, "Not connected", true},
					field{fieldError, err.Error(), true},
				)
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
	w.showWSFields(s, field{fieldCode, "Disconnected", false}, field{})
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
	w.showWSFields(s, status, field{fieldError, reason, true})
}

// showWSLog shows the messages that have gone over a connection, oldest
// first, each under a line saying which way it went and when, and keeps them
// to be saved to a file.
func (w *Window) showWSLog(s *wsSession) {
	var log strings.Builder
	for i, m := range s.messages {
		if i > 0 {
			log.WriteString("\n\n")
		}
		way := "← Received"
		if m.Sent {
			way = "→ Sent"
		}
		fmt.Fprintf(&log, "%s %s\n%s", way, m.At.Format("15:04:05.000"), m.Text)
	}

	r := w.wsResponse
	r.setBody(log.String(), false)
	r.setContent([]byte(log.String()), "messages.txt")
	// The newest message is the one wanted.
	bar := r.body.VerticalScrollBar()
	bar.SetValue(bar.Maximum())

	w.showWSFields(s, field{fieldCode, "Connected", false}, field{})
}

// showWSFields fills the bar beneath the messages: the state of the
// connection, any error, how many messages went each way and how long the
// handshake took.
func (w *Window) showWSFields(s *wsSession, status, problem field) {
	fields := []field{status, problem, {label: "Messages", value: fmt.Sprintf("%d sent, %d received", s.sent, s.received)}}
	if s.conn != nil {
		fields = append(fields, field{label: "Handshake", value: formatDuration(s.conn.Duration)})
	}
	w.wsResponse.setFields(fields...)
}

// Package ws is the WebSocket engine: it opens a connection to a server,
// sends text messages over it and receives what the server sends. It has no
// dependency on the UI.
package ws

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coder/websocket"

	"github.com/nomad-software/triggerapi/internal/rest"
)

// readLimit is the largest message that is received, which is far above the
// library's default so that large documents are not refused.
const readLimit = 64 << 20

// Request is a saved WebSocket request: where to connect, the headers and
// authorization of the handshake, and the message to send once connected.
type Request struct {
	// URL is the address of the server. A ws:// or wss:// scheme is usual,
	// http:// and https:// are taken as the same, and ws:// is assumed
	// when there is none.
	URL     string          `json:"url"`
	Headers []rest.KeyValue `json:"headers,omitempty"`
	Auth    rest.Auth       `json:"auth"`
	// Message is the text to send.
	Message string `json:"message,omitempty"`
}

// Expand applies f to every part of the request that can hold a placeholder.
func (r Request) Expand(f func(string) string) Request {
	r.URL = f(r.URL)
	r.Headers = rest.ExpandPairs(r.Headers, f)
	r.Auth = r.Auth.Expand(f)
	r.Message = f(r.Message)
	return r
}

// Target returns the address to connect to, with a ws or wss scheme.
func (r Request) Target() (string, error) {
	raw := strings.TrimSpace(r.URL)
	if raw == "" {
		return "", errors.New("URL is empty")
	}
	if !strings.Contains(raw, "://") {
		raw = "ws://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	switch strings.ToLower(u.Scheme) {
	case "ws", "http":
		u.Scheme = "ws"
	case "wss", "https":
		u.Scheme = "wss"
	default:
		return "", fmt.Errorf("unsupported scheme %q", u.Scheme)
	}
	if u.Host == "" {
		return "", errors.New("URL has no host")
	}
	return u.String(), nil
}

// Header returns the headers of the handshake: the authorization, and the
// enabled headers, which take precedence over it. A Host header sets the
// host that is asked for, and is returned apart.
func (r Request) Header() (header http.Header, host string) {
	header = http.Header{}
	if value, ok := r.Auth.Header(); ok {
		header.Set("Authorization", value)
	}
	seen := map[string]bool{}
	for _, h := range r.Headers {
		if !h.Enabled || h.Key == "" {
			continue
		}
		key := http.CanonicalHeaderKey(h.Key)
		if key == "Host" {
			host = h.Value
			continue
		}
		if !seen[key] {
			header.Del(key)
			seen[key] = true
		}
		header.Add(key, h.Value)
	}
	return header, host
}

// Message is a message sent or received over a connection.
type Message struct {
	// Sent is set for a message that was sent, and not received.
	Sent bool
	// Text is the message, which for a binary one is a description of it.
	Text   string
	Binary bool
	At     time.Time
}

// Conn is an open connection.
type Conn struct {
	conn *websocket.Conn
	// Headers are those of the server's answer to the handshake.
	Headers http.Header
	// Duration is how long the handshake took.
	Duration time.Duration
}

// Dial opens a connection to the server of a request, which ctx limits the
// handshake of and no more. The error of a handshake that the server refuses
// has its status in it.
func Dial(ctx context.Context, client *http.Client, r Request) (*Conn, error) {
	target, err := r.Target()
	if err != nil {
		return nil, err
	}
	header, host := r.Header()

	start := time.Now()
	conn, res, err := websocket.Dial(ctx, target, &websocket.DialOptions{HTTPClient: client, HTTPHeader: header, Host: host})
	if err != nil {
		if res != nil && res.StatusCode != http.StatusSwitchingProtocols {
			return nil, fmt.Errorf("the server answered %s: %w", res.Status, err)
		}
		return nil, err
	}
	conn.SetReadLimit(readLimit)
	return &Conn{conn: conn, Headers: res.Header, Duration: time.Since(start)}, nil
}

// Send sends a text message.
func (c *Conn) Send(ctx context.Context, text string) (Message, error) {
	if err := c.conn.Write(ctx, websocket.MessageText, []byte(text)); err != nil {
		return Message{}, err
	}
	return Message{Sent: true, Text: text, At: time.Now()}, nil
}

// Receive waits for the next message from the server. It returns an error
// once the connection has closed, which Closed describes.
func (c *Conn) Receive(ctx context.Context) (Message, error) {
	typ, data, err := c.conn.Read(ctx)
	if err != nil {
		return Message{}, err
	}
	m := Message{Text: string(data), At: time.Now()}
	if typ == websocket.MessageBinary {
		m.Binary = true
		m.Text = fmt.Sprintf("(binary, %d bytes)", len(data))
	}
	return m, nil
}

// Close closes the connection normally.
func (c *Conn) Close() error {
	return c.conn.Close(websocket.StatusNormalClosure, "")
}

// Closed describes how a connection ended, from the error Receive returned:
// the close code and reason the server gave, or else the error itself. The
// code is -1 if there was none.
func Closed(err error) (code int, reason string) {
	var closeErr websocket.CloseError
	if errors.As(err, &closeErr) {
		return int(closeErr.Code), closeErr.Reason
	}
	return -1, err.Error()
}

// Normal reports whether a close code is that of a connection closed
// normally, or because the other side went away.
func Normal(code int) bool {
	return code == int(websocket.StatusNormalClosure) || code == int(websocket.StatusGoingAway)
}

// CodeName returns the name of a close code, such as NORMAL_CLOSURE, or ""
// for one that has none.
func CodeName(code int) string {
	names := map[int]string{
		1000: "NORMAL_CLOSURE",
		1001: "GOING_AWAY",
		1002: "PROTOCOL_ERROR",
		1003: "UNSUPPORTED_DATA",
		1005: "NO_STATUS",
		1006: "ABNORMAL_CLOSURE",
		1007: "INVALID_PAYLOAD",
		1008: "POLICY_VIOLATION",
		1009: "MESSAGE_TOO_BIG",
		1010: "MANDATORY_EXTENSION",
		1011: "INTERNAL_ERROR",
		1012: "SERVICE_RESTART",
		1013: "TRY_AGAIN_LATER",
		1014: "BAD_GATEWAY",
		1015: "TLS_HANDSHAKE",
	}
	return names[code]
}

package ws

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/nomad-software/triggerapi/internal/rest"
)

// echoServer accepts connections which send back each message, with the
// Authorization and X-Test headers of the handshake as the first message.
func echoServer(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/refuse" {
			http.Error(w, "no", http.StatusForbidden)
			return
		}
		w.Header().Set("X-Server", "echo")
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		ctx := r.Context()
		conn.Write(ctx, websocket.MessageText, []byte(r.Header.Get("Authorization")+"|"+r.Header.Get("X-Test")))
		for {
			typ, data, err := conn.Read(ctx)
			if err != nil {
				return
			}
			if string(data) == "bye" {
				conn.Close(websocket.StatusPolicyViolation, "told to go")
				return
			}
			conn.Write(ctx, typ, data)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestTarget(t *testing.T) {
	for raw, want := range map[string]string{
		"localhost:8080/socket":   "ws://localhost:8080/socket",
		"ws://example.test/a?b=c": "ws://example.test/a?b=c",
		"WSS://example.test":      "wss://example.test",
		"http://example.test":     "ws://example.test",
		"https://example.test/x":  "wss://example.test/x",
	} {
		if got, err := (Request{URL: raw}).Target(); err != nil || got != want {
			t.Errorf("Target(%q) = %q, %v, want %q", raw, got, err, want)
		}
	}
	for _, raw := range []string{"", "  ", "ftp://example.test", "ws://"} {
		if _, err := (Request{URL: raw}).Target(); err == nil {
			t.Errorf("Target(%q) has no error", raw)
		}
	}
}

func TestExchange(t *testing.T) {
	srv := echoServer(t)
	r := Request{
		URL:     strings.Replace(srv.URL, "http://", "ws://", 1) + "/{{path}}",
		Headers: []rest.KeyValue{{Key: "X-Test", Value: "{{value}}", Enabled: true}, {Key: "X-Off", Value: "no", Enabled: false}},
		Auth:    rest.Auth{Type: rest.AuthBearer, Token: "{{token}}"},
		Message: "hello {{value}}",
	}
	vars := map[string]string{"{{path}}": "socket", "{{value}}": "v", "{{token}}": "t"}
	r = r.Expand(func(s string) string {
		for k, v := range vars {
			s = strings.ReplaceAll(s, k, v)
		}
		return s
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := Dial(ctx, nil, r)
	if err != nil {
		t.Fatal(err)
	}
	if conn.Headers.Get("X-Server") != "echo" {
		t.Errorf("headers %v", conn.Headers)
	}

	// The handshake carried the authorization and the enabled headers.
	if m, err := conn.Receive(ctx); err != nil || m.Text != "Bearer t|v" || m.Sent {
		t.Errorf("first message %+v, %v", m, err)
	}

	sent, err := conn.Send(ctx, r.Message)
	if err != nil || !sent.Sent || sent.Text != "hello v" {
		t.Fatalf("sent %+v, %v", sent, err)
	}
	if m, err := conn.Receive(ctx); err != nil || m.Text != "hello v" {
		t.Errorf("echo %+v, %v", m, err)
	}

	// How the server closed the connection is reported.
	conn.Send(ctx, "bye")
	_, err = conn.Receive(ctx)
	if code, reason := Closed(err); code != 1008 || reason != "told to go" || Normal(code) || CodeName(code) != "POLICY_VIOLATION" {
		t.Errorf("closed with %d %q", code, reason)
	}
}

func TestRefused(t *testing.T) {
	srv := echoServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := Dial(ctx, nil, Request{URL: srv.URL + "/refuse"})
	if err == nil || !strings.Contains(err.Error(), "403") {
		t.Errorf("error %v", err)
	}
}

func TestCloseNormally(t *testing.T) {
	srv := echoServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := Dial(ctx, nil, Request{URL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	conn.Receive(ctx)
	done := make(chan error, 1)
	go func() {
		_, err := conn.Receive(ctx)
		done <- err
	}()
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	if code, _ := Closed(<-done); !Normal(code) && code != -1 {
		t.Errorf("closed with %d", code)
	}
}

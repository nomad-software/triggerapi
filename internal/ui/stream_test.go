package ui

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mappu/miqt/qt6/mainthread"

	"github.com/nomad-software/triggerapi/internal/capture"
	"github.com/nomad-software/triggerapi/internal/env"
	"github.com/nomad-software/triggerapi/internal/rest"
)

// streamServer serves streams of server-sent events: /events sends two,
// waits for release to be closed and sends a third, and /forever sends one
// and stays open until the request is given up.
func streamServer(t *testing.T, release chan struct{}) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		send := func(n int) {
			fmt.Fprintf(w, "event: tick\nid: %d\ndata: {\"n\": %d}\n\n", n, n)
			flusher.Flush()
		}
		send(1)
		if r.URL.Path == "/forever" {
			<-r.Context().Done()
			return
		}
		send(2)
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		send(3)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestStream(t *testing.T) {
	release := make(chan struct{})
	srv := streamServer(t, release)
	files := testFiles(t)
	envs := &env.Store{Active: 0, Environments: []env.Environment{{Name: "Local"}}}
	if err := envs.Save(files.Environments); err != nil {
		t.Fatal(err)
	}

	var w *Window
	field := func(label string) string { return fieldValue(w.response, label) }
	body := func() string { return w.response.body.ToPlainText() }
	mainthread.Wait(func() {
		w = NewWindow(files.Files)
		w.Show()
		w.request.load(rest.Request{Method: "GET", URL: srv.URL + "/events"})
		setCaptureRules(w.request.captures, []capture.Rule{{Variable: "n", Path: "n", Enabled: true}})
		w.request.send.Click()
	})

	// The events are shown as they come, while the stream is open.
	waitFor(t, "the first events", func() bool { return strings.Contains(body(), `{"n": 2}`) })
	mainthread.Wait(func() {
		if field("Stream") != streamOpen || field(fieldCode) != "200 OK" || field("Events") != "2" || w.request.send.Text() != "Cancel" {
			t.Errorf("stream %q, status %q, events %q, button %q", field("Stream"), field(fieldCode), field("Events"), w.request.send.Text())
		}
		if !strings.HasPrefix(body(), "← +") || !strings.Contains(body(), "tick  id 1\n{\"n\": 1}\n\n← ") {
			t.Errorf("body %q", body())
		}
		if w.response.headers.RowCount() == 0 {
			t.Error("the headers are not shown while the stream is open")
		}
	})

	close(release)
	waitFor(t, "the end of the stream", func() bool { return field("Stream") == streamEnded })
	mainthread.Wait(func() {
		if field("Events") != "3" || !strings.HasSuffix(body(), `{"n": 3}`) || strings.Count(body(), "← ") != 3 {
			t.Errorf("events %q, body %q", field("Events"), body())
		}
		if field(fieldError) != "" || field("First event") == "" || w.request.send.Text() != "Send" {
			t.Errorf("error %q, first event %q, button %q", field(fieldError), field("First event"), w.request.send.Text())
		}
		// Values are captured from the last event, and the stream is
		// saved as it came.
		if value := w.expand("{{n}}"); value != "3" {
			t.Errorf("captured %q", value)
		}
		if !strings.Contains(string(w.response.content), "data: {\"n\": 3}") || !strings.HasSuffix(w.response.fileName, ".txt") {
			t.Errorf("content %q, file %q", w.response.content, w.response.fileName)
		}

		// It is kept in the history with its events.
		e := w.history.store.Entries[0]
		if e.HTTP == nil || len(e.HTTP.Events) != 3 || e.HTTP.Interrupted != "" {
			t.Errorf("entry %+v", e.HTTP)
		}
	})
}

// TestStreamStopped checks that a stream is not ended by the time limit of
// the settings, which is on its headers, and that Cancel stops it, keeping
// what had come.
func TestStreamStopped(t *testing.T) {
	srv := streamServer(t, nil)
	var w *Window
	field := func(label string) string { return fieldValue(w.response, label) }
	mainthread.Wait(func() {
		w = NewWindow(testFiles(t).Files)
		w.config.Timeout = 1
		w.Show()
		w.request.load(rest.Request{Method: "GET", URL: srv.URL + "/forever"})
		w.request.send.Click()
	})
	waitFor(t, "the event", func() bool { return field("Events") == "1" })
	time.Sleep(1200 * time.Millisecond)
	mainthread.Wait(func() {
		if field("Stream") != streamOpen {
			t.Errorf("stream %q after the time limit, error %q", field("Stream"), field(fieldError))
		}
		w.request.send.Click()
	})
	waitFor(t, "the stream to stop", func() bool { return field("Stream") == rest.StreamStopped })
	mainthread.Wait(func() {
		if field("Events") != "1" || field(fieldCode) != "200 OK" || !strings.Contains(w.response.body.ToPlainText(), `{"n": 1}`) {
			t.Errorf("events %q, status %q, body %q", field("Events"), field(fieldCode), w.response.body.ToPlainText())
		}
		if e := w.history.store.Entries[0]; e.HTTP == nil || e.HTTP.Interrupted != rest.StreamStopped {
			t.Errorf("entry %+v", e)
		}
	})
}

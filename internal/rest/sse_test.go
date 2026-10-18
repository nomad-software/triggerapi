package rest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestParseEvents(t *testing.T) {
	stream := "\uFEFF: a comment\n" +
		"data: first\n\n" +
		"event: delta\r\nid: 7\r\ndata: two\r\ndata:  lines\r\n\r\n" +
		"retry: 1000\n\n" +
		"data\n\n" +
		"id: 8\ndata: {\"n\": 3}\n\n" +
		"data: carries the id\n\n" +
		"data: never ended\n"
	want := []Event{
		{Data: "first"},
		{Type: "delta", ID: "7", Data: "two\n lines"},
		{ID: "7", Data: ""},
		{ID: "8", Data: `{"n": 3}`},
		{ID: "8", Data: "carries the id"},
	}
	if got := ParseEvents([]byte(stream)); !reflect.DeepEqual(got, want) {
		t.Errorf("ParseEvents = %#v, want %#v", got, want)
	}
}

func TestEventLog(t *testing.T) {
	events := []Event{
		{At: 1500 * time.Millisecond, Data: "hello"},
		{Type: "done", ID: "2", Data: "{}"},
	}
	want := "← +1.500 s  message\nhello\n\n← done  id 2\n{}"
	if got := EventLog(events); got != want {
		t.Errorf("EventLog = %q, want %q", got, want)
	}
}

// eventServer serves a stream of a number of events, flushing each, and
// then ends it, unless open is set, when it is kept open until the request
// is given up.
func eventServer(t *testing.T, count int, open bool) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
		flusher := w.(http.Flusher)
		for i := range count {
			fmt.Fprintf(w, "id: %d\ndata: {\"n\": %d}\n\n", i+1, i+1)
			flusher.Flush()
		}
		if open {
			<-r.Context().Done()
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestSendStream(t *testing.T) {
	srv := eventServer(t, 3, false)

	var mu sync.Mutex
	var seen []int
	req := Request{Method: http.MethodGet, URL: srv.URL}
	req.Options.Stream = func(res *Response) {
		mu.Lock()
		seen = append(seen, len(res.Events))
		mu.Unlock()
	}
	res, err := Send(context.Background(), srv.Client(), req)
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsEventStream() || len(res.Events) != 3 || res.Interrupted != "" {
		t.Fatalf("response %+v, want a stream of 3 events that ended", res)
	}
	if res.Events[2].ID != "3" || res.Events[2].At <= 0 || res.Document() != `{"n": 3}` {
		t.Errorf("last event %+v, document %q", res.Events[2], res.Document())
	}
	if !strings.Contains(string(res.Body), "id: 2\n") {
		t.Errorf("body %q, want the stream as it came", res.Body)
	}
	if want := []int{0, 1, 2, 3}; !reflect.DeepEqual(seen, want) {
		t.Errorf("stream given %v events, want %v", seen, want)
	}

	// The response is kept in the history with its events.
	data, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	var back Response
	if err := json.Unmarshal(data, &back); err != nil || len(back.Events) != 3 || back.Text() != res.Text() {
		t.Errorf("after JSON, %d events and text %q, want %q (%v)", len(back.Events), back.Text(), res.Text(), err)
	}
}

// TestSendStreamStopped checks that a stream which the server keeps open is
// not ended by the time limit, which is on its headers, and that what had
// come is kept when it is stopped.
func TestSendStreamStopped(t *testing.T) {
	srv := eventServer(t, 2, true)

	ctx, cancel := context.WithCancel(context.Background())
	req := Request{Method: http.MethodGet, URL: srv.URL}
	req.Options.Timeout = 50 * time.Millisecond
	req.Options.Stream = func(res *Response) {
		if len(res.Events) == 2 {
			// The stream outlives the time limit, and is then stopped.
			time.AfterFunc(200*time.Millisecond, cancel)
		}
	}
	res, err := Send(ctx, srv.Client(), req)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Events) != 2 || res.Interrupted != StreamStopped {
		t.Errorf("%d events, interrupted %q, want 2 and %q", len(res.Events), res.Interrupted, StreamStopped)
	}
	if res.Duration < 200*time.Millisecond {
		t.Errorf("duration %v, want the time until it was stopped", res.Duration)
	}
}

func TestSendTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	}))
	t.Cleanup(srv.Close)

	req := Request{Method: http.MethodGet, URL: srv.URL}
	req.Options.Timeout = 50 * time.Millisecond
	if _, err := Send(context.Background(), srv.Client(), req); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("error %v, want %v", err, context.DeadlineExceeded)
	}
}

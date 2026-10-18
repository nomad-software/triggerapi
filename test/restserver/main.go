// Command restserver is a small HTTP server to try the REST requests of
// TriggerAPI against by hand, above all streams of server-sent events, which
// TriggerAPI shows as they come. It logs every request it is given.
//
//	go run ./test/restserver                  # http://localhost:8001
//	go run ./test/restserver -addr :9000
//
// Its endpoints are listed at /. It is not part of the program, and is not
// used by the tests.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func main() {
	addr := flag.String("addr", "localhost:8001", "the address to listen on")
	flag.Parse()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", index)
	mux.HandleFunc("GET /events", events)
	mux.HandleFunc("GET /clock", clock)
	mux.HandleFunc("GET /broken", broken)
	mux.HandleFunc("GET /slow", slow)
	mux.HandleFunc("GET /json", plainJSON)
	mux.HandleFunc("POST /v1/chat/completions", chatCompletions)
	mux.HandleFunc("POST /v1/messages", messages)

	log.Printf("REST server at http://%s, with its endpoints listed at /", *addr)
	log.Fatal(http.ListenAndServe(*addr, logged(mux)))
}

// endpoints are what index lists.
const endpoints = `Endpoints of the sample REST server of TriggerAPI:

GET  /events?count=10&interval=500ms
     A stream of server-sent events, numbered, each with an id and every
     third of the type "tick", which ends after count events.
GET  /clock
     A stream that never ends: the time every second, with a comment
     between to keep it open. Stop it with Cancel.
GET  /broken?after=3
     A stream that the server breaks off after a few events.
GET  /slow?delay=5s
     Waits before answering, to try the time limit of the settings.
GET  /json
     An ordinary JSON response, to compare.
POST /v1/chat/completions
     Answers {"messages": [{"role": "user", "content": "..."}], "stream": true}
     word by word, as OpenAI's API streams, ending with data: [DONE]. Without
     "stream": true it answers with one JSON response.
POST /v1/messages
     Answers {"messages": [...], "stream": true} as Anthropic's Messages API
     streams, with an event type for each event.
`

func index(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	fmt.Fprint(w, endpoints)
}

// stream is a response of server-sent events.
type stream struct {
	w       http.ResponseWriter
	flusher http.Flusher
}

// newStream starts a stream of server-sent events.
func newStream(w http.ResponseWriter) *stream {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	s := &stream{w: w, flusher: w.(http.Flusher)}
	s.flusher.Flush()
	return s
}

// send sends an event, which has a type and an id if they are not empty,
// and is sent at once.
func (s *stream) send(event, id, data string) {
	if event != "" {
		fmt.Fprintf(s.w, "event: %s\n", event)
	}
	if id != "" {
		fmt.Fprintf(s.w, "id: %s\n", id)
	}
	for line := range strings.SplitSeq(data, "\n") {
		fmt.Fprintf(s.w, "data: %s\n", line)
	}
	fmt.Fprint(s.w, "\n")
	s.flusher.Flush()
}

// sendJSON sends an event whose data is a value as JSON.
func (s *stream) sendJSON(event, id string, value any) {
	data, _ := json.Marshal(value)
	s.send(event, id, string(data))
}

// comment sends a comment, which says nothing but keeps the stream open.
func (s *stream) comment(text string) {
	fmt.Fprintf(s.w, ": %s\n\n", text)
	s.flusher.Flush()
}

// pause waits for a while, and reports false if the client has gone.
func pause(r *http.Request, d time.Duration) bool {
	select {
	case <-time.After(d):
		return true
	case <-r.Context().Done():
		log.Printf("%s %s: the client went", r.Method, r.URL.Path)
		return false
	}
}

// query returns a number of the query, or what it is if not given.
func query(r *http.Request, name string, otherwise int) int {
	if n, err := strconv.Atoi(r.URL.Query().Get(name)); err == nil && n >= 0 {
		return n
	}
	return otherwise
}

// queryDuration returns a duration of the query, or what it is if not given.
func queryDuration(r *http.Request, name string, otherwise time.Duration) time.Duration {
	if d, err := time.ParseDuration(r.URL.Query().Get(name)); err == nil && d >= 0 {
		return d
	}
	return otherwise
}

func events(w http.ResponseWriter, r *http.Request) {
	count := query(r, "count", 10)
	interval := queryDuration(r, "interval", 500*time.Millisecond)
	s := newStream(w)
	for n := 1; n <= count; n++ {
		if n > 1 && !pause(r, interval) {
			return
		}
		event := ""
		if n%3 == 0 {
			event = "tick"
		}
		s.sendJSON(event, strconv.Itoa(n), map[string]any{"n": n, "of": count, "at": time.Now().Format(time.RFC3339Nano)})
	}
}

func clock(w http.ResponseWriter, r *http.Request) {
	s := newStream(w)
	for n := 1; ; n++ {
		s.sendJSON("time", strconv.Itoa(n), map[string]string{"time": time.Now().Format(time.TimeOnly)})
		if !pause(r, 500*time.Millisecond) {
			return
		}
		s.comment("keep-alive")
		if !pause(r, 500*time.Millisecond) {
			return
		}
	}
}

func broken(w http.ResponseWriter, r *http.Request) {
	after := query(r, "after", 3)
	s := newStream(w)
	for n := 1; n <= after; n++ {
		s.sendJSON("", strconv.Itoa(n), map[string]int{"n": n})
		if !pause(r, 300*time.Millisecond) {
			return
		}
	}
	// The connection is dropped without the stream being ended.
	panic(http.ErrAbortHandler)
}

func slow(w http.ResponseWriter, r *http.Request) {
	if !pause(r, queryDuration(r, "delay", 5*time.Second)) {
		return
	}
	plainJSON(w, r)
}

func plainJSON(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"ok": true, "time": time.Now().Format(time.RFC3339)})
}

// chat is the body of a request to either of the APIs of language models.
type chat struct {
	Model    string `json:"model"`
	Stream   bool   `json:"stream"`
	Messages []struct {
		Role    string `json:"role"`
		Content any    `json:"content"`
	} `json:"messages"`
}

// reply returns the words of the answer to a chat, which says back the last
// thing the user said.
func (c chat) reply() []string {
	said := "nothing"
	for _, m := range c.Messages {
		if text, ok := m.Content.(string); ok && m.Role == "user" {
			said = text
		}
	}
	return strings.Fields("You said: " + said + ". This answer comes a word at a time, as the answer of a language model does.")
}

// readChat reads the body of a request to a chat API, answering with an
// error if it cannot be read.
func readChat(w http.ResponseWriter, r *http.Request) (chat, bool) {
	var c chat
	if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
		http.Error(w, `{"error": "the body is not JSON"}`, http.StatusBadRequest)
		return c, false
	}
	if c.Model == "" {
		c.Model = "sample-model"
	}
	return c, true
}

func chatCompletions(w http.ResponseWriter, r *http.Request) {
	c, ok := readChat(w, r)
	if !ok {
		return
	}
	words := c.reply()
	if !c.Stream {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"id": "chatcmpl-1", "object": "chat.completion", "model": c.Model,
			"choices": []any{map[string]any{"index": 0, "message": map[string]string{"role": "assistant", "content": strings.Join(words, " ")}, "finish_reason": "stop"}},
		})
		return
	}

	s := newStream(w)
	chunk := func(delta map[string]string, finish any) map[string]any {
		return map[string]any{
			"id": "chatcmpl-1", "object": "chat.completion.chunk", "model": c.Model,
			"choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": finish}},
		}
	}
	s.sendJSON("", "", chunk(map[string]string{"role": "assistant"}, nil))
	for i, word := range words {
		if !pause(r, 80*time.Millisecond) {
			return
		}
		if i > 0 {
			word = " " + word
		}
		s.sendJSON("", "", chunk(map[string]string{"content": word}, nil))
	}
	s.sendJSON("", "", chunk(map[string]string{}, "stop"))
	s.send("", "", "[DONE]")
}

func messages(w http.ResponseWriter, r *http.Request) {
	c, ok := readChat(w, r)
	if !ok {
		return
	}
	words := c.reply()
	if !c.Stream {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"id": "msg_1", "type": "message", "role": "assistant", "model": c.Model,
			"content":     []any{map[string]string{"type": "text", "text": strings.Join(words, " ")}},
			"stop_reason": "end_turn",
		})
		return
	}

	s := newStream(w)
	s.sendJSON("message_start", "", map[string]any{"type": "message_start", "message": map[string]any{"id": "msg_1", "type": "message", "role": "assistant", "model": c.Model, "content": []any{}}})
	s.sendJSON("content_block_start", "", map[string]any{"type": "content_block_start", "index": 0, "content_block": map[string]string{"type": "text", "text": ""}})
	s.sendJSON("ping", "", map[string]string{"type": "ping"})
	for i, word := range words {
		if !pause(r, 80*time.Millisecond) {
			return
		}
		if i > 0 {
			word = " " + word
		}
		s.sendJSON("content_block_delta", "", map[string]any{"type": "content_block_delta", "index": 0, "delta": map[string]string{"type": "text_delta", "text": word}})
	}
	s.sendJSON("content_block_stop", "", map[string]any{"type": "content_block_stop", "index": 0})
	s.sendJSON("message_delta", "", map[string]any{"type": "message_delta", "delta": map[string]string{"stop_reason": "end_turn"}, "usage": map[string]int{"output_tokens": len(words)}})
	s.sendJSON("message_stop", "", map[string]string{"type": "message_stop"})
}

// logged logs each request, and how long it took.
func logged(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		log.Printf("%s %s", r.Method, r.URL.RequestURI())
		defer func() {
			log.Printf("%s %s done after %s", r.Method, r.URL.Path, time.Since(start).Round(time.Millisecond))
		}()
		next.ServeHTTP(w, r)
	})
}

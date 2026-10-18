package history

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"

	"github.com/nomad-software/triggerapi/internal/collection"
	"github.com/nomad-software/triggerapi/internal/crypt"
	"github.com/nomad-software/triggerapi/internal/crypt/crypttest"
	"github.com/nomad-software/triggerapi/internal/graphql"
	"github.com/nomad-software/triggerapi/internal/mcp"
	"github.com/nomad-software/triggerapi/internal/rest"
	"github.com/nomad-software/triggerapi/internal/rpc"
	"github.com/nomad-software/triggerapi/internal/ws"
)

func testKeys(t *testing.T) *crypt.Keys {
	t.Helper()
	public, private := crypttest.Pair(t, t.TempDir(), "id", "")
	keys, err := crypt.Open(public, private, nil)
	if err != nil {
		t.Fatal(err)
	}
	return keys
}

func restEntry(at time.Time, name, url string, status int, body string) *Entry {
	return &Entry{
		Time:    at,
		Request: collection.NewREST(name, rest.Request{Method: http.MethodGet, URL: url}),
		URL:     url,
		HTTP:    &rest.Response{Status: http.StatusText(status), StatusCode: status, Body: []byte(body)},
	}
}

func TestSaveAndLoad(t *testing.T) {
	keys := testKeys(t)
	path := filepath.Join(t.TempDir(), "history.json")
	at := time.Date(2026, 10, 17, 9, 30, 0, 0, time.UTC)

	store := &Store{}
	store.Add(restEntry(at, "List rooms", "https://hotel.test/rooms", 200, `{"rooms":[]}`), 10)
	grpc := &Entry{
		Time:    at.Add(time.Minute),
		Request: collection.NewGRPC("", rpc.Request{URL: "localhost:9000", Method: "echo.Echo/Say"}),
		URL:     "localhost:9000",
		Path:    []string{"Echo", "Say"},
		GRPC:    &rpc.Response{Code: 5, CodeName: "NOT_FOUND", Messages: []string{"{}"}, Trailers: http.Header{"X-T": {"1"}}},
	}
	store.Add(grpc, 10)
	socket := &Entry{
		Time:      at.Add(2 * time.Minute),
		Request:   collection.NewWebSocket("Feed", ws.Request{URL: "ws://feed.test"}),
		WebSocket: &WebSocket{Status: "Disconnected", Messages: []ws.Message{{Sent: true, Text: "hi", At: at}, {Text: "hello", At: at}}},
	}
	store.Add(socket, 10)
	called := &Entry{
		Time:        at.Add(3 * time.Minute),
		Request:     collection.NewMCP("", mcp.Request{URL: "http://tools.test/mcp", Name: "add"}),
		Environment: "Local",
		MCP:         &mcp.Response{Error: &jsonrpc.Error{Code: jsonrpc.CodeInvalidParams, Message: "bad"}},
	}
	store.Add(called, 10)

	if err := store.SaveWith(path, keys); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(path); !crypt.Encrypted(data) {
		t.Error("the history was not encrypted")
	}
	loaded, err := LoadWith(path, keys)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Entries) != 4 {
		t.Fatalf("%d entries", len(loaded.Entries))
	}
	// Newest first, with everything kept.
	for i, want := range []*Entry{called, socket, grpc, store.Entries[3]} {
		got := loaded.Entries[i]
		if !got.Time.Equal(want.Time) || got.Request.Type != want.Request.Type || got.Title() != want.Title() {
			t.Errorf("entry %d = %s %s", i, got.Request.Type, got.Title())
		}
	}
	if got := loaded.Entries[2]; !reflect.DeepEqual(got.GRPC, grpc.GRPC) || !reflect.DeepEqual(got.Path, grpc.Path) {
		t.Errorf("gRPC entry = %+v at %q", got.GRPC, got.Path)
	}
	if got := loaded.Entries[1].WebSocket; got.Status != "Disconnected" || len(got.Messages) != 2 || !got.Messages[0].Sent || got.Messages[1].Text != "hello" {
		t.Errorf("WebSocket entry = %+v", got)
	}
	if got := loaded.Entries[0]; got.Environment != "Local" || got.MCP.Error == nil || got.MCP.Error.Message != "bad" {
		t.Errorf("MCP entry = %+v", got)
	}
	if got := string(loaded.Entries[3].HTTP.Body); got != `{"rooms":[]}` {
		t.Errorf("body = %s", got)
	}
	// The request of each holds exactly the field for its type.
	if r := loaded.Entries[2].Request; r.Request != nil || r.GRPC == nil || r.GRPC.Method != "echo.Echo/Say" {
		t.Errorf("request = %+v", r)
	}
}

func TestLoadMissingAndUnreadable(t *testing.T) {
	keys := testKeys(t)
	dir := t.TempDir()
	store, err := LoadWith(filepath.Join(dir, "none.json"), keys)
	if err != nil || len(store.Entries) != 0 {
		t.Errorf("missing file = %+v, %v", store, err)
	}

	bad := filepath.Join(dir, "bad.json")
	os.WriteFile(bad, []byte("not json"), 0o600)
	if store, err := LoadWith(bad, keys); err == nil || store == nil || len(store.Entries) != 0 {
		t.Errorf("bad file = %+v, %v", store, err)
	}

	// Entries without a request are left out, and the rest put in order.
	odd := filepath.Join(dir, "odd.json")
	os.WriteFile(odd, []byte(`{"entries": [
		{"time": "2026-10-16T10:00:00Z", "request": {"name": "Old", "type": "rest", "method": "GET", "url": "a"}},
		{"time": "2026-10-17T10:00:00Z"},
		null,
		{"time": "2026-10-17T11:00:00Z", "request": {"name": "New", "type": "grpc"}}
	]}`), 0o600)
	store, err = LoadWith(odd, keys)
	if err != nil {
		t.Fatal(err)
	}
	if len(store.Entries) != 2 || store.Entries[0].Title() != "New" || store.Entries[1].Title() != "Old" || store.Entries[0].Request.GRPC == nil {
		t.Errorf("entries = %+v", store.Entries)
	}
}

func TestKeep(t *testing.T) {
	at := time.Now()
	store := &Store{}
	for i := range 5 {
		store.Add(restEntry(at.Add(time.Duration(i)*time.Second), "", "u", 200, "{}"), 3)
	}
	if len(store.Entries) != 3 || !store.Entries[0].Time.Equal(at.Add(4*time.Second)) {
		t.Errorf("kept %d, newest %v", len(store.Entries), store.Entries[0].Time)
	}
	store.Keep(1)
	if len(store.Entries) != 1 {
		t.Errorf("kept %d", len(store.Entries))
	}
	// There is always room for one.
	store.Keep(0)
	if len(store.Entries) != 1 {
		t.Errorf("kept %d", len(store.Entries))
	}

	// The oldest are dropped to keep the bodies within MaxTotal.
	store = &Store{}
	big := strings.Repeat("x", MaxBody)
	for range MaxTotal/MaxBody + 3 {
		store.Add(restEntry(at, "", "u", 200, big), 1000)
	}
	if len(store.Entries) != MaxTotal/MaxBody {
		t.Errorf("kept %d of the largest", len(store.Entries))
	}
}

func TestTrim(t *testing.T) {
	res := &rest.Response{StatusCode: 200, Body: []byte(strings.Repeat("x", MaxBody+1))}
	e := &Entry{Request: collection.New(collection.TypeREST, ""), HTTP: res}
	store := &Store{}
	store.Add(e, 10)
	if e.HTTP.Body != nil || e.Omitted != MaxBody+1 || e.HTTP.StatusCode != 200 {
		t.Errorf("entry = %+v, omitted %d", e.HTTP, e.Omitted)
	}
	// What was recorded is left as it was, as it may still be on show.
	if len(res.Body) != MaxBody+1 {
		t.Error("the response was changed")
	}

	small := restEntry(time.Now(), "", "u", 200, strings.Repeat("x", MaxBody))
	store.Add(small, 10)
	if len(small.HTTP.Body) != MaxBody || small.Omitted != 0 {
		t.Errorf("a body of MaxBody was left out")
	}

	messages := &Entry{Request: collection.New(collection.TypeGRPC, ""), GRPC: &rpc.Response{Messages: []string{strings.Repeat("x", MaxBody), "{}"}}}
	store.Add(messages, 10)
	if messages.GRPC.Messages != nil || messages.Omitted != MaxBody+2 {
		t.Errorf("messages = %d, omitted %d", len(messages.GRPC.Messages), messages.Omitted)
	}
}

func TestRemove(t *testing.T) {
	store := &Store{}
	a := restEntry(time.Now(), "a", "u", 200, "")
	b := restEntry(time.Now(), "b", "u", 200, "")
	c := restEntry(time.Now(), "c", "u", 200, "")
	for _, e := range []*Entry{a, b, c} {
		store.Add(e, 10)
	}
	store.Remove(func(e *Entry) bool { return e == a || e == c })
	if len(store.Entries) != 1 || store.Entries[0] != b {
		t.Errorf("entries = %v", store.Entries)
	}
}

func TestStatus(t *testing.T) {
	graphqlRequest := collection.NewGraphQL("", graphql.Request{})
	errorsBody := []byte(`{"errors":[{"message":"no"}]}`)
	toolError, _ := json.Marshal(map[string]any{"isError": true, "content": []map[string]string{{"type": "text", "text": "broken"}}})
	for _, test := range []struct {
		entry  *Entry
		status string
		failed bool
	}{
		{&Entry{Error: "refused"}, "Error", true},
		{restEntry(time.Now(), "", "", 200, ""), "200", false},
		{restEntry(time.Now(), "", "", 404, ""), "404", true},
		{&Entry{Request: graphqlRequest, HTTP: &rest.Response{StatusCode: 200, Body: errorsBody}}, "200", true},
		{&Entry{Request: collection.New(collection.TypeREST, ""), HTTP: &rest.Response{StatusCode: 200, Body: errorsBody}}, "200", false},
		{&Entry{GRPC: &rpc.Response{CodeName: "OK"}}, "OK", false},
		{&Entry{GRPC: &rpc.Response{Code: 5, CodeName: "NOT_FOUND"}}, "NOT_FOUND", true},
		{&Entry{MCP: &mcp.Response{Result: json.RawMessage(`{}`)}}, "OK", false},
		{&Entry{MCP: &mcp.Response{Result: toolError}}, "Tool error", true},
		{&Entry{MCP: &mcp.Response{Error: &jsonrpc.Error{Code: jsonrpc.CodeMethodNotFound}}}, "-32601 METHOD_NOT_FOUND", true},
		{&Entry{WebSocket: &WebSocket{Status: "1008 policy violation", Failed: true}}, "1008 policy violation", true},
	} {
		if status, failed := test.entry.Status(); status != test.status || failed != test.failed {
			t.Errorf("%+v: status %q, %v, want %q, %v", test.entry, status, failed, test.status, test.failed)
		}
	}
}

func TestTitleAndMatches(t *testing.T) {
	for _, test := range []struct {
		entry *Entry
		title string
	}{
		{restEntry(time.Now(), "List rooms", "https://hotel.test/rooms", 200, ""), "List rooms"},
		{restEntry(time.Now(), " ", "https://hotel.test/rooms?page=2", 200, ""), "GET https://hotel.test/rooms?page=2"},
		{&Entry{Request: collection.NewGRPC("", rpc.Request{Method: "echo.Echo/Say"}), URL: "localhost:9000"}, "localhost:9000 echo.Echo/Say"},
		{&Entry{Request: collection.NewMCP("", mcp.Request{Name: "add"}), URL: "http://tools.test/mcp"}, "http://tools.test/mcp add"},
		{&Entry{Request: collection.NewWebSocket("", ws.Request{}), URL: "ws://feed.test"}, "ws://feed.test"},
	} {
		if got := test.entry.Title(); got != test.title {
			t.Errorf("title = %q, want %q", got, test.title)
		}
	}

	e := restEntry(time.Now(), "List rooms", "https://hotel.test/rooms", 200, "")
	for words, want := range map[string]bool{"list": true, "rooms hotel": true, "list missing": false, "": true} {
		if got := e.Matches(strings.Fields(words)); got != want {
			t.Errorf("Matches(%q) = %v", words, got)
		}
	}
}

func TestCounts(t *testing.T) {
	w := &WebSocket{Messages: []ws.Message{{Sent: true}, {}, {}, {Sent: true}, {}}}
	if sent, received := w.Counts(); sent != 2 || received != 3 {
		t.Errorf("counts = %d, %d", sent, received)
	}
}

func TestText(t *testing.T) {
	at := time.Date(2026, 10, 17, 9, 30, 0, 0, time.UTC)
	for _, test := range []struct {
		entry *Entry
		text  string
	}{
		{&Entry{Error: "refused"}, "refused"},
		{&Entry{HTTP: &rest.Response{Headers: http.Header{"Content-Type": {"application/json"}}, Body: []byte(`{"a":1}`)}}, "{\n  \"a\": 1\n}"},
		{&Entry{HTTP: &rest.Response{Body: []byte("plain")}}, "plain"},
		{&Entry{GRPC: &rpc.Response{Messages: []string{"{}", "[]"}}}, "{}\n\n[]"},
		{&Entry{MCP: &mcp.Response{Result: json.RawMessage(`{"b":2}`)}}, "{\n  \"b\": 2\n}"},
		{&Entry{MCP: &mcp.Response{Error: &jsonrpc.Error{Code: -1, Message: "no"}}}, "{\n  \"code\": -1,\n  \"message\": \"no\"\n}"},
		{&Entry{MCP: &mcp.Response{}}, ""},
		{&Entry{WebSocket: &WebSocket{Messages: []ws.Message{{Text: "x", At: at}}}}, "← Received 09:30:00.000\nx"},
		{&Entry{}, ""},
	} {
		if got := test.entry.Text(); got != test.text {
			t.Errorf("text = %q, want %q", got, test.text)
		}
	}
}

// TestEvents checks that a history from before streams of events and the
// events of MCP calls loads as it did, and that their events are kept, and
// counted with the body against MaxBody.
func TestEvents(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.json")
	old := `{"entries": [
		{"time": "2026-10-01T10:00:00Z", "request": {"name": "a", "type": "rest", "method": "GET", "url": "x", "auth": {}},
		 "http": {"status": "200 OK", "statusCode": 200, "headers": {"Content-Type": ["text/event-stream"]}, "body": "ZGF0YTogaGkKCg==", "duration": 5}},
		{"time": "2026-10-01T09:00:00Z", "request": {"name": "b", "type": "mcp", "mcp": {"url": "x", "auth": {}}},
		 "mcp": {"result": {"content": []}, "duration": 5}}
	]}`
	if err := os.WriteFile(path, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := LoadWith(path, nil)
	if err != nil || len(store.Entries) != 2 {
		t.Fatalf("%v, %d entries", err, len(store.Entries))
	}
	// The events of an old stream are read from its body.
	if e := store.Entries[0]; e.HTTP.Events != nil || e.HTTP.Interrupted != "" || e.Text() != "← message\nhi" {
		t.Errorf("an old stream has events %v, text %q", e.HTTP.Events, e.Text())
	}
	if e := store.Entries[1]; e.MCP.Events != nil || e.Text() != "{\n  \"content\": []\n}" {
		t.Errorf("an old call has events %v, text %q", e.MCP.Events, e.Text())
	}

	// Events are saved and read again.
	store.Entries[0].HTTP.Events = []rest.Event{{At: time.Second, Data: "hi"}}
	store.Entries[1].MCP.Events = []mcp.Event{{Kind: mcp.EventProgress, Progress: 1}}
	if err := store.SaveWith(path, nil); err != nil {
		t.Fatal(err)
	}
	again, err := LoadWith(path, nil)
	if err != nil || !reflect.DeepEqual(again.Entries[0].HTTP.Events, store.Entries[0].HTTP.Events) || !reflect.DeepEqual(again.Entries[1].MCP.Events, store.Entries[1].MCP.Events) {
		t.Errorf("events after loading %+v %+v, %v", again.Entries[0].HTTP, again.Entries[1].MCP, err)
	}

	// The events count towards the size of the body, and go with it.
	half := strings.Repeat("x", MaxBody/2+1)
	stream := &Entry{Request: collection.New(collection.TypeREST, ""), HTTP: &rest.Response{Body: []byte(half), Events: []rest.Event{{Data: half}}}}
	stream.trim()
	if stream.HTTP.Body != nil || stream.HTTP.Events != nil || stream.Omitted != 2*len(half) {
		t.Errorf("a stream over MaxBody kept %d bytes and %d events", len(stream.HTTP.Body), len(stream.HTTP.Events))
	}
	call := &Entry{Request: collection.New(collection.TypeMCP, ""), MCP: &mcp.Response{Result: []byte(`{}`), Events: []mcp.Event{{Kind: mcp.EventLogged, Message: strings.Repeat("x", MaxBody)}}}}
	call.trim()
	if call.MCP.Result != nil || call.MCP.Events != nil || call.Omitted != MaxBody+2 {
		t.Errorf("a call over MaxBody kept %s and %d events", call.MCP.Result, len(call.MCP.Events))
	}
}

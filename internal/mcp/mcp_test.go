package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nomad-software/triggerapi/internal/mcp/mcptest"
	"github.com/nomad-software/triggerapi/internal/rest"
)

func testClient(t *testing.T, r Request) *Client {
	t.Helper()
	c := NewClient(&http.Client{}, r, nil)
	t.Cleanup(func() { c.Close() })
	return c
}

// stateless is the version of the protocol from which servers are
// stateless, and sessions deprecated.
const stateless = "2026-07-28"

// servers are the kinds of server a client works with: those of now, which
// are stateless, and older ones, which give it a session.
var servers = []struct {
	name  string
	start func(*testing.T) *httptest.Server
	// legacy is set for a server from before the protocol was stateless.
	legacy bool
}{
	{"stateless", mcptest.NewServer, false},
	{"legacy", mcptest.NewLegacyServer, true},
}

// checkProtocol checks that a client agreed the protocol of the server it
// connected to: the stateless one, without a session, or one from before it,
// with a session.
func checkProtocol(t *testing.T, c *Client, protocol string, legacy bool) {
	t.Helper()
	session := c.current().ID()
	switch {
	case !legacy && (protocol != stateless || session != ""):
		t.Errorf("protocol %q, session %q, want %s and none", protocol, session, stateless)
	case legacy && (protocol == "" || protocol >= stateless || session == ""):
		t.Errorf("protocol %q, session %q, want one before %s with a session", protocol, session, stateless)
	}
}

func testContext(t *testing.T) context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func TestDiscover(t *testing.T) {
	for _, server := range servers {
		t.Run(server.name, func(t *testing.T) {
			srv := server.start(t)
			c := testClient(t, Request{URL: strings.TrimPrefix(srv.URL, "http://") + "/mcp"})
			testDiscover(t, c, server.legacy)
		})
	}
}

func testDiscover(t *testing.T, c *Client, legacy bool) {
	schema, err := c.Discover(testContext(t))
	if err != nil {
		t.Fatal(err)
	}
	checkProtocol(t, c, schema.Protocol, legacy)
	var names []string
	for _, item := range schema.Items {
		names = append(names, string(item.Kind)+" "+item.Name)
	}
	if strings.Join(names, ", ") != "tool echo, tool fail, resource test://greeting, prompt greet" {
		t.Errorf("items %q", names)
	}
	if schema.Server != "test-server 1.2.3" {
		t.Errorf("server %q", schema.Server)
	}

	echo, ok := schema.Find("", "echo")
	if !ok || echo.Description != "Says the text back." {
		t.Fatalf("echo %+v", echo)
	}
	example, err := echo.Example()
	var args map[string]any
	if err != nil || json.Unmarshal([]byte(example), &args) != nil || args["text"] != "" || args["times"] != float64(0) {
		t.Errorf("echo example %s, %v", example, err)
	}
	greet, _ := schema.Find(KindPrompt, "greet")
	if example, err := greet.Example(); err != nil || example != "{\n  \"who\": \"\"\n}" {
		t.Errorf("greet example %s, %v", example, err)
	}
	greeting, _ := schema.Find(KindResource, "test://greeting")
	if example, err := greeting.Example(); err != nil || example != "" {
		t.Errorf("resource example %q, %v", example, err)
	}
	if _, ok := schema.Find(KindTool, "greet"); ok {
		t.Error("a prompt was found as a tool")
	}
}

func TestCall(t *testing.T) {
	for _, server := range servers {
		t.Run(server.name, func(t *testing.T) {
			srv := server.start(t)
			testCall(t, Request{
				URL:     srv.URL + "/mcp",
				Headers: []rest.KeyValue{{Key: "X-Test", Value: "yes", Enabled: true}},
			}, server.legacy)
		})
	}
}

func testCall(t *testing.T, server Request, legacy bool) {
	c := testClient(t, server)
	ctx := testContext(t)
	call := func(kind Kind, name, arguments string) *Response {
		t.Helper()
		r := server
		r.Kind, r.Name, r.Arguments = kind, name, arguments
		res, err := c.Call(ctx, r)
		if err != nil {
			t.Fatalf("%s %s: %v", kind, name, err)
		}
		return res
	}

	// A tool is called with its arguments and the headers of the request.
	res := call("", "echo", `{"text": "ab", "times": 2}`)
	var result struct {
		StructuredContent EchoResult `json:"structuredContent"`
	}
	if err := json.Unmarshal(res.Result, &result); err != nil || result.StructuredContent != (EchoResult{Said: "abab", Header: "yes"}) || !res.OK() {
		t.Errorf("echo %s, %v", res.Result, err)
	}
	if !strings.HasPrefix(res.Status, "200") || res.Server != "test-server 1.2.3" {
		t.Errorf("status %q, server %q", res.Status, res.Server)
	}
	checkProtocol(t, c, res.Protocol, legacy)
	if status, problem, failed := res.Outcome(); status != "OK" || problem != "" || failed {
		t.Errorf("echo outcome %q, %q, %v", status, problem, failed)
	}

	// A tool's error is a result, but not OK.
	res = call(KindTool, "fail", "")
	if message, failed := res.ToolError(); !failed || message != "it went wrong" || res.OK() {
		t.Errorf("fail %s", res.Result)
	}
	if status, problem, failed := res.Outcome(); status != "Tool error" || problem != "it went wrong" || !failed {
		t.Errorf("fail outcome %q, %q, %v", status, problem, failed)
	}

	// A JSON-RPC error is a response too.
	res = call(KindTool, "none", "")
	if res.Error == nil || res.OK() {
		t.Errorf("unknown tool %+v %s", res.Error, res.Result)
	}
	if status, problem, failed := res.Outcome(); !strings.HasSuffix(status, CodeName(res.Error.Code)) || problem != res.Error.Message || !failed {
		t.Errorf("unknown tool outcome %q, %q, %v", status, problem, failed)
	}

	if res = call(KindResource, "test://greeting", "ignored"); !strings.Contains(string(res.Result), `"text":"hello"`) {
		t.Errorf("resource %s", res.Result)
	}
	if res = call(KindPrompt, "greet", `{"who": "Ada"}`); !strings.Contains(string(res.Result), "Hello Ada") {
		t.Errorf("prompt %s", res.Result)
	}

	// Arguments that are not an object are not sent.
	for _, arguments := range []string{"[1]", "{", `"a"`} {
		if _, err := c.Call(ctx, Request{URL: server.URL, Kind: KindTool, Name: "echo", Arguments: arguments}); err == nil || !strings.Contains(err.Error(), "JSON object") {
			t.Errorf("arguments %s: %v", arguments, err)
		}
	}
	if _, err := c.Call(ctx, Request{URL: server.URL}); err == nil {
		t.Error("nothing was called")
	}
}

// EchoResult is what the echo tool of mcptest gives back.
type EchoResult = mcptest.EchoResult

// An older server can end the session it gave, after which the client
// connects again.
func TestLegacySessionEnded(t *testing.T) {
	srv := mcptest.NewLegacyServer(t)
	c := testClient(t, Request{URL: srv.URL + "/mcp"})
	ctx := testContext(t)
	if _, err := c.Discover(ctx); err != nil {
		t.Fatal(err)
	}

	req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/mcp", nil)
	req.Header.Set("Mcp-Session-Id", c.current().ID())
	if res, err := http.DefaultClient.Do(req); err != nil {
		t.Fatal(err)
	} else {
		res.Body.Close()
	}
	if res, err := c.Call(ctx, Request{URL: srv.URL + "/mcp", Name: "echo", Arguments: `{"text": "x"}`}); err != nil || !res.OK() {
		t.Errorf("after the session ended: %v", err)
	}
}

func TestFailures(t *testing.T) {
	ctx := testContext(t)
	for _, r := range []Request{
		{URL: ""},
		{URL: "http://127.0.0.1:1/mcp"},
		{URL: "localhost/mcp", Transport: "stdio"},
	} {
		if _, err := testClient(t, r).Discover(ctx); err == nil {
			t.Errorf("%+v was connected to", r)
		}
	}
}

func TestExpandAndServer(t *testing.T) {
	f := func(s string) string { return strings.ReplaceAll(s, "{{x}}", "1") }
	r := Request{
		URL:       "h/{{x}}",
		Headers:   []rest.KeyValue{{Key: "A", Value: "{{x}}", Enabled: true}},
		Auth:      rest.Auth{Type: rest.AuthBearer, Token: "{{x}}"},
		Kind:      KindTool,
		Name:      "t{{x}}",
		Arguments: `{"a": {{x}}}`,
	}.Expand(f)
	if r.URL != "h/1" || r.Headers[0].Value != "1" || r.Auth.Token != "1" || r.Name != "t1" || r.Arguments != `{"a": 1}` {
		t.Errorf("%+v", r)
	}

	other := r
	other.Name, other.Arguments = "else", ""
	if r.Server() != other.Server() {
		t.Error("what is called changed the server")
	}
	other.Auth.Token = "2"
	if r.Server() == other.Server() {
		t.Error("the authorization did not change the server")
	}
}

// recorder is a transport that records the Authorization header of every
// request it makes.
type recorder struct {
	mu   sync.Mutex
	seen []string
}

func (r *recorder) RoundTrip(req *http.Request) (*http.Response, error) {
	r.mu.Lock()
	r.seen = append(r.seen, req.Header.Get("Authorization"))
	r.mu.Unlock()
	return http.DefaultTransport.RoundTrip(req)
}

// The authorizer completes the authorization of every HTTP request, so a
// token that changes is followed.
func TestAuthorizer(t *testing.T) {
	srv := mcptest.NewServer(t)
	r := Request{URL: srv.URL + "/mcp", Auth: rest.Auth{Type: rest.AuthOAuth2}, Name: "echo", Arguments: `{"text": "x"}`}
	token := 0
	authorize := func(ctx context.Context, a *rest.Auth) error {
		token++
		a.Access = fmt.Sprintf("t%d", token)
		return nil
	}
	rec := &recorder{}
	c := NewClient(&http.Client{Transport: rec}, r, authorize)
	t.Cleanup(func() { c.Close() })
	if _, err := c.Call(testContext(t), r); err != nil {
		t.Fatal(err)
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.seen) < 2 || rec.seen[0] != "Bearer t1" || rec.seen[len(rec.seen)-1] != fmt.Sprintf("Bearer t%d", token) {
		t.Errorf("seen %q", rec.seen)
	}

	failing := NewClient(&http.Client{}, r, func(context.Context, *rest.Auth) error { return errors.New("no token") })
	t.Cleanup(func() { failing.Close() })
	if res, err := failing.Call(testContext(t), r); err == nil || !strings.Contains(err.Error(), "no token") {
		t.Errorf("err = %v, res %+v %s", err, res, res.Result)
	}
}

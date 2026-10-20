package rpc_test

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/nomad-software/triggerapi/internal/network"
	"github.com/nomad-software/triggerapi/internal/rest"
	"github.com/nomad-software/triggerapi/internal/rpc"
	"github.com/nomad-software/triggerapi/internal/rpc/rpctest"
)

func server(t *testing.T, reflection rpctest.Reflection) *httptest.Server {
	srv := rpctest.NewServer(reflection)
	t.Cleanup(srv.Close)
	return srv
}

// streamingServer starts a server which has the methods that take a stream
// of messages too.
func streamingServer(t *testing.T) *httptest.Server {
	srv := rpctest.NewStreamingServer(rpctest.ReflectionAll)
	t.Cleanup(srv.Close)
	return srv
}

func TestTarget(t *testing.T) {
	tests := []struct {
		req     rpc.Request
		want    string
		wantErr bool
	}{
		{req: rpc.Request{URL: " localhost:50051 "}, want: "http://localhost:50051"},
		{req: rpc.Request{URL: "localhost:50051", TLS: true}, want: "https://localhost:50051"},
		{req: rpc.Request{URL: "grpc://localhost:50051/"}, want: "http://localhost:50051"},
		{req: rpc.Request{URL: "http://localhost:50051"}, want: "http://localhost:50051"},
		{req: rpc.Request{URL: "grpcs://api.test"}, want: "https://api.test"},
		{req: rpc.Request{URL: "HTTPS://api.test"}, want: "https://api.test"},
		{req: rpc.Request{URL: "  "}, wantErr: true},
		{req: rpc.Request{URL: "grpc://"}, wantErr: true},
		{req: rpc.Request{URL: "ftp://api.test"}, wantErr: true},
	}

	for _, tt := range tests {
		got, err := tt.req.Target()
		if (err != nil) != tt.wantErr || got != tt.want {
			t.Errorf("Target(%q) = %q, %v, want %q", tt.req.URL, got, err, tt.want)
		}
	}
}

func TestExpand(t *testing.T) {
	got := rpc.Request{
		URL:      "a",
		Method:   "a.B/c",
		Message:  "a",
		Metadata: []rest.KeyValue{{Key: "a", Value: "a", Enabled: true}},
		Auth:     rest.Auth{Token: "a"},
	}.Expand(strings.ToUpper)

	want := rpc.Request{
		URL:      "A",
		Method:   "a.B/c",
		Message:  "A",
		Metadata: []rest.KeyValue{{Key: "A", Value: "A", Enabled: true}},
		Auth:     rest.Auth{Token: "A"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestReflect(t *testing.T) {
	for name, reflection := range map[string]rpctest.Reflection{
		"both versions": rpctest.ReflectionAll,
		"alpha only":    rpctest.ReflectionAlphaOnly,
	} {
		t.Run(name, func(t *testing.T) {
			srv := server(t, reflection)

			schema, err := rpc.Reflect(context.Background(), rpc.Request{URL: srv.URL})
			if err != nil {
				t.Fatal(err)
			}

			var names []string
			for _, m := range schema.Methods {
				names = append(names, m.Name)
			}
			want := []string{"test.v1.Echo/Fail", "test.v1.Echo/Forever", "test.v1.Echo/Repeat", "test.v1.Echo/Say"}
			if !reflect.DeepEqual(names, want) {
				t.Errorf("methods = %v, want %v", names, want)
			}

			if m, ok := schema.Method("test.v1.Echo/Repeat"); !ok || !m.ServerStreaming || m.ClientStreaming {
				t.Errorf("Repeat = %+v, %v", m, ok)
			}
			if _, ok := schema.Method("test.v1.Echo/Missing"); ok {
				t.Error("found a method that does not exist")
			}
		})
	}
}

func TestReflectUnsupported(t *testing.T) {
	srv := server(t, rpctest.ReflectionNone)

	_, err := rpc.Reflect(context.Background(), rpc.Request{URL: srv.URL})
	if err == nil || !strings.Contains(err.Error(), "does not support reflection") {
		t.Errorf("err = %v", err)
	}
}

func TestReflectUnreachable(t *testing.T) {
	srv := rpctest.NewServer(rpctest.ReflectionAll)
	srv.Close()

	if _, err := rpc.Reflect(context.Background(), rpc.Request{URL: srv.URL}); err == nil {
		t.Error("expected an error")
	}
}

func TestExample(t *testing.T) {
	srv := streamingServer(t)

	schema, err := rpc.Reflect(context.Background(), rpc.Request{URL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}

	got, err := schema.Example("test.v1.Echo/Say")
	if err != nil {
		t.Fatal(err)
	}
	want := `{
  "text": "",
  "count": 0,
  "inner": {
    "flag": false
  },
  "tags": [
    ""
  ]
}`
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}

	// A method that takes a stream of messages is given two.
	got, err = schema.Example("test.v1.Echo/Gather")
	if lines := strings.Split(want, "\n"); err != nil || got != "[\n  "+strings.Join(lines, "\n  ")+",\n  "+strings.Join(lines, "\n  ")+"\n]" {
		t.Errorf("stream example %v:\n%s", err, got)
	}

	if _, err := schema.Example("test.v1.Echo/Missing"); err == nil {
		t.Error("expected an error for an unknown method")
	}
}

func TestInvokeUnary(t *testing.T) {
	srv := server(t, rpctest.ReflectionAll)

	req := rpc.Request{
		URL:     srv.URL,
		Method:  "test.v1.Echo/Say",
		Message: `{"text": "hello", "inner": {"flag": true}}`,
		Auth:    rest.Auth{Type: rest.AuthBearer, Token: "secret"},
		Metadata: []rest.KeyValue{
			{Key: "x-trace", Value: "abc", Enabled: true},
			{Key: "x-off", Value: "1"},
		},
	}

	// The schema is fetched by the call if it is not given.
	res, err := rpc.Invoke(context.Background(), req, nil)
	if err != nil {
		t.Fatal(err)
	}

	if !res.OK() || res.CodeName != "OK" || res.Status != "" {
		t.Errorf("status = %d %s %q", res.Code, res.CodeName, res.Status)
	}
	want := `{
  "text": "hello",
  "authorization": "Bearer secret",
  "trace": "abc"
}`
	if len(res.Messages) != 1 || res.Messages[0] != want {
		t.Errorf("messages = %v", res.Messages)
	}
	if res.Protocol != "gRPC (proto)" {
		t.Errorf("protocol = %q, content type %q", res.Protocol, res.Headers.Get("Content-Type"))
	}
	if res.Headers.Get("X-Server") != "rpctest" || res.Trailers.Get("X-Done") != "yes" {
		t.Errorf("headers = %v, trailers = %v", res.Headers, res.Trailers)
	}
}

func TestInvokeMetadataOverridesAuth(t *testing.T) {
	srv := server(t, rpctest.ReflectionAll)

	res, err := rpc.Invoke(context.Background(), rpc.Request{
		URL:      srv.URL,
		Method:   "test.v1.Echo/Say",
		Auth:     rest.Auth{Type: rest.AuthBasic, Username: "u", Password: "p"},
		Metadata: []rest.KeyValue{{Key: "Authorization", Value: "custom", Enabled: true}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Messages) != 1 || !strings.Contains(res.Messages[0], `"authorization": "custom"`) {
		t.Errorf("messages = %v", res.Messages)
	}
}

func TestInvokeServerStream(t *testing.T) {
	srv := server(t, rpctest.ReflectionAll)

	schema, err := rpc.Reflect(context.Background(), rpc.Request{URL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}

	res, err := rpc.Invoke(context.Background(), rpc.Request{
		URL:     srv.URL,
		Method:  "test.v1.Echo/Repeat",
		Message: `{"text": "hi", "count": 3}`,
	}, schema)
	if err != nil {
		t.Fatal(err)
	}

	if !res.OK() || len(res.Messages) != 3 || !strings.Contains(res.Messages[2], `"index": 2`) {
		t.Errorf("status %s, messages = %v", res.CodeName, res.Messages)
	}
}

// TestInvokeClientStream checks that a method which takes a stream of
// messages is sent each of those given, one after another or in an array.
func TestInvokeClientStream(t *testing.T) {
	srv := streamingServer(t)
	schema, err := rpc.Reflect(context.Background(), rpc.Request{URL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	if m, ok := schema.Method("test.v1.Echo/Talk"); !ok || !m.ServerStreaming || !m.ClientStreaming {
		t.Errorf("Talk = %+v, %v", m, ok)
	}
	invoke := func(method, message string) (*rpc.Response, error) {
		return rpc.Invoke(context.Background(), rpc.Request{URL: srv.URL, Method: "test.v1.Echo/" + method, Message: message}, schema)
	}

	for _, test := range []struct{ message, want string }{
		{`{"text": "a"} {"text": "b"}` + "\n" + `{"text": "c"}`, `"text": "a b c"`},
		{`[{"text": "a"}, {"text": "b"}]`, `"text": "a b"`},
		{`{"text": "only"}`, `"text": "only"`},
		{``, `"index": 1`},
		{`[]`, `{}`},
	} {
		res, err := invoke("Gather", test.message)
		if err != nil || !res.OK() || len(res.Messages) != 1 || !strings.Contains(res.Messages[0], test.want) {
			t.Errorf("%q: %v %+v", test.message, err, res)
		}
	}

	// A bidirectional method answers each.
	res, err := invoke("Talk", `[{"text": "a"}, {"text": "b"}, {"text": "c"}]`)
	if err != nil || !res.OK() || len(res.Messages) != 3 || !strings.Contains(res.Messages[2], `"text": "c"`) {
		t.Errorf("chat: %v %+v", err, res)
	}

	// A method that takes one message is given one, and each message has
	// to be valid.
	if _, err := invoke("Say", `{"text": "a"} {"text": "b"}`); err == nil || !strings.Contains(err.Error(), "takes one message, and 2 were given") {
		t.Errorf("two messages to Say: %v", err)
	}
	if _, err := invoke("Say", `[{"text": "a"}]`); err == nil {
		t.Error("an array was sent to Say")
	}
	if _, err := invoke("Gather", `{"text": "a"} {"nope": 1}`); err == nil || !strings.Contains(err.Error(), "message 2 is not valid") {
		t.Errorf("an invalid second message: %v", err)
	}
}

func TestInvokeStatus(t *testing.T) {
	srv := server(t, rpctest.ReflectionAll)

	res, err := rpc.Invoke(context.Background(), rpc.Request{URL: srv.URL, Method: "test.v1.Echo/Fail"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.OK() || res.Code != 5 || res.CodeName != "NOT_FOUND" || res.Status != "no such thing" || len(res.Messages) != 0 {
		t.Errorf("status = %d %s %q, messages = %v", res.Code, res.CodeName, res.Status, res.Messages)
	}
}

func TestInvokeCancelledKeepsMessages(t *testing.T) {
	srv := server(t, rpctest.ReflectionAll)

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	res, err := rpc.Invoke(ctx, rpc.Request{
		URL:     srv.URL,
		Method:  "test.v1.Echo/Forever",
		Message: `{"text": "one"}`,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.CodeName != "DEADLINE_EXCEEDED" || len(res.Messages) != 1 {
		t.Errorf("status = %s, messages = %v", res.CodeName, res.Messages)
	}
}

func TestInvokeErrors(t *testing.T) {
	srv := server(t, rpctest.ReflectionAll)

	tests := map[string]rpc.Request{
		"no method":      {URL: srv.URL},
		"unknown method": {URL: srv.URL, Method: "test.v1.Echo/Missing"},
		"invalid JSON":   {URL: srv.URL, Method: "test.v1.Echo/Say", Message: "{oops"},
		"unknown field":  {URL: srv.URL, Method: "test.v1.Echo/Say", Message: `{"nope": 1}`},
		"empty URL":      {Method: "test.v1.Echo/Say"},
	}
	for name, req := range tests {
		if res, err := rpc.Invoke(context.Background(), req, nil); err == nil {
			t.Errorf("%s: expected an error, got %+v", name, res)
		}
	}
}

func TestInvokeUnreachable(t *testing.T) {
	srv := rpctest.NewServer(rpctest.ReflectionAll)
	schema, err := rpc.Reflect(context.Background(), rpc.Request{URL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	srv.Close()

	// With the schema already known the failure is that of the call itself.
	res, err := rpc.Invoke(context.Background(), rpc.Request{URL: srv.URL, Method: "test.v1.Echo/Say"}, schema)
	if err != nil {
		t.Fatal(err)
	}
	if res.CodeName != "UNAVAILABLE" || res.Status == "" {
		t.Errorf("status = %s %q", res.CodeName, res.Status)
	}
}

func TestProtocol(t *testing.T) {
	tests := map[string]string{
		"application/grpc":                 "gRPC (proto)",
		"application/grpc+proto":           "gRPC (proto)",
		"Application/gRPC+JSON; charset=x": "gRPC (json)",
		"application/grpc-web+proto":       "gRPC-Web (proto)",
		"application/grpc-web-text":        "gRPC-Web text (proto)",
		"application/connect+proto":        "Connect stream (proto)",
		"application/connect+json":         "Connect stream (json)",
		"application/proto":                "Connect (proto)",
		"application/json":                 "Connect (json)",
		"application/grpcx":                "",
		"text/html":                        "",
		"":                                 "",
	}
	for contentType, want := range tests {
		if got := rpc.Protocol(contentType); got != want {
			t.Errorf("Protocol(%q) = %q, want %q", contentType, got, want)
		}
	}
}

func TestInvokeNetwork(t *testing.T) {
	srv := server(t, rpctest.ReflectionAll)
	_, port, _ := strings.Cut(strings.TrimPrefix(srv.URL, "http://"), ":")

	// The name does not exist: the settings say where it is reached.
	client, err := network.Load(network.Config{Hosts: []network.Host{{Pattern: "grpc.example.invalid", Address: "127.0.0.1"}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	req := rpc.Request{URL: "grpc.example.invalid:" + port, Method: "test.v1.Echo/Say", Message: `{"text": "hi"}`, Network: client}
	res, err := rpc.Invoke(context.Background(), req, nil)
	if err != nil || !res.OK() {
		t.Fatalf("response %+v, %v", res, err)
	}

	// Without them it is not reached.
	req.Network = nil
	if _, err := rpc.Invoke(context.Background(), req, nil); err == nil {
		t.Error("a name that does not exist was reached")
	}
}

// TestInvokeTiming checks that a call says where its time went, as a REST
// request does.
func TestInvokeTiming(t *testing.T) {
	srv := server(t, rpctest.ReflectionAll)
	schema, err := rpc.Reflect(context.Background(), rpc.Request{URL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}

	res, err := rpc.Invoke(context.Background(), rpc.Request{URL: srv.URL, Method: "test.v1.Echo/Say", Message: `{"text": "hi"}`}, schema)
	if err != nil || !res.OK() {
		t.Fatalf("response %+v, %v", res, err)
	}
	timing := res.Timing
	if timing == nil {
		t.Fatal("no timing")
	}
	// The server is at an IP address, so no name is looked up, and there is
	// no TLS.
	var stages []rest.Stage
	var last time.Duration
	for _, p := range timing.Phases {
		stages = append(stages, p.Stage)
		if p.Start < last || p.End < p.Start || p.End > res.Duration {
			t.Errorf("phase %+v is out of order (previous ended at %v, total %v)", p, last, res.Duration)
		}
		last = p.End
	}
	if want := []rest.Stage{rest.StageConnect, rest.StageSend, rest.StageWait, rest.StageDownload}; !reflect.DeepEqual(stages, want) {
		t.Errorf("stages = %v, want %v", stages, want)
	}
	if timing.Address != strings.TrimPrefix(srv.URL, "http://") || timing.Reused || timing.TLS != "" || timing.Redirects != 0 {
		t.Errorf("timing = %+v", timing)
	}
}

// TestResponseWithoutTiming checks that a response kept in the history
// from before there were timings is read as it was, with none.
func TestResponseWithoutTiming(t *testing.T) {
	var res rpc.Response
	if err := json.Unmarshal([]byte(`{"code":0,"codeName":"OK","messages":["{}"],"duration":1000}`), &res); err != nil {
		t.Fatal(err)
	}
	if res.Timing != nil || res.CodeName != "OK" || len(res.Messages) != 1 {
		t.Errorf("response = %+v", res)
	}
	data, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "timing") {
		t.Errorf("a response without a timing is written with one: %s", data)
	}
}

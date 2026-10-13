package rpc_test

import (
	"context"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/nomad-software/triggerapi/internal/rest"
	"github.com/nomad-software/triggerapi/internal/rpc"
	"github.com/nomad-software/triggerapi/internal/rpc/rpctest"
)

func server(t *testing.T, reflection rpctest.Reflection) *httptest.Server {
	srv := rpctest.NewServer(reflection)
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
	srv := server(t, rpctest.ReflectionAll)

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

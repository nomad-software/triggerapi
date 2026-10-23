// Package rpc discovers and calls gRPC services. Services are described by
// the server itself through gRPC server reflection, so no protobuf files are
// needed. It has no dependency on the UI.
package rpc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"strings"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/dynamicpb"

	"github.com/nomad-software/triggerapi/internal/assert"
	"github.com/nomad-software/triggerapi/internal/capture"
	"github.com/nomad-software/triggerapi/internal/network"
	"github.com/nomad-software/triggerapi/internal/rest"
)

// Request describes a gRPC call as entered by the user.
type Request struct {
	// URL is the address of the server as host:port. A grpc:// or http://
	// scheme is allowed, and grpcs:// or https:// turn TLS on.
	URL string `json:"url"`
	TLS bool   `json:"tls,omitempty"`
	// Method is the full name of the method, as package.Service/Method.
	Method string `json:"method"`
	// Message is the request message as JSON.
	Message  string          `json:"message,omitempty"`
	Metadata []rest.KeyValue `json:"metadata,omitempty"`
	Auth     rest.Auth       `json:"auth"`

	// Captures are the values to take from the last response message and
	// keep in variables of the active environment. See rest.Request.
	Captures []capture.Rule `json:"captures,omitempty"`
	// Assertions are as those of a rest.Request, the body being the last
	// response message.
	Assertions []assert.Check `json:"assertions,omitempty"`

	// Network is how the server is reached: the proxy, TLS and what is
	// particular to its host. It is not saved: whoever calls gives it, and
	// nil is the default.
	Network *network.Client `json:"-"`
}

// Response is the result of a call. It is kept in the history as JSON.
type Response struct {
	// Code is the gRPC status code the call ended with, which is 0 for OK,
	// and CodeName its name, such as NOT_FOUND. Status is the message that
	// came with it.
	Code     int    `json:"code"`
	CodeName string `json:"codeName"`
	Status   string `json:"status,omitempty"`
	// Messages are the response messages as indented JSON. A unary method
	// has at most one.
	Messages []string      `json:"messages,omitempty"`
	Headers  http.Header   `json:"headers,omitempty"`
	Trailers http.Header   `json:"trailers,omitempty"`
	Duration time.Duration `json:"duration"`
	// Protocol is the RPC protocol the server answered in, such as
	// "gRPC (proto)", as its Content-Type header says. It is empty if there
	// was no answer.
	Protocol string `json:"protocol,omitempty"`
	// Timing says where the time of the call went, as for a REST request:
	// sending is that of every message, and the download that of the
	// response from its headers to the end of the call. A response kept in
	// the history from before has none.
	Timing *rest.Timing `json:"timing,omitempty"`
}

// Protocol returns the RPC protocol, and the encoding of its messages, that
// a response with a content type is in: "gRPC (proto)" for
// application/grpc+proto. It returns an empty string for a content type
// that is not of an RPC.
//
// gRPC and gRPC-Web have one content type however many messages there are.
// The Connect protocol has one for a call with a single response, which is
// just the type of the message, and another for a stream.
func Protocol(contentType string) string {
	mediaType, _, _ := strings.Cut(strings.ToLower(contentType), ";")
	mediaType = strings.TrimSpace(mediaType)

	for _, known := range []struct{ prefix, name string }{
		{"application/grpc-web-text", "gRPC-Web text"},
		{"application/grpc-web", "gRPC-Web"},
		{"application/grpc", "gRPC"},
		{"application/connect", "Connect stream"},
	} {
		if rest, ok := strings.CutPrefix(mediaType, known.prefix); ok {
			switch {
			case rest == "":
				// Messages are protobuf unless it says otherwise.
				return known.name + " (proto)"
			case strings.HasPrefix(rest, "+"):
				return known.name + " (" + rest[1:] + ")"
			}
		}
	}

	switch mediaType {
	case "application/proto":
		return "Connect (proto)"
	case "application/json":
		return "Connect (json)"
	}
	return ""
}

// OK reports if the call succeeded.
func (r *Response) OK() bool {
	return r.Code == 0
}

// Expand returns a copy of the request with f applied to every field the user
// can type into. It is used to substitute environment variables.
func (r Request) Expand(f func(string) string) Request {
	r.URL = f(r.URL)
	r.Message = f(r.Message)
	r.Metadata = rest.ExpandPairs(r.Metadata, f)
	r.Auth = r.Auth.Expand(f)
	return r
}

// Target returns the base URL of the server, which has an https scheme if TLS
// is used and an http scheme if not.
func (r Request) Target() (string, error) {
	address := strings.TrimSpace(r.URL)
	secure := r.TLS

	if scheme, rest, ok := strings.Cut(address, "://"); ok {
		switch strings.ToLower(scheme) {
		case "grpc", "http":
		case "grpcs", "https":
			secure = true
		default:
			return "", fmt.Errorf("unsupported scheme %q", scheme)
		}
		address = rest
	}

	address = strings.TrimRight(address, "/")
	if address == "" {
		return "", errors.New("URL is empty")
	}

	if secure {
		return "https://" + address, nil
	}
	return "http://" + address, nil
}

// client returns the base URL of the server and an HTTP client to reach it
// with. gRPC needs HTTP/2, which is used without TLS if the server is not
// secure. The connections are closed by calling the returned function.
func (r Request) client() (target string, client *http.Client, closer func(), err error) {
	target, err = r.Target()
	if err != nil {
		return "", nil, nil, err
	}

	protocols := new(http.Protocols)
	if strings.HasPrefix(target, "https://") {
		protocols.SetHTTP2(true)
	} else {
		protocols.SetUnencryptedHTTP2(true)
	}
	settings := r.network()
	u, err := url.Parse(target)
	if err != nil {
		return "", nil, nil, err
	}
	transport, err := settings.Transport(u.Hostname(), protocols)
	if err != nil {
		return "", nil, nil, err
	}

	return target, &http.Client{Transport: transport}, transport.CloseIdleConnections, nil
}

// network returns what the server is reached with, the default if the
// caller gave nothing.
func (r Request) network() *network.Client {
	if r.Network == nil {
		return network.Default()
	}
	return r.Network
}

// headers returns the metadata and authorization of the request as HTTP
// headers, which is how gRPC sends them. User supplied metadata takes
// precedence over the authorization.
func (r Request) headers() http.Header {
	headers := http.Header{}
	if value, ok := r.Auth.Header(); ok {
		headers.Set("Authorization", value)
	}

	seen := map[string]bool{}
	for _, m := range r.Metadata {
		if !m.Enabled || m.Key == "" {
			continue
		}
		key := http.CanonicalHeaderKey(m.Key)
		if !seen[key] {
			headers.Del(key)
			seen[key] = true
		}
		headers.Add(key, m.Value)
	}

	return headers
}

// Invoke calls the method of the request, sending its message, or for a
// method that takes a stream its messages (see splitMessages), and
// collecting the responses until the server ends the call. The schema
// describes the server; if it is nil it is fetched first using reflection.
//
// A call that the server rejects, or which is cancelled, is not an error: the
// status and any messages received before it are in the response.
func Invoke(ctx context.Context, r Request, schema *Schema) (*Response, error) {
	if schema == nil {
		var err error
		if schema, err = Reflect(ctx, r); err != nil {
			return nil, err
		}
	}

	method, ok := schema.Method(r.Method)
	if !ok {
		if r.Method == "" {
			return nil, errors.New("no method selected")
		}
		return nil, fmt.Errorf("the server does not have a method named %s", r.Method)
	}

	// A method that takes a stream of messages can be given several, and
	// any other exactly one.
	texts := splitMessages(r.Message, method.ClientStreaming)
	if !method.ClientStreaming && len(texts) != 1 {
		return nil, fmt.Errorf("%s takes one message, and %d were given", r.Method, len(texts))
	}
	ins := make([]*dynamicpb.Message, len(texts))
	for i, text := range texts {
		ins[i] = dynamicpb.NewMessage(method.desc.Input())
		if err := (protojson.UnmarshalOptions{Resolver: schema.types}).Unmarshal([]byte(text), ins[i]); err != nil {
			if len(texts) > 1 {
				return nil, fmt.Errorf("message %d is not valid: %w", i+1, err)
			}
			return nil, fmt.Errorf("invalid message: %w", err)
		}
	}

	target, httpClient, closer, err := r.client()
	if err != nil {
		return nil, err
	}
	defer closer()

	client := connect.NewClient[dynamicpb.Message, dynamicpb.Message](
		httpClient,
		target+"/"+method.Name,
		connect.WithGRPC(),
		// Response messages have no type until they are given the one the
		// method returns.
		connect.WithResponseInitializer(func(_ connect.Spec, message any) error {
			if dynamic, ok := message.(*dynamicpb.Message); ok {
				*dynamic = *dynamicpb.NewMessage(method.desc.Output())
			}
			return nil
		}),
	)

	// Cancelling the context when done releases the stream if the call is
	// abandoned part way through.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	res := &Response{}
	start := time.Now()
	trace := rest.NewTracer(start)

	// A bidirectional stream lets one code path call every kind of method:
	// the messages are sent, which for most methods is one, and responses
	// are read until the server finishes.
	stream := client.CallBidiStream(httptrace.WithClientTrace(ctx, trace.Trace()))
	maps.Copy(stream.RequestHeader(), r.headers())

	err = func() error {
		// A send fails with io.EOF if the server has already ended the
		// call, in which case the receive below gives the real status.
		for _, in := range ins {
			if err := stream.Send(in); errors.Is(err, io.EOF) {
				break
			} else if err != nil {
				return err
			}
		}
		if err := stream.CloseRequest(); err != nil {
			return err
		}

		for {
			out, err := stream.Receive()
			if errors.Is(err, io.EOF) {
				return nil
			} else if err != nil {
				return err
			}

			text, err := schema.format(out)
			if err != nil {
				return err
			}
			res.Messages = append(res.Messages, text)
		}
	}()

	end := time.Now()
	res.Duration = end.Sub(start)
	// The HTTP request is connect's own, so one of the same address stands
	// for it in saying what the network settings made of it.
	req, _ := http.NewRequest(http.MethodPost, target+"/"+method.Name, nil)
	res.Timing = trace.Timing(end, nil).Annotate(r.network(), req)
	res.Headers = stream.ResponseHeader()
	res.Protocol = Protocol(res.Headers.Get("Content-Type"))
	res.Trailers = stream.ResponseTrailer()
	_ = stream.CloseResponse()

	res.CodeName = "OK"
	if err != nil {
		code := connect.CodeOf(err)
		res.Code = int(code)
		res.CodeName = strings.ToUpper(code.String())
		res.Status = err.Error()

		// The message of a status sent by the server is given without the
		// code in front of it.
		var status *connect.Error
		if errors.As(err, &status) {
			res.Status = status.Message()
		}
	}

	return res, nil
}

// format returns the message as indented JSON.
func (s *Schema) format(message *dynamicpb.Message) (string, error) {
	data, err := protojson.MarshalOptions{Resolver: s.types}.Marshal(message)
	if err != nil {
		return "", err
	}
	return indent(data)
}

// indent formats JSON. protojson deliberately varies its whitespace, so the
// indenting is done separately to keep it stable.
func indent(data []byte) (string, error) {
	var out bytes.Buffer
	if err := json.Indent(&out, data, "", "  "); err != nil {
		return "", err
	}
	return out.String(), nil
}

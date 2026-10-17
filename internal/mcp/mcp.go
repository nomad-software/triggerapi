// Package mcp is the engine of Model Context Protocol requests: it connects
// to an MCP server over the Streamable HTTP transport, discovers the tools,
// resources and prompts it has, and calls them. It is built on the official
// Go SDK (github.com/modelcontextprotocol/go-sdk). It has no dependency on
// the UI.
//
// Only Streamable HTTP is supported. The stdio transport, which starts the
// server as a program, is left out on purpose, so that a request never runs
// anything; Request.Transport is there so that it can be added. The older
// HTTP+SSE transport is not supported.
package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"

	"github.com/nomad-software/triggerapi/internal/capture"
	"github.com/nomad-software/triggerapi/internal/rest"
)

// Transport is how a server is reached.
type Transport string

// TransportHTTP is the Streamable HTTP transport, which is what an empty
// Transport means.
const TransportHTTP Transport = "http"

// Kind is the kind of thing on a server that a request calls.
type Kind string

const (
	KindTool     Kind = "tool"
	KindResource Kind = "resource"
	KindPrompt   Kind = "prompt"
)

// Request is a saved MCP request: the server, the headers and authorization
// of the HTTP requests made to it, the tool, resource or prompt to call, its
// arguments, and what to capture from the result.
type Request struct {
	// Transport is empty for Streamable HTTP, the only one there is.
	Transport Transport       `json:"transport,omitempty"`
	URL       string          `json:"url"`
	Headers   []rest.KeyValue `json:"headers,omitempty"`
	Auth      rest.Auth       `json:"auth"`
	// Kind and Name say what is called: the name of a tool or a prompt,
	// or the URI of a resource. A request with a name and no kind calls a
	// tool.
	Kind Kind   `json:"kind,omitempty"`
	Name string `json:"name,omitempty"`
	// Arguments are those of a tool or a prompt, as the text of a JSON
	// object. Empty is none. A resource has none.
	Arguments string `json:"arguments,omitempty"`
	// Captures are as those of a rest.Request.
	Captures []capture.Rule `json:"captures,omitempty"`
}

// Expand applies f to every part of the request that can hold a placeholder.
// The captures are left as they are.
func (r Request) Expand(f func(string) string) Request {
	r.URL = f(r.URL)
	r.Headers = rest.ExpandPairs(r.Headers, f)
	r.Auth = r.Auth.Expand(f)
	r.Name = f(r.Name)
	r.Arguments = f(r.Arguments)
	return r
}

// Server returns what identifies the server and how it is asked: its
// address, headers and authorization. Requests that give the same can share
// a Client.
func (r Request) Server() string {
	data, _ := json.Marshal(struct {
		Transport Transport
		URL       string
		Headers   []rest.KeyValue
		Auth      rest.Auth
	}{r.Transport, strings.TrimSpace(r.URL), r.Headers, r.Auth})
	return string(data)
}

// arguments returns the arguments as JSON, or nil if there are none. Any
// that are not a JSON object are an error.
func (r Request) arguments() (json.RawMessage, error) {
	text := strings.TrimSpace(r.Arguments)
	if text == "" || r.Kind == KindResource {
		return nil, nil
	}
	var object map[string]any
	if err := json.Unmarshal([]byte(text), &object); err != nil {
		return nil, fmt.Errorf("the arguments are not a JSON object: %w", err)
	}
	if object == nil {
		return nil, nil
	}
	return json.RawMessage(text), nil
}

// endpoint returns the address of the server, with http:// assumed if it
// has no scheme.
func (r Request) endpoint() (string, error) {
	if r.Transport != "" && r.Transport != TransportHTTP {
		return "", fmt.Errorf("the transport %q is not supported", r.Transport)
	}
	raw := strings.TrimSpace(r.URL)
	if raw == "" {
		return "", errors.New("URL is empty")
	}
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	return raw, nil
}

// Response is what came of a call: its result, or the error the server
// answered with. What the server sends while a call is under way, such as
// its progress, is not kept: the SDK hands it over apart from the answer, so
// it cannot be told when all of it has come. It is kept in the history as
// JSON.
type Response struct {
	// Result is the result of the call as JSON, or nil if Error is set.
	Result json.RawMessage `json:"result,omitempty"`
	Error  *jsonrpc.Error  `json:"error,omitempty"`
	// Status and Headers are those of the HTTP response the answer came
	// in, if it was seen.
	Status   string        `json:"status,omitempty"`
	Headers  http.Header   `json:"headers,omitempty"`
	Duration time.Duration `json:"duration"`
	// Protocol is the version of the protocol the client agreed on, and
	// Server the name and version the server gave.
	Protocol string `json:"protocol,omitempty"`
	Server   string `json:"server,omitempty"`
}

// ToolError returns what a tool said went wrong, if the result is that of a
// tool which reports an error. It reports false if it does not.
func (r *Response) ToolError() (string, bool) {
	var result struct {
		IsError bool `json:"isError"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if r.Result == nil || json.Unmarshal(r.Result, &result) != nil || !result.IsError {
		return "", false
	}
	var texts []string
	for _, c := range result.Content {
		if c.Type == "text" && strings.TrimSpace(c.Text) != "" {
			texts = append(texts, strings.TrimSpace(c.Text))
		}
	}
	if len(texts) == 0 {
		return "the tool reported an error", true
	}
	return strings.Join(texts, "; "), true
}

// OK reports whether the call succeeded: there is a result, and it is not a
// tool's error.
func (r *Response) OK() bool {
	_, failed := r.ToolError()
	return r.Error == nil && !failed
}

// Outcome returns how the call went: OK, Tool error for a tool which
// reported an error, or the code of a JSON-RPC error and its name, with
// what went wrong and whether it is a failure.
func (r *Response) Outcome() (status, problem string, failed bool) {
	if r.Error != nil {
		return fmt.Sprintf("%d %s", r.Error.Code, CodeName(r.Error.Code)), r.Error.Message, true
	}
	if message, toolFailed := r.ToolError(); toolFailed {
		return "Tool error", message, true
	}
	return "OK", "", false
}

// CodeName returns the name of a JSON-RPC error code, or ERROR for one that
// has none.
func CodeName(code int64) string {
	switch code {
	case jsonrpc.CodeParseError:
		return "PARSE_ERROR"
	case jsonrpc.CodeInvalidRequest:
		return "INVALID_REQUEST"
	case jsonrpc.CodeMethodNotFound:
		return "METHOD_NOT_FOUND"
	case jsonrpc.CodeInvalidParams:
		return "INVALID_PARAMS"
	case jsonrpc.CodeInternalError:
		return "INTERNAL_ERROR"
	case -32002:
		return "RESOURCE_NOT_FOUND"
	}
	return "ERROR"
}

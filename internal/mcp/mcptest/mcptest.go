// Package mcptest is an MCP server for tests, served over Streamable HTTP
// with the official Go SDK. It has an echo tool, a tool which fails, a
// resource and a prompt. NewServer speaks the stateless protocol of
// 2026-07-28 and later, and NewLegacyServer the protocol from before it,
// with sessions.
package mcptest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// EchoArgs are the arguments of the echo tool.
type EchoArgs struct {
	Text  string `json:"text" jsonschema:"what to say"`
	Times int    `json:"times,omitempty" jsonschema:"how many times to say it"`
}

// EchoResult is what the echo tool gives back as structured content: the
// text the number of times asked, and the X-Test header of the request.
type EchoResult struct {
	Said   string `json:"said"`
	Header string `json:"header"`
}

// NewServer starts the server, at the address of the result followed by
// /mcp, and stops it when the test ends. It is stateless, as servers are
// since protocol version 2026-07-28.
func NewServer(t *testing.T) *httptest.Server {
	return newServer(t, true)
}

// NewLegacyServer starts the server as NewServer does, but as one from
// before protocol version 2026-07-28, which gives each client a session.
func NewLegacyServer(t *testing.T) *httptest.Server {
	return newServer(t, false)
}

func newServer(t *testing.T, stateless bool) *httptest.Server {
	server := sdk.NewServer(&sdk.Implementation{Name: "test-server", Version: "1.2.3"}, nil)

	sdk.AddTool(server, &sdk.Tool{Name: "echo", Description: "Says the text back."},
		func(ctx context.Context, req *sdk.CallToolRequest, args EchoArgs) (*sdk.CallToolResult, EchoResult, error) {
			times := max(args.Times, 1)
			return nil, EchoResult{Said: strings.Repeat(args.Text, times), Header: req.Extra.Header.Get("X-Test")}, nil
		})
	server.AddTool(&sdk.Tool{Name: "fail", Description: "Always fails.", InputSchema: map[string]any{"type": "object"}},
		func(context.Context, *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
			return &sdk.CallToolResult{IsError: true, Content: []sdk.Content{&sdk.TextContent{Text: "it went wrong"}}}, nil
		})
	server.AddResource(&sdk.Resource{URI: "test://greeting", Name: "greeting", Description: "A greeting.", MIMEType: "text/plain"},
		func(_ context.Context, req *sdk.ReadResourceRequest) (*sdk.ReadResourceResult, error) {
			return &sdk.ReadResourceResult{Contents: []*sdk.ResourceContents{{URI: req.Params.URI, MIMEType: "text/plain", Text: "hello"}}}, nil
		})
	server.AddPrompt(&sdk.Prompt{Name: "greet", Description: "Greets someone.", Arguments: []*sdk.PromptArgument{{Name: "who", Required: true}}},
		func(_ context.Context, req *sdk.GetPromptRequest) (*sdk.GetPromptResult, error) {
			return &sdk.GetPromptResult{Messages: []*sdk.PromptMessage{{Role: "user", Content: &sdk.TextContent{Text: "Hello " + req.Params.Arguments["who"]}}}}, nil
		})

	handler := sdk.NewStreamableHTTPHandler(func(*http.Request) *sdk.Server { return server }, &sdk.StreamableHTTPOptions{Stateless: stateless})
	mux := http.NewServeMux()
	mux.Handle("/mcp", handler)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

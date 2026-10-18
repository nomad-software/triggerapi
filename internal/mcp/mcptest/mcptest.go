//lint:file-ignore SA1019 Sampling and logging are deprecated since protocol version 2026-07-28, but servers still use them while they are, and the client is tested with them.

// Package mcptest is an MCP server for tests, served over Streamable HTTP
// with the official Go SDK. It has an echo tool, a tool which fails, tools
// which send their progress and log messages, ask the user for something
// (elicitation) and ask for a message of a language model (sampling), a
// resource and a prompt. NewServer speaks the stateless protocol of
// 2026-07-28 and later, and NewLegacyServer the protocol from before it,
// with sessions.
package mcptest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
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
	sdk.AddTool(server, &sdk.Tool{Name: "progress", Description: "Counts to the steps, sending its progress and logging each."}, progress)
	sdk.AddTool(server, &sdk.Tool{Name: "who", Description: "Asks the user who they are."}, who)
	sdk.AddTool(server, &sdk.Tool{Name: "sample", Description: "Asks a language model to say hello."}, sample)
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

// ProgressArgs are the arguments of the progress tool.
type ProgressArgs struct {
	Steps int `json:"steps" jsonschema:"how many steps to count"`
}

// progress sends a notification of its progress for each step, and logs it,
// before it answers.
func progress(ctx context.Context, req *sdk.CallToolRequest, args ProgressArgs) (*sdk.CallToolResult, map[string]int, error) {
	for step := 1; step <= args.Steps; step++ {
		message := "step " + strconv.Itoa(step)
		if token := req.Params.GetProgressToken(); token != nil {
			req.Session.NotifyProgress(ctx, &sdk.ProgressNotificationParams{ProgressToken: token, Progress: float64(step), Total: float64(args.Steps), Message: message})
		}
		req.Session.Log(ctx, &sdk.LoggingMessageParams{Level: "info", Logger: "counter", Data: message})
	}
	return nil, map[string]int{"steps": args.Steps}, nil
}

// WhoResult is what the who tool gives back: how the user answered, and
// what they filled in.
type WhoResult struct {
	Action string  `json:"action"`
	Name   string  `json:"name,omitempty"`
	Age    float64 `json:"age,omitempty"`
}

// who asks the user for their name and age, and says what they answered.
func who(_ context.Context, req *sdk.CallToolRequest, _ struct{}) (*sdk.CallToolResult, WhoResult, error) {
	answer, ok := req.Params.InputResponses["who"].(*sdk.ElicitResult)
	if !ok {
		minimum := 0.0
		return &sdk.CallToolResult{InputRequests: sdk.InputRequestMap{"who": &sdk.ElicitParams{
			Message: "Who are you?",
			RequestedSchema: &jsonschema.Schema{
				Type: "object",
				Properties: map[string]*jsonschema.Schema{
					"name": {Type: "string", Title: "Name"},
					"age":  {Type: "integer", Minimum: &minimum},
				},
				Required: []string{"name"},
			},
		}}}, WhoResult{}, nil
	}
	result := WhoResult{Action: answer.Action}
	result.Name, _ = answer.Content["name"].(string)
	result.Age, _ = answer.Content["age"].(float64)
	return nil, result, nil
}

// SampleResult is what the sample tool gives back: what the language model
// said, and which it was.
type SampleResult struct {
	Text  string `json:"text"`
	Model string `json:"model"`
}

// sample asks for a message of a language model, and gives it back.
func sample(_ context.Context, req *sdk.CallToolRequest, _ struct{}) (*sdk.CallToolResult, SampleResult, error) {
	answer, ok := req.Params.InputResponses["hello"].(*sdk.CreateMessageWithToolsResult)
	if !ok {
		return &sdk.CallToolResult{InputRequests: sdk.InputRequestMap{"hello": &sdk.CreateMessageParams{
			SystemPrompt: "Be brief.",
			Messages:     []*sdk.SamplingMessage{{Role: "user", Content: &sdk.TextContent{Text: "Say hello."}}},
			MaxTokens:    50,
		}}}, SampleResult{}, nil
	}
	result := SampleResult{Model: answer.Model}
	for _, content := range answer.Content {
		if text, ok := content.(*sdk.TextContent); ok {
			result.Text += text.Text
		}
	}
	return nil, result, nil
}

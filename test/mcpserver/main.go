//lint:file-ignore SA1019 Sampling and logging are deprecated since protocol version 2026-07-28, but servers still use them while they are, and the client handles them.

// Command mcpserver is a small MCP server to try the MCP requests of
// TriggerAPI against by hand. It serves Streamable HTTP at /mcp, with a few
// tools, resources and a prompt, and logs every call it is given. Some of
// the tools send their progress and log messages while they work, ask the
// user to fill in a form (elicitation) or ask for the message of a language
// model (sampling), which TriggerAPI shows in the Events tab of the response
// and asks in a dialog.
//
//	go run ./test/mcpserver                  # http://localhost:8000/mcp
//	go run ./test/mcpserver -token secret    # needs Authorization: Bearer secret
//	go run ./test/mcpserver -legacy          # an older server, with sessions
//
// It is not part of the program, and is not used by the tests, which have
// their own server in internal/mcp/mcptest.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	addr := flag.String("addr", "localhost:8000", "the address to listen on")
	token := flag.String("token", "", "a bearer token every request must give, or none")
	legacy := flag.Bool("legacy", false, "behave as a server from before protocol version 2026-07-28, which gives each client a session")
	flag.Parse()

	handler := sdk.NewStreamableHTTPHandler(func(*http.Request) *sdk.Server { return newServer() },
		&sdk.StreamableHTTPOptions{Stateless: !*legacy})
	mux := http.NewServeMux()
	mux.Handle("/mcp", logged(authorized(*token, handler)))

	kind := "stateless (protocol 2026-07-28 and later)"
	if *legacy {
		kind = "with sessions (protocols before 2026-07-28)"
	}
	log.Printf("MCP server %s at http://%s/mcp", kind, *addr)
	if *token != "" {
		log.Printf("requests need the header Authorization: Bearer %s", *token)
	}
	log.Fatal(http.ListenAndServe(*addr, mux))
}

// newServer returns the server with its tools, resources and prompt.
func newServer() *sdk.Server {
	server := sdk.NewServer(&sdk.Implementation{Name: "triggerapi-sample", Version: "1.0.0"}, nil)

	sdk.AddTool(server, &sdk.Tool{Name: "add", Description: "Adds two numbers."}, add)
	sdk.AddTool(server, &sdk.Tool{Name: "echo", Description: "Says the text back, as many times as asked."}, echo)
	sdk.AddTool(server, &sdk.Tool{Name: "headers", Description: "Lists the HTTP headers the call came with, to check headers, authorization and placeholders."}, headers)
	sdk.AddTool(server, &sdk.Tool{Name: "time", Description: "Gives the time now in a time zone, such as Europe/London."}, now)
	sdk.AddTool(server, &sdk.Tool{Name: "progress", Description: "Works through a number of steps, a moment each, sending its progress and logging each step."}, progress)
	sdk.AddTool(server, &sdk.Tool{Name: "log", Description: "Logs a message at every level, from debug to emergency."}, logLevels)
	sdk.AddTool(server, &sdk.Tool{Name: "book_flight", Description: "Asks the user for the details of a flight to book, in a form with a field of every kind."}, bookFlight)
	sdk.AddTool(server, &sdk.Tool{Name: "confirm_delete", Description: "Asks the user to confirm deleting a file, in a form with no fields."}, confirmDelete)
	sdk.AddTool(server, &sdk.Tool{Name: "summarise", Description: "Asks a language model, which is the user, to summarise some text."}, summarise)
	server.AddTool(&sdk.Tool{Name: "fail", Description: "Always reports an error, to see how a failed tool is shown.", InputSchema: map[string]any{"type": "object"}},
		func(context.Context, *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
			return &sdk.CallToolResult{IsError: true, Content: []sdk.Content{&sdk.TextContent{Text: "this tool always fails"}}}, nil
		})

	server.AddResource(&sdk.Resource{URI: "sample://readme", Name: "readme", Description: "A short text.", MIMEType: "text/plain"},
		text("This is the sample MCP server of TriggerAPI."))
	server.AddResource(&sdk.Resource{URI: "sample://config", Name: "config", Description: "Some JSON.", MIMEType: "application/json"},
		text(`{"environment": "local", "features": ["tools", "resources", "prompts"]}`))

	server.AddPrompt(&sdk.Prompt{
		Name:        "review",
		Description: "Asks for a review of something.",
		Arguments:   []*sdk.PromptArgument{{Name: "topic", Description: "What to review", Required: true}},
	}, func(_ context.Context, req *sdk.GetPromptRequest) (*sdk.GetPromptResult, error) {
		return &sdk.GetPromptResult{Messages: []*sdk.PromptMessage{{
			Role:    "user",
			Content: &sdk.TextContent{Text: "Please review " + req.Params.Arguments["topic"] + "."},
		}}}, nil
	})

	return server
}

type addArgs struct {
	A float64 `json:"a" jsonschema:"the first number"`
	B float64 `json:"b" jsonschema:"the second number"`
}

type addResult struct {
	Sum float64 `json:"sum"`
}

func add(_ context.Context, _ *sdk.CallToolRequest, args addArgs) (*sdk.CallToolResult, addResult, error) {
	return nil, addResult{Sum: args.A + args.B}, nil
}

type echoArgs struct {
	Text  string `json:"text" jsonschema:"what to say"`
	Times int    `json:"times,omitempty" jsonschema:"how many times to say it, once if not given"`
}

type echoResult struct {
	Said string `json:"said"`
}

func echo(_ context.Context, _ *sdk.CallToolRequest, args echoArgs) (*sdk.CallToolResult, echoResult, error) {
	return nil, echoResult{Said: strings.Repeat(args.Text, max(args.Times, 1))}, nil
}

type headersResult struct {
	Headers map[string]string `json:"headers"`
}

func headers(_ context.Context, req *sdk.CallToolRequest, _ struct{}) (*sdk.CallToolResult, headersResult, error) {
	found := map[string]string{}
	for name, values := range req.Extra.Header {
		found[name] = strings.Join(values, ", ")
	}
	return nil, headersResult{Headers: found}, nil
}

type timeArgs struct {
	Zone string `json:"zone,omitempty" jsonschema:"an IANA time zone, such as Europe/London; UTC if not given"`
}

type timeResult struct {
	Time string `json:"time"`
	Zone string `json:"zone"`
}

func now(_ context.Context, _ *sdk.CallToolRequest, args timeArgs) (*sdk.CallToolResult, timeResult, error) {
	zone := args.Zone
	if zone == "" {
		zone = "UTC"
	}
	location, err := time.LoadLocation(zone)
	if err != nil {
		// An error from the handler is reported as the tool's error.
		return nil, timeResult{}, fmt.Errorf("%q is not a time zone", zone)
	}
	return nil, timeResult{Time: time.Now().In(location).Format(time.RFC3339), Zone: zone}, nil
}

type progressArgs struct {
	Steps int `json:"steps,omitempty" jsonschema:"how many steps to work through, 5 if not given"`
	Delay int `json:"delay,omitempty" jsonschema:"how long each step takes, in milliseconds, 500 if not given"`
}

type progressResult struct {
	Steps int `json:"steps"`
}

// progress sends a notification of its progress, and logs a message, for
// each step. A client is only sent its progress if it gave a progress
// token, and messages logged at the level it asked for and above.
func progress(ctx context.Context, req *sdk.CallToolRequest, args progressArgs) (*sdk.CallToolResult, progressResult, error) {
	steps, delay := args.Steps, time.Duration(args.Delay)*time.Millisecond
	if steps <= 0 {
		steps = 5
	}
	if delay <= 0 {
		delay = 500 * time.Millisecond
	}
	token := req.Params.GetProgressToken()
	for step := 1; step <= steps; step++ {
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			log.Printf("progress given up at step %d", step)
			return nil, progressResult{}, ctx.Err()
		}
		message := fmt.Sprintf("Step %d of %d done", step, steps)
		if token != nil {
			req.Session.NotifyProgress(ctx, &sdk.ProgressNotificationParams{ProgressToken: token, Progress: float64(step), Total: float64(steps), Message: message})
		}
		req.Session.Log(ctx, &sdk.LoggingMessageParams{Level: "info", Logger: "progress", Data: message})
	}
	return nil, progressResult{Steps: steps}, nil
}

// logLevels logs a message at each level, the last as JSON.
func logLevels(ctx context.Context, req *sdk.CallToolRequest, _ struct{}) (*sdk.CallToolResult, map[string]int, error) {
	levels := []sdk.LoggingLevel{"debug", "info", "notice", "warning", "error", "critical", "alert"}
	for _, level := range levels {
		req.Session.Log(ctx, &sdk.LoggingMessageParams{Level: level, Logger: "levels", Data: "a message at the level " + string(level)})
	}
	req.Session.Log(ctx, &sdk.LoggingMessageParams{Level: "emergency", Logger: "levels", Data: map[string]any{"structured": true, "level": "emergency"}})
	return nil, map[string]int{"logged": len(levels) + 1}, nil
}

// bookFlight asks the user for the details of a flight in a form with a
// field of each kind there is, and says what they answered. A stateless
// server asks by returning what it wants from the user, and is called again
// with the answer.
func bookFlight(_ context.Context, req *sdk.CallToolRequest, _ struct{}) (*sdk.CallToolResult, map[string]any, error) {
	answer, ok := req.Params.InputResponses["flight"].(*sdk.ElicitResult)
	if !ok {
		one, nine, three := 1.0, 9.0, 3
		return &sdk.CallToolResult{InputRequests: sdk.InputRequestMap{"flight": &sdk.ElicitParams{
			Message: "Where would you like to fly to, and how?",
			RequestedSchema: &jsonschema.Schema{
				Type: "object",
				Properties: map[string]*jsonschema.Schema{
					"destination": {Type: "string", Title: "Destination", Description: "A city or the code of an airport", MinLength: &three},
					"email":       {Type: "string", Title: "Email", Format: "email", Description: "Where the tickets are sent"},
					"date":        {Type: "string", Title: "Date", Format: "date"},
					"passengers":  {Type: "integer", Title: "Passengers", Minimum: &one, Maximum: &nine, Default: json.RawMessage("1")},
					"budget":      {Type: "number", Title: "Budget", Description: "The most to spend, in pounds"},
					"class": {Type: "string", Title: "Class", Default: json.RawMessage(`"economy"`),
						Enum: []any{"economy", "premium", "business", "first"}, Extra: map[string]any{"enumNames": []any{"Economy", "Premium economy", "Business", "First"}}},
					"window": {Type: "boolean", Title: "Window seat", Default: json.RawMessage("true")},
					"extras": {Type: "array", Title: "Extras", Items: &jsonschema.Schema{AnyOf: []*jsonschema.Schema{
						{Const: constant("bag"), Title: "Checked bag"},
						{Const: constant("meal"), Title: "Meal"},
						{Const: constant("lounge"), Title: "Lounge"},
					}}},
				},
				Required: []string{"destination", "email"},
			},
		}}}, nil, nil
	}
	log.Printf("book_flight answered %s with %v", answer.Action, answer.Content)
	return nil, map[string]any{"action": answer.Action, "booking": answer.Content}, nil
}

// constant returns a value for the const of a schema.
func constant(v any) *any { return &v }

// confirmDelete asks the user to say yes or no, with a form of no fields.
func confirmDelete(_ context.Context, req *sdk.CallToolRequest, _ struct{}) (*sdk.CallToolResult, map[string]any, error) {
	answer, ok := req.Params.InputResponses["confirm"].(*sdk.ElicitResult)
	if !ok {
		return &sdk.CallToolResult{InputRequests: sdk.InputRequestMap{"confirm": &sdk.ElicitParams{
			Message:         "Delete report-2026.pdf? This cannot be undone.",
			RequestedSchema: &jsonschema.Schema{Type: "object"},
		}}}, nil, nil
	}
	deleted := answer.Action == "accept"
	return nil, map[string]any{"action": answer.Action, "deleted": deleted}, nil
}

type summariseArgs struct {
	Text string `json:"text,omitempty" jsonschema:"the text to summarise; a paragraph of its own if not given"`
}

// summarise asks for a summary from the client's language model, which in
// TriggerAPI is the user, and gives back what it said. Declining fails the
// call.
func summarise(_ context.Context, req *sdk.CallToolRequest, args summariseArgs) (*sdk.CallToolResult, map[string]any, error) {
	answer, ok := req.Params.InputResponses["summary"].(*sdk.CreateMessageWithToolsResult)
	if !ok {
		text := args.Text
		if text == "" {
			text = "TriggerAPI is a desktop client for REST, GraphQL, gRPC, MCP and WebSocket APIs, written in Go with a Qt 6 interface. " +
				"It keeps requests in workspaces, encrypts them with SSH keys, and records what was sent in a history."
		}
		return &sdk.CallToolResult{InputRequests: sdk.InputRequestMap{"summary": &sdk.CreateMessageParams{
			SystemPrompt: "You summarise text in one sentence.",
			Messages:     []*sdk.SamplingMessage{{Role: "user", Content: &sdk.TextContent{Text: "Summarise this:\n\n" + text}}},
			MaxTokens:    100,
			Temperature:  0.2,
			ModelPreferences: &sdk.ModelPreferences{
				Hints:         []*sdk.ModelHint{{Name: "claude-haiku-5-5"}, {Name: "claude"}},
				SpeedPriority: 0.8,
			},
		}}}, nil, nil
	}
	var said []string
	for _, content := range answer.Content {
		if text, ok := content.(*sdk.TextContent); ok {
			said = append(said, text.Text)
		}
	}
	log.Printf("summarise was answered by %s", answer.Model)
	return nil, map[string]any{"summary": strings.Join(said, ""), "model": answer.Model}, nil
}

// text returns a handler of a resource which is the text given.
func text(content string) sdk.ResourceHandler {
	return func(_ context.Context, req *sdk.ReadResourceRequest) (*sdk.ReadResourceResult, error) {
		return &sdk.ReadResourceResult{Contents: []*sdk.ResourceContents{{URI: req.Params.URI, Text: content}}}, nil
	}
}

// authorized has every request give the bearer token, if there is one.
func authorized(token string, next http.Handler) http.Handler {
	if token == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			log.Printf("refused %s: no bearer token, or the wrong one", r.Method)
			http.Error(w, "a bearer token is needed", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// logged logs the JSON-RPC method of each request, and what it calls.
func logged(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		r.Body = io.NopCloser(bytes.NewReader(body))

		var message struct {
			Method string `json:"method"`
			Params struct {
				Name string `json:"name"`
				URI  string `json:"uri"`
			} `json:"params"`
		}
		line := r.Method
		if json.Unmarshal(body, &message) == nil && message.Method != "" {
			line += " " + message.Method
			if what := message.Params.Name + message.Params.URI; what != "" && slices.Contains([]string{"tools/call", "resources/read", "prompts/get"}, message.Method) {
				line += " " + what
			}
		}
		if session := r.Header.Get("Mcp-Session-Id"); session != "" {
			line += " (session " + session + ")"
		}
		log.Print(line)

		next.ServeHTTP(w, r)
	})
}

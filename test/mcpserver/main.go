// Command mcpserver is a small MCP server to try the MCP requests of
// TriggerAPI against by hand. It serves Streamable HTTP at /mcp, with a few
// tools, resources and a prompt, and logs every call it is given.
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

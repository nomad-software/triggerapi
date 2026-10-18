package ui

import (
	"context"
	"strings"

	"github.com/mappu/miqt/qt6/mainthread"

	"github.com/nomad-software/triggerapi/internal/assert"
	"github.com/nomad-software/triggerapi/internal/collection"
	"github.com/nomad-software/triggerapi/internal/history"
	"github.com/nomad-software/triggerapi/internal/mcp"
)

// How the window talks to MCP servers: through one client at a time, shared
// by the calls and the discovery of what the server has. See mcp.Client for
// why it is kept: a server is stateless, but one from before protocol
// version 2026-07-28 gives the client a session.

// mcpClientFor returns the client of the server of a request, which has had
// its placeholders replaced. The client there is is used if it is of the
// same server, asked in the same way; otherwise it is closed and another
// made, which connects when it is first used.
func (w *Window) mcpClientFor(r mcp.Request) *mcp.Client {
	server := r.Server()
	if w.mcpClient != nil && w.mcpServer == server {
		return w.mcpClient
	}
	w.closeMCP()
	// The token of OAuth 2.0 is given to each HTTP request, as it can be
	// refreshed while the client is in use.
	w.mcpClient, w.mcpServer = mcp.NewClient(w.client, r, w.backgroundAuthorizer()), server
	return w.mcpClient
}

// closeMCP closes the client of an MCP server, if there is one, which ends
// the session of an older server. That is done in the background, so that a
// server which does not answer holds nothing up.
func (w *Window) closeMCP() {
	if w.mcpClient != nil {
		go w.mcpClient.Close()
		w.mcpClient, w.mcpServer = nil, ""
	}
}

// callMCP calls the tool, resource or prompt of the MCP request, or cancels
// the request in flight.
func (w *Window) callMCP() {
	request := w.mcp.request()
	req := request.Expand(w.expander())
	client := w.mcpClientFor(req)
	entry := w.historyEntry(w.requestUI(collection.TypeMCP), req.URL)

	listener := w.mcpListener(strings.TrimSpace(req.URL))

	// A token that needs the user to sign in is got before the call, which
	// the client then finds.
	w.run(w.mcp.call, w.mcpResponse, entry, w.authorizer(w.requestUI(collection.TypeMCP), &req.Auth), w.timeout(), func(ctx context.Context) (outcome, error) {
		res, err := client.Call(ctx, req, listener)
		var facts *assert.Facts
		if res != nil {
			f := mcpFacts(res)
			facts = &f
		}

		// Values are captured from the result of a call that succeeded.
		var captured []captureResult
		return outcome{
			capture: func() {
				if res.OK() {
					captured = w.capture(request.Captures, string(res.Result))
				}
			},
			show: func() {
				w.mcpResponse.showMCP(res)
				w.mcpResponse.showCaptured(captured)
			},
			record: func(e *history.Entry) { e.MCP = res },
			checks: request.Assertions,
			facts:  facts,
		}, err
	})
}

// mcpListener returns what is told what the server sends while a call is
// under way, which is shown while the request on show is the one called, and
// asks the user what the server asks, naming the server as it is addressed.
// What is asked is given up with the call.
func (w *Window) mcpListener(server string) *mcp.Listener {
	shown := w.shown
	return &mcp.Listener{
		Event: func(e mcp.Event) {
			mainthread.Start(func() {
				if shown == w.shown {
					w.mcpResponse.noteEvent(e)
				}
			})
		},
		Elicit: func(ctx context.Context, e *mcp.Elicitation) (*mcp.ElicitAnswer, error) {
			answer, ok := askUser(ctx, func(done func(*mcp.ElicitAnswer)) func() {
				return askElicitation(w.QWidget, server, e, done)
			})
			if !ok {
				return &mcp.ElicitAnswer{Action: mcp.ActionCancel}, nil
			}
			return answer, nil
		},
		Sample: func(ctx context.Context, s *mcp.Sampling) (*mcp.SampleAnswer, error) {
			answer, ok := askUser(ctx, func(done func(*mcp.SampleAnswer)) func() {
				return askSampling(w.QWidget, server, s, done)
			})
			switch {
			case !ok:
				return nil, ctx.Err()
			case answer == nil:
				return nil, mcp.ErrDeclined
			}
			return answer, nil
		},
	}
}

// askUser opens what asks the user something, on the main thread, and waits
// for the answer it is given, or for ctx to be done, when it is closed with
// the function it returned and false is reported.
func askUser[T any](ctx context.Context, open func(done func(T)) (close func())) (T, bool) {
	answers := make(chan T, 1)
	var shut func()
	mainthread.Wait(func() {
		shut = open(func(answer T) {
			select {
			case answers <- answer:
			default:
			}
		})
	})
	select {
	case answer := <-answers:
		return answer, true
	case <-ctx.Done():
		mainthread.Start(shut)
		var none T
		return none, false
	}
}

// discoverMCP loads what the server of the MCP request has, to offer its
// tools, resources and prompts. What was loaded before is used again unless
// force is set.
func (w *Window) discoverMCP(force bool) {
	w.mcp.status.set("")
	req := w.mcp.request().Expand(w.expand)
	if strings.TrimSpace(req.URL) == "" {
		// There is no server to ask yet.
		w.mcpSchemas.drop()
		return
	}

	client := w.mcpClientFor(req)
	w.mcpSchemas.load(req.Server(), force, client.Discover, func(schema *mcp.Schema, err error) {
		if err != nil {
			w.mcp.setSchemaError(err)
			return
		}
		w.mcp.setSchema(schema)
		w.syncEndpoints()
	})
}

// useMCPExample replaces the arguments of the MCP request with an example
// for what it calls, made from what the server says it takes.
func (w *Window) useMCPExample() {
	item, ok := w.mcp.chosen()
	if !ok {
		w.mcp.status.set("An example needs the capabilities of the server, which have not been loaded. Enter the server address and press Reload.")
		return
	}
	example, err := item.Example()
	if err != nil {
		w.mcp.status.set("An example could not be made: " + err.Error())
		return
	}
	w.mcp.arguments.SetPlainText(example)
}

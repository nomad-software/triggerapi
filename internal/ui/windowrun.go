package ui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	qt "github.com/mappu/miqt/qt6"
	"github.com/mappu/miqt/qt6/mainthread"

	"github.com/nomad-software/triggerapi/internal/assert"
	"github.com/nomad-software/triggerapi/internal/capture"
	"github.com/nomad-software/triggerapi/internal/collection"
	"github.com/nomad-software/triggerapi/internal/graphql"
	"github.com/nomad-software/triggerapi/internal/history"
	"github.com/nomad-software/triggerapi/internal/network"
	"github.com/nomad-software/triggerapi/internal/rest"
	"github.com/nomad-software/triggerapi/internal/rpc"
)

// How the window sends a request and shows what comes back, loads the
// service definitions of gRPC servers and the schemas of GraphQL ones, and
// makes requests of the endpoints of a server.

// options returns how requests are sent, as the settings say.
func (w *Window) options() rest.Options {
	return rest.Options{Timeout: w.timeout(), NoRedirects: w.config.NoRedirects, Network: w.net}
}

// timeoutError is the error of a request that took longer than the limit of
// the settings, which is a number of seconds.
func timeoutError(limit int) error {
	return fmt.Errorf("timed out after %s, which is the limit in the settings", plural(limit, "second"))
}

// timeout returns how long a request can take, which is zero for no limit.
func (w *Window) timeout() time.Duration {
	return time.Duration(w.config.Timeout) * time.Second
}

// withLimit returns a context which is done when ctx is, or once limit has
// passed, unless limit is zero, which is no limit.
func withLimit(ctx context.Context, limit time.Duration) (context.Context, context.CancelFunc) {
	if limit > 0 {
		return context.WithTimeout(ctx, limit)
	}
	return context.WithCancel(ctx)
}

// outcome is what is done, on the main thread, with the result of the work
// of a request that ended without an error.
type outcome struct {
	// capture sets variables of the active environment from the result.
	// It is called once, even if the result is no longer wanted on show.
	capture func()
	// show shows the result. It is called again whenever the tab of the
	// request is gone back to.
	show func()
	// record keeps the result in an entry of the history.
	record func(*history.Entry)
	// checks are the assertions of the request, and facts what of the
	// result they check, or nil if there is nothing of it to check.
	checks []assert.Check
	facts  *assert.Facts
}

// run does the work of a button which sends a request (Send, Invoke, Call or
// Execute) in the background, or cancels the work already in flight. The
// work returns what to do with its result, which is done on the main
// thread. Unless it is cancelled, what came of it is kept in the entry of the
// history, which is then added to the history; there is none if it is nil.
// The request is first given its authorization by authorize, unless that is
// nil (see Window.authorizer), and is not sent if it cannot be. The work is
// then given up once it has taken as long as limit, unless that is zero,
// when it keeps to the time limit of the settings itself, as rest.Send does
// to the headers of a stream of events.
func (w *Window) run(button *qt.QPushButton, response *responsePanel, entry *history.Entry, authorize func(context.Context) error, limit time.Duration, work func(context.Context) (outcome, error)) {
	if w.cancel != nil {
		w.cancel()
		return
	}

	base, cancel := context.WithCancel(context.Background())
	w.cancel = cancel
	seconds := w.config.Timeout
	shown := w.shown
	tab := w.tabs.current()

	label := button.Text()
	button.SetText("Cancel")
	response.clear()

	// Qt widgets may only be touched from the main thread, so the result is
	// handed back to it.
	go func() {
		// Signing in takes as long as the user does, so the time of the
		// request starts once it has its authorization.
		var err error
		if authorize != nil {
			err = authorize(base)
		}
		start := time.Now()
		var done outcome
		if err == nil {
			// The work is given up on when it has taken as long as it
			// is allowed, which is the time limit of the settings.
			ctx, stop := withLimit(base, limit)
			done, err = work(ctx)
			stop()
		}
		err = network.Explain(err)
		elapsed := time.Since(start)

		mainthread.Start(func() {
			cancel()
			w.cancel = nil
			button.SetText(label)

			// The request was made for what it captures as much as for
			// what it shows, so that is kept whatever is on show by now.
			// The response is then checked, with what was captured.
			var asserted []assert.Result
			if err == nil {
				done.capture()
				if done.facts != nil {
					asserted = w.runAssertions(done.checks, *done.facts)
				}
			}
			// It is kept in the history likewise, unless it was given up.
			if entry != nil && !errors.Is(err, context.Canceled) {
				entry.Duration = elapsed
				switch {
				case errors.Is(err, context.DeadlineExceeded):
					entry.Error = timeoutError(seconds).Error()
				case err != nil:
					entry.Error = err.Error()
				default:
					done.record(entry)
					entry.Assertions = asserted
				}
				w.history.add(entry)
			}

			var result func()
			switch {
			case shown != w.shown:
				// Another request has been shown since, so the result is
				// no longer wanted.
				return
			case errors.Is(err, context.Canceled):
				result = func() { response.showCancelled(elapsed) }
			case errors.Is(err, context.DeadlineExceeded):
				timedOut := timeoutError(seconds)
				result = func() { response.showError(timedOut, elapsed) }
			case err != nil:
				result = func() { response.showError(err, elapsed) }
			default:
				result = func() {
					done.show()
					response.showAsserted(asserted)
				}
			}

			// The tab keeps the result, to show it again when the tab is
			// gone back to.
			result()
			tab.replay = result
		})
	}()
}

// send starts the REST request, or cancels the request in flight.
func (w *Window) send() {
	w.request.url.commit()
	request := w.request.request()
	req := request.Expand(w.expander())
	req.Options = w.options()
	req.Options.Stream = w.streamer(w.response)
	ui := w.requestUI(collection.TypeREST)
	entry := w.historyEntry(ui, rest.JoinURL(req.URL, req.Params))

	w.run(w.request.send, w.response, entry, w.authorizer(ui, &req.Auth), 0, func(ctx context.Context) (outcome, error) {
		res, err := rest.Send(ctx, w.client, req)
		return w.restOutcome(res, request.Captures, request.Assertions, w.response, w.response.show), err
	})
}

// executeCurl makes the REST request by running the curl command that is
// shown for it, or cancels the request in flight.
func (w *Window) executeCurl() {
	request := w.request.request()
	// What runs is the command as it is shown: a value made by a function
	// would not be the same if it were made again.
	req := w.request.commanded
	ui := w.requestUI(collection.TypeREST)
	entry := w.historyEntry(ui, rest.JoinURL(req.URL, req.Params))

	w.run(w.request.execute, w.response, entry, w.authorizer(ui, &req.Auth), w.timeout(), func(ctx context.Context) (outcome, error) {
		res, err := rest.RunCurl(ctx, req)
		return w.restOutcome(res, request.Captures, request.Assertions, w.response, w.response.show), err
	})
}

// streamer returns what shows a stream of server-sent events in a response
// panel as it comes, while the request on show is the one sent (see
// rest.Options.Stream). Events can come faster than they can be shown, so
// only the latest of what came while the main thread was busy is.
func (w *Window) streamer(response *responsePanel) func(*rest.Response) {
	shown := w.shown
	var pending atomic.Bool
	var latest atomic.Pointer[rest.Response]
	return func(res *rest.Response) {
		latest.Store(res)
		if !pending.CompareAndSwap(false, true) {
			return
		}
		mainthread.Start(func() {
			pending.Store(false)
			if shown == w.shown {
				response.showStream(latest.Load(), true)
			}
		})
	}
}

// restOutcome returns what to do with the response to a request sent over
// HTTP, which is nil if there was an error instead: show shows it in the
// response panel. Values are captured from a response with a status that is
// a success, or for a stream of events from the last event, and the checks
// are of any response.
func (w *Window) restOutcome(res *rest.Response, captures []capture.Rule, checks []assert.Check, response *responsePanel, show func(*rest.Response)) outcome {
	var captured []captureResult
	return outcome{
		checks: checks,
		facts:  factsOf(res, httpFacts),
		capture: func() {
			if res.StatusCode >= 200 && res.StatusCode < 300 {
				captured = w.capture(captures, res.Document())
			}
		},
		show: func() {
			show(res)
			response.showCaptured(captured)
		},
		record: func(e *history.Entry) { e.HTTP = res },
	}
}

// sendGraphQL starts the GraphQL request, or cancels the request in flight.
// It is sent as a REST request, with the same options.
func (w *Window) sendGraphQL() {
	request := w.graphql.request()
	expanded := request.Expand(w.expander())
	req, err := expanded.HTTP()
	req.Options = w.options()
	req.Options.Stream = w.streamer(w.graphqlResponse)
	ui := w.requestUI(collection.TypeGraphQL)
	entry := w.historyEntry(ui, expanded.URL)

	w.run(w.graphql.send, w.graphqlResponse, entry, w.authorizer(ui, &req.Auth), 0, func(ctx context.Context) (outcome, error) {
		if err != nil {
			return outcome{}, err
		}
		res, err := rest.Send(ctx, w.client, req)
		return w.restOutcome(res, request.Captures, request.Assertions, w.graphqlResponse, w.graphqlResponse.showGraphQL), err
	})
}

// loadGraphQLSchema loads the schema of the server the GraphQL request is
// for in the background, with the headers and authorization of the request,
// and shows it in its Schema tab, and has the query completed from it. One
// loaded before is used again unless force is set.
func (w *Window) loadGraphQLSchema(force bool) {
	w.graphql.setSchema(nil)
	req := w.graphql.request().Expand(w.expand)
	target := strings.TrimSpace(req.URL)
	if target == "" {
		// There is no server to ask yet.
		w.graphSchemas.drop()
		return
	}

	authorize := w.backgroundAuthorizer()
	w.graphSchemas.load(target, force, func(ctx context.Context) (*graphql.Schema, error) {
		if err := authorize(ctx, &req.Auth); err != nil {
			return nil, err
		}
		return graphql.Introspect(ctx, w.client, req)
	}, func(schema *graphql.Schema, err error) {
		if err != nil {
			w.graphql.setSchemaError(err)
			return
		}
		w.graphql.setSchema(schema)
	})
}

// invoke starts the gRPC request, or cancels the request in flight.
func (w *Window) invoke() {
	request := w.grpc.request()
	req := request.Expand(w.expander())
	req.Network = w.net

	// The service definition is loaded by the call if it has not been yet.
	var schema *rpc.Schema
	if target, err := req.Target(); err == nil {
		schema, _ = w.schemas.get(target)
	}

	ui := w.requestUI(collection.TypeGRPC)
	entry := w.historyEntry(ui, req.URL)

	w.run(w.grpc.invoke, w.grpcResponse, entry, w.authorizer(ui, &req.Auth), w.timeout(), func(ctx context.Context) (outcome, error) {
		res, err := rpc.Invoke(ctx, req, schema)

		// Values are captured from the last message of a call that ended
		// with OK, which is the only message of a unary method.
		var captured []captureResult
		return outcome{
			capture: func() {
				switch {
				case !res.OK():
				case len(res.Messages) == 0:
					captured = failedCaptures(request.Captures, errors.New("there was no response message"))
				default:
					captured = w.capture(request.Captures, res.Messages[len(res.Messages)-1])
				}
			},
			show: func() {
				w.grpcResponse.showRPC(res)
				w.grpcResponse.showCaptured(captured)
			},
			record: func(e *history.Entry) { e.GRPC = res },
			checks: request.Assertions,
			facts:  factsOf(res, rpcFacts),
		}, err
	})
}

// endpointCollection returns the folder of requests made from endpoints that
// the request on show is in, if it is of the type of a request UI which has
// endpoints, or nil if it is in none. That is the folder which holds the
// folder the request is in, when that one is named after a heading of the
// endpoints of the server, such as a service of a gRPC one. What the outer
// folder is called makes no difference.
func (w *Window) endpointCollection(ui *requestUI) *collection.Folder {
	if w.open == nil || w.open.Type != ui.typ || ui.endpoints == nil {
		return nil
	}
	heading := w.requests.store.FolderOf(w.open)
	if heading == nil || !ui.endpoints.table.hasService(heading.Name) {
		return nil
	}
	outer, _ := w.requests.store.ParentOf(heading)
	return outer
}

// syncEndpoints has the endpoints which the collection of the request on
// show has requests for ticked in the table of endpoints of its type.
func (w *Window) syncEndpoints() {
	for _, ui := range w.uis {
		if ui.endpoints == nil {
			continue
		}
		if outer := w.endpointCollection(ui); outer != nil {
			ui.endpoints.table.setExisting(ui.endpoints.kind.existing(outer), true)
		} else {
			ui.endpoints.table.setExisting(nil, false)
		}
	}
}

// createRequests makes a request of the type of a request UI for each of the
// endpoints, for the server of the request on show, and shows them in the
// sidebar. They are added to the collection that request is in, and to no
// other. If it is in none they are a new folder at the top level, named
// after the server.
func (w *Window) createRequests(ui *requestUI, endpoints []string) {
	url := ui.endpoints.url()
	if outer := w.endpointCollection(ui); outer != nil {
		ui.endpoints.kind.add(outer, url, endpoints)
		w.requests.extended(w.open)
		w.sidebar.SetCurrentIndex(tabRequests)
		return
	}

	name := strings.TrimSpace(url)
	if name == "" {
		name = "New Folder"
	}

	w.requests.adopt(ui.endpoints.kind.folder(name, url, endpoints))
	w.sidebar.SetCurrentIndex(tabRequests)
}

// reflect loads the service definition of the server the gRPC request is
// for, to offer its methods. One loaded before is used again unless force is
// set.
func (w *Window) reflect(force bool) {
	w.grpc.status.set("")
	req := w.grpc.request().Expand(w.expand)
	req.Network = w.net
	target, err := req.Target()
	if err != nil {
		// There is no server to ask yet.
		w.schemas.drop()
		return
	}

	authorize := w.backgroundAuthorizer()
	w.schemas.load(target, force, func(ctx context.Context) (*rpc.Schema, error) {
		if err := authorize(ctx, &req.Auth); err != nil {
			return nil, err
		}
		return rpc.Reflect(ctx, req)
	}, func(schema *rpc.Schema, err error) {
		if err != nil {
			w.grpc.setSchemaError(err)
			return
		}
		w.grpc.setSchema(schema)
		w.syncEndpoints()
	})
}

// useExample replaces the message of the gRPC request with an example for
// the selected method.
func (w *Window) useExample() {
	req := w.grpc.request().Expand(w.expand)

	target, _ := req.Target()
	schema, ok := w.schemas.get(target)
	if !ok {
		w.grpc.status.set("An example needs the service definition, which has not been loaded. Enter the server address and press Reload.")
		return
	}

	example, err := schema.Example(req.Method)
	if err != nil {
		w.grpc.status.set("An example could not be made: " + err.Error())
		return
	}
	w.grpc.message.SetPlainText(example)
}

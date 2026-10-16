package ui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	qt "github.com/mappu/miqt/qt6"
	"github.com/mappu/miqt/qt6/mainthread"

	"github.com/nomad-software/triggerapi/internal/capture"
	"github.com/nomad-software/triggerapi/internal/collection"
	"github.com/nomad-software/triggerapi/internal/graphql"
	"github.com/nomad-software/triggerapi/internal/rest"
	"github.com/nomad-software/triggerapi/internal/rpc"
)

// How the window sends a request and shows what comes back, and loads the
// service definitions of gRPC servers and the schemas of GraphQL ones.

// options returns how requests are sent, as the settings say.
func (w *Window) options() rest.Options {
	return rest.Options{Timeout: w.timeout(), NoRedirects: w.config.NoRedirects}
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

// outcome is what is done, on the main thread, with the result of the work
// of a request that ended without an error.
type outcome struct {
	// capture sets variables of the active environment from the result.
	// It is called once, even if the result is no longer wanted on show.
	capture func()
	// show shows the result. It is called again whenever the tab of the
	// request is gone back to.
	show func()
}

// run does the work of the Send or Invoke button in the background, or
// cancels the work already in flight. The work returns what to do with its
// result, which is done on the main thread.
func (w *Window) run(button *qt.QPushButton, response *responsePanel, work func(context.Context) (outcome, error)) {
	if w.cancel != nil {
		w.cancel()
		return
	}

	// The work is given up on when it has taken as long as the settings
	// allow, which is the same for every kind of request.
	limit := w.config.Timeout
	var ctx context.Context
	var cancel context.CancelFunc
	if limit > 0 {
		ctx, cancel = context.WithTimeout(context.Background(), w.timeout())
	} else {
		ctx, cancel = context.WithCancel(context.Background())
	}
	w.cancel = cancel
	shown := w.shown
	tab := w.tabs.current()

	label := button.Text()
	button.SetText("Cancel")
	response.clear()
	start := time.Now()

	// Qt widgets may only be touched from the main thread, so the result is
	// handed back to it.
	go func() {
		done, err := work(ctx)
		elapsed := time.Since(start)

		mainthread.Start(func() {
			cancel()
			w.cancel = nil
			button.SetText(label)

			// The request was made for what it captures as much as for
			// what it shows, so that is kept whatever is on show by now.
			if err == nil {
				done.capture()
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
				timedOut := timeoutError(limit)
				result = func() { response.showError(timedOut, elapsed) }
			case err != nil:
				result = func() { response.showError(err, elapsed) }
			default:
				result = done.show
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

	w.run(w.request.send, w.response, func(ctx context.Context) (outcome, error) {
		res, err := rest.Send(ctx, w.client, req)
		return w.restOutcome(res, request.Captures, w.response, w.response.show), err
	})
}

// executeCurl makes the REST request by running the curl command that is
// shown for it, or cancels the request in flight.
func (w *Window) executeCurl() {
	request := w.request.request()
	// What runs is the command as it is shown: a value made by a function
	// would not be the same if it were made again.
	req := w.request.commanded

	w.run(w.request.execute, w.response, func(ctx context.Context) (outcome, error) {
		res, err := rest.RunCurl(ctx, req)
		return w.restOutcome(res, request.Captures, w.response, w.response.show), err
	})
}

// restOutcome returns what to do with the response to a request sent over
// HTTP, which is nil if there was an error instead: show shows it in the
// response panel. Values are captured from a response with a status that is
// a success.
func (w *Window) restOutcome(res *rest.Response, captures []capture.Rule, response *responsePanel, show func(*rest.Response)) outcome {
	var captured []captureResult
	return outcome{
		capture: func() {
			if res.StatusCode >= 200 && res.StatusCode < 300 {
				captured = w.capture(captures, string(res.Body))
			}
		},
		show: func() {
			show(res)
			response.showCaptured(captured)
		},
	}
}

// sendGraphQL starts the GraphQL request, or cancels the request in flight.
// It is sent as a REST request, with the same options.
func (w *Window) sendGraphQL() {
	request := w.graphql.request()
	req, err := request.Expand(w.expander()).HTTP()
	req.Options = w.options()

	w.run(w.graphql.send, w.graphqlResponse, func(ctx context.Context) (outcome, error) {
		if err != nil {
			return outcome{}, err
		}
		res, err := rest.Send(ctx, w.client, req)
		return w.restOutcome(res, request.Captures, w.graphqlResponse, w.graphqlResponse.showGraphQL), err
	})
}

// loadGraphQLSchema loads the schema of the server the GraphQL request is
// for in the background, with the headers and authorization of the request,
// and shows it in its Schema tab. One loaded before is used again unless
// force is set.
func (w *Window) loadGraphQLSchema(force bool) {
	w.introspecting++
	introspecting := w.introspecting

	req := w.graphql.request().Expand(w.expand)
	target := strings.TrimSpace(req.URL)
	if target == "" {
		// There is no server to ask yet.
		w.graphql.setSchema("")
		return
	}
	if sdl, ok := w.graphSchemas[target]; ok && !force {
		w.graphql.setSchema(sdl)
		return
	}

	w.graphql.setSchema("")

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), reflectTimeout)
		defer cancel()

		sdl, err := graphql.Introspect(ctx, w.client, req)

		mainthread.Start(func() {
			if err == nil {
				w.graphSchemas[target] = sdl
			}
			// A later request for a schema has replaced this one.
			if introspecting != w.introspecting {
				return
			}
			if err != nil {
				w.graphql.setSchemaError(err)
				return
			}
			w.graphql.setSchema(sdl)
		})
	}()
}

// invoke starts the gRPC request, or cancels the request in flight.
func (w *Window) invoke() {
	request := w.grpc.request()
	req := request.Expand(w.expander())

	// The service definition is loaded by the call if it has not been yet.
	var schema *rpc.Schema
	if target, err := req.Target(); err == nil {
		schema = w.schemas[target]
	}

	w.run(w.grpc.invoke, w.grpcResponse, func(ctx context.Context) (outcome, error) {
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
		}, err
	})
}

// endpointCollection returns the folder of requests made from endpoints that
// the gRPC request on show is in, or nil if it is in none. That is the
// folder which holds the folder the request is in, when that one is named
// after a service of the server. What the outer folder is called makes no
// difference.
func (w *Window) endpointCollection() *collection.Folder {
	if w.open == nil || w.open.Type != collection.TypeGRPC {
		return nil
	}
	service := w.requests.store.FolderOf(w.open)
	if service == nil || !w.grpc.endpoints.hasService(service.Name) {
		return nil
	}
	outer, _ := w.requests.store.ParentOf(service)
	return outer
}

// syncEndpoints has the endpoints which the collection of the request on
// show has requests for ticked in the Service Definitions tab.
func (w *Window) syncEndpoints() {
	if outer := w.endpointCollection(); outer != nil {
		w.grpc.endpoints.setExisting(endpointsOf(outer), true)
		return
	}
	w.grpc.endpoints.setExisting(nil, false)
}

// createRequests makes a gRPC request for each of the methods, for the
// server of the request on show, and shows them in the sidebar. They are
// added to the collection that request is in, and to no other. If it is in
// none they are a new folder at the top level, named after the server.
func (w *Window) createRequests(methods []string) {
	url := w.grpc.url.Text()
	if outer := w.endpointCollection(); outer != nil {
		addEndpoints(outer, url, methods)
		w.requests.extended(w.open)
		w.sidebar.SetCurrentIndex(tabRequests)
		return
	}

	name := strings.TrimSpace(url)
	if name == "" {
		name = "New Folder"
	}

	w.requests.adopt(endpointFolder(name, url, methods))
	w.sidebar.SetCurrentIndex(tabRequests)
}

// reflect loads the service definition of the server the gRPC request is
// for, to offer its methods. One loaded before is used again unless force is
// set.
func (w *Window) reflect(force bool) {
	w.reflecting++
	reflecting := w.reflecting

	req := w.grpc.request().Expand(w.expand)
	target, err := req.Target()
	if err != nil {
		// There is no server to ask yet.
		w.grpc.status.set("")
		return
	}

	if schema, ok := w.schemas[target]; ok && !force {
		w.grpc.setSchema(schema)
		w.syncEndpoints()
		return
	}

	w.grpc.status.set("")

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), reflectTimeout)
		defer cancel()

		schema, err := rpc.Reflect(ctx, req)

		mainthread.Start(func() {
			if err == nil {
				w.schemas[target] = schema
			}
			// A later request for a definition has replaced this one.
			if reflecting != w.reflecting {
				return
			}
			if err != nil {
				w.grpc.setSchemaError(err)
				return
			}
			w.grpc.setSchema(schema)
			w.syncEndpoints()
		})
	}()
}

// useExample replaces the message of the gRPC request with an example for
// the selected method.
func (w *Window) useExample() {
	req := w.grpc.request().Expand(w.expand)

	target, _ := req.Target()
	schema, ok := w.schemas[target]
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

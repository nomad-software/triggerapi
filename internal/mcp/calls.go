//lint:file-ignore SA1019 Sampling and logging are deprecated since protocol version 2026-07-28, but servers still use them while they are.

package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// How a client keeps what a server sends while a call is under way, and
// answers what it asks.
//
// The client is told of it by the handlers the SDK is given, which belong to
// the client and not to a call, so each is matched to its call: progress by
// the progress token the call was sent with, and what is asked, by a
// stateless server, by the context of the call, which the SDK answers it in.
// An older server asks through its session, and its messages logged come
// with nothing to tell their call by, so those go to the latest call under
// way, as there is seldom more than one.

// call is a call under way: what its server has sent of it, and who is
// told.
type call struct {
	// ctx is that of the call, which what is asked of it is given up
	// with.
	ctx      context.Context
	start    time.Time
	token    string
	listener *Listener

	mu     sync.Mutex
	events []Event
}

// callKey is the key of the call in its context.
type callKey struct{}

// begin returns a call under way, with ctx, which is told to the listener.
func (c *Client) begin(ctx context.Context, listener *Listener) *call {
	call := &call{
		ctx:      ctx,
		start:    time.Now(),
		token:    fmt.Sprintf("triggerapi-%d", c.tokens.Add(1)),
		listener: listener,
	}
	c.live.Lock()
	c.calls = append(c.calls, call)
	c.live.Unlock()
	return call
}

// end takes a call off those under way, after which nothing more is kept
// of it.
func (c *Client) end(done *call) {
	c.live.Lock()
	defer c.live.Unlock()
	c.calls = slices.DeleteFunc(c.calls, func(call *call) bool { return call == done })
}

// find returns the call a context is of, if it is still under way, or else
// the latest call under way, or nil if there is none. An older server asks in
// the context of its session, which has the values of the context of the
// call it was connected for, which may have ended.
func (c *Client) find(ctx context.Context) *call {
	of, _ := ctx.Value(callKey{}).(*call)
	c.live.Lock()
	defer c.live.Unlock()
	if slices.Contains(c.calls, of) {
		return of
	}
	if len(c.calls) == 0 {
		return nil
	}
	return c.calls[len(c.calls)-1]
}

// byToken returns the call under way that was sent with a progress token,
// or nil if there is none.
func (c *Client) byToken(token any) *call {
	c.live.Lock()
	defer c.live.Unlock()
	for _, call := range c.calls {
		if call.token == fmt.Sprint(token) {
			return call
		}
	}
	return nil
}

// statelessProtocol is the version of the protocol from which servers are
// stateless, and sessions deprecated.
const statelessProtocol = "2026-07-28"

// logLevel is the least important level of the messages a server is asked
// to send while a call is under way, which is all of them.
const logLevel = "debug"

// askToLog asks a server from before the protocol was stateless, which has
// a session, to send every message it logs. A stateless one is asked with
// each request (see prepare).
func askToLog(ctx context.Context, conn *sdk.ClientSession) {
	info := conn.InitializeResult()
	if info != nil && info.ProtocolVersion < statelessProtocol && info.Capabilities != nil && info.Capabilities.Logging != nil {
		_ = conn.SetLoggingLevel(ctx, &sdk.SetLoggingLevelParams{Level: logLevel})
	}
}

// prepare gives the parameters of a call its progress token, so that the
// server sends its progress, and asks a stateless server for every message
// it logs.
func (call *call) prepare(params sdk.RequestParams) {
	params.SetProgressToken(call.token)
	params.GetMeta()[sdk.MetaKeyLogLevel] = logLevel
}

// add keeps an event of the call and tells the listener of it.
func (call *call) add(e Event) {
	e.At = time.Since(call.start)
	call.mu.Lock()
	call.events = append(call.events, e)
	call.mu.Unlock()
	if call.listener != nil && call.listener.Event != nil {
		call.listener.Event(e)
	}
}

// settle waits for the events that came ahead of the answer to the call,
// which the SDK hands to a queue of their own that the answer can overtake:
// until no more has come for a moment, and no longer than settleLimit.
func (call *call) settle() {
	count := func() int {
		call.mu.Lock()
		defer call.mu.Unlock()
		return len(call.events)
	}
	deadline := time.Now().Add(settleLimit)
	for n := count(); time.Now().Before(deadline); {
		time.Sleep(settleTime)
		m := count()
		if m == n {
			return
		}
		n = m
	}
}

// settleTime is how long no event has to have come for those of a call to
// be taken to have all come, and settleLimit the longest that is waited.
const (
	settleTime  = 5 * time.Millisecond
	settleLimit = 100 * time.Millisecond
)

// kept returns the events of the call so far, which are not changed after.
func (call *call) kept() []Event {
	call.mu.Lock()
	defer call.mu.Unlock()
	return call.events[:len(call.events):len(call.events)]
}

// asking returns a context for answering what is asked of a call in ctx,
// which is also given up when the call is.
func (call *call) asking(ctx context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(call.ctx, cancel)
	return ctx, func() {
		stop()
		cancel()
	}
}

// options returns the options of the SDK's client, with the handlers of
// what a server sends and asks while a call is under way.
func (c *Client) options() *sdk.ClientOptions {
	return &sdk.ClientOptions{
		ProgressNotificationHandler: func(_ context.Context, req *sdk.ProgressNotificationClientRequest) {
			if call := c.byToken(req.Params.ProgressToken); call != nil {
				call.add(Event{Kind: EventProgress, Message: req.Params.Message, Progress: req.Params.Progress, Total: req.Params.Total})
			}
		},
		LoggingMessageHandler: func(ctx context.Context, req *sdk.LoggingMessageRequest) {
			if call := c.find(ctx); call != nil {
				call.add(Event{Kind: EventLogged, Level: string(req.Params.Level), Logger: req.Params.Logger, Message: logText(req.Params.Data)})
			}
		},
		ElicitationHandler:   c.elicit,
		CreateMessageHandler: c.sample,
	}
}

// logText returns what a message logged says: its text, or its data as
// JSON.
func logText(data any) string {
	if text, ok := data.(string); ok {
		return text
	}
	encoded, err := json.Marshal(data)
	if err != nil {
		return fmt.Sprint(data)
	}
	return string(encoded)
}

// elicit asks the listener of the call what the server asks the user for.
// It is declined if the call has no one to ask, or asks the user to go to a
// page, which is not done.
func (c *Client) elicit(ctx context.Context, req *sdk.ElicitRequest) (*sdk.ElicitResult, error) {
	call := c.find(ctx)
	if call == nil {
		return &sdk.ElicitResult{Action: ActionDecline}, nil
	}
	event := Event{Kind: EventElicitation, Message: req.Params.Message, Outcome: ActionDecline}
	if req.Params.Mode == "url" {
		event.Message = strings.TrimSpace(event.Message + "\nAsked to go to " + req.Params.URL + ", which is not done.")
		call.add(event)
		return &sdk.ElicitResult{Action: ActionDecline}, nil
	}

	asked, err := elicitation(req.Params)
	if err != nil {
		return nil, err
	}
	answer := &ElicitAnswer{Action: ActionDecline}
	if call.listener != nil && call.listener.Elicit != nil {
		ctx, stop := call.asking(ctx)
		answer, err = call.listener.Elicit(ctx, asked)
		stop()
		if err != nil {
			return nil, err
		}
	}
	event.Outcome = answer.Action
	if answer.Action == ActionAccept && answer.Content != nil {
		event.Answer, _ = json.Marshal(answer.Content)
	}
	call.add(event)
	return &sdk.ElicitResult{Action: answer.Action, Content: answer.Content}, nil
}

// declined is the error a request for sampling is answered with when the
// user declines it, which the specification gives the code -1.
var declined = &jsonrpc.Error{Code: -1, Message: "User rejected sampling request"}

// sample asks the listener of the call for the message the server asks a
// language model for. It is declined if the call has no one to ask.
//
//lint:ignore SA1019 servers still ask for it, while sampling is deprecated.
func (c *Client) sample(ctx context.Context, req *sdk.CreateMessageRequest) (*sdk.CreateMessageResult, error) {
	call := c.find(ctx)
	if call == nil {
		return nil, declined
	}
	asked := sampling(req.Params)
	event := Event{Kind: EventSampling, Outcome: SamplingDeclined}
	if n := len(asked.Messages); n > 0 {
		event.Message = asked.Messages[n-1].Text
	}

	if call.listener == nil || call.listener.Sample == nil {
		call.add(event)
		return nil, declined
	}
	ctx, stop := call.asking(ctx)
	answer, err := call.listener.Sample(ctx, asked)
	stop()
	switch {
	case errors.Is(err, ErrDeclined):
		call.add(event)
		return nil, declined
	case err != nil:
		return nil, err
	}

	event.Outcome = SamplingReplied
	event.Answer, _ = json.Marshal(map[string]string{"model": answer.Model, "text": answer.Text})
	call.add(event)
	stopReason := answer.StopReason
	if stopReason == "" {
		stopReason = "endTurn"
	}
	return &sdk.CreateMessageResult{
		Content:    &sdk.TextContent{Text: answer.Text},
		Model:      answer.Model,
		Role:       "assistant",
		StopReason: stopReason,
	}, nil
}

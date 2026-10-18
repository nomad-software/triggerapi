package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/nomad-software/triggerapi/internal/mcp/mcptest"
)

// TestEvents checks that what a server sends while a call is under way is
// given to the listener and kept with the response, and that what it asks is
// answered by the listener, by servers of both kinds.
func TestEvents(t *testing.T) {
	for _, server := range servers {
		t.Run(server.name, func(t *testing.T) {
			srv := server.start(t)
			c := testClient(t, Request{URL: srv.URL + "/mcp"})
			ctx := testContext(t)

			var mu sync.Mutex
			var told []Event
			var asked *Elicitation
			var sampled *Sampling
			listener := &Listener{
				Event: func(e Event) {
					mu.Lock()
					told = append(told, e)
					mu.Unlock()
				},
				Elicit: func(_ context.Context, e *Elicitation) (*ElicitAnswer, error) {
					asked = e
					return &ElicitAnswer{Action: ActionAccept, Content: map[string]any{"name": "Ada", "age": 36}}, nil
				},
				Sample: func(_ context.Context, s *Sampling) (*SampleAnswer, error) {
					sampled = s
					return &SampleAnswer{Text: "Hello!", Model: "by-hand"}, nil
				},
			}
			call := func(name, arguments string, listener *Listener) (*Response, error) {
				return c.Call(ctx, Request{URL: srv.URL + "/mcp", Name: name, Arguments: arguments}, listener)
			}

			// Progress, and messages logged.
			res, err := call("progress", `{"steps": 3}`, listener)
			if err != nil || !res.OK() {
				t.Fatalf("progress: %v %s", err, res.Text())
			}
			var progress, logged []string
			for _, e := range res.Events {
				switch e.Kind {
				case EventProgress:
					progress = append(progress, e.Fraction()+" "+e.Message)
				case EventLogged:
					logged = append(logged, e.Level+" "+e.Logger+" "+e.Message)
				}
			}
			if want := "1/3 (33%) step 1, 2/3 (67%) step 2, 3/3 (100%) step 3"; strings.Join(progress, ", ") != want {
				t.Errorf("progress %q, want %q", progress, want)
			}
			if want := "info counter step 1, info counter step 2, info counter step 3"; strings.Join(logged, ", ") != want {
				t.Errorf("logged %q, want %q", logged, want)
			}
			mu.Lock()
			if len(told) != len(res.Events) {
				t.Errorf("the listener was told %d events, and %d were kept", len(told), len(res.Events))
			}
			mu.Unlock()

			// Elicitation, answered by the listener.
			res, err = call("who", "", listener)
			if err != nil || !res.OK() {
				t.Fatalf("who: %v %s", err, res.Text())
			}
			var result struct {
				StructuredContent mcptest.WhoResult `json:"structuredContent"`
			}
			if json.Unmarshal(res.Result, &result) != nil || result.StructuredContent != (mcptest.WhoResult{Action: ActionAccept, Name: "Ada", Age: 36}) {
				t.Errorf("who %s", res.Result)
			}
			if asked == nil || asked.Message != "Who are you?" || len(asked.Fields) != 2 || asked.Fields[1].Name != "name" || !asked.Fields[1].Required {
				t.Errorf("asked %+v", asked)
			}
			if last := res.Events[len(res.Events)-1]; last.Kind != EventElicitation || last.Outcome != ActionAccept || !strings.Contains(string(last.Answer), `"Ada"`) {
				t.Errorf("event %+v", last)
			}

			// Without a listener what is asked is declined.
			res, err = call("who", "", nil)
			if err != nil || !strings.Contains(string(res.Result), `"action":"decline"`) {
				t.Errorf("who without a listener: %v %s", err, res.Result)
			}

			// Sampling, answered by the listener.
			res, err = call("sample", "", listener)
			if err != nil || !strings.Contains(string(res.Result), `"text":"Hello!"`) || !strings.Contains(string(res.Result), `"model":"by-hand"`) {
				t.Fatalf("sample: %v %s", err, res.Result)
			}
			if sampled == nil || sampled.SystemPrompt != "Be brief." || len(sampled.Messages) != 1 || sampled.Messages[0].Text != "Say hello." || sampled.MaxTokens != 50 {
				t.Errorf("sampled %+v", sampled)
			}
			if last := res.Events[len(res.Events)-1]; last.Kind != EventSampling || last.Outcome != SamplingReplied || last.Message != "Say hello." {
				t.Errorf("event %+v", last)
			}

			// Declining sampling fails the call.
			declining := &Listener{Sample: func(context.Context, *Sampling) (*SampleAnswer, error) { return nil, ErrDeclined }}
			if res, err := call("sample", "", declining); err == nil && res.OK() {
				t.Errorf("declined sampling: %s", res.Result)
			}
		})
	}
}

// TestElicitCancelled checks that what is asked is given up with the call.
func TestElicitCancelled(t *testing.T) {
	srv := mcptest.NewServer(t)
	c := testClient(t, Request{URL: srv.URL + "/mcp"})
	ctx, cancel := context.WithCancel(testContext(t))
	listener := &Listener{Elicit: func(ctx context.Context, _ *Elicitation) (*ElicitAnswer, error) {
		cancel()
		<-ctx.Done()
		return &ElicitAnswer{Action: ActionCancel}, nil
	}}
	if _, err := c.Call(ctx, Request{URL: srv.URL + "/mcp", Name: "who"}, listener); err == nil {
		t.Error("a call given up while asking succeeded")
	}
}

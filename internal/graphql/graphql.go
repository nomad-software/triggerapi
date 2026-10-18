// Package graphql is the GraphQL engine: a GraphQL request is a query, with
// its variables, sent over HTTP as JSON to one endpoint. It is built on the
// rest package, which sends it. It has no dependency on the UI.
package graphql

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/nomad-software/triggerapi/internal/assert"
	"github.com/nomad-software/triggerapi/internal/capture"
	"github.com/nomad-software/triggerapi/internal/rest"
)

// Request is a saved GraphQL request: the endpoint, the headers and
// authorization of the HTTP request, the query and its variables, and what
// to capture from the response.
type Request struct {
	URL     string          `json:"url"`
	Headers []rest.KeyValue `json:"headers,omitempty"`
	Auth    rest.Auth       `json:"auth"`
	// Query is the GraphQL document: a query, a mutation or a
	// subscription, which is sent as it is.
	Query string `json:"query,omitempty"`
	// Variables are the values of the variables of the query, as the text
	// of a JSON object. Empty is none.
	Variables string `json:"variables,omitempty"`
	// Captures are as those of a rest.Request.
	Captures []capture.Rule `json:"captures,omitempty"`
	// Assertions are as those of a rest.Request.
	Assertions []assert.Check `json:"assertions,omitempty"`
}

// Expand applies f to every part of the request that can hold a placeholder.
// The captures are left as they are.
func (r Request) Expand(f func(string) string) Request {
	r.URL = f(r.URL)
	r.Headers = rest.ExpandPairs(r.Headers, f)
	r.Auth = r.Auth.Expand(f)
	r.Query = f(r.Query)
	r.Variables = f(r.Variables)
	return r
}

// Body returns what is sent: a JSON object holding the query and, if there
// are any, the variables. Variables that are not a JSON object are an error.
func (r Request) Body() (string, error) {
	payload := struct {
		Query     string          `json:"query"`
		Variables json.RawMessage `json:"variables,omitempty"`
	}{Query: r.Query}

	if text := strings.TrimSpace(r.Variables); text != "" {
		var variables map[string]any
		if err := json.Unmarshal([]byte(text), &variables); err != nil {
			return "", fmt.Errorf("the variables are not a JSON object: %w", err)
		}
		if variables != nil {
			payload.Variables = json.RawMessage(text)
		}
	}

	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// HTTP returns the request as the HTTP request that is sent: a POST of the
// body to the endpoint, with the headers and authorization of the request.
func (r Request) HTTP() (rest.Request, error) {
	if strings.TrimSpace(r.Query) == "" {
		return rest.Request{}, errors.New("the query is empty")
	}
	body, err := r.Body()
	if err != nil {
		return rest.Request{}, err
	}
	return rest.Request{
		Method:   http.MethodPost,
		URL:      r.URL,
		Headers:  r.Headers,
		Auth:     r.Auth,
		BodyType: rest.BodyJSON,
		Body:     body,
	}, nil
}

// Errors returns the messages of the errors a GraphQL response reports,
// which it can do with any HTTP status, joined into one. It is empty if the
// body is not a GraphQL response or reports none.
func Errors(body []byte) string {
	var response struct {
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if json.Unmarshal(body, &response) != nil {
		return ""
	}
	var messages []string
	for _, e := range response.Errors {
		if message := strings.TrimSpace(e.Message); message != "" {
			messages = append(messages, message)
		}
	}
	return strings.Join(messages, "; ")
}

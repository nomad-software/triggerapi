// Package rest builds and sends HTTP requests. It has no dependency on the UI.
package rest

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/nomad-software/triggerapi/internal/capture"
	"github.com/nomad-software/triggerapi/internal/network"
)

// Methods lists the HTTP methods offered by the client.
var Methods = []string{
	http.MethodGet,
	http.MethodPost,
	http.MethodPut,
	http.MethodPatch,
	http.MethodDelete,
	http.MethodHead,
	http.MethodOptions,
}

// KeyValue is a single query parameter, header or form field.
type KeyValue struct {
	Key     string `json:"key"`
	Value   string `json:"value"`
	Enabled bool   `json:"enabled"`
}

// AuthType selects how a request is authorised. The zero value means none.
type AuthType string

const (
	AuthNone   AuthType = "none"
	AuthBasic  AuthType = "basic"
	AuthBearer AuthType = "bearer"
	AuthOAuth2 AuthType = "oauth2"
)

// Auth holds the credentials for a request.
type Auth struct {
	Type     AuthType `json:"type,omitempty"`
	Username string   `json:"username,omitempty"`
	Password string   `json:"password,omitempty"`
	Token    string   `json:"token,omitempty"`
	// OAuth says how a token is got for AuthOAuth2. It is nil if none of
	// it has been filled in, as in a file from before there was OAuth.
	OAuth *OAuth `json:"oauth,omitempty"`

	// Access is the access token OAuth 2.0 gave, which is sent as a bearer
	// token. It is never saved: whoever sends the request fills it in
	// (see the oauth package), and until then there is no Authorization
	// header.
	Access string `json:"-"`
}

// BodyType selects what is sent as the request body. The zero value means
// none.
type BodyType string

const (
	BodyNone BodyType = "none"
	BodyJSON BodyType = "json"
	BodyText BodyType = "text"
	BodyXML  BodyType = "xml"
	BodyForm BodyType = "form"
)

// ContentType returns the Content-Type header that goes with the body type.
func (t BodyType) ContentType() string {
	switch t {
	case BodyJSON:
		return "application/json"
	case BodyText:
		return "text/plain"
	case BodyXML:
		return "application/xml"
	case BodyForm:
		return "application/x-www-form-urlencoded"
	}
	return ""
}

// Request describes an HTTP request as entered by the user. It holds
// everything in the request UI, including fields the chosen auth and body
// types do not use, so it can be saved and restored without loss.
type Request struct {
	Method  string     `json:"method"`
	URL     string     `json:"url"`
	Params  []KeyValue `json:"params,omitempty"`
	Headers []KeyValue `json:"headers,omitempty"`
	Auth    Auth       `json:"auth"`

	// Body is sent for the raw body types and Form for BodyForm. The body
	// type's content type is used unless the user sets that header.
	BodyType BodyType   `json:"bodyType,omitempty"`
	Body     string     `json:"body,omitempty"`
	Form     []KeyValue `json:"form,omitempty"`

	// Captures are the values to take from the response and keep in
	// variables of the active environment, as the capture package reads
	// them. Placeholders in them are not replaced.
	Captures []capture.Rule `json:"captures,omitempty"`

	// Options say how the request is sent. They are not part of the
	// request as it is saved: whoever sends it gives them.
	Options Options `json:"-"`
}

// Options say how a request is sent, whatever the request is.
type Options struct {
	// Timeout is how long the request can take, to the end of its
	// response, or for a stream of server-sent events, which can go on for
	// as long as the server likes, to its headers. There is no limit if it
	// is zero.
	Timeout time.Duration
	// NoRedirects has a response that redirects to another address
	// returned as it is, where the address would otherwise be followed.
	NoRedirects bool
	// Network is what the request is sent with, which the curl command
	// follows too, and which says what proxy it went through. Send is
	// given the client that uses it. Nil is the default of each.
	Network *network.Client
	// Stream is given a stream of server-sent events as it comes: the
	// response so far once its headers have come, and again after each
	// event. It is called on the goroutine of Send, and the response is not
	// changed after. It is not called if it is nil.
	Stream func(*Response)
}

// Response is the result of sending a request. It is kept in the history
// as JSON.
type Response struct {
	Status     string        `json:"status"`
	StatusCode int           `json:"statusCode"`
	Proto      string        `json:"proto,omitempty"`
	Headers    http.Header   `json:"headers,omitempty"`
	Body       []byte        `json:"body,omitempty"`
	Duration   time.Duration `json:"duration"`
	// Timing says where the time went, if it is known.
	Timing *Timing `json:"timing,omitempty"`
	// Events are those of a stream of server-sent events, in the order
	// they came, and nil for any other response. The body is the stream as
	// it was received.
	Events []Event `json:"events,omitempty"`
	// Interrupted says why a stream of events ended before the server
	// ended it: StreamStopped if it was stopped, or the error there was. It
	// is empty otherwise.
	Interrupted string `json:"interrupted,omitempty"`
}

// StreamStopped is the Interrupted of a stream of events that was stopped
// before the server ended it.
const StreamStopped = "Stopped"

// Expand returns a copy of the credentials with f applied to each of them.
func (a Auth) Expand(f func(string) string) Auth {
	a.Username = f(a.Username)
	a.Password = f(a.Password)
	a.Token = f(a.Token)
	if a.OAuth != nil {
		o := a.OAuth.Expand(f)
		a.OAuth = &o
	}
	return a
}

// Settings returns how an OAuth 2.0 token is got, which is nothing if none
// of it has been filled in.
func (a Auth) Settings() OAuth {
	if a.OAuth == nil {
		return OAuth{}
	}
	return *a.OAuth
}

// Header returns the value of the Authorization header for the credentials.
// It reports false if the auth type does not use one.
func (a Auth) Header() (string, bool) {
	switch a.Type {
	case AuthBasic:
		credentials := base64.StdEncoding.EncodeToString([]byte(a.Username + ":" + a.Password))
		return "Basic " + credentials, true
	case AuthBearer:
		return "Bearer " + a.Token, true
	case AuthOAuth2:
		if a.Access != "" {
			return "Bearer " + a.Access, true
		}
	}
	return "", false
}

// SortPairs puts the pairs in alphabetical order of their keys, ignoring
// case, with those that have no key last. Pairs with the same key keep their
// order, as which comes last can matter.
func SortPairs(pairs []KeyValue) {
	slices.SortStableFunc(pairs, ComparePairs)
}

// ComparePairs gives the order SortPairs puts two pairs in.
func ComparePairs(a, b KeyValue) int {
	if (a.Key == "") != (b.Key == "") {
		if a.Key == "" {
			return 1
		}
		return -1
	}
	return strings.Compare(strings.ToLower(a.Key), strings.ToLower(b.Key))
}

// ExpandPairs returns a copy of the pairs with f applied to every key and
// value.
func ExpandPairs(pairs []KeyValue, f func(string) string) []KeyValue {
	out := make([]KeyValue, len(pairs))
	for i, p := range pairs {
		out[i] = KeyValue{Key: f(p.Key), Value: f(p.Value), Enabled: p.Enabled}
	}
	return out
}

// Expand returns a copy of the request with f applied to every field the user
// can type into. It is used to substitute environment variables.
func (r Request) Expand(f func(string) string) Request {
	r.URL = f(r.URL)
	r.Params = ExpandPairs(r.Params, f)
	r.Headers = ExpandPairs(r.Headers, f)
	r.Auth = r.Auth.Expand(f)
	r.Body = f(r.Body)
	r.Form = ExpandPairs(r.Form, f)

	return r
}

// Build converts the request into an *http.Request.
func (r Request) Build(ctx context.Context) (*http.Request, error) {
	u, err := r.buildURL()
	if err != nil {
		return nil, err
	}

	content := r.content()
	var body io.Reader
	if content != "" {
		body = strings.NewReader(content)
	}

	req, err := http.NewRequestWithContext(ctx, r.Method, u, body)
	if err != nil {
		return nil, err
	}

	if content != "" {
		req.Header.Set("Content-Type", r.BodyType.ContentType())
	}

	if value, ok := r.Auth.Header(); ok {
		req.Header.Set("Authorization", value)
	}

	// User supplied headers take precedence over generated ones.
	seen := map[string]bool{}
	for _, h := range r.Headers {
		if !h.Enabled || h.Key == "" {
			continue
		}
		key := http.CanonicalHeaderKey(h.Key)
		if key == "Host" {
			req.Host = h.Value
			continue
		}
		if !seen[key] {
			req.Header.Del(key)
			seen[key] = true
		}
		req.Header.Add(key, h.Value)
	}

	return req, nil
}

// content returns the body to send for the body type.
func (r Request) content() string {
	switch r.BodyType {
	case BodyJSON, BodyText, BodyXML:
		return r.Body
	case BodyForm:
		return EncodeForm(r.Form)
	}
	return ""
}

// buildURL adds a default scheme and appends the enabled params to the query,
// preserving the order in which they were entered.
func (r Request) buildURL() (string, error) {
	raw := strings.TrimSpace(r.URL)
	if raw == "" {
		return "", errors.New("URL is empty")
	}
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}

	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("unsupported scheme %q", u.Scheme)
	}
	if u.Host == "" {
		return "", errors.New("URL has no host")
	}

	if query := encode(r.Params); query != "" {
		if u.RawQuery != "" {
			u.RawQuery += "&"
		}
		u.RawQuery += query
	}

	return u.String(), nil
}

// EncodeForm encodes the enabled fields as an
// application/x-www-form-urlencoded body.
func EncodeForm(fields []KeyValue) string {
	return encode(fields)
}

func encode(pairs []KeyValue) string {
	var parts []string
	for _, p := range pairs {
		if !p.Enabled || p.Key == "" {
			continue
		}
		parts = append(parts, url.QueryEscape(p.Key)+"="+url.QueryEscape(p.Value))
	}
	return strings.Join(parts, "&")
}

// Send performs the request using the passed client and reads the whole
// response body. Duration covers the time until the body is fully read, and
// Timing says how it was spent. The options of the request say how long it
// can take and if redirects are followed; a request that takes too long fails
// with context.DeadlineExceeded.
//
// A stream of server-sent events is read as it comes, and given to the
// Stream of the options, until the server ends it or ctx is done, when it is
// returned as it is, with how it ended as its Interrupted. Its time limit is
// on its headers alone.
func Send(ctx context.Context, client *http.Client, r Request) (*Response, error) {
	// The time limit is kept by a timer rather than the context, so that it
	// can be stopped once the headers of a stream have come.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var timedOut atomic.Bool
	var timer *time.Timer
	if r.Options.Timeout > 0 {
		timer = time.AfterFunc(r.Options.Timeout, func() {
			timedOut.Store(true)
			cancel()
		})
		defer timer.Stop()
	}
	limited := func(err error) error {
		if timedOut.Load() {
			return context.DeadlineExceeded
		}
		return err
	}

	start := time.Now()
	trace := newTracer(start)
	req, err := r.Build(httptrace.WithClientTrace(ctx, trace.trace()))
	if err != nil {
		return nil, err
	}

	if r.Options.NoRedirects {
		// The client is the caller's, so a copy is the one changed.
		single := *client
		single.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		client = &single
	}

	res, err := client.Do(req)
	if err != nil {
		return nil, limited(err)
	}
	defer res.Body.Close()

	response := func(body []byte, end time.Time) *Response {
		return &Response{
			Status:     res.Status,
			StatusCode: res.StatusCode,
			Proto:      res.Proto,
			Headers:    res.Header,
			Body:       body,
			Duration:   end.Sub(start),
			Timing:     r.Options.annotate(trace.timing(end, res), res.Request),
		}
	}

	if isEventStream(res.Header) {
		// The limit was on the headers, which have come, unless it was
		// reached just as they did.
		if timer != nil && !timer.Stop() {
			return nil, context.DeadlineExceeded
		}
		return readStream(ctx, res.Body, start, response, r.Options.Stream), nil
	}

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, limited(err)
	}
	return response(body, time.Now()), nil
}

// readStream reads a stream of server-sent events until it ends, or ctx is
// done, giving the response so far to stream, unless it is nil, once at the
// start and again after each event. response makes a response of the body
// read by a time.
func readStream(ctx context.Context, body io.Reader, start time.Time, response func([]byte, time.Time) *Response, stream func(*Response)) *Response {
	var raw bytes.Buffer
	var events []Event
	// Each response given is a slice of the events up to then, which are
	// not changed after, as appending puts the next beyond its length.
	snapshot := func(end time.Time) *Response {
		res := response(bytes.Clone(raw.Bytes()), end)
		res.Events = events[:len(events):len(events)]
		return res
	}
	if stream != nil {
		stream(snapshot(time.Now()))
	}
	err := readEvents(io.TeeReader(body, &raw), func(e Event) {
		e.At = time.Since(start)
		events = append(events, e)
		if stream != nil {
			stream(snapshot(time.Now()))
		}
	})

	res := snapshot(time.Now())
	switch {
	case ctx.Err() != nil:
		res.Interrupted = StreamStopped
	case err != nil:
		res.Interrupted = err.Error()
	}
	return res
}

// IsJSON reports if the response declares a JSON content type.
func (r *Response) IsJSON() bool {
	ct := strings.ToLower(r.Headers.Get("Content-Type"))
	return strings.Contains(ct, "json")
}

// Text returns the body for display, indenting it if it is valid JSON, or
// the events of a stream of server-sent events.
func (r *Response) Text() string {
	if r.IsEventStream() {
		return EventLog(r.StreamEvents())
	}
	if r.IsJSON() {
		var out bytes.Buffer
		if err := json.Indent(&out, r.Body, "", "  "); err == nil {
			return out.String()
		}
	}
	return string(r.Body)
}

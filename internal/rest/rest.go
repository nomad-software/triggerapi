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
	"net/url"
	"slices"
	"strings"
	"time"
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
)

// Auth holds the credentials for a request.
type Auth struct {
	Type     AuthType `json:"type,omitempty"`
	Username string   `json:"username,omitempty"`
	Password string   `json:"password,omitempty"`
	Token    string   `json:"token,omitempty"`
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
}

// Response is the result of sending a request.
type Response struct {
	Status     string
	StatusCode int
	Proto      string
	Headers    http.Header
	Body       []byte
	Duration   time.Duration
}

// Expand returns a copy of the credentials with f applied to each of them.
func (a Auth) Expand(f func(string) string) Auth {
	a.Username = f(a.Username)
	a.Password = f(a.Password)
	a.Token = f(a.Token)
	return a
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
	}
	return "", false
}

// SortPairs puts the pairs in alphabetical order of their keys, ignoring
// case, with those that have no key last. Pairs with the same key keep their
// order, as which comes last can matter.
func SortPairs(pairs []KeyValue) {
	slices.SortStableFunc(pairs, func(a, b KeyValue) int {
		return ComparePairs(a, b)
	})
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
// response body. Duration covers the time until the body is fully read.
func Send(ctx context.Context, client *http.Client, r Request) (*Response, error) {
	req, err := r.Build(ctx)
	if err != nil {
		return nil, err
	}

	start := time.Now()

	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}

	return &Response{
		Status:     res.Status,
		StatusCode: res.StatusCode,
		Proto:      res.Proto,
		Headers:    res.Header,
		Body:       body,
		Duration:   time.Since(start),
	}, nil
}

// IsJSON reports if the response declares a JSON content type.
func (r *Response) IsJSON() bool {
	ct := strings.ToLower(r.Headers.Get("Content-Type"))
	return strings.Contains(ct, "json")
}

// Text returns the body for display, indenting it if it is valid JSON.
func (r *Response) Text() string {
	if r.IsJSON() {
		var out bytes.Buffer
		if err := json.Indent(&out, r.Body, "", "  "); err == nil {
			return out.String()
		}
	}
	return string(r.Body)
}

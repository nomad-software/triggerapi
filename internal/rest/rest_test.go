package rest

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestBuildURL(t *testing.T) {
	tests := []struct {
		name    string
		req     Request
		want    string
		wantErr bool
	}{
		{name: "default scheme", req: Request{URL: "example.com/a"}, want: "http://example.com/a"},
		{name: "keeps https", req: Request{URL: " https://example.com "}, want: "https://example.com"},
		{
			name: "appends params in order",
			req: Request{URL: "example.com?z=1", Params: []KeyValue{
				{Key: "b", Value: "x y", Enabled: true},
				{Key: "skipped", Value: "1"},
				{Key: "", Value: "1", Enabled: true},
				{Key: "a", Value: "&", Enabled: true},
			}},
			want: "http://example.com?z=1&b=x+y&a=%26",
		},
		{name: "empty", req: Request{URL: "  "}, wantErr: true},
		{name: "bad scheme", req: Request{URL: "ftp://example.com"}, wantErr: true},
		{name: "no host", req: Request{URL: "http://"}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.req.buildURL()
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBuildHeaders(t *testing.T) {
	req, err := Request{
		Method:   http.MethodPost,
		URL:      "example.com",
		Body:     "{}",
		BodyType: BodyJSON,
		Auth:     Auth{Type: AuthBearer, Token: "abc"},
		Headers: []KeyValue{
			{Key: "content-type", Value: "text/plain", Enabled: true},
			{Key: "Accept", Value: "a", Enabled: true},
			{Key: "Accept", Value: "b", Enabled: true},
			{Key: "X-Off", Value: "1"},
			{Key: "Host", Value: "other.test", Enabled: true},
		},
	}.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if got := req.Header.Get("Content-Type"); got != "text/plain" {
		t.Errorf("Content-Type = %q", got)
	}
	if got := req.Header.Values("Accept"); len(got) != 2 {
		t.Errorf("Accept = %v", got)
	}
	if got := req.Header.Get("Authorization"); got != "Bearer abc" {
		t.Errorf("Authorization = %q", got)
	}
	if _, ok := req.Header["X-Off"]; ok {
		t.Error("disabled header was sent")
	}
	if req.Host != "other.test" {
		t.Errorf("Host = %q", req.Host)
	}
}

func TestBuildBody(t *testing.T) {
	form := []KeyValue{{Key: "a", Value: "b c", Enabled: true}}

	tests := []struct {
		name            string
		req             Request
		body, mediaType string
	}{
		{"json", Request{BodyType: BodyJSON, Body: "{}", Form: form}, "{}", "application/json"},
		{"form", Request{BodyType: BodyForm, Body: "{}", Form: form}, "a=b+c", "application/x-www-form-urlencoded"},
		{"none ignores body", Request{BodyType: BodyNone, Body: "{}", Form: form}, "", ""},
		{"zero value is none", Request{Body: "{}"}, "", ""},
		{"empty body has no content type", Request{BodyType: BodyJSON}, "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.req.Method = http.MethodPost
			tt.req.URL = "example.com"

			req, err := tt.req.Build(context.Background())
			if err != nil {
				t.Fatal(err)
			}

			var body []byte
			if req.Body != nil {
				body, _ = io.ReadAll(req.Body)
			}
			if string(body) != tt.body {
				t.Errorf("body = %q, want %q", body, tt.body)
			}
			if got := req.Header.Get("Content-Type"); got != tt.mediaType {
				t.Errorf("Content-Type = %q, want %q", got, tt.mediaType)
			}
		})
	}
}

func TestEncodeForm(t *testing.T) {
	got := EncodeForm([]KeyValue{
		{Key: "name", Value: "a b", Enabled: true},
		{Key: "off", Value: "1"},
		{Key: "x", Value: "=", Enabled: true},
	})
	if want := "name=a+b&x=%3D"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSend(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		user, pass, _ := r.BasicAuth()
		if r.Method != http.MethodPut || string(body) != "hello" || user != "u" || pass != "p" || r.URL.Query().Get("q") != "1" {
			t.Errorf("unexpected request: %s %s %q %s:%s", r.Method, r.URL, body, user, pass)
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusCreated)
		io.WriteString(w, `{"ok":true}`)
	}))
	defer srv.Close()

	res, err := Send(context.Background(), srv.Client(), Request{
		Method:   http.MethodPut,
		URL:      srv.URL,
		Params:   []KeyValue{{Key: "q", Value: "1", Enabled: true}},
		Auth:     Auth{Type: AuthBasic, Username: "u", Password: "p"},
		BodyType: BodyText,
		Body:     "hello",
	})
	if err != nil {
		t.Fatal(err)
	}

	if res.StatusCode != http.StatusCreated {
		t.Errorf("StatusCode = %d", res.StatusCode)
	}
	if want := "{\n  \"ok\": true\n}"; res.Text() != want {
		t.Errorf("Text = %q, want %q", res.Text(), want)
	}
}

func TestSendCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := Send(ctx, http.DefaultClient, Request{Method: http.MethodGet, URL: "example.com"}); err == nil {
		t.Error("expected an error")
	}
}

func TestTextInvalidJSON(t *testing.T) {
	res := &Response{Headers: http.Header{"Content-Type": {"application/json"}}, Body: []byte("{oops")}
	if res.Text() != "{oops" {
		t.Errorf("Text = %q", res.Text())
	}
}

func TestExpand(t *testing.T) {
	orig := Request{
		URL:     "a",
		Params:  []KeyValue{{Key: "a", Value: "a", Enabled: true}},
		Headers: []KeyValue{{Key: "a", Value: "a"}},
		Auth:    Auth{Username: "a", Password: "a", Token: "a"},
		Body:    "a",
		Form:    []KeyValue{{Key: "a", Value: "a"}},
	}

	got := orig.Expand(strings.ToUpper)

	want := Request{
		URL:     "A",
		Params:  []KeyValue{{Key: "A", Value: "A", Enabled: true}},
		Headers: []KeyValue{{Key: "A", Value: "A"}},
		Auth:    Auth{Username: "A", Password: "A", Token: "A"},
		Body:    "A",
		Form:    []KeyValue{{Key: "A", Value: "A"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
	if orig.Params[0].Key != "a" {
		t.Error("original request was modified")
	}
}

func TestSortPairs(t *testing.T) {
	pairs := []KeyValue{{Key: "b", Value: "1"}, {Key: ""}, {Key: "B", Value: "2"}, {Key: "a"}, {Key: "C"}}
	SortPairs(pairs)

	var got []string
	for _, p := range pairs {
		got = append(got, p.Key+p.Value)
	}
	if want := "a b1 B2 C "; strings.Join(got, " ") != want {
		t.Errorf("sorted = %q, want %q", strings.Join(got, " "), want)
	}
}

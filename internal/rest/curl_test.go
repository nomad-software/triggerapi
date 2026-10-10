package rest

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"
)

func TestCurl(t *testing.T) {
	tests := []struct {
		name    string
		request Request
		want    string
	}{
		{
			"get",
			Request{Method: "GET", URL: "api.test/users", Params: []KeyValue{{Key: "q", Value: "a b", Enabled: true}, {Key: "off", Value: "1"}}},
			`curl --location 'http://api.test/users?q=a+b'`,
		},
		{
			"head",
			Request{Method: "HEAD", URL: "https://api.test"},
			`curl --location --head https://api.test`,
		},
		{
			"json body",
			Request{
				Method:   "POST",
				URL:      "https://api.test/users",
				Headers:  []KeyValue{{Key: "x-key", Value: "it's", Enabled: true}, {Key: "Accept", Value: "", Enabled: true}, {Key: "Off", Value: "1"}},
				Auth:     Auth{Type: AuthBearer, Token: "abc"},
				BodyType: BodyJSON,
				Body:     "{\n  \"name\": \"O'Neil\"\n}",
			},
			"curl --location --request POST https://api.test/users \\\n" +
				"  --header 'Accept;' \\\n" +
				"  --header 'Authorization: Bearer abc' \\\n" +
				"  --header 'Content-Type: application/json' \\\n" +
				"  --header 'X-Key: it'\\''s' \\\n" +
				"  --data-raw '{\n  \"name\": \"O'\\''Neil\"\n}'",
		},
		{
			"basic authorization and a form",
			Request{
				Method:   "PUT",
				URL:      "https://api.test",
				Auth:     Auth{Type: AuthBasic, Username: "me", Password: "p w"},
				BodyType: BodyForm,
				Form:     []KeyValue{{Key: "a", Value: "1 2", Enabled: true}},
			},
			"curl --location --request PUT https://api.test \\\n" +
				"  --user 'me:p w' \\\n" +
				"  --header 'Content-Type: application/x-www-form-urlencoded' \\\n" +
				"  --data-raw a=1+2",
		},
		{
			"basic authorization that cannot be given as a name and password",
			Request{Method: "GET", URL: "https://api.test", Auth: Auth{Type: AuthBasic, Username: "a:b", Password: "c"}},
			"curl --location https://api.test \\\n  --header 'Authorization: Basic YTpiOmM='",
		},
		{
			"authorization replaced by a header",
			Request{
				Method:  "GET",
				URL:     "https://api.test",
				Auth:    Auth{Type: AuthBasic, Username: "a", Password: "b"},
				Headers: []KeyValue{{Key: "authorization", Value: "mine", Enabled: true}, {Key: "Host", Value: "other.test", Enabled: true}},
			},
			"curl --location https://api.test \\\n  --header 'Host: other.test' \\\n  --header 'Authorization: mine'",
		},
		{
			"placeholder left in the address",
			Request{Method: "GET", URL: "https://api.test/{{id}}"},
			`curl --location https://api.test/%7B%7Bid%7D%7D`,
		},
	}

	for _, test := range tests {
		got, err := test.request.Curl()
		if err != nil || got != test.want {
			t.Errorf("%s:\n got %s\nwant %s\n(%v)", test.name, got, test.want, err)
		}
	}

	if _, err := (Request{Method: "GET"}).Curl(); err == nil {
		t.Error("no error for a request without a URL")
	}
}

// TestCurlRuns runs a command with curl and checks that the server is sent
// what the request describes.
func TestCurlRuns(t *testing.T) {
	if _, err := exec.LookPath("curl"); err != nil {
		t.Skip("curl is not installed")
	}

	var got *http.Request
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		got, body = r, string(data)
	}))
	defer srv.Close()

	request := Request{
		Method:   "PATCH",
		URL:      srv.URL + "/a/[1]",
		Params:   []KeyValue{{Key: "q", Value: "x&y z", Enabled: true}},
		Headers:  []KeyValue{{Key: "X-Quote", Value: `it's "$HOME"`, Enabled: true}},
		Auth:     Auth{Type: AuthBasic, Username: "me", Password: "p'w"},
		BodyType: BodyJSON,
		Body:     "@{\n\t\"a\": 'b'\n}",
	}
	command, err := request.Curl()
	if err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("sh", "-c", command+" --silent").CombinedOutput(); err != nil {
		t.Fatalf("%v: %s\n%s", err, out, command)
	}

	if got == nil {
		t.Fatal("nothing was sent")
	}
	user, password, _ := got.BasicAuth()
	if got.Method != "PATCH" || got.URL.Path != "/a/[1]" || got.URL.Query().Get("q") != "x&y z" ||
		got.Header.Get("X-Quote") != `it's "$HOME"` || got.Header.Get("Content-Type") != "application/json" ||
		user != "me" || password != "p'w" || body != request.Body {
		t.Errorf("sent %s %s %v %q\n%s", got.Method, got.URL, got.Header, body, strings.TrimSpace(command))
	}
}

func TestRunCurl(t *testing.T) {
	if _, err := exec.LookPath("curl"); err != nil {
		t.Skip("curl is not installed")
	}

	release := make(chan struct{})
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/old":
			http.Redirect(w, r, srv.URL+"/new", http.StatusFound)
		case "/slow":
			<-release
		default:
			data, _ := io.ReadAll(r.Body)
			w.Header().Set("Content-Type", "application/json")
			w.Header().Add("X-Many", "1")
			w.Header().Add("X-Many", "2")
			w.WriteHeader(http.StatusCreated)
			w.Write([]byte(`{"path": "` + r.URL.Path + `", "key": "` + r.Header.Get("X-Key") + `", "body": "` + string(data) + `"}`))
		}
	}))
	defer srv.Close()
	defer close(release)

	// The response is the one after the redirect.
	res, err := RunCurl(context.Background(), Request{
		Method:  "GET",
		URL:     srv.URL + "/old",
		Headers: []KeyValue{{Key: "X-Key", Value: "it's", Enabled: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != 201 || res.Status != "201 Created" || res.Proto != "HTTP/1.1" || !res.IsJSON() ||
		len(res.Headers.Values("X-Many")) != 2 || string(res.Body) != `{"path": "/new", "key": "it's", "body": ""}` || res.Duration <= 0 {
		t.Errorf("response = %+v, body %s", res, res.Body)
	}

	// A body is sent.
	res, err = RunCurl(context.Background(), Request{Method: "POST", URL: srv.URL + "/a", BodyType: BodyText, Body: "hello"})
	if err != nil || string(res.Body) != `{"path": "/a", "key": "", "body": "hello"}` {
		t.Errorf("response = %+v, %v", res, err)
	}

	// A response to HEAD has headers and no body.
	res, err = RunCurl(context.Background(), Request{Method: "HEAD", URL: srv.URL + "/a"})
	if err != nil || res.StatusCode != 201 || len(res.Body) != 0 {
		t.Errorf("response = %+v, %v", res, err)
	}

	// What curl says of a failure is the error.
	unused := httptest.NewServer(nil)
	unused.Close()
	if _, err := RunCurl(context.Background(), Request{Method: "GET", URL: unused.URL}); err == nil || !strings.Contains(err.Error(), "curl: (7)") {
		t.Errorf("error = %v", err)
	}

	// A request can be cancelled.
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error)
	go func() {
		_, err := RunCurl(ctx, Request{Method: "GET", URL: srv.URL + "/slow"})
		done <- err
	}()
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v", err)
	}
}

func TestParseCurlHeaders(t *testing.T) {
	res, err := parseCurlHeaders("HTTP/1.1 100 Continue\r\n\r\nHTTP/2 404 \r\ncontent-type: text/plain\r\nx-a: b: c\r\n\r\n")
	if err != nil || res.StatusCode != 404 || res.Status != "404 Not Found" || res.Proto != "HTTP/2.0" ||
		res.Headers.Get("Content-Type") != "text/plain" || res.Headers.Get("X-A") != "b: c" {
		t.Errorf("response = %+v, %v", res, err)
	}

	if _, err := parseCurlHeaders(""); err == nil {
		t.Error("no error when there are no headers")
	}
}

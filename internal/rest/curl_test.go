package rest

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/nomad-software/triggerapi/internal/network"
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
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/old":
			// The address is that of this server, which the handler
			// cannot be given as the server is made with it.
			http.Redirect(w, r, "http://"+r.Host+"/new", http.StatusFound)
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
	// The times curl measured are given, and not taken for the body.
	if res.Timing == nil || res.Timing.Redirects != 1 || res.Timing.Address != strings.TrimPrefix(srv.URL, "http://") {
		t.Errorf("timing = %+v", res.Timing)
	} else if _, ok := res.Timing.Phase(StageRedirects); !ok {
		t.Errorf("phases = %+v", res.Timing.Phases)
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
	if _, err := RunCurl(context.Background(), Request{Method: "GET", URL: unused.URL}); err == nil || !strings.Contains(err.Error(), "curl: (7)") || strings.Contains(err.Error(), curlTimesMark) {
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

func TestCurlTiming(t *testing.T) {
	ms := time.Millisecond

	// Without a redirect each stage is given.
	message, timing := splitCurlTimes("\n" + curlTimesMark + " 0.001 0.003 0.010 0.011 0.050 0.060 0.000000 0 ::1 8080\n")
	if message != "" || timing == nil {
		t.Fatalf("message %q, timing %+v", message, timing)
	}
	want := []Phase{{StageDNS, 0, 1 * ms}, {StageConnect, 1 * ms, 3 * ms}, {StageTLS, 3 * ms, 10 * ms}, {StageWait, 11 * ms, 50 * ms}, {StageDownload, 50 * ms, 60 * ms}}
	if !reflect.DeepEqual(timing.Phases, want) || timing.Address != "[::1]:8080" || timing.Redirects != 0 {
		t.Errorf("timing = %+v", timing)
	}

	// Without TLS there is no handshake, and with redirects only what
	// follows them can be told apart.
	_, timing = splitCurlTimes(curlTimesMark + " 0.001 0.003 0 0.004 0.500 0.510 0.300 2 127.0.0.1 80")
	want = []Phase{{StageRedirects, 0, 300 * ms}, {StageWait, 300 * ms, 500 * ms}, {StageDownload, 500 * ms, 510 * ms}}
	if !reflect.DeepEqual(timing.Phases, want) || timing.Redirects != 2 {
		t.Errorf("timing = %+v", timing)
	}

	// The reason for a failure is kept apart from the times.
	message, timing = splitCurlTimes("curl: (7) Failed to connect\n\n" + curlTimesMark + " 0 0 0 0 0 0 0 0  0\n")
	if message != "curl: (7) Failed to connect" || timing == nil {
		t.Errorf("message %q, timing %+v", message, timing)
	}

	// Times that cannot be read are not given.
	if _, timing := splitCurlTimes(curlTimesMark + " x"); timing != nil {
		t.Errorf("timing = %+v", timing)
	}
}

func TestCurlNetwork(t *testing.T) {
	if _, err := exec.LookPath("curl"); err != nil {
		t.Skip("curl is not installed")
	}

	// The proxy answers in place of the server, if it is given the password.
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Proxy-Authorization") != "Basic "+base64.StdEncoding.EncodeToString([]byte("gary:p\"w")) {
			w.WriteHeader(http.StatusProxyAuthRequired)
			return
		}
		w.Write([]byte("proxied"))
	}))
	defer proxy.Close()
	secure := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("secure"))
	}))
	defer secure.Close()

	c, err := network.Load(network.Config{
		Proxy:   strings.Replace(proxy.URL, "http://", "http://gary@", 1),
		NoProxy: []string{"127.0.0.1"},
		Hosts:   []network.Host{{Pattern: "127.0.0.1", Insecure: true}},
	}, func(network.Secret) (string, error) { return `p"w`, nil })
	if err != nil {
		t.Fatal(err)
	}
	options := Options{Network: c, NoRedirects: true}

	// The command says how the server is reached, and never the password.
	command, err := Request{Method: "GET", URL: "http://example.invalid/", Options: options}.Curl()
	if err != nil || !strings.Contains(command, "--proxy "+proxy.URL) || !strings.Contains(command, "--proxy-user gary") ||
		!strings.Contains(command, "--noproxy 127.0.0.1") || strings.Contains(command, `p"w`) {
		t.Errorf("command = %s, %v", command, err)
	}
	if command, _ := (Request{Method: "GET", URL: secure.URL, Options: options}).Curl(); !strings.Contains(command, "--insecure") {
		t.Errorf("command = %s", command)
	}

	// Run, the password reaches the proxy, and what was used is recorded.
	res, err := RunCurl(context.Background(), Request{Method: "GET", URL: "http://example.invalid/", Options: options})
	if err != nil || string(res.Body) != "proxied" || res.Timing.Proxy != proxy.URL {
		t.Errorf("response %+v, %v", res, err)
	}
	res, err = RunCurl(context.Background(), Request{Method: "GET", URL: secure.URL, Options: options})
	if err != nil || string(res.Body) != "secure" || !res.Timing.Unverified {
		t.Errorf("response %+v, %v", res, err)
	}
}

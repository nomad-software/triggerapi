package rest

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"reflect"
	"strings"
	"testing"
	"time"
)

// stages returns the stages of the phases of a timing, in order.
func stages(timing *Timing) []Stage {
	var out []Stage
	for _, p := range timing.Phases {
		out = append(out, p.Stage)
	}
	return out
}

// checkPhases fails the test if the phases are not in order, one after the
// other, within the time the request took.
func checkPhases(t *testing.T, res *Response) {
	t.Helper()
	var last time.Duration
	for _, p := range res.Timing.Phases {
		if p.Start < last || p.End < p.Start || p.End > res.Duration {
			t.Errorf("phase %+v is out of order (previous ended at %v, total %v)", p, last, res.Duration)
		}
		last = p.End
	}
}

func TestSendTiming(t *testing.T) {
	const wait = 50 * time.Millisecond
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/old" {
			http.Redirect(w, r, "/new", http.StatusFound)
			return
		}
		time.Sleep(wait)
		w.Write([]byte("hello"))
	}))
	defer srv.Close()
	client := &http.Client{}

	// The first request connects. The server is at an IP address, so no
	// name is looked up.
	res, err := Send(context.Background(), client, Request{Method: "GET", URL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	timing := res.Timing
	if timing == nil {
		t.Fatal("no timing")
	}
	if got, want := stages(timing), []Stage{StageConnect, StageSend, StageWait, StageDownload}; !reflect.DeepEqual(got, want) {
		t.Errorf("stages = %v, want %v", got, want)
	}
	if timing.Reused || timing.Redirects != 0 || timing.TLS != "" || timing.Address != strings.TrimPrefix(srv.URL, "http://") {
		t.Errorf("timing = %+v", timing)
	}
	if p, _ := timing.Phase(StageWait); p.Duration() < wait {
		t.Errorf("waited %v, want at least %v", p.Duration(), wait)
	}
	checkPhases(t, res)

	// The next goes on the same connection.
	res, err = Send(context.Background(), client, Request{Method: "GET", URL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := stages(res.Timing), []Stage{StageSend, StageWait, StageDownload}; !reflect.DeepEqual(got, want) || !res.Timing.Reused {
		t.Errorf("stages = %v, reused %v", got, res.Timing.Reused)
	}

	// The time of a redirect comes first, and the rest is of the request
	// that was answered.
	res, err = Send(context.Background(), client, Request{Method: "GET", URL: srv.URL + "/old"})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := stages(res.Timing), []Stage{StageRedirects, StageSend, StageWait, StageDownload}; !reflect.DeepEqual(got, want) || res.Timing.Redirects != 1 {
		t.Errorf("stages = %v, redirects %d", got, res.Timing.Redirects)
	}
	if p, _ := res.Timing.Phase(StageRedirects); p.Start != 0 {
		t.Errorf("redirects = %+v", p)
	}
	checkPhases(t, res)
}

func TestSendTimingTLS(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()

	res, err := Send(context.Background(), srv.Client(), Request{Method: "GET", URL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := res.Timing.Phase(StageTLS); !ok || !strings.HasPrefix(res.Timing.TLS, "TLS 1.") {
		t.Errorf("timing = %+v", res.Timing)
	}
	checkPhases(t, res)
}

func TestTimingJSON(t *testing.T) {
	// A response from before there was timing has none.
	var old Response
	if err := json.Unmarshal([]byte(`{"status": "200 OK", "statusCode": 200, "duration": 5}`), &old); err != nil || old.Timing != nil {
		t.Errorf("response = %+v, %v", old, err)
	}
	data, _ := json.Marshal(old)
	if strings.Contains(string(data), "timing") {
		t.Errorf("JSON = %s", data)
	}

	// Timing is kept.
	res := Response{Timing: &Timing{Phases: []Phase{{StageWait, 1, 2}}, Redirects: 1, Reused: true, Address: "a:1", TLS: "TLS 1.3"}}
	data, _ = json.Marshal(res)
	var back Response
	if err := json.Unmarshal(data, &back); err != nil || !reflect.DeepEqual(back.Timing, res.Timing) {
		t.Errorf("timing = %+v, %v", back.Timing, err)
	}
}

// TestTracerTLSWithoutResponse checks that the TLS of a request is known
// from its connection where the response is not to hand, as with gRPC.
func TestTracerTLSWithoutResponse(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()

	trace := NewTracer(time.Now())
	req, err := http.NewRequestWithContext(httptrace.WithClientTrace(context.Background(), trace.Trace()), http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()

	timing := trace.Timing(time.Now(), nil)
	if want := tls.VersionName(res.TLS.Version) + ", " + tls.CipherSuiteName(res.TLS.CipherSuite); timing.TLS != want {
		t.Errorf("TLS = %q, want %q", timing.TLS, want)
	}
	if _, ok := timing.Phase(StageTLS); !ok {
		t.Errorf("no TLS handshake in %+v", timing.Phases)
	}
}

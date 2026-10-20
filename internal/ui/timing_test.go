package ui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	qt "github.com/mappu/miqt/qt6"
	"github.com/mappu/miqt/qt6/mainthread"

	"github.com/nomad-software/triggerapi/internal/rest"
	"github.com/nomad-software/triggerapi/internal/rpc"
	"github.com/nomad-software/triggerapi/internal/rpc/rpctest"
)

// timingRows returns the stages and times of the rows of a Timing tab that
// are on show.
func timingRows(tab *timingTab) map[string]string {
	rows := map[string]string{}
	for row := range tab.table.RowCount() {
		if !tab.table.IsRowHidden(row) {
			rows[tab.table.Item(row, colTimingStage).Text()] = tab.table.Item(row, colTimingTime).Text()
		}
	}
	return rows
}

// barColour returns the colour of the middle of a bar, which is that of the
// total and so covers the width.
func barColour(bar *qt.QWidget) string {
	image := bar.Grab().ToImage()
	return image.PixelColor(image.Width()/2, image.Height()/2).Name()
}

func TestTimingTab(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/old" {
			http.Redirect(w, r, "/new", http.StatusFound)
			return
		}
		time.Sleep(20 * time.Millisecond)
		w.Write([]byte("hello"))
	}))
	defer srv.Close()

	var w *Window
	mainthread.Wait(func() {
		w = NewWindow(testFiles(t).Files)
		w.Show()

		// Only the panels of requests that are traced have the tab, after
		// the Assertions tab.
		for _, p := range []*responsePanel{w.response, w.graphqlResponse, w.grpcResponse} {
			if p.timing == nil || p.tabs.Count() != tabTiming+1 || p.tabs.TabText(tabTiming) != "Timing" || p.tabs.IndexOf(p.timing.root) != tabTiming {
				t.Errorf("the tabs are %d, the timing %v", p.tabs.Count(), p.timing)
			}
		}
		for _, p := range []*responsePanel{w.wsResponse, w.mcpResponse} {
			if p.timing != nil {
				t.Error("a panel of a request that is not traced has a Timing tab")
			}
		}

		// Before there is a response there is a hint.
		if w.response.timing.pages.CurrentIndex() != timingPageHint {
			t.Error("the hint is not shown")
		}

		w.request.load(rest.Request{Method: "GET", URL: srv.URL})
		w.request.send.Click()
	})
	waitFor(t, "the response", func() bool { return fieldValue(w.response, "Status") != "" })

	mainthread.Wait(func() {
		tab := w.response.timing
		if tab.pages.CurrentIndex() != timingPageTable {
			t.Fatal("the timing is not shown")
		}
		rows := timingRows(tab)
		for _, stage := range []string{"TCP connect", "Request sent", "Waiting (TTFB)", "Download", "Total"} {
			if !strings.HasSuffix(rows[stage], " ms") && !strings.HasSuffix(rows[stage], " s") {
				t.Errorf("%s = %q", stage, rows[stage])
			}
		}
		// The server is at an IP address and without TLS.
		for _, stage := range []string{"DNS lookup", "TLS handshake", "Redirects"} {
			if _, ok := rows[stage]; ok {
				t.Errorf("there is a row for %s", stage)
			}
		}
		if details := tab.details.Text(); !strings.Contains(details, strings.TrimPrefix(srv.URL, "http://")) || !strings.Contains(details, "New") {
			t.Errorf("details = %q", details)
		}

		w.request.load(rest.Request{Method: "GET", URL: srv.URL + "/old"})
		w.request.send.Click()
	})
	waitFor(t, "the redirect", func() bool { _, ok := timingRows(w.response.timing)["Redirects (1)"]; return ok })

	mainthread.Wait(func() {
		// The connection was kept from the request before.
		rows := timingRows(w.response.timing)
		if _, ok := rows["TCP connect"]; ok || !strings.Contains(w.response.timing.details.Text(), "Reused") {
			t.Errorf("rows = %v, details %q", rows, w.response.timing.details.Text())
		}

		// The bars are in the colour of the settings for them.
		total := w.response.timing.bars[len(rest.Stages)].widget
		if got := barColour(total); got != defaultColour(colourTiming) {
			t.Errorf("the bar is %s", got)
		}
		s := w.config
		s.Colours.Timing = "#110000"
		if err := w.applySettings(s); err != nil {
			t.Error(err)
		}
		if got := barColour(total); got != "#110000" {
			t.Errorf("the bar is %s after the colour was changed", got)
		}

		// A response with no timing, such as one kept in the history from
		// before there was any, has the hint, as does a failure.
		w.response.show(&rest.Response{Status: "200 OK", StatusCode: 200})
		if w.response.timing.pages.CurrentIndex() != timingPageHint {
			t.Error("the hint is not shown for a response with no timing")
		}
		w.response.show(&rest.Response{Status: "200 OK", StatusCode: 200, Timing: &rest.Timing{}})
		w.response.showError(http.ErrHandlerTimeout, time.Second)
		if w.response.timing.pages.CurrentIndex() != timingPageHint {
			t.Error("the hint is not shown for an error")
		}
	})
}

// TestTimingTabGRPC checks that a gRPC call says where its time went, and
// that one kept in the history from before there were timings has the hint.
func TestTimingTabGRPC(t *testing.T) {
	srv := rpctest.NewServer(rpctest.ReflectionAll)
	defer srv.Close()

	var w *Window
	mainthread.Wait(func() {
		w = NewWindow(testFiles(t).Files)
		w.Show()
		w.requests.addFolder()
		addGRPCRequest(w)
		w.grpc.url.SetText(srv.URL)
		w.grpc.targetChanged()
	})
	waitFor(t, "the methods", func() bool { return w.grpc.method.count() == 4 })

	mainthread.Wait(func() {
		w.grpc.method.selectMethod("test.v1.Echo/Say")
		w.grpc.message.SetPlainText(`{"text": "hello"}`)
		w.grpc.invoke.Click()
	})
	waitFor(t, "the response", func() bool { return strings.Contains(w.grpcResponse.result.Text(), "0 OK") })

	mainthread.Wait(func() {
		tab := w.grpcResponse.timing
		if tab.pages.CurrentIndex() != timingPageTable {
			t.Fatal("the timing is not shown")
		}
		rows := timingRows(tab)
		for _, stage := range []string{"TCP connect", "Request sent", "Waiting (TTFB)", "Download", "Total"} {
			if !strings.HasSuffix(rows[stage], " ms") && !strings.HasSuffix(rows[stage], " s") {
				t.Errorf("%s = %q", stage, rows[stage])
			}
		}
		if details := tab.details.Text(); !strings.Contains(details, strings.TrimPrefix(srv.URL, "http://")) {
			t.Errorf("details = %q", details)
		}

		w.grpcResponse.showRPC(&rpc.Response{CodeName: "OK"})
		if tab.pages.CurrentIndex() != timingPageHint {
			t.Error("the hint is not shown for a response with no timing")
		}
	})
}

func TestFormatTiming(t *testing.T) {
	for d, want := range map[time.Duration]string{
		120 * time.Microsecond:   "0.12 ms",
		3500 * time.Microsecond:  "3.50 ms",
		42300 * time.Microsecond: "42.3 ms",
		1500 * time.Millisecond:  "1.50 s",
	} {
		if got := formatTiming(d); got != want {
			t.Errorf("formatTiming(%v) = %q, want %q", d, got, want)
		}
	}
}

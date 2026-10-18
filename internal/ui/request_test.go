package ui

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	qt "github.com/mappu/miqt/qt6"
	"github.com/mappu/miqt/qt6/mainthread"

	"github.com/nomad-software/triggerapi/internal/env"
	"github.com/nomad-software/triggerapi/internal/rest"
	"github.com/nomad-software/triggerapi/internal/settings"
)

// pressEscape presses the Escape key in a widget. It must be called on the
// main thread.
func pressEscape(widget *qt.QWidget) {
	event := qt.NewQKeyEvent(qt.QEvent__KeyPress, int(qt.Key_Escape), qt.NoModifier)
	defer event.Delete()
	qt.QCoreApplication_SendEvent(widget.QObject, event.QEvent)
}

func TestSendRequest(t *testing.T) {
	type received struct {
		method, query, header, auth, contentType, body string
	}
	got := make(chan received, 1)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		got <- received{
			method:      r.Method,
			query:       r.URL.RawQuery,
			header:      r.Header.Get("X-Test"),
			auth:        r.Header.Get("Authorization"),
			contentType: r.Header.Get("Content-Type"),
			body:        string(body),
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		io.WriteString(w, `{"id":1}`)
	}))
	defer srv.Close()

	var w *Window
	mainthread.Wait(func() {
		w = NewWindow(testFiles(t).Files)
		w.Show()

		w.request.method.SetCurrentIndex(1) // POST
		w.request.url.SetText(srv.URL)
		w.request.params.add("page", "2")
		w.request.headers.add("X-Test", "yes")
		w.request.auth.typ.SetCurrentIndex(2) // Bearer Token
		w.request.auth.token.SetText("secret")
		w.request.bodyType.SetCurrentIndex(1) // JSON
		w.request.bodyRaw.SetPlainText(`{"name":"triggerapi"}`)

		w.request.send.Click()
	})

	select {
	case r := <-got:
		want := received{"POST", "page=2", "yes", "Bearer secret", "application/json", `{"name":"triggerapi"}`}
		if r != want {
			t.Errorf("server received %+v, want %+v", r, want)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("request was not sent")
	}

	waitFor(t, "the response", func() bool {
		return fieldValue(w.response, "Status") == "201 Created"
	})

	mainthread.Wait(func() {
		if want := "{\n  \"id\": 1\n}"; w.response.body.ToPlainText() != want {
			t.Errorf("body = %q, want %q", w.response.body.ToPlainText(), want)
		}
		if w.response.headers.RowCount() == 0 {
			t.Error("no response headers shown")
		}
		if w.request.send.Text() != "Send" {
			t.Errorf("send button = %q", w.request.send.Text())
		}
		if w.request.params.table.RowCount() != 2 {
			t.Errorf("params rows = %d, want 2", w.request.params.table.RowCount())
		}
	})
}

func TestParamsShownInURL(t *testing.T) {
	mainthread.Wait(func() {
		w := NewWindow(testFiles(t).Files)
		p := w.request

		// Params appear in the URL as they are added.
		p.url.SetText("{{host}}/users")
		p.params.add("page", "2")
		p.params.add("q", "a&b")
		if got, want := p.url.Text(), "{{host}}/users?page=2&q=a%26b"; got != want {
			t.Errorf("url = %q, want %q", got, want)
		}

		// A disabled param is taken out of the URL but stays in the table.
		p.params.table.Item(0, colEnabled).SetCheckState(qt.Unchecked)
		if got, want := p.url.Text(), "{{host}}/users?q=a%26b"; got != want {
			t.Errorf("url = %q, want %q", got, want)
		}

		// Typing a query string into the URL fills the table.
		p.url.SetText("{{host}}/users?q=x&sort=name")
		want := []rest.KeyValue{{Key: "page", Value: "2"}, {Key: "q", Value: "x", Enabled: true}, {Key: "sort", Value: "name", Enabled: true}}
		if got := p.params.items(); !reflect.DeepEqual(got, want) {
			t.Errorf("params = %+v, want %+v", got, want)
		}
		if got := p.url.Text(); got != "{{host}}/users?q=x&sort=name" {
			t.Errorf("typed url was rewritten to %q", got)
		}

		// The request keeps the query in the params only, and loads back
		// with it shown in the URL.
		req := p.request()
		if req.URL != "{{host}}/users" || !reflect.DeepEqual(req.Params, want) {
			t.Errorf("request = %q %+v", req.URL, req.Params)
		}
		p.load(rest.Request{Method: "GET"})
		p.load(req)
		if got := p.url.Text(); got != "{{host}}/users?q=x&sort=name" {
			t.Errorf("loaded url = %q", got)
		}

		// Removing the last enabled param removes the query string.
		p.url.SetText("{{host}}/users")
		if got := p.params.items(); len(got) != 1 || got[0].Enabled {
			t.Errorf("params = %+v", got)
		}
	})
}

func TestRESTErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/missing":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			io.WriteString(w, `{"error":"no such user"}`)
		case "/empty":
			w.WriteHeader(http.StatusInternalServerError)
		default:
			io.WriteString(w, "fine")
		}
	}))
	defer srv.Close()

	var w *Window
	send := func(path, code string) {
		t.Helper()
		mainthread.Wait(func() {
			w.request.url.SetText(srv.URL + path)
			w.request.send.Click()
		})
		waitFor(t, "the response to "+path, func() bool { return fieldValue(w.response, "Status") == code })
	}

	mainthread.Wait(func() {
		w = NewWindow(testFiles(t).Files)
		w.Show()
		// The response is empty until there is one, with no hint in it.
		if w.response.body.PlaceholderText() != "" || w.response.body.ToPlainText() != "" || len(w.response.fields) != 0 {
			t.Errorf("response starts with %q, bar %+v", w.response.body.PlaceholderText(), w.response.fields)
		}
	})

	// An error status goes in the bar beneath the response, in red, with
	// the time taken. The response holds the body alone.
	send("/missing", "404 Not Found")
	mainthread.Wait(func() {
		if got, want := w.response.body.ToPlainText(), "{\n  \"error\": \"no such user\"\n}"; got != want {
			t.Errorf("body = %q, want %q", got, want)
		}
		if fieldValue(w.response, "Status") != "404 Not Found" || !isFailure(w.response) || !strings.Contains(fieldValue(w.response, "Time"), "ms") || fieldValue(w.response, "Size") != "24 B" {
			t.Errorf("bar = %+v", w.response.fields)
		}
		// The code is at the left of the bar and the rest at the right.
		left, right := w.response.result.Text(), w.response.details.Text()
		if !strings.Contains(left, ">Status:</b>") || strings.Contains(left, "Time") || strings.Contains(left, "Error") {
			t.Errorf("left of the bar = %q", left)
		}
		if !strings.Contains(right, ">Time:</b>") || !strings.Contains(right, ">Size:</b>") || !strings.Contains(right, ">Protocol:</b>") || strings.Contains(right, "Status") {
			t.Errorf("right of the bar = %q", right)
		}
	})

	send("/empty", "500 Internal Server Error")
	mainthread.Wait(func() {
		if w.response.body.ToPlainText() != "" || fieldValue(w.response, "Status") != "500 Internal Server Error" || !isFailure(w.response) {
			t.Errorf("body = %q, bar = %+v", w.response.body.ToPlainText(), w.response.fields)
		}
	})

	// A success has its status in the bar too, but not as a failure.
	send("/ok", "200 OK")
	mainthread.Wait(func() {
		if w.response.body.ToPlainText() != "fine" || fieldValue(w.response, "Status") != "200 OK" || isFailure(w.response) {
			t.Errorf("body = %q, bar = %+v", w.response.body.ToPlainText(), w.response.fields)
		}

		// Starting another request empties the bar.
		w.request.send.Click()
		// Nothing is put in the response while it is awaited either.
		if w.response.result.Text() != "" || w.response.details.Text() != "" || len(w.response.fields) != 0 || w.response.body.PlaceholderText() != "" || w.response.body.ToPlainText() != "" {
			t.Errorf("bar = %q", w.response.result.Text())
		}
	})
	waitFor(t, "the last response", func() bool { return fieldValue(w.response, "Status") == "200 OK" })
}

func TestURLPreview(t *testing.T) {
	files := testFiles(t)
	store := env.Store{Environments: []env.Environment{
		{Name: "Local", Variables: []env.Variable{{Key: "host", Value: "http://local.test", Enabled: true}, {Key: "page", Value: "", Enabled: true}}},
		{Name: "Remote", Variables: []env.Variable{{Key: "host", Value: "https://remote.test", Enabled: true}}},
	}}
	if err := store.Save(files.Environments); err != nil {
		t.Fatal(err)
	}

	mainthread.Wait(func() {
		w := NewWindow(files.Files)
		w.Show()
		p := w.request

		p.url.SetText("{{host}}/users/{{id}}?page={{page}}&size=2")
		if p.preview.IsChecked() || p.url.IsHidden() || !p.resolved.IsHidden() {
			t.Error("the preview is shown at first")
		}

		// The preview is in place of the field, and cannot be edited. A
		// placeholder with no value is shown as a mark, whether it is not
		// defined (id) or is defined as nothing (page).
		p.preview.Click()
		if p.resolved.IsHidden() || !p.url.IsHidden() || !p.resolved.IsReadOnly() {
			t.Error("the preview is not shown")
		}
		if got, want := p.resolved.Text(), "http://local.test/users/■?page=■&size=2"; got != want {
			t.Errorf("preview = %q, want %q", got, want)
		}
		// The value is in the colour of a placeholder that is found, the
		// marks in that of one that is not, and the rest in neither.
		colourAt := func(position int) string {
			cursor := qt.NewQTextCursor2(p.resolved.Document())
			defer cursor.Delete()
			// The format of a cursor is that of the character before it.
			cursor.SetPosition(position + 1)
			format := cursor.CharFormat()
			if !format.HasProperty(int(qt.QTextFormat__ForegroundBrush)) {
				return ""
			}
			return formatColour(format)
		}
		found, missing := themeColour(colourFound), themeColour(colourNotFound)
		for position, want := range map[int]string{0: found, 16: found, 17: "", 24: missing, 25: "", 31: missing, 32: ""} {
			if got := colourAt(position); got != want {
				t.Errorf("character %d is %q, want %q", position, got, want)
			}
		}
		if got, want := p.url.Text(), "{{host}}/users/{{id}}?page={{page}}&size=2"; got != want {
			t.Errorf("the URL became %q", got)
		}

		// It follows the environment and the request.
		w.environments.list.SetCurrentRow(1)
		if got, want := p.resolved.Text(), "https://remote.test/users/■?page=■&size=2"; got != want {
			t.Errorf("preview = %q, want %q", got, want)
		}
		p.params.setItems([]rest.KeyValue{{Key: "size", Value: "5", Enabled: true}})
		p.paramsChanged()
		if got, want := p.resolved.Text(), "https://remote.test/users/■?size=5"; got != want {
			t.Errorf("preview = %q, want %q", got, want)
		}

		// Escape goes back to the field, as does the button.
		pressEscape(p.resolved.QWidget)
		if p.preview.IsChecked() || p.url.IsHidden() || !p.resolved.IsHidden() {
			t.Error("Escape did not close the preview")
		}
		p.preview.Click()
		p.preview.Click()
		if p.url.IsHidden() || !p.resolved.IsHidden() {
			t.Error("the button did not close the preview")
		}

		// The curl command is closed before the preview beneath it.
		p.preview.Click()
		p.curl.Click()
		pressEscape(p.command.QWidget)
		if p.pages.CurrentIndex() != pageFields || !p.preview.IsChecked() {
			t.Errorf("after Escape: page %d, preview %v", p.pages.CurrentIndex(), p.preview.IsChecked())
		}
		pressEscape(p.resolved.QWidget)
		if p.preview.IsChecked() {
			t.Error("Escape did not close the preview")
		}

		// The environment editor is closed first of all, and what is under
		// it is left alone.
		p.preview.Click()
		w.environments.edit()
		if w.stack.CurrentIndex() != pageEditor {
			t.Error("the editor did not open")
		}
		pressEscape(w.QWidget)
		if w.stack.CurrentIndex() != pageRequest || !p.preview.IsChecked() {
			t.Errorf("after Escape: page %d, preview %v", w.stack.CurrentIndex(), p.preview.IsChecked())
		}
	})
}

func TestURLWraps(t *testing.T) {
	files := testFiles(t)
	var w *Window
	var short int
	sent := 0

	mainthread.Wait(func() {
		w = NewWindow(files.Files)
		w.Show()
		w.request.url.SetText("example.test/users")
	})
	waitFor(t, "the URL field to be laid out", func() bool { return w.request.url.Height() > 0 && w.request.url.Width() > 100 })

	mainthread.Wait(func() {
		url := w.request.url
		short = url.Height()

		// A URL too long for the field is wrapped, and all of it is kept.
		long := "example.test/users?" + strings.Repeat("filter=some-long-value&", 40) + "end=1"
		url.SetText(long)
		if url.Text() != long || w.request.request().URL != "example.test/users" {
			t.Errorf("the URL became %q", url.Text())
		}
	})
	waitFor(t, "the URL field to grow", func() bool { return w.request.url.Height() >= 3*short-short/2 })

	mainthread.Wait(func() {
		url := w.request.url
		url.SetText("example.test/users")
	})
	waitFor(t, "the URL field to shrink", func() bool { return w.request.url.Height() == short })

	mainthread.Wait(func() {
		url := w.request.url

		// It is one line however it is filled.
		url.SetPlainText("example.test/\nusers\r\n")
		if got := url.Text(); got != "example.test/users" || url.ToPlainText() != got {
			t.Errorf("text = %q, shown as %q", got, url.ToPlainText())
		}

		// What is typed is reported only when Return is pressed, which
		// adds nothing and does not send the request.
		if url.onReturn != nil {
			t.Error("Return in the URL sends the request")
		}
		url.onReturn = func() { sent++ }
		typed := 0
		report := url.onChange
		url.onChange = func(text string) { typed++; report(text) }
		url.MoveCursor(qt.QTextCursor__End)
		key := qt.NewQKeyEvent3(qt.QEvent__KeyPress, int(qt.Key_S), qt.NoModifier, "s")
		defer key.Delete()
		qt.QCoreApplication_SendEvent(url.QObject, key.QEvent)
		if typed != 0 || url.Text() != "example.test/userss" {
			t.Errorf("typing reported %d changes, text = %q", typed, url.Text())
		}
		event := qt.NewQKeyEvent(qt.QEvent__KeyPress, int(qt.Key_Return), qt.NoModifier)
		defer event.Delete()
		qt.QCoreApplication_SendEvent(url.QObject, event.QEvent)
		if sent != 1 || typed != 1 || url.Text() != "example.test/userss" {
			t.Errorf("return: %d calls, %d changes, text = %q", sent, typed, url.Text())
		}
		url.onChange = report
		url.SetText("example.test/users")

		// Recolouring the placeholders is not an edit.
		changes := 0
		url.onChange = func(string) { changes++ }
		w.request.highlight.refresh()
		if changes != 0 {
			t.Errorf("recolouring was reported as %d changes", changes)
		}
	})
}

func TestParamsOrder(t *testing.T) {
	mainthread.Wait(func() {
		w := NewWindow(testFiles(t).Files)

		// A request is shown with its params and headers in order, in the
		// URL too.
		w.request.load(rest.Request{
			Method:  "GET",
			URL:     "http://a.test/x",
			Params:  []rest.KeyValue{{Key: "page", Value: "2", Enabled: true}, {Key: "all", Value: "1", Enabled: true}},
			Headers: []rest.KeyValue{{Key: "X-B", Value: "b", Enabled: true}, {Key: "Accept", Value: "a", Enabled: true}},
		})
		if got := w.request.url.Text(); got != "http://a.test/x?all=1&page=2" {
			t.Errorf("url = %q", got)
		}
		got := w.request.request()
		if got.Params[0].Key != "all" || got.Headers[0].Key != "Accept" {
			t.Errorf("request = %+v", got)
		}

		// A param added in the table goes to its place in both.
		w.request.params.add("limit", "5")
		if got := w.request.url.Text(); got != "http://a.test/x?all=1&limit=5&page=2" {
			t.Errorf("url = %q", got)
		}

		// Params typed into the URL are listed in order.
		w.request.url.SetText("http://a.test/x?z=1&b=2")
		if got := w.request.request().Params; len(got) != 2 || got[0].Key != "b" || got[1].Key != "z" {
			t.Errorf("params = %+v", got)
		}
	})
}

func TestCurlView(t *testing.T) {
	files := testFiles(t)

	store := &env.Store{Active: 0, Environments: []env.Environment{
		{Name: "Local", Variables: []env.Variable{{Key: "host", Value: "https://local.test", Enabled: true}}},
		{Name: "Staging", Variables: []env.Variable{{Key: "host", Value: "https://staging.test", Enabled: true}}},
	}}
	if err := store.Save(files.Environments); err != nil {
		t.Fatal(err)
	}

	mainthread.Wait(func() {
		w := NewWindow(files.Files)
		w.Show()
		p := w.request

		p.load(rest.Request{
			Method:   "POST",
			URL:      "{{host}}/users",
			Params:   []rest.KeyValue{{Key: "dry", Value: "1", Enabled: true}},
			Headers:  []rest.KeyValue{{Key: "X-Key", Value: "{{missing}}", Enabled: true}},
			BodyType: rest.BodyJSON,
			Body:     `{"name": "it's"}`,
		})

		// The fields are shown until the button is pressed.
		if p.pages.CurrentIndex() != pageFields || p.curl.IsChecked() {
			t.Fatal("the command is shown at first")
		}

		p.curl.Click()
		want := "curl --location --request POST 'https://local.test/users?dry=1' \\\n" +
			"  --header 'Content-Type: application/json' \\\n" +
			"  --header 'X-Key: {{missing}}' \\\n" +
			"  --data-raw '{\"name\": \"it'\\''s\"}'"
		if got := p.command.ToPlainText(); p.pages.CurrentIndex() != pageCurl || got != want {
			t.Errorf("command = %s\nwant %s", got, want)
		}
		if !p.command.IsReadOnly() || !p.copy.IsEnabled() {
			t.Error("the command can be edited, or cannot be copied")
		}

		// It can be copied.
		p.copy.Click()
		if got := qt.QGuiApplication_Clipboard().Text(); got != want {
			t.Errorf("copied %q", got)
		}

		// It follows the environment, and the request that is shown.
		w.environments.list.SetCurrentRow(1)
		if got := p.command.ToPlainText(); !strings.Contains(got, "https://staging.test/users?dry=1") {
			t.Errorf("command = %s", got)
		}
		p.load(rest.Request{Method: "GET", URL: "{{host}}/ping"})
		if got := p.command.ToPlainText(); got != "curl --location https://staging.test/ping" {
			t.Errorf("command = %s", got)
		}

		// A request that cannot be made is explained, and not copied.
		p.load(rest.Request{Method: "GET"})
		if got := p.command.ToPlainText(); !strings.HasPrefix(got, "# ") || !strings.Contains(got, "URL is empty") || p.copy.IsEnabled() {
			t.Errorf("command = %s", got)
		}

		// Pressing the button again brings the fields back.
		p.curl.Click()
		if p.pages.CurrentIndex() != pageFields {
			t.Error("the fields did not come back")
		}

		// So does the Close button beside the command.
		p.curl.Click()
		p.closeCurl.Click()
		if p.pages.CurrentIndex() != pageFields || p.curl.IsChecked() {
			t.Error("Close did not bring the fields back")
		}

		// The gRPC page has no such button.
		if w.grpc.root.IsAncestorOf(p.curl.QWidget) || !p.root.IsAncestorOf(p.curl.QWidget) || w.response.root.IsAncestorOf(p.curl.QWidget) {
			t.Error("the button is not in the REST request panel alone")
		}
	})
}

func TestExecuteCurl(t *testing.T) {
	if _, err := exec.LookPath("curl"); err != nil {
		t.Skip("curl is not installed")
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/missing" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"agent":"` + strings.SplitN(r.UserAgent(), "/", 2)[0] + `","key":"` + r.Header.Get("X-Key") + `"}`))
	}))
	defer srv.Close()

	files := testFiles(t)
	store := &env.Store{Active: 0, Environments: []env.Environment{
		{Name: "Local", Variables: []env.Variable{{Key: "host", Value: srv.URL, Enabled: true}}},
	}}
	if err := store.Save(files.Environments); err != nil {
		t.Fatal(err)
	}

	var w *Window
	mainthread.Wait(func() {
		w = NewWindow(files.Files)
		w.Show()
		w.request.load(rest.Request{Method: "GET", URL: "{{host}}/a", Headers: []rest.KeyValue{{Key: "X-Key", Value: "abc", Enabled: true}}})
		w.request.curl.Click()
		w.request.execute.Click()
	})

	// The response to the command is shown as any other is.
	waitFor(t, "the response", func() bool { return fieldValue(w.response, "Status") != "" })
	mainthread.Wait(func() {
		if got := fieldValue(w.response, "Status"); got != "200 OK" || isFailure(w.response) {
			t.Errorf("code = %q", got)
		}
		if got := w.response.body.ToPlainText(); !strings.Contains(got, `"agent": "curl"`) || !strings.Contains(got, `"key": "abc"`) {
			t.Errorf("body = %s", got)
		}
		if fieldValue(w.response, "Protocol") != "HTTP/1.1" || fieldValue(w.response, "Time") == "" || w.request.execute.Text() != "Execute" {
			t.Errorf("protocol = %q, button %q", fieldValue(w.response, "Protocol"), w.request.execute.Text())
		}

		w.request.load(rest.Request{Method: "GET", URL: "{{host}}/missing"})
		w.request.execute.Click()
	})

	// So is a failure.
	waitFor(t, "the failure", func() bool { return strings.HasPrefix(fieldValue(w.response, "Status"), "404") })
	mainthread.Wait(func() {
		if !isFailure(w.response) {
			t.Error("a 404 is not shown as a failure")
		}

		// A request that cannot be made cannot be executed.
		w.request.load(rest.Request{Method: "GET"})
		if w.request.execute.IsEnabled() {
			t.Error("a request without a URL can be executed")
		}
	})
}

// The tab of the request shown is the one its method is likely to need: the
// body for a method that sends one, and otherwise the params.
func TestRequestTabFollowsMethod(t *testing.T) {
	files := testFiles(t)

	mainthread.Wait(func() {
		w := NewWindow(files.Files)
		p := w.request

		for _, method := range rest.Methods {
			want := tabParams
			if method == "POST" || method == "PUT" || method == "PATCH" {
				want = tabBody
			}

			// Another tab is chosen first, to see that it is left.
			p.tabs.SetCurrentIndex(tabHeaders)
			p.load(rest.Request{Method: method})
			if got := p.tabs.CurrentIndex(); got != want {
				t.Errorf("tab after loading %s = %d, want %d", method, got, want)
			}

			// The method is chosen from another, or it is no change.
			other := "OPTIONS"
			if method == other {
				other = "HEAD"
			}
			p.load(rest.Request{Method: other})
			p.tabs.SetCurrentIndex(tabHeaders)
			p.method.SetCurrentIndex(slices.Index(rest.Methods, method))
			if got := p.tabs.CurrentIndex(); got != want {
				t.Errorf("tab after choosing %s = %d, want %d", method, got, want)
			}
		}
	})
}

// The Format button sets out the JSON of a REST body and of a gRPC message,
// keeping the placeholders in it, and leaves alone what is not JSON.
func TestFormatJSONButton(t *testing.T) {
	files := testFiles(t)

	mainthread.Wait(func() {
		w := NewWindow(files.Files)
		w.Show()
		w.requests.addFolder()
		w.requests.addRequest()
		p := w.request

		// The button is beside the type of the body, for a JSON body only.
		if !p.root.IsAncestorOf(p.format.button.QWidget) || !p.format.button.IsHidden() {
			t.Fatal("the button is shown without a JSON body, or is not in the panel")
		}
		for i, kind := range bodyTypes {
			p.bodyType.SetCurrentIndex(i)
			if got, want := !p.format.button.IsHidden(), kind.typ == rest.BodyJSON; got != want {
				t.Errorf("with a body of %s the button is shown: %v", kind.label, got)
			}
		}
		p.load(rest.Request{BodyType: rest.BodyJSON, Body: `{"id":{{id}},"tags":["a"]}`})
		if p.format.button.IsHidden() {
			t.Error("the button is not shown for a loaded JSON body")
		}

		var warned []string
		p.format.warn = func(message string) { warned = append(warned, message) }

		// The body is set out and saved, and can be put back.
		const formatted = "{\n  \"id\": {{id}},\n  \"tags\": [\n    \"a\"\n  ]\n}"
		p.format.button.Click()
		if got := p.bodyRaw.ToPlainText(); got != formatted || w.open.Request.Body != formatted || len(warned) != 0 {
			t.Errorf("body = %q, saved %q, warned %q", got, w.open.Request.Body, warned)
		}
		if saved, err := loadRequests(files.Requests); err != nil || saved.Folders[0].Requests[0].Request.Body != formatted {
			t.Errorf("saved %+v, %v", saved, err)
		}
		p.bodyRaw.Undo()
		if got := p.bodyRaw.ToPlainText(); got != `{"id":{{id}},"tags":["a"]}` {
			t.Errorf("after undo: %q", got)
		}

		// What is not JSON is left as it is, and the user told.
		p.bodyRaw.SetPlainText(`{"id":`)
		p.format.button.Click()
		if got := p.bodyRaw.ToPlainText(); got != `{"id":` || len(warned) != 1 || !strings.Contains(warned[0], "not valid JSON") {
			t.Errorf("body = %q, warned %q", got, warned)
		}

		// The message of a gRPC request is set out the same way.
		addGRPCRequest(w)
		g := w.grpc
		if !g.root.IsAncestorOf(g.format.button.QWidget) || g.format.button.IsHidden() {
			t.Fatal("the button of the message is not shown")
		}
		g.message.SetPlainText(`{"text":"{{said}}","count":2}`)
		g.format.button.Click()
		if got, want := g.message.ToPlainText(), "{\n  \"text\": \"{{said}}\",\n  \"count\": 2\n}"; got != want || w.open.GRPC.Message != want {
			t.Errorf("message = %q, saved %q", got, w.open.GRPC.Message)
		}
	})
}

// How long a request can take, and whether redirects are followed, are set
// in the settings.
func TestRequestSettings(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/old":
			http.Redirect(w, r, "/new", http.StatusFound)
		case "/slow":
			select {
			case <-release:
			case <-r.Context().Done():
			}
		default:
			io.WriteString(w, "arrived")
		}
	}))
	defer srv.Close()
	defer close(release)

	files := testFiles(t)
	var w *Window
	mainthread.Wait(func() {
		w = NewWindow(files.Files)
		w.Show()

		// There is no limit, and redirects are followed, to begin with.
		d := newSettingsDialog(w.QWidget, w.config, files.Files)
		defer d.DeleteLater()
		if page := d.tabs.Widget(settingsTabRequests); !page.IsAncestorOf(d.timeout.QWidget) || !page.IsAncestorOf(d.redirects.QWidget) {
			t.Error("the timeout and the redirects are not on the Requests tab")
		}
		if d.timeout.Value() != 0 || d.timeout.Text() != "No limit" || !d.redirects.IsChecked() {
			t.Errorf("at first: timeout %q, redirects %v", d.timeout.Text(), d.redirects.IsChecked())
		}
		if got := d.settings(w.config); !reflect.DeepEqual(got, w.config) {
			t.Errorf("unchanged: %+v", got)
		}

		w.request.load(rest.Request{Method: "GET", URL: srv.URL + "/old"})
		w.request.send.Click()
	})
	waitFor(t, "the redirect to be followed", func() bool { return fieldValue(w.response, "Status") == "200 OK" })

	mainthread.Wait(func() {
		d := newSettingsDialog(w.QWidget, w.config, files.Files)
		defer d.DeleteLater()
		d.timeout.SetValue(1)
		d.redirects.SetChecked(false)
		if d.timeout.Text() != "1 s" {
			t.Errorf("timeout is shown as %q", d.timeout.Text())
		}
		if err := w.applySettings(d.settings(w.config)); err != nil {
			t.Error(err)
		}
		if got := settings.Load(files.Settings); got.Timeout != 1 || !got.NoRedirects {
			t.Errorf("saved %+v", got)
		}

		// The command says the same as the settings, at once.
		w.request.curl.Click()
		if got, want := w.request.command.ToPlainText(), "curl --max-time 1 "+srv.URL+"/old"; got != want {
			t.Errorf("command = %q, want %q", got, want)
		}
		w.request.curl.Click()

		w.request.send.Click()
	})
	waitFor(t, "the redirect to be shown", func() bool { return strings.HasPrefix(fieldValue(w.response, "Status"), "302") })

	mainthread.Wait(func() {
		w.request.load(rest.Request{Method: "GET", URL: srv.URL + "/slow"})
		w.request.send.Click()
	})
	waitFor(t, "the request to time out", func() bool { return fieldValue(w.response, "Error") != "" })
	mainthread.Wait(func() {
		if got := fieldValue(w.response, "Error"); got != "timed out after 1 second, which is the limit in the settings" || !isFailure(w.response) || w.request.send.Text() != "Send" {
			t.Errorf("error = %q, button %q", got, w.request.send.Text())
		}
	})
}

func TestCancelRequest(t *testing.T) {
	// The server answers nothing until the request is given up on.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)
	files := testFiles(t)

	var w *Window
	mainthread.Wait(func() {
		w = NewWindow(files.Files)
		w.Show()
		w.request.url.SetText(srv.URL)
		w.request.send.Click()
		if w.request.send.Text() != "Cancel" {
			t.Errorf("while in flight Send reads %q", w.request.send.Text())
		}
		// Pressing it again gives the request up.
		w.request.send.Click()
	})
	waitFor(t, "the cancel", func() bool {
		return mainthread.Wait2(func() bool { return fieldValue(w.response, fieldError) == "Cancelled" })
	})
	mainthread.Wait(func() {
		if w.request.send.Text() != "Send" || !isFailure(w.response) || fieldValue(w.response, "Time") == "" || w.response.body.ToPlainText() != "" {
			t.Errorf("after cancelling: button %q, body %q", w.request.send.Text(), w.response.body.ToPlainText())
		}
	})
}

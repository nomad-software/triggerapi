package ui

import (
	"reflect"
	"testing"

	qt "github.com/mappu/miqt/qt6"
	"github.com/mappu/miqt/qt6/mainthread"

	"github.com/nomad-software/triggerapi/internal/rest"
)

// paste puts text on the clipboard and pastes it into the URL field of the
// REST request UI. It must be called on the main thread.
func paste(w *Window, text string) {
	qt.QGuiApplication_Clipboard().SetText(text)
	w.request.url.SetFocus()
	w.request.url.Paste()
	w.request.url.commit()
}

func TestPasteCurlIntoUnsavedRequest(t *testing.T) {
	var warned string
	var got rest.Request
	mainthread.Wait(func() {
		w := NewWindow(testFiles(t).Files)
		w.Show()
		w.request.warnPaste = func(message string) { warned = message }

		paste(w, "curl 'https://api.test/users?page=2' \\\n  -X PUT \\\n  -H 'X-Test: yes' \\\n  -H 'Authorization: Bearer abc' \\\n  --data-raw '{\"a\":1}' -H 'Content-Type: application/json'")
		got = w.request.request()

		// The draft is kept by its tab.
		w.stash(w.tabs.current())
		if d := w.tabs.current().draft; d == nil || d.Request.URL != "https://api.test/users" {
			t.Errorf("draft not kept: %+v", d)
		}
	})

	want := rest.Request{
		Method:   "PUT",
		URL:      "https://api.test/users",
		Params:   []rest.KeyValue{{Key: "page", Value: "2", Enabled: true}},
		Headers:  []rest.KeyValue{{Key: "X-Test", Value: "yes", Enabled: true}},
		Auth:     rest.Auth{Type: rest.AuthBearer, Token: "abc"},
		BodyType: rest.BodyJSON,
		Body:     `{"a":1}`,
	}
	got.Form, got.Captures = nil, nil
	if !reflect.DeepEqual(got, want) {
		t.Errorf("pasted request = %+v, want %+v", got, want)
	}
	if warned != "" {
		t.Errorf("warned: %s", warned)
	}
}

func TestPasteCurlNotTaken(t *testing.T) {
	mainthread.Wait(func() {
		w := NewWindow(testFiles(t).Files)
		w.Show()
		var warned string
		w.request.warnPaste = func(message string) { warned = message }

		// What is not a curl command is pasted as it is.
		paste(w, "https://api.test/a")
		if text := w.request.url.Text(); text != "https://api.test/a" {
			t.Errorf("URL = %q after pasting an address", text)
		}

		// A command that cannot be a request is not pasted.
		w.request.url.SetText("")
		paste(w, "curl -F a=1 https://api.test")
		if text := w.request.url.Text(); text != "" || warned == "" {
			t.Errorf("URL = %q, warned %q, after pasting a multipart command", text, warned)
		}

		// In a saved request, a command is pasted as text.
		w.requests.insert(w.composed("Saved"))
		if w.open == nil {
			t.Error("the request was not saved")
			return
		}
		w.request.url.SetText("")
		paste(w, "curl -X POST https://api.test")
		if text := w.request.url.Text(); text != "curl -X POST https://api.test" || w.request.method.CurrentText() != "GET" {
			t.Errorf("URL = %q, method %s, after pasting into a saved request", text, w.request.method.CurrentText())
		}
	})
}

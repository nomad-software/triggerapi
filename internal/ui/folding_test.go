package ui

import (
	"net/http"
	"reflect"
	"strings"
	"testing"

	qt "github.com/mappu/miqt/qt6"
	"github.com/mappu/miqt/qt6/mainthread"

	"github.com/nomad-software/triggerapi/internal/rest"
)

// visibleLines returns the lines of an editor that are not hidden by a fold.
func visibleLines(edit *qt.QPlainTextEdit) []string {
	var lines []string
	for block := edit.Document().FirstBlock(); block.IsValid(); block = block.Next() {
		if block.IsVisible() {
			lines = append(lines, block.Text())
		}
	}
	return lines
}

func TestFolding(t *testing.T) {
	body := `{"user":{"name":"a","tags":["x","y"]},"ok":true}`

	var w *Window
	mainthread.Wait(func() {
		w = NewWindow(testFiles(t).Files)
		w.Show()
		w.response.show(&rest.Response{
			Status:     "200 OK",
			StatusCode: 200,
			Headers:    http.Header{"Content-Type": {"application/json"}},
			Body:       []byte(body),
		})

		// The response is indented as:
		//  0 {
		//  1   "user": {
		//  2     "name": "a",
		//  3     "tags": [
		//  4       "x",
		//  5       "y"
		//  6     ]
		//  7   },
		//  8   "ok": true
		//  9 }
		f := w.response.fold
		if want := map[int]int{0: 9, 1: 7, 3: 6}; !reflect.DeepEqual(f.regions, want) {
			t.Fatalf("regions = %v, want %v", f.regions, want)
		}

		// Folding hides the lines inside an object, leaving its braces.
		f.toggle(1)
		want := []string{"{", `  "user": {`, "  },", `  "ok": true`, "}"}
		if got := visibleLines(w.response.body); !reflect.DeepEqual(got, want) {
			t.Errorf("folded = %q, want %q", got, want)
		}

		// The text itself is untouched.
		if got := w.response.body.ToPlainText(); !strings.Contains(got, `"name": "a"`) {
			t.Errorf("text = %q", got)
		}

		// A fold inside another is remembered while the outer one is folded.
		f.toggle(1)
		f.toggle(3)
		f.toggle(1)
		f.toggle(1)
		want = []string{"{", `  "user": {`, `    "name": "a",`, `    "tags": [`, "    ]", "  },", `  "ok": true`, "}"}
		if got := visibleLines(w.response.body); !reflect.DeepEqual(got, want) {
			t.Errorf("inner fold = %q, want %q", got, want)
		}

		// A line that opens nothing cannot be folded.
		f.toggle(2)
		if got := visibleLines(w.response.body); !reflect.DeepEqual(got, want) {
			t.Errorf("after folding a plain line = %q", got)
		}

		// Another response starts unfolded.
		w.response.show(&rest.Response{
			Status:     "200 OK",
			StatusCode: 200,
			Headers:    http.Header{"Content-Type": {"application/json"}},
			Body:       []byte(body),
		})
		if got := visibleLines(w.response.body); len(got) != 10 || len(f.folded) != 0 {
			t.Errorf("new response has %d lines visible", len(got))
		}

		// A response that is not JSON has nothing to fold.
		w.response.show(&rest.Response{Status: "200 OK", StatusCode: 200, Headers: http.Header{}, Body: []byte("{\n  not json\n\n}")})
		if len(f.regions) != 0 || w.response.json {
			t.Errorf("regions of plain text = %v", f.regions)
		}
	})
}

func TestFoldingWhileEditing(t *testing.T) {
	var w *Window
	mainthread.Wait(func() {
		w = NewWindow(testFiles(t).Files)
		w.Show()

		// Only a JSON body can be folded.
		w.request.bodyRaw.SetPlainText("{\n  \"a\": [\n    1,\n    2\n  ],\n  \"b\": 3\n}")
	})
	waitFor(t, "the text to be looked at", func() bool { return w.request.bodyFold.text != "" })

	mainthread.Wait(func() {
		f := w.request.bodyFold
		if len(f.regions) != 0 {
			t.Errorf("regions with no body type = %v", f.regions)
		}

		w.request.bodyType.SetCurrentIndex(1) // JSON
		if want := map[int]int{0: 6, 1: 4}; !reflect.DeepEqual(f.regions, want) {
			t.Fatalf("regions = %v, want %v", f.regions, want)
		}

		f.toggle(1)
		if got := visibleLines(w.request.bodyRaw); len(got) != 5 {
			t.Errorf("folded = %q", got)
		}

		// What is sent and saved is the whole body, folded or not.
		if got := w.request.request().Body; !strings.Contains(got, "    1,\n    2\n") {
			t.Errorf("body = %q", got)
		}

		// Typing after the fold leaves it folded.
		cursor := w.request.bodyRaw.TextCursor()
		cursor.MovePosition(qt.QTextCursor__End)
		cursor.InsertText(" ")
	})
	waitFor(t, "the edit to be looked at", func() bool { return strings.HasSuffix(w.request.bodyFold.text, "} ") })

	mainthread.Wait(func() {
		f := w.request.bodyFold
		if got := visibleLines(w.request.bodyRaw); len(got) != 5 || len(f.folded) != 1 {
			t.Errorf("after typing below the fold: %q", got)
		}

		// Changing the array that is folded unfolds it, so nothing that has
		// changed is left hidden.
		cursor := w.request.bodyRaw.TextCursor()
		cursor.SetPosition(0)
		cursor.InsertText("[\n")
	})
	waitFor(t, "the edit to be looked at", func() bool { return strings.HasPrefix(w.request.bodyFold.text, "[\n") })

	mainthread.Wait(func() {
		f := w.request.bodyFold
		if got := visibleLines(w.request.bodyRaw); len(got) != 8 || len(f.folded) != 0 {
			t.Errorf("after moving the fold: %d lines visible, folded %v", len(got), f.folded)
		}

		// Text that is not JSON any more is shown in full.
		f.toggle(2)
		w.request.bodyType.SetCurrentIndex(2) // Text
		if got := visibleLines(w.request.bodyRaw); len(got) != 8 || len(f.regions) != 0 {
			t.Errorf("as text: %d lines visible, regions %v", len(got), f.regions)
		}
	})
}

package ui

import (
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/mappu/miqt/qt6/mainthread"
)

func TestPlaceholderRanges(t *testing.T) {
	expand := func(s string) string {
		s = strings.ReplaceAll(s, "{{host}}", "example.com")
		return strings.ReplaceAll(s, "{{empty}}", "")
	}

	tests := []struct {
		text string
		want []placeholderRange
	}{
		{"no placeholders", nil},
		{"{{host}}/users/{{id}}", []placeholderRange{{0, 8, true}, {15, 6, false}}},
		// One that is defined as nothing is not known.
		{"{{host}}/{{empty}}", []placeholderRange{{0, 8, true}, {9, 9, false}}},
		// Positions are counted in UTF-16 code units: é is one, 😀 is two.
		{"é😀{{host}}😀{{id}}", []placeholderRange{{3, 8, true}, {13, 6, false}}},
	}

	for _, tt := range tests {
		if got := placeholderRanges(tt.text, expand); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("placeholderRanges(%q) = %v, want %v", tt.text, got, tt.want)
		}
	}
}

func TestPlaceholderColouringLeavesRequestAlone(t *testing.T) {
	files := testFiles(t)

	mainthread.Wait(func() {
		w := NewWindow(files.Files)
		w.Show()

		w.request.url.SetText("{{host}}/users?id={{id}}")
		w.request.bodyType.SetCurrentIndex(1) // JSON
		w.request.bodyRaw.SetPlainText(`{"url": "{{host}}"}`)
		w.requests.addRequest()
		request := w.requests.store.Folders[0].Requests[0]
		before := *request.Request

		// Changing the environment recolours the fields, which must not
		// alter them or the saved request.
		w.environments.add()
		w.editor.variables.add("host", "example.com")
		w.editor.close.Click()

		if !reflect.DeepEqual(*request.Request, before) || !reflect.DeepEqual(w.request.request(), before) {
			t.Errorf("request changed: %+v", request.Request)
		}
		if got := w.request.url.Text(); got != "{{host}}/users?id={{id}}" {
			t.Errorf("url = %q", got)
		}
	})
}

// Colouring a line edit creates short-lived Qt objects. Freeing one that Go
// also frees crashes when the garbage collector next runs.
func TestPlaceholderColouringSurvivesGC(t *testing.T) {
	mainthread.Wait(func() {
		w := NewWindow(testFiles(t).Files)
		w.Show()

		for range 50 {
			w.request.url.SetText("{{host}}/users/{{id}}")
			w.request.url.SetText("")
		}
	})

	for range 5 {
		runtime.GC()
		mainthread.Wait(func() {})
	}
}

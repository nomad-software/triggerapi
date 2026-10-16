package ui

import (
	"net/http"
	"reflect"
	"testing"

	qt "github.com/mappu/miqt/qt6"
	"github.com/mappu/miqt/qt6/mainthread"

	"github.com/nomad-software/triggerapi/internal/rest"
)

func TestFindAll(t *testing.T) {
	tests := []struct {
		name      string
		text      string
		query     string
		matchCase bool
		want      []span
	}{
		{"nothing to find", "abc", "", false, nil},
		{"not there", "abc", "d", false, nil},
		{"longer than the text", "ab", "abc", false, nil},
		{"each place", "alpha, Alphabet", "alpha", false, []span{{0, 5}, {7, 5}}},
		{"capitals told apart", "alpha, Alphabet", "Alpha", true, []span{{7, 5}}},
		{"capitals in what is typed", "alpha", "ALPHA", false, []span{{0, 5}}},
		{"no overlapping", "aaaa", "aa", false, []span{{0, 2}, {2, 2}}},
		{"across lines", "a\nb\na", "a", false, []span{{0, 1}, {4, 1}}},
		// Positions are in UTF-16 code units: é is one, and 😀 two.
		{"after other letters", "é😀 id, 😀", "id", false, []span{{4, 2}}},
		{"other letters", "é😀 id, 😀", "😀", false, []span{{1, 2}, {8, 2}}},
	}

	for _, test := range tests {
		if got := findAll(test.text, test.query, test.matchCase); !reflect.DeepEqual(got, test.want) {
			t.Errorf("%s: findAll(%q, %q, %v) = %v, want %v", test.name, test.text, test.query, test.matchCase, got, test.want)
		}
	}
}

// Text is found in a response with the bar above its body: every match is
// counted, the one gone to is selected, and what it is folded away in is
// unfolded.
func TestFindInResponse(t *testing.T) {
	mainthread.Wait(func() {
		w := NewWindow(testFiles(t).Files)
		w.Show()

		for _, p := range []*responsePanel{w.response, w.grpcResponse} {
			f := p.find
			selected := func() string {
				cursor := p.body.TextCursor()
				defer release(cursor)
				return cursor.SelectedText()
			}
			show := func(body string) {
				p.show(&rest.Response{Status: "200 OK", StatusCode: 200, Headers: http.Header{"Content-Type": {"application/json"}}, Body: []byte(body)})
			}

			// The bar is above the body, and hidden until it is asked for.
			if !f.root.IsHidden() || f.isOpen() || !p.root.IsAncestorOf(f.button.QWidget) || !p.root.IsAncestorOf(f.root) {
				t.Fatal("the bar is shown at first, or it or its button is not in the panel")
			}
			if layout := f.root.ParentWidget().Layout(); layout.IndexOf(f.root) != 0 || layout.IndexOf(p.body.QWidget) != 1 {
				t.Error("the bar is not above the body")
			}

			show(`{"items":[{"name":"alpha"},{"name":"Alphabet"}],"other":"beta"}`)
			p.tabs.SetCurrentIndex(tabResponseHeaders)
			f.open()
			if f.root.IsHidden() || !f.isOpen() || p.tabs.CurrentIndex() != tabResponse {
				t.Fatal("the bar is not shown, or not on the tab of the response")
			}

			// The first match is selected as soon as there is one.
			f.field.SetText("alpha")
			if got := f.count.Text(); got != "1 of 2" || selected() != "alpha" {
				t.Errorf("count = %q, selected %q", got, selected())
			}
			f.step(1)
			if got := f.count.Text(); got != "2 of 2" || selected() != "Alpha" {
				t.Errorf("after next: count = %q, selected %q", got, selected())
			}
			// It goes round from the last to the first, and back.
			f.step(1)
			if got := f.count.Text(); got != "1 of 2" {
				t.Errorf("after the last: count = %q", got)
			}
			f.step(-1)
			if got := f.count.Text(); got != "2 of 2" {
				t.Errorf("before the first: count = %q", got)
			}

			// Typing more of a word stays on the match it is in.
			f.field.SetText("alphab")
			if got := f.count.Text(); got != "1 of 1" || selected() != "Alphab" {
				t.Errorf("after more: count = %q, selected %q", got, selected())
			}

			f.matchCase.SetChecked(true)
			if got := f.count.Text(); got != "No matches" || selected() != "" || f.field.StyleSheet() == "" {
				t.Errorf("with capitals told apart: count = %q, selected %q, style %q", got, selected(), f.field.StyleSheet())
			}
			f.matchCase.SetChecked(false)
			if got := f.count.Text(); got != "1 of 1" || f.field.StyleSheet() != "" {
				t.Errorf("with capitals not told apart: count = %q, style %q", got, f.field.StyleSheet())
			}

			// A match that is folded away is counted, and unfolded when
			// it is gone to.
			f.field.SetText("")
			p.fold.toggle(1)
			line := p.body.Document().FindBlockByNumber(3)
			if line.IsVisible() {
				t.Fatal("the items are not folded")
			}
			f.field.SetText("alphabet")
			if got := f.count.Text(); got != "1 of 1" || !line.IsVisible() || len(p.fold.folded) != 0 {
				t.Errorf("in a fold: count = %q, folded %v", got, p.fold.folded)
			}

			// What is being looked for is looked for in the next response.
			f.field.SetText("beta")
			show(`{"a":"beta","b":"beta","c":"beta"}`)
			if got := f.count.Text(); got != "1 of 3" || selected() != "beta" {
				t.Errorf("in another response: count = %q, selected %q", got, selected())
			}

			// Escape closes the bar, which keeps what was typed and
			// selected.
			w.stack.SetCurrentWidget(p.root.ParentWidget())
			w.escape()
			if f.isOpen() || !f.root.IsHidden() || f.query != "" || selected() != "beta" {
				t.Errorf("after escape: open %v, query %q, selected %q", f.isOpen(), f.query, selected())
			}

			// Text selected in the response is what is looked for.
			cursor := p.body.TextCursor()
			cursor.SetPosition(5)
			cursor.SetPosition2(6, qt.QTextCursor__KeepAnchor)
			p.body.SetTextCursor(cursor)
			w.find()
			if !f.isOpen() || f.field.Text() != `a` || f.count.Text() != "1 of 4" {
				t.Errorf("from the selection: open %v, text %q, count %q", f.isOpen(), f.field.Text(), f.count.Text())
			}
			f.close()
		}
	})
}

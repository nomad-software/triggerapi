package ui

// Benchmarks of the work of the user interface that costs the most: making
// the window, drawing the request tree and the history, showing a large JSON
// response and switching between requests. Each runs its loop on the GUI
// thread, so what is measured is the work of Qt and the bindings, not the
// handing of work between threads. Run them with make bench.

import (
	"github.com/nomad-software/qute"
	"github.com/nomad-software/qute/qtcore"

	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/nomad-software/triggerapi/internal/collection"
	"github.com/nomad-software/triggerapi/internal/history"
	"github.com/nomad-software/triggerapi/internal/rest"
)

// benchWindow makes a window that is shown, for a benchmark.
func benchWindow(b *testing.B) *Window {
	b.Helper()
	files := testFiles(b).Files
	var w *Window
	qute.PostSync(func() {
		w = NewWindow(files)
		w.Show()
	})
	return w
}

// firstOf returns the first request of a type in the store.
func firstOf(store *collection.Store, typ collection.Type) *collection.Request {
	for _, f := range store.Folders {
		for _, r := range f.All() {
			if r.Type == typ {
				return r
			}
		}
	}
	return nil
}

// benchStore is a store of folders requests of every type, as many as
// folders times perFolder.
func benchStore(folders, perFolder int) *collection.Store {
	store := &collection.Store{}
	for f := range folders {
		folder := store.AddFolder(fmt.Sprintf("Folder %03d", f))
		sub := folder.AddFolder("Inner")
		for r := range perFolder {
			typ := requestTypes[r%len(requestTypes)].typ
			into := folder
			if r%3 == 0 {
				into = sub
			}
			req := collection.New(typ, fmt.Sprintf("Request %03d", r))
			if req.Request != nil {
				req.URL = fmt.Sprintf("https://example.com/{{base}}/items/%d", r)
			}
			into.Add(req)
		}
	}
	store.Sort()
	return store
}

// benchJSON is a JSON body of n objects, set out on one line, as a server
// sends one.
func benchJSON(n int) []byte {
	var b strings.Builder
	b.WriteString(`{"items":[`)
	for i := range n {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, `{"id":%d,"name":"item %d","price":%d.5,"tags":["a","b","c"],"active":true,"owner":{"name":"someone","email":"someone@example.com"}}`, i, i, i)
	}
	b.WriteString(`]}`)
	return []byte(b.String())
}

// BenchmarkNewWindow makes a window, shows it and closes it.
func BenchmarkNewWindow(b *testing.B) {
	files := testFiles(b).Files
	qute.PostSync(func() {
		b.ResetTimer()
		for range b.N {
			w := NewWindow(files)
			w.Show()
			w.Close()
		}
	})
}

// BenchmarkDrawRequests fills the tree from a store of 2,000 requests in 50
// folders, which happens on every change to it.
func BenchmarkDrawRequests(b *testing.B) {
	w := benchWindow(b)
	qute.PostSync(func() {
		w.requests.load(benchStore(50, 40), "")
		b.ResetTimer()
		for range b.N {
			w.requests.draw(nil)
		}
	})
}

// BenchmarkSearchRequests narrows the tree of 2,000 requests to those
// matching a search, and clears it.
func BenchmarkSearchRequests(b *testing.B) {
	w := benchWindow(b)
	qute.PostSync(func() {
		w.requests.load(benchStore(50, 40), "")
		b.ResetTimer()
		for range b.N {
			w.requests.search.SetText("request 01")
			w.requests.search.SetText("")
		}
	})
}

// BenchmarkDrawHistory fills the history from 1,000 entries over 20 days.
func BenchmarkDrawHistory(b *testing.B) {
	w := benchWindow(b)
	qute.PostSync(func() {
		store := &history.Store{}
		start := time.Now()
		for i := range 1000 {
			r := collection.New(requestTypes[i%len(requestTypes)].typ, fmt.Sprintf("Request %d", i))
			store.Entries = append(store.Entries, &history.Entry{
				Time:     start.Add(-time.Duration(i) * 30 * time.Minute),
				Request:  r,
				URL:      fmt.Sprintf("https://example.com/items/%d", i),
				Duration: time.Duration(i) * time.Millisecond,
				HTTP:     &rest.Response{Status: "200 OK", StatusCode: 200},
			})
		}
		w.history.load(store, "")
		b.ResetTimer()
		for range b.N {
			w.history.draw(nil)
		}
	})
}

// BenchmarkShowJSON shows a response of 2,000 objects (about 300 KB), which
// is set out, coloured and given its folds.
func BenchmarkShowJSON(b *testing.B) {
	w := benchWindow(b)
	res := &rest.Response{
		Status:     "200 OK",
		StatusCode: 200,
		Headers:    http.Header{"Content-Type": {"application/json"}},
		Body:       benchJSON(2000),
	}
	qute.PostSync(func() {
		b.ResetTimer()
		for range b.N {
			w.response.show(res)
			// The folds are found a turn of the event loop later.
			qtcore.QCoreApplicationProcessEvents()
		}
	})
}

// BenchmarkSwitchRequests goes back and forth between a REST and a gRPC
// request, as choosing them in the tree or their tabs does.
func BenchmarkSwitchRequests(b *testing.B) {
	w := benchWindow(b)
	qute.PostSync(func() {
		w.requests.load(benchStore(2, 10), "")
		rest := firstOf(w.requests.store, collection.TypeREST)
		grpc := firstOf(w.requests.store, collection.TypeGRPC)
		b.ResetTimer()
		for i := range b.N {
			if i%2 == 0 {
				w.show(rest)
			} else {
				w.show(grpc)
			}
		}
	})
}

// BenchmarkDrawIcons draws the icon of every type of request, as the
// colours changing does.
func BenchmarkDrawIcons(b *testing.B) {
	qute.PostSync(func() {
		b.ResetTimer()
		for range b.N {
			for _, t := range requestTypes {
				icon := drawIcon(iconSize*2, themeColour(t.colour), t.glyph)
				icon.Delete()
			}
		}
	})
}

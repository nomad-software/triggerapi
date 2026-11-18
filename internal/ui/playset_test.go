package ui

import (
	"github.com/nomad-software/qute"

	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/nomad-software/triggerapi/internal/assert"
	"github.com/nomad-software/triggerapi/internal/capture"
	"github.com/nomad-software/triggerapi/internal/collection"
	"github.com/nomad-software/triggerapi/internal/env"
	"github.com/nomad-software/triggerapi/internal/rest"
)

func TestPlaySet(t *testing.T) {
	var mu sync.Mutex
	hits := map[string]int{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits[r.URL.Query().Get("n")]++
		mu.Unlock()
		if r.URL.Path == "/fail" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":"7"}`))
	}))
	defer srv.Close()
	sent := func() map[string]int {
		mu.Lock()
		defer mu.Unlock()
		copied := map[string]int{}
		for k, v := range hits {
			copied[k] = v
		}
		clear(hits)
		return copied
	}

	files := testFiles(t)
	envs := env.Store{Environments: []env.Environment{{Name: "Dev"}}}
	if err := envs.Save(files.Environments); err != nil {
		t.Fatal(err)
	}
	store := &collection.Store{}
	folder := store.AddFolder("One")
	get := func(name string) *collection.Request {
		return folder.Add(collection.NewREST(name, rest.Request{Method: "GET", URL: srv.URL + "/ok", Params: []rest.KeyValue{{Key: "n", Value: name, Enabled: true}}}))
	}
	a, b, c := get("A"), get("B"), get("C")
	a.Captures = []capture.Rule{{Variable: "id", Path: "id", Enabled: true}}
	store.AddSet("Work", []*collection.Request{a, b, c})
	if err := store.Save(files.Requests); err != nil {
		t.Fatal(err)
	}

	var w *Window
	var played *collection.Request
	// play plays the tabs, and waits for the playing to end.
	play := func(what string) {
		t.Helper()
		qute.PostSync(func() {
			if !w.environment.play.IsEnabled() {
				t.Errorf("%s: play is disabled", what)
			}
			w.environment.play.Click()
		})
		waitFor(t, what, func() bool { return w.playing == nil })
		played = onGUI(func() *collection.Request { return w.tabs.current().request })
	}
	// request returns a request of the window's store by its name.
	request := func(name string) *collection.Request {
		return onGUI(func() *collection.Request {
			for _, r := range w.requests.store.Folders[0].Requests {
				if r.Name == name {
					return r
				}
			}
			return nil
		})
	}
	qute.PostSync(func() {
		w = NewWindow(files.Files)
		w.Show()
		w.environments.list.SetCurrentRow(0)
		if w.environment.play.IsEnabled() {
			t.Error("play is enabled with no pinned tabs")
		}
		w.requests.sets.list.SetCurrentItem(w.requests.sets.list.Item(0))
	})

	// Every tab is played, from left to right, ending at the last.
	play("everything played")
	if got := sent(); got["A"] != 1 || got["B"] != 1 || got["C"] != 1 {
		t.Errorf("sent %v", got)
	}
	if played == nil || played.Name != "C" {
		t.Errorf("ended at %+v", played)
	}
	if got := onGUI(func() string { return w.expand("{{id}}") }); got != "7" {
		t.Errorf("captured %q", got)
	}

	// It stops at a failed status, a value not captured or a failed
	// assertion, with the tab of the request on show.
	stops := []struct {
		what  string
		spoil func(*collection.Request)
	}{
		{"a failed status", func(r *collection.Request) { r.URL = srv.URL + "/fail" }},
		{"a failed capture", func(r *collection.Request) {
			r.URL = srv.URL + "/ok"
			r.Captures = []capture.Rule{{Variable: "x", Path: "missing", Enabled: true}}
		}},
		{"a failed assertion", func(r *collection.Request) {
			r.Captures = nil
			r.Assertions = []assert.Check{{Subject: assert.Status, Comparison: assert.Equals, Value: "201", Enabled: true}}
		}},
	}
	for _, stop := range stops {
		r := request("B")
		qute.PostSync(func() { stop.spoil(r) })
		play(stop.what)
		if got := sent(); got["A"] != 1 || got["B"] != 1 || got["C"] != 0 {
			t.Errorf("%s: sent %v", stop.what, got)
		}
		if played == nil || played.Name != "B" {
			t.Errorf("%s: stopped at %+v", stop.what, played)
		}
	}

	// Stopping cancels the request in flight and plays no more.
	qute.PostSync(func() {
		request := w.requests.store.Folders[0].Requests[1]
		request.Assertions = nil
		w.playSet()
		if w.playing == nil {
			t.Error("not playing")
			return
		}
		w.playSet()
		if w.playing != nil {
			t.Error("not stopped")
		}
	})
}

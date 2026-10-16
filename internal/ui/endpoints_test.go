package ui

import (
	"reflect"
	"slices"
	"testing"

	qt "github.com/mappu/miqt/qt6"
	"github.com/mappu/miqt/qt6/mainthread"

	"github.com/nomad-software/triggerapi/internal/collection"
	"github.com/nomad-software/triggerapi/internal/rpc"
	"github.com/nomad-software/triggerapi/internal/rpc/rpctest"
)

func TestGRPCEndpoints(t *testing.T) {
	srv := rpctest.NewServer(rpctest.ReflectionAll)
	defer srv.Close()

	files := testFiles(t)
	var w *Window
	mainthread.Wait(func() {
		w = NewWindow(files.Files)
		w.Show()

		// There is nothing to choose until the server has been asked.
		addGRPCRequest(w)
		e := w.grpc.endpoints
		if e.pages.CurrentIndex() != endpointPageHint || e.toggle.IsEnabled() || e.create.IsEnabled() {
			t.Error("endpoints can be chosen before there are any")
		}

		w.grpc.url.SetText(srv.URL)
		w.grpc.tls.SetChecked(false)
		w.grpc.message.SetPlainText(`{"text": "hello"}`)
		w.grpc.reload.Click()
	})

	waitFor(t, "the endpoints", func() bool { return w.grpc.endpoints.count() == 4 })

	mainthread.Wait(func() {
		e := w.grpc.endpoints

		// The methods are listed by their short names under the service,
		// with none of them ticked.
		if e.pages.CurrentIndex() != endpointPageTable || e.table.RowCount() != 5 || e.table.Item(0, colEndpointChosen).Text() != "test.v1.Echo" || e.table.ColumnSpan(0, colEndpointChosen) != 2 {
			t.Errorf("rows = %d, names %q", e.table.RowCount(), e.names)
		}
		row := slices.Index(e.names, "test.v1.Echo/Say")
		if row < 1 || e.table.Item(row, colEndpointName).Text() != "Say" {
			t.Fatalf("Say is on row %d of %q", row, e.names)
		}
		if len(e.chosen()) != 0 || e.create.IsEnabled() || e.toggle.Text() != "Select All" {
			t.Errorf("chosen = %q, toggle %q", e.chosen(), e.toggle.Text())
		}

		// The one button ticks them all, and then none.
		e.toggle.Click()
		if len(e.chosen()) != 4 || !e.create.IsEnabled() || e.toggle.Text() != "Select None" {
			t.Errorf("chosen = %q, toggle %q", e.chosen(), e.toggle.Text())
		}
		e.toggle.Click()
		if len(e.chosen()) != 0 || e.create.IsEnabled() || e.toggle.Text() != "Select All" {
			t.Errorf("chosen = %q, toggle %q", e.chosen(), e.toggle.Text())
		}

		// Ticking the last of them is the same as ticking them all.
		for r, name := range e.names {
			if name != "" {
				e.table.Item(r, colEndpointChosen).SetCheckState(qt.Checked)
			}
		}
		if e.toggle.Text() != "Select None" {
			t.Errorf("toggle = %q with all ticked", e.toggle.Text())
		}
		e.toggle.Click()

		// A request is made of each that is ticked, in a folder for its
		// service inside one for the server.
		e.table.Item(row, colEndpointChosen).SetCheckState(qt.Checked)
		e.table.Item(slices.Index(e.names, "test.v1.Echo/Fail"), colEndpointChosen).SetCheckState(qt.Checked)
		open := w.open
		w.sidebar.SetCurrentIndex(tabEnvironments)
		e.create.Click()

		if w.open != open || w.sidebar.CurrentIndex() != tabRequests {
			t.Errorf("open = %+v, sidebar tab %d", w.open, w.sidebar.CurrentIndex())
		}
		if len(e.chosen()) != 2 {
			t.Errorf("chosen = %q after creating", e.chosen())
		}
	})

	saved, err := loadRequests(files.Requests)
	if err != nil {
		t.Fatal(err)
	}
	index := slices.IndexFunc(saved.Folders, func(f *collection.Folder) bool { return f.Name == srv.URL })
	if index < 0 {
		t.Fatalf("no folder named %q", srv.URL)
	}
	folder := saved.Folders[index]
	if len(folder.Requests) != 0 || len(folder.Folders) != 1 || folder.Folders[0].Name != "test.v1.Echo" {
		t.Fatalf("folder = %+v", folder)
	}
	requests := folder.Folders[0].Requests
	if len(requests) != 2 || requests[0].Name != "Fail" || requests[1].Name != "Say" {
		t.Fatalf("requests = %+v", requests)
	}
	for _, r := range requests {
		// The requests are for the same server, and have no message.
		want := rpc.Request{URL: srv.URL, Method: "test.v1.Echo/" + r.Name}
		if r.Type != collection.TypeGRPC || !reflect.DeepEqual(*r.GRPC, want) {
			t.Errorf("request %q = %+v, want %+v", r.Name, *r.GRPC, want)
		}
	}
}

func TestGRPCEndpointSearch(t *testing.T) {
	srv := rpctest.NewServer(rpctest.ReflectionAll)
	defer srv.Close()

	var w *Window
	mainthread.Wait(func() {
		w = NewWindow(testFiles(t).Files)
		w.Show()

		// There is nothing to search until the server has been asked.
		addGRPCRequest(w)
		if w.grpc.endpoints.search.IsEnabled() {
			t.Error("the search can be used before there are endpoints")
		}
		w.grpc.url.SetText(srv.URL)
		w.grpc.reload.Click()
	})

	waitFor(t, "the endpoints", func() bool { return w.grpc.endpoints.count() == 4 })

	mainthread.Wait(func() {
		e := w.grpc.endpoints

		// shown lists the rows that are not hidden, by the name of the
		// service or the method.
		shown := func() []string {
			var rows []string
			for row := range e.table.RowCount() {
				if e.table.IsRowHidden(row) {
					continue
				}
				if e.names[row] == "" {
					rows = append(rows, e.table.Item(row, colEndpointChosen).Text())
				} else {
					rows = append(rows, e.table.Item(row, colEndpointName).Text())
				}
			}
			return rows
		}
		everything := []string{"test.v1.Echo", "Fail", "Forever", "Repeat", "Say"}
		if !e.search.IsEnabled() || !reflect.DeepEqual(shown(), everything) {
			t.Errorf("shown = %q", shown())
		}

		// The methods that match are left under their service, whatever
		// the case of what is typed.
		e.search.SetText("FOR")
		if got := shown(); !reflect.DeepEqual(got, []string{"test.v1.Echo", "Forever"}) {
			t.Errorf("shown = %q for FOR", got)
		}
		// Every word has to be found, in the method or its service.
		e.search.SetText("echo re")
		if got := shown(); !reflect.DeepEqual(got, []string{"test.v1.Echo", "Forever", "Repeat"}) {
			t.Errorf("shown = %q for echo re", got)
		}

		// The toggle is for the methods on show.
		if e.toggle.Text() != "Select All" {
			t.Errorf("toggle = %q", e.toggle.Text())
		}
		e.toggle.Click()
		if got := e.chosen(); !reflect.DeepEqual(got, []string{"test.v1.Echo/Forever", "test.v1.Echo/Repeat"}) || e.toggle.Text() != "Select None" {
			t.Errorf("chosen = %q, toggle %q", got, e.toggle.Text())
		}

		// A service with nothing found loses its heading, and what is
		// hidden stays ticked.
		e.search.SetText("nothing like it")
		if got := shown(); len(got) != 0 || e.toggle.IsEnabled() || len(e.chosen()) != 2 || !e.create.IsEnabled() {
			t.Errorf("shown = %q, chosen %q", got, e.chosen())
		}

		// Clearing the search shows them all again, not all of them ticked.
		e.search.Clear()
		if !reflect.DeepEqual(shown(), everything) || e.toggle.Text() != "Select All" || len(e.chosen()) != 2 {
			t.Errorf("shown = %q, toggle %q", shown(), e.toggle.Text())
		}

		// A search carries on into the endpoints listed next.
		e.search.SetText("say")
		w.grpc.reload.Click()
	})

	waitFor(t, "the endpoints again", func() bool {
		return mainthread.Wait2(func() bool {
			e := w.grpc.endpoints
			return e.count() == 4 && len(e.chosen()) == 0 && !e.table.IsRowHidden(slices.Index(e.names, "test.v1.Echo/Say"))
		})
	})
	mainthread.Wait(func() {
		e := w.grpc.endpoints
		if e.table.IsRowHidden(0) || !e.table.IsRowHidden(slices.Index(e.names, "test.v1.Echo/Fail")) {
			t.Error("the search was not applied to the endpoints listed again")
		}
	})
}

func TestGRPCEndpointCollection(t *testing.T) {
	srv := rpctest.NewServer(rpctest.ReflectionAll)
	defer srv.Close()

	const fail, forever, repeat, say = "test.v1.Echo/Fail", "test.v1.Echo/Forever", "test.v1.Echo/Repeat", "test.v1.Echo/Say"

	files := testFiles(t)
	var w *Window
	mainthread.Wait(func() {
		w = NewWindow(files.Files)
		w.Show()
		w.requests.confirm = func(string) bool { return true }

		addGRPCRequest(w)
		w.grpc.url.SetText(srv.URL)
		w.grpc.reload.Click()
	})

	waitFor(t, "the endpoints", func() bool { return w.grpc.endpoints.count() == 4 })

	// tick ticks the box of a method as a click would, which does nothing
	// to one that cannot be changed.
	tick := func(method string, state qt.CheckState) {
		e := w.grpc.endpoints
		if item := e.table.Item(slices.Index(e.names, method), colEndpointChosen); item.Flags()&qt.ItemIsUserCheckable != 0 {
			item.SetCheckState(state)
		}
	}
	// named returns the methods of the requests in the service folder of a
	// collection.
	methods := func(outer *collection.Folder) []string {
		var methods []string
		for _, r := range outer.Folders[0].Requests {
			methods = append(methods, r.GRPC.Method)
		}
		return methods
	}
	open := func(r *collection.Request) {
		w.requests.selectRequest(r)
		w.openRequest(r)
	}

	var first, second *collection.Folder
	mainthread.Wait(func() {
		e := w.grpc.endpoints

		// Two collections are made, each with the same two requests. The
		// request on show is in neither, so nothing is ticked for it.
		tick(fail, qt.Checked)
		tick(say, qt.Checked)
		e.create.Click()
		e.create.Click()
		if e.create.Text() != "Create Requests" || len(e.existing) != 0 {
			t.Errorf("button = %q, existing %v", e.create.Text(), e.existing)
		}
		for _, f := range w.requests.store.Folders {
			if f.Name != srv.URL {
				continue
			}
			if first == nil {
				first = f
			} else {
				second = f
			}
		}
		if first == nil || second == nil {
			t.Error("the collections were not made")
			return
		}
		// The name of the collection is not what it is known by.
		first.Name = "Anything"

		// A request of one of them has its endpoints ticked, and they
		// cannot be unticked.
		open(first.Folders[0].Requests[1])
	})
	if first == nil || second == nil {
		t.FailNow()
	}

	waitFor(t, "the endpoints of the collection", func() bool { return len(w.grpc.endpoints.existing) == 2 })

	mainthread.Wait(func() {
		e := w.grpc.endpoints

		if w.open.GRPC.Method != say || !reflect.DeepEqual(e.chosen(), []string{fail, say}) || len(e.added()) != 0 {
			t.Errorf("open %q, chosen %q, added %q", w.open.GRPC.Method, e.chosen(), e.added())
		}
		if e.create.Text() != "Update Requests" || e.create.IsEnabled() || e.toggle.Text() != "Select All" {
			t.Errorf("button = %q, toggle %q", e.create.Text(), e.toggle.Text())
		}
		tick(fail, qt.Unchecked)
		tick(say, qt.Unchecked)
		if !reflect.DeepEqual(e.chosen(), []string{fail, say}) {
			t.Errorf("chosen = %q after unticking", e.chosen())
		}

		// The toggle is for the others alone.
		e.toggle.Click()
		if !reflect.DeepEqual(e.added(), []string{forever, repeat}) || e.toggle.Text() != "Select None" || !e.create.IsEnabled() {
			t.Errorf("added = %q, toggle %q", e.added(), e.toggle.Text())
		}
		e.toggle.Click()
		if !reflect.DeepEqual(e.chosen(), []string{fail, say}) || e.create.IsEnabled() {
			t.Errorf("chosen = %q after select none", e.chosen())
		}

		// Another that is ticked is added to this collection and to no
		// other, and the request stays the one on show.
		was := w.open
		tick(repeat, qt.Checked)
		e.create.Click()
		if got := methods(first); !reflect.DeepEqual(got, []string{fail, repeat, say}) {
			t.Errorf("collection = %q", got)
		}
		if got := methods(second); !reflect.DeepEqual(got, []string{fail, say}) || len(w.requests.store.Folders) != 3 {
			t.Errorf("other collection = %q of %d folders", got, len(w.requests.store.Folders))
		}
		added := first.Folders[0].Requests[1]
		if added.Name != "Repeat" || !reflect.DeepEqual(*added.GRPC, rpc.Request{URL: srv.URL, Method: repeat}) {
			t.Errorf("added %q = %+v", added.Name, *added.GRPC)
		}
		if w.open != was || len(e.existing) != 3 || len(e.added()) != 0 || e.create.IsEnabled() {
			t.Errorf("open = %+v, existing %v, added %q", w.open, e.existing, e.added())
		}
		tick(repeat, qt.Unchecked)
		if len(e.chosen()) != 3 {
			t.Errorf("chosen = %q after unticking the one added", e.chosen())
		}

		// A request is only removed in the sidebar, which unticks it.
		w.requests.selectRequest(added)
		w.requests.remove()
		open(was)
	})

	waitFor(t, "the endpoints of the collection", func() bool { return len(w.grpc.endpoints.existing) == 2 })

	mainthread.Wait(func() {
		if e := w.grpc.endpoints; !reflect.DeepEqual(e.chosen(), []string{fail, say}) || len(e.free()) != 2 {
			t.Errorf("chosen = %q after removing, %d free", e.chosen(), e.free())
		}
	})

	saved, err := loadRequests(files.Requests)
	if err != nil {
		t.Fatal(err)
	}
	if got := methods(saved.Folders[0]); saved.Folders[0].Name != "Anything" || !reflect.DeepEqual(got, []string{fail, say}) {
		t.Errorf("saved %q = %q", saved.Folders[0].Name, got)
	}
}

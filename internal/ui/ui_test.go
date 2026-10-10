package ui

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	qt "github.com/mappu/miqt/qt6"
	"github.com/mappu/miqt/qt6/mainthread"

	"github.com/nomad-software/triggerapi/internal/collection"
	"github.com/nomad-software/triggerapi/internal/env"
	"github.com/nomad-software/triggerapi/internal/rest"
	"github.com/nomad-software/triggerapi/internal/rpc"
	"github.com/nomad-software/triggerapi/internal/rpc/rpctest"
	"github.com/nomad-software/triggerapi/internal/settings"
)

// TestMain runs the Qt event loop on the main thread and the tests on another
// goroutine. Tests must touch widgets through mainthread.Wait.
func TestMain(m *testing.M) {
	os.Setenv("QT_QPA_PLATFORM", "offscreen")
	qt.NewQApplication(os.Args[:1])
	// Tests close windows, which must not end the event loop.
	qt.QGuiApplication_SetQuitOnLastWindowClosed(false)
	// Answer the request type dialog rather than show it.
	askRequestType = func(*qt.QWidget) (collection.Type, bool) {
		return testRequestType, testRequestType != ""
	}

	code := 0
	go func() {
		code = m.Run()
		mainthread.Start(qt.QCoreApplication_Quit)
	}()

	qt.QApplication_Exec()
	os.Exit(code)
}

// testRequestType is the type given to requests added by a test. An empty
// type stands for the user cancelling the dialog.
var testRequestType = collection.TypeREST

func testFiles(t *testing.T) Files {
	dir := t.TempDir()
	return Files{
		Environments: filepath.Join(dir, "environments.json"),
		Requests:     filepath.Join(dir, "requests.json"),
		Settings:     filepath.Join(dir, "settings.json"),
	}
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
		w = NewWindow(testFiles(t))
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
		return fieldValue(w.response, "Code") == "201 Created"
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

func TestKeyValueTable(t *testing.T) {
	mainthread.Wait(func() {
		table := newKeyValueTable("Key", "Value")

		// Typing into the trailing row adds a new empty one.
		table.table.Item(0, colKey).SetText("a")
		if table.table.RowCount() != 2 {
			t.Fatalf("rows = %d, want 2", table.table.RowCount())
		}

		table.add("b", "2")
		table.table.Item(1, colEnabled).SetCheckState(qt.Unchecked)

		items := table.items()
		if len(items) != 2 || !items[0].Enabled || items[1].Enabled || items[1].Value != "2" {
			t.Errorf("items = %+v", items)
		}

		table.table.SetCurrentCell(0, colKey)
		table.removeCurrent()
		if items := table.items(); len(items) != 1 || items[0].Key != "b" {
			t.Errorf("items after remove = %+v", items)
		}
	})
}

func TestEnvironments(t *testing.T) {
	got := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got <- r.URL.Path + " " + r.Header.Get("X-Key")
	}))
	defer srv.Close()

	path := filepath.Join(t.TempDir(), "environments.json")

	mainthread.Wait(func() {
		w := NewWindow(Files{Environments: path, Requests: filepath.Join(filepath.Dir(path), "requests.json")})
		w.Show()

		// Editing with nothing selected does nothing.
		w.environments.edit()
		if w.stack.CurrentIndex() != pageRequest {
			t.Error("editor opened with no environment selected")
		}

		// Adding an environment opens it in the editor.
		w.environments.add()
		if w.stack.CurrentIndex() != pageEditor {
			t.Fatal("editor not opened for a new environment")
		}
		w.editor.name.SetText("Local")
		w.editor.variables.add("host", srv.URL)
		w.editor.variables.add("key", "wrong")
		if got := w.environments.list.Item(0).Text(); got != "Local" {
			t.Errorf("list item = %q", got)
		}

		w.environments.add()
		w.editor.name.SetText("Other")
		w.editor.variables.add("key", "secret")

		// The open editor follows the selection.
		w.environments.list.SetCurrentRow(0)
		if items := w.editor.variables.items(); w.editor.name.Text() != "Local" || len(items) != 2 || items[0].Key != "host" {
			t.Errorf("editor shows %q %+v", w.editor.name.Text(), items)
		}
		w.editor.variables.table.Item(1, colValue).SetText("secret")

		w.editor.close.Click()
		if w.stack.CurrentIndex() != pageRequest {
			t.Error("editor not closed")
		}

		// Selecting with the editor closed does not reopen it.
		w.environments.list.SetCurrentRow(1)
		if w.stack.CurrentIndex() != pageRequest {
			t.Error("editor reopened by selection")
		}

		w.request.url.SetText("{{host}}/users/{{id}}")
		if got := w.request.preview.Text(); !strings.Contains(got, "{{id}}") || !strings.Contains(got, "{{host}}") {
			t.Errorf("preview = %q", got)
		}
		w.environments.list.SetCurrentRow(0)
		if got := w.request.preview.Text(); !strings.Contains(got, "{{id}}") || strings.Contains(got, "{{host}}") {
			t.Errorf("preview = %q", got)
		}
		w.request.url.SetText("plain.test")
		if !w.request.preview.IsHidden() {
			t.Error("preview shown without placeholders")
		}
		w.request.url.SetText("{{host}}/users")
		if got, want := w.request.preview.Text(), "→ "+srv.URL+"/users"; got != want || w.request.preview.IsHidden() {
			t.Errorf("preview = %q, want %q", got, want)
		}

		w.request.headers.add("X-Key", "{{key}}")
		w.request.send.Click()
	})

	select {
	case r := <-got:
		if r != "/users secret" {
			t.Errorf("server received %q", r)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("request was not sent")
	}

	want := &env.Store{Active: 0, Environments: []env.Environment{
		{Name: "Local", Variables: []env.Variable{
			{Key: "host", Value: srv.URL, Enabled: true},
			{Key: "key", Value: "secret", Enabled: true},
		}},
		{Name: "Other", Variables: []env.Variable{{Key: "key", Value: "secret", Enabled: true}}},
	}}
	saved, err := env.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(saved, want) {
		t.Errorf("saved %+v, want %+v", saved, want)
	}

	// A new window restores the environments and the selection.
	mainthread.Wait(func() {
		w := NewWindow(Files{Environments: path, Requests: filepath.Join(filepath.Dir(path), "requests.json")})

		if w.environments.list.Count() != 2 || w.environments.list.CurrentRow() != 0 || w.environments.list.Item(1).Text() != "Other" {
			t.Errorf("list not restored: count %d, current %d", w.environments.list.Count(), w.environments.list.CurrentRow())
		}
		if w.stack.CurrentIndex() != pageRequest {
			t.Error("editor open at startup")
		}

		w.environments.edit()
		wantItems := []rest.KeyValue{{Key: "host", Value: srv.URL, Enabled: true}, {Key: "key", Value: "secret", Enabled: true}}
		if items := w.editor.variables.items(); !reflect.DeepEqual(items, wantItems) {
			t.Errorf("variables = %+v", items)
		}

		// Removing the environment being edited moves the editor to the
		// next one, and closes it when none are left.
		w.environments.remove()
		if w.environments.list.Count() != 1 || w.environments.store.Environments[0].Name != "Other" || w.editor.name.Text() != "Other" {
			t.Errorf("remove left %+v, editing %q", w.environments.store.Environments, w.editor.name.Text())
		}
		w.environments.remove()
		if w.stack.CurrentIndex() != pageRequest || w.environments.store.Active != -1 {
			t.Errorf("editor still open with no environments, active %d", w.environments.store.Active)
		}
	})
}

func TestEnvironmentsUnreadableFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "environments.json")
	os.WriteFile(path, []byte("{oops"), 0o644)

	mainthread.Wait(func() {
		w := NewWindow(Files{Environments: path, Requests: filepath.Join(filepath.Dir(path), "requests.json")})
		if !strings.Contains(w.StatusBar().CurrentMessage(), "could not be loaded") {
			t.Error("no error shown")
		}
		w.environments.add()
	})

	if data, _ := os.ReadFile(path); string(data) != "{oops" {
		t.Errorf("file was overwritten: %q", data)
	}
}

func TestRequests(t *testing.T) {
	files := testFiles(t)

	mainthread.Wait(func() {
		w := NewWindow(files)
		w.Show()
		tree := w.requests.tree

		// Adding a request with nothing open saves what is being composed,
		// creating a folder to hold it.
		w.request.url.SetText("{{host}}/users")
		w.requests.addRequest()
		if tree.TopLevelItemCount() != 1 || tree.TopLevelItem(0).ChildCount() != 1 {
			t.Fatal("folder and request not created")
		}
		list := w.requests.store.Folders[0].Requests[0]
		if w.open != list || list.URL != "{{host}}/users" || w.request.header.request.Text() != "New Request" {
			t.Errorf("open = %+v, title %q", w.open, w.request.header.request.Text())
		}

		// Renaming updates the title.
		tree.TopLevelItem(0).SetText(0, "Api")
		tree.TopLevelItem(0).Child(0).SetText(0, "Fetch")
		if list.Name != "Fetch" || w.request.header.request.Text() != "Fetch" || w.requests.store.Folders[0].Name != "Api" {
			t.Errorf("rename: %q, title %q", list.Name, w.request.header.request.Text())
		}

		// Adding with a request open starts from an empty request in the
		// same folder.
		w.requests.addRequest()
		create := w.requests.store.Folders[0].Requests[1]
		if w.open != create || w.request.url.Text() != "" || tree.TopLevelItem(0).ChildCount() != 2 {
			t.Errorf("second request: open %+v, url %q", w.open, w.request.url.Text())
		}
		tree.TopLevelItem(0).Child(1).SetText(0, "Post")

		// Edits are written to the open request.
		w.request.method.SetCurrentIndex(1) // POST
		w.request.url.SetText("{{host}}/users")
		w.request.params.add("dry", "1")
		w.request.headers.add("X-Test", "yes")
		w.request.auth.typ.SetCurrentIndex(1) // Basic Auth
		w.request.auth.username.SetText("me")
		w.request.auth.password.SetText("pw")
		w.request.bodyType.SetCurrentIndex(1) // JSON
		w.request.bodyRaw.SetPlainText(`{"name":"a"}`)
		w.request.bodyForm.add("f", "1")

		// Switching between requests swaps the contents of the request UI.
		tree.SetCurrentItem(tree.TopLevelItem(0).Child(0))
		if w.open != list || w.request.method.CurrentText() != "GET" || w.request.bodyRaw.ToPlainText() != "" || len(w.request.headers.items()) != 0 || w.request.auth.typ.CurrentIndex() != 0 {
			t.Errorf("first request not shown: %+v", w.request.request())
		}
		if list.Method != "GET" || create.Method != "POST" {
			t.Errorf("switching modified a request: %q %q", list.Method, create.Method)
		}

		// Selecting a folder leaves the open request alone. With nothing
		// selected a folder is added at the top level.
		w.requests.deselect()
		w.requests.addFolder()
		if w.open != list || tree.TopLevelItemCount() != 2 {
			t.Errorf("open after adding a folder = %+v", w.open)
		}

		// Selecting a request closes the environment editor.
		w.environments.add()
		if w.stack.CurrentIndex() != pageEditor {
			t.Fatal("editor not open")
		}
		tree.SetCurrentItem(tree.TopLevelItem(0).Child(1))
		if w.stack.CurrentIndex() != pageRequest || w.open != create {
			t.Error("selecting a request did not return to the request UI")
		}
	})

	want := rest.Request{
		Method:   "POST",
		URL:      "{{host}}/users",
		Params:   []rest.KeyValue{{Key: "dry", Value: "1", Enabled: true}},
		Headers:  []rest.KeyValue{{Key: "X-Test", Value: "yes", Enabled: true}},
		Auth:     rest.Auth{Type: rest.AuthBasic, Username: "me", Password: "pw"},
		BodyType: rest.BodyJSON,
		Body:     `{"name":"a"}`,
		Form:     []rest.KeyValue{{Key: "f", Value: "1", Enabled: true}},
	}

	saved, err := collection.Load(files.Requests)
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.Folders) != 2 || saved.Folders[0].Name != "Api" || len(saved.Folders[0].Requests) != 2 {
		t.Fatalf("saved %+v", saved.Folders)
	}
	if got := saved.Folders[0].Requests[1]; got.Name != "Post" || !reflect.DeepEqual(*got.Request, want) {
		t.Errorf("saved request %+v, want %+v", got, want)
	}

	// A new window restores the tree, and a request loads back unchanged.
	mainthread.Wait(func() {
		w := NewWindow(files)
		tree := w.requests.tree

		if tree.TopLevelItemCount() != 2 || tree.TopLevelItem(0).Text(0) != "Api" || tree.TopLevelItem(0).Child(1).Text(0) != "Post" {
			t.Fatal("tree not restored")
		}
		if w.open != nil || !w.request.header.request.IsHidden() {
			t.Error("a request is open at startup")
		}

		tree.SetCurrentItem(tree.TopLevelItem(0).Child(1))
		if got := w.request.request(); !reflect.DeepEqual(got, want) {
			t.Errorf("loaded %+v, want %+v", got, want)
		}

		// Removing the open request opens its neighbour.
		w.requests.remove()
		if folder := w.requests.store.Folders[0]; len(folder.Requests) != 1 || w.open != folder.Requests[0] || w.request.header.request.Text() != "Fetch" {
			t.Errorf("after remove: open %+v", w.open)
		}

		// Removing a folder that has requests needs confirmation.
		tree.SetCurrentItem(tree.TopLevelItem(0))
		w.requests.confirm = func(string) bool { return false }
		w.requests.remove()
		if tree.TopLevelItemCount() != 2 {
			t.Error("folder removed without confirmation")
		}

		w.requests.confirm = func(string) bool { return true }
		w.requests.remove()
		if tree.TopLevelItemCount() != 1 || len(w.requests.store.Folders) != 1 {
			t.Error("folder not removed")
		}
		if w.open != nil || w.request.url.Text() != "" || !w.request.header.request.IsHidden() {
			t.Errorf("removed request still open: %+v", w.open)
		}

		// An empty folder is removed without asking.
		w.requests.confirm = func(string) bool { t.Error("asked about an empty folder"); return false }
		tree.SetCurrentItem(tree.TopLevelItem(0))
		w.requests.remove()
		if len(w.requests.store.Folders) != 0 {
			t.Error("empty folder not removed")
		}
	})

	if saved, err := collection.Load(files.Requests); err != nil || len(saved.Folders) != 0 {
		t.Errorf("removals not saved: %+v, %v", saved, err)
	}
}

func TestRequestsUnreadableFile(t *testing.T) {
	files := testFiles(t)
	os.WriteFile(files.Requests, []byte("{oops"), 0o644)

	mainthread.Wait(func() {
		w := NewWindow(files)
		if !strings.Contains(w.StatusBar().CurrentMessage(), "could not be loaded") {
			t.Error("no error shown")
		}
		w.requests.addRequest()
		w.environments.add()
	})

	if data, _ := os.ReadFile(files.Requests); string(data) != "{oops" {
		t.Errorf("file was overwritten: %q", data)
	}
	if _, err := os.Stat(files.Environments); err != nil {
		t.Errorf("environments not saved: %v", err)
	}
}

func TestWindowSizeRemembered(t *testing.T) {
	files := testFiles(t)

	mainthread.Wait(func() {
		w := NewWindow(files)
		if w.Width() != 1024 || w.Height() != 768 {
			t.Errorf("default size = %dx%d", w.Width(), w.Height())
		}
		w.Show()
		w.Resize(900, 650)
		w.Close()
	})

	if got, want := withoutLayout(settings.Load(files.Settings)), (settings.Settings{Width: 900, Height: 650, ResponseFont: "Iosevka Term Curly"}); got != want {
		t.Errorf("saved %+v, want %+v", got, want)
	}

	mainthread.Wait(func() {
		w := NewWindow(files)
		if w.Width() != 900 || w.Height() != 650 {
			t.Errorf("restored size = %dx%d", w.Width(), w.Height())
		}
	})

	// A maximised window is restored maximised and keeps its normal size.
	settings.Settings{Width: 900, Height: 650, Maximized: true}.Save(files.Settings)
	mainthread.Wait(func() {
		w := NewWindow(files)
		w.Show()
		if !w.IsMaximized() {
			t.Error("window not maximised")
		}
		w.Close()
	})
	if got, want := withoutLayout(settings.Load(files.Settings)), (settings.Settings{Width: 900, Height: 650, Maximized: true, ResponseFont: "Iosevka Term Curly"}); got != want {
		t.Errorf("saved %+v, want %+v", got, want)
	}
}

func TestParamsShownInURL(t *testing.T) {
	mainthread.Wait(func() {
		w := NewWindow(testFiles(t))
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

func TestRequestReopened(t *testing.T) {
	mainthread.Wait(func() {
		w := NewWindow(testFiles(t))
		w.Show()

		w.requests.addRequest()
		request := w.requests.store.Folders[0].Requests[0]

		// With the environment editor open, opening the request that is
		// already current in the tree returns to it.
		w.environments.add()
		if w.stack.CurrentIndex() != pageEditor {
			t.Fatal("editor not open")
		}
		w.requests.open()
		if w.stack.CurrentIndex() != pageRequest || w.open != request {
			t.Error("request not shown again")
		}
	})
}

func TestSidebarTabs(t *testing.T) {
	mainthread.Wait(func() {
		w := NewWindow(testFiles(t))
		if w.sidebar.Count() != 2 || w.sidebar.TabToolTip(0) != "Environments" || w.sidebar.TabToolTip(1) != "Requests" {
			t.Errorf("tabs = %d: %q, %q", w.sidebar.Count(), w.sidebar.TabToolTip(0), w.sidebar.TabToolTip(1))
		}

		// The tabs have icons and no text.
		for i := range w.sidebar.Count() {
			if w.sidebar.TabText(i) != "" || w.sidebar.TabIcon(i).IsNull() {
				t.Errorf("tab %d has text %q", i, w.sidebar.TabText(i))
			}
		}
	})
}

func TestMenuBar(t *testing.T) {
	files := testFiles(t)

	mainthread.Wait(func() {
		w := NewWindow(files)
		w.Show()

		if w.about.Text() != "&About" {
			t.Errorf("about action = %q", w.about.Text())
		}

		// Quit closes the window, which saves the settings.
		w.Resize(900, 500)
		w.quit.Trigger()
		if w.IsVisible() {
			t.Error("window still open after quit")
		}
	})

	if got := settings.Load(files.Settings); got.Width != 900 || got.Height != 500 {
		t.Errorf("settings not saved on quit: %+v", got)
	}
}

func TestResponseFont(t *testing.T) {
	files := testFiles(t)
	settings.Settings{Width: 800, Height: 600, ResponseFont: "Some Font"}.Save(files.Settings)

	mainthread.Wait(func() {
		w := NewWindow(files)
		w.Show()

		families := w.response.body.Font().Families()
		if len(families) < 2 || families[0] != "Some Font" {
			t.Errorf("families = %v", families)
		}

		// The setting survives the settings being saved on close.
		w.Close()
	})

	if got := settings.Load(files.Settings).ResponseFont; got != "Some Font" {
		t.Errorf("saved font = %q", got)
	}
}

func TestPlaceholderColouringLeavesRequestAlone(t *testing.T) {
	files := testFiles(t)

	mainthread.Wait(func() {
		w := NewWindow(files)
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
		w := NewWindow(testFiles(t))
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

func TestAboutText(t *testing.T) {
	text := mainthread.Wait2(aboutText)
	for _, want := range []string{"TriggerAPI v0.0.1", "Copyright © 2026 Gary Willoughby", "Qt version 6.", "LGPLv3", "https://www.gnu.org/licenses/lgpl-3.0.html", "The Qt Company Ltd"} {
		if !strings.Contains(text, want) {
			t.Errorf("about text does not mention %q", want)
		}
	}
}

// withoutLayout returns the settings without the sizes of the sidebar and
// the response, for tests that are not about them.
func withoutLayout(s settings.Settings) settings.Settings {
	s.SidebarWidth, s.ResponseHeight = 0, 0
	return s
}

// fieldValue returns the value shown for a label in the bar beneath a
// response, or an empty string if the bar does not have it.
func fieldValue(p *responsePanel, label string) string {
	for _, f := range p.fields {
		if f.label == label {
			return f.value
		}
	}
	return ""
}

// isFailure reports if the bar beneath a response shows a failure, which is
// in red.
func isFailure(p *responsePanel) bool {
	return strings.Contains(p.result.Text(), failureColour)
}

// itemType returns the type whose icon an item of the request tree has, or
// an empty type if it has the icon of a folder.
func itemType(item *qt.QTreeWidgetItem) collection.Type {
	icon := item.Icon(colName)
	if icon.IsNull() || (folderIcon != nil && icon.CacheKey() == folderIcon.CacheKey()) {
		return ""
	}
	for typ, known := range requestTypeIcons {
		if known.CacheKey() == icon.CacheKey() && item.ToolTip(colName) == typeLabels[typ] {
			return typ
		}
	}
	return "unknown"
}

// waitFor polls a condition on the main thread until it holds.
func waitFor(t *testing.T, what string, condition func() bool) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for !mainthread.Wait2(condition) {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// addGRPCRequest adds a gRPC request to the window. It must be called on the
// main thread.
func addGRPCRequest(w *Window) *collection.Request {
	testRequestType = collection.TypeGRPC
	defer func() { testRequestType = collection.TypeREST }()

	w.requests.addRequest()
	return w.open
}

func TestGRPCRequest(t *testing.T) {
	srv := rpctest.NewServer(rpctest.ReflectionAll)
	defer srv.Close()

	files := testFiles(t)
	var w *Window
	var request *collection.Request

	mainthread.Wait(func() {
		w = NewWindow(files)
		w.Show()

		request = addGRPCRequest(w)
		if request == nil || request.Type != collection.TypeGRPC || request.Request != nil {
			t.Fatalf("request = %+v", request)
		}
		if w.stack.CurrentIndex() != pageGRPC || w.grpc.header.request.Text() != "New Request" {
			t.Errorf("page = %d, title %q", w.stack.CurrentIndex(), w.grpc.header.request.Text())
		}
		if got := itemType(w.requests.tree.TopLevelItem(0).Child(0)); got != collection.TypeGRPC {
			t.Errorf("type shown as %q", got)
		}

		// An example needs the service definition.
		w.grpc.example.Click()
		if w.grpc.status.IsHidden() || w.grpc.message.ToPlainText() != "" {
			t.Errorf("example without a definition: status %q", w.grpc.status.Text())
		}

		// The server address is a variable of the environment.
		w.environments.add()
		w.editor.variables.add("server", srv.URL)
		w.editor.close.Click()
		if w.stack.CurrentIndex() != pageGRPC {
			t.Error("closing the editor did not return to the gRPC request")
		}

		w.grpc.url.SetText("{{server}}")
		w.grpc.targetChanged()
	})

	waitFor(t, "the methods", func() bool { return w.grpc.method.count() == 4 })

	mainthread.Wait(func() {
		// A request with no method takes the first one.
		if request.GRPC.Method != "test.v1.Echo/Fail" || request.GRPC.URL != "{{server}}" {
			t.Errorf("request = %+v", request.GRPC)
		}
		if !strings.Contains(w.grpc.definition.ToPlainText(), "rpc Say(test.v1.EchoRequest) returns (test.v1.EchoResponse);") {
			t.Errorf("definition = %q", w.grpc.definition.ToPlainText())
		}
		// Loading the definition is only reported if it fails.
		if !w.grpc.status.IsHidden() || w.grpc.status.Text() != "" {
			t.Errorf("status = %q", w.grpc.status.Text())
		}

		w.grpc.method.selectMethod("test.v1.Echo/Say")
		w.grpc.example.Click()
		if got := w.grpc.message.ToPlainText(); !strings.Contains(got, `"text": ""`) || !strings.Contains(got, `"flag": false`) {
			t.Errorf("example = %q", got)
		}

		w.grpc.message.SetPlainText(`{"text": "hello"}`)
		w.grpc.metadata.add("x-trace", "abc")
		w.grpc.auth.typ.SetCurrentIndex(2) // Bearer Token
		w.grpc.auth.token.SetText("secret")
		w.grpc.invoke.Click()
	})

	waitFor(t, "the response", func() bool {
		return fieldValue(w.grpcResponse, "Code") == "0 OK"
	})

	mainthread.Wait(func() {
		want := "{\n  \"text\": \"hello\",\n  \"authorization\": \"Bearer secret\",\n  \"trace\": \"abc\"\n}"
		if got := w.grpcResponse.body.ToPlainText(); got != want {
			t.Errorf("body = %q, want %q", got, want)
		}
		if w.grpc.invoke.Text() != "Invoke" {
			t.Errorf("invoke button = %q", w.grpc.invoke.Text())
		}

		// The server's header and trailer are both listed.
		var names []string
		for row := range w.grpcResponse.headers.RowCount() {
			names = append(names, w.grpcResponse.headers.Item(row, 0).Text())
		}
		if !slices.Contains(names, "X-Server") || !slices.Contains(names, "X-Done (trailer)") {
			t.Errorf("headers = %v", names)
		}

		// A status other than OK is a response, not an error.
		w.grpc.method.selectMethod("test.v1.Echo/Fail")
		w.grpc.invoke.Click()
	})

	waitFor(t, "the failure", func() bool { return fieldValue(w.grpcResponse, "Code") == "5 NOT_FOUND" })

	mainthread.Wait(func() {
		// The failure is in the bar beneath the response, not in it.
		if !isFailure(w.grpcResponse) || w.grpcResponse.body.ToPlainText() != "" || fieldValue(w.grpcResponse, "Error") != "no such thing" || fieldValue(w.grpcResponse, "Time") == "" {
			t.Errorf("response = %q, bar = %+v", w.grpcResponse.body.ToPlainText(), w.grpcResponse.fields)
		}
		if got := w.StatusBar().CurrentMessage(); got != "" {
			t.Errorf("status bar = %q", got)
		}
	})

	mainthread.Wait(func() {
		// A message that is not valid is reported as an error.
		w.grpc.method.selectMethod("test.v1.Echo/Say")
		w.grpc.message.SetPlainText("{oops")
		w.grpc.invoke.Click()
	})

	waitFor(t, "the error", func() bool { return strings.Contains(fieldValue(w.grpcResponse, "Error"), "invalid message") })

	mainthread.Wait(func() {
		if got := w.grpcResponse.body.ToPlainText(); got != "" || !isFailure(w.grpcResponse) {
			t.Errorf("response = %q", got)
		}
		w.grpc.message.SetPlainText(`{"text": "saved"}`)
		w.grpc.tls.SetChecked(true)
		w.grpc.tls.SetChecked(false)
	})

	want := rpc.Request{
		URL:      "{{server}}",
		Method:   "test.v1.Echo/Say",
		Message:  `{"text": "saved"}`,
		Metadata: []rest.KeyValue{{Key: "x-trace", Value: "abc", Enabled: true}},
		Auth:     rest.Auth{Type: rest.AuthBearer, Token: "secret"},
	}

	saved, err := collection.Load(files.Requests)
	if err != nil {
		t.Fatal(err)
	}
	if got := saved.Folders[0].Requests[0]; got.Type != collection.TypeGRPC || got.Request != nil || !reflect.DeepEqual(*got.GRPC, want) {
		t.Errorf("saved %+v %+v, want %+v", got, got.GRPC, want)
	}

	// A new window restores the request, and loads the methods when it is
	// opened without changing the one that was saved.
	mainthread.Wait(func() {
		w = NewWindow(files)
		w.Show()

		tree := w.requests.tree
		tree.SetCurrentItem(tree.TopLevelItem(0).Child(0))
		if w.stack.CurrentIndex() != pageGRPC || w.grpc.method.current() != "test.v1.Echo/Say" {
			t.Errorf("page = %d, method = %q", w.stack.CurrentIndex(), w.grpc.method.current())
		}
	})

	waitFor(t, "the methods", func() bool { return w.grpc.method.count() == 4 })

	mainthread.Wait(func() {
		if got := w.grpc.request(); !reflect.DeepEqual(got, want) {
			t.Errorf("loaded %+v, want %+v", got, want)
		}
	})
}

func TestGRPCAndRESTRequests(t *testing.T) {
	mainthread.Wait(func() {
		w := NewWindow(testFiles(t))
		w.Show()
		tree := w.requests.tree

		w.request.url.SetText("example.com")
		w.requests.addRequest()
		restRequest := w.open

		grpcRequest := addGRPCRequest(w)
		w.grpc.url.SetText("localhost:1")

		if restRequest.Type != collection.TypeREST || restRequest.URL != "example.com" || grpcRequest.GRPC.URL != "localhost:1" {
			t.Errorf("requests = %+v, %+v", restRequest.Request, grpcRequest.GRPC)
		}
		if got := itemType(tree.TopLevelItem(0).Child(0)); got != collection.TypeREST {
			t.Errorf("type shown as %q", got)
		}
		if tree.ColumnCount() != 1 || itemType(tree.TopLevelItem(0)) != "" {
			t.Errorf("columns = %d, folder shown as %q", tree.ColumnCount(), itemType(tree.TopLevelItem(0)))
		}

		// Each type of request opens in its own UI, and leaves the other
		// request alone.
		tree.SetCurrentItem(tree.TopLevelItem(0).Child(0))
		if w.stack.CurrentIndex() != pageRequest || w.open != restRequest || w.request.url.Text() != "example.com" {
			t.Errorf("REST request not shown: page %d", w.stack.CurrentIndex())
		}
		w.request.url.SetText("changed.test")

		tree.SetCurrentItem(tree.TopLevelItem(0).Child(1))
		if w.stack.CurrentIndex() != pageGRPC || w.open != grpcRequest || w.grpc.url.Text() != "localhost:1" {
			t.Errorf("gRPC request not shown: page %d", w.stack.CurrentIndex())
		}
		if restRequest.URL != "changed.test" || grpcRequest.GRPC.URL != "localhost:1" || grpcRequest.Request != nil || restRequest.GRPC != nil {
			t.Errorf("requests = %+v, %+v", restRequest.Request, grpcRequest.GRPC)
		}

		// Renaming a gRPC request updates its title.
		tree.TopLevelItem(0).Child(1).SetText(colName, "Say")
		if grpcRequest.Name != "Say" || w.grpc.header.request.Text() != "Say" {
			t.Errorf("name = %q, title = %q", grpcRequest.Name, w.grpc.header.request.Text())
		}

		// Removing the open gRPC request returns to the REST one beside it.
		w.requests.remove()
		if w.stack.CurrentIndex() != pageRequest || w.open != restRequest {
			t.Errorf("after remove: page %d, open %+v", w.stack.CurrentIndex(), w.open)
		}

		// Cancelling the type dialog adds nothing.
		testRequestType = ""
		w.requests.addRequest()
		testRequestType = collection.TypeREST
		if n := len(w.requests.store.Folders[0].Requests); n != 1 {
			t.Errorf("requests = %d, want 1", n)
		}
	})
}

func TestGRPCDefinitionError(t *testing.T) {
	srv := rpctest.NewServer(rpctest.ReflectionNone)
	defer srv.Close()

	var w *Window
	mainthread.Wait(func() {
		w = NewWindow(testFiles(t))
		w.Show()

		addGRPCRequest(w)
		w.grpc.url.SetText(srv.URL)
		w.grpc.reload.Click()
	})

	waitFor(t, "the error", func() bool { return strings.Contains(w.grpc.status.Text(), "does not support reflection") })

	mainthread.Wait(func() {
		if w.grpc.method.count() != 0 {
			t.Errorf("methods = %d", w.grpc.method.count())
		}
	})
}

func TestGRPCCancel(t *testing.T) {
	srv := rpctest.NewServer(rpctest.ReflectionAll)
	defer srv.Close()

	var w *Window
	mainthread.Wait(func() {
		w = NewWindow(testFiles(t))
		w.Show()

		addGRPCRequest(w)
		w.grpc.url.SetText(srv.URL)
		w.grpc.reload.Click()
	})

	waitFor(t, "the methods", func() bool { return w.grpc.method.count() == 4 })

	mainthread.Wait(func() {
		w.grpc.method.selectMethod("test.v1.Echo/Forever")
		w.grpc.message.SetPlainText(`{"text": "one"}`)
		w.grpc.invoke.Click()
		if w.grpc.invoke.Text() != "Cancel" {
			t.Errorf("invoke button = %q", w.grpc.invoke.Text())
		}
	})

	// Give the server time to send its one message before cancelling.
	time.Sleep(300 * time.Millisecond)
	mainthread.Wait(func() { w.grpc.invoke.Click() })

	// The message received before the cancel is still shown.
	waitFor(t, "the cancel", func() bool { return fieldValue(w.grpcResponse, "Code") == "1 CANCELED" })

	mainthread.Wait(func() {
		// The response holds the message alone.
		got := w.grpcResponse.body.ToPlainText()
		if !strings.HasPrefix(got, "{\n  \"text\": \"one\"") || strings.Contains(got, "CANCELED") {
			t.Errorf("body = %q", got)
		}
	})
}

func TestRequestTypeDialog(t *testing.T) {
	mainthread.Wait(func() {
		w := NewWindow(testFiles(t))

		for _, typ := range []collection.Type{collection.TypeREST, collection.TypeGRPC} {
			dialog := newRequestTypeDialog(w.QWidget)
			dialog.Show()

			button := dialog.buttons[typ]
			if button == nil || button.Text() != typeLabels[typ] {
				t.Fatalf("no button labelled %q", typeLabels[typ])
			}

			button.Click()
			if dialog.chosen != typ || dialog.Result() != int(qt.QDialog__Accepted) || dialog.IsVisible() {
				t.Errorf("clicking %s chose %q with result %d", typ, dialog.chosen, dialog.Result())
			}
		}
	})
}

func TestMethodCombo(t *testing.T) {
	mainthread.Wait(func() {
		c := newMethodCombo()
		changes := 0
		c.OnCurrentIndexChanged(func(int) { changes++ })

		// Before the methods are known only the saved one is listed.
		c.setMethods(nil, "a.Users/Get")
		if c.current() != "a.Users/Get" || c.count() != 1 {
			t.Errorf("current = %q, count = %d", c.current(), c.count())
		}

		// Methods are listed by their short names under their services.
		c.setMethods([]string{"a.Orders/List", "a.Users/Get", "a.Users/List"}, "a.Users/Get")
		var rows []string
		for row := range c.Count() {
			rows = append(rows, c.ItemText(row))
		}
		want := []string{"a.Orders", "    List", "a.Users", "    Get", "    List"}
		if !reflect.DeepEqual(rows, want) {
			t.Errorf("rows = %q, want %q", rows, want)
		}
		if c.current() != "a.Users/Get" || c.count() != 3 {
			t.Errorf("current = %q, count = %d", c.current(), c.count())
		}

		// Two methods with the same short name are told apart.
		c.selectMethod("a.Orders/List")
		if c.current() != "a.Orders/List" {
			t.Errorf("current = %q", c.current())
		}
		c.selectMethod("a.Users/List")
		if c.current() != "a.Users/List" {
			t.Errorf("current = %q", c.current())
		}

		// A heading cannot be chosen.
		c.selectMethod("")
		c.selectMethod("a.Users")
		if c.current() != "a.Users/List" {
			t.Errorf("current = %q", c.current())
		}

		// With nothing to keep, the first method is selected, not its heading.
		c.setMethods([]string{"a.Orders/List"}, "")
		if c.current() != "a.Orders/List" {
			t.Errorf("current = %q", c.current())
		}

		// A selected method the server does not have stays, on its own.
		c.setMethods([]string{"a.Orders/List"}, "a.Gone/Old")
		if c.current() != "a.Gone/Old" || c.count() != 2 || c.ItemText(0) != "a.Gone/Old" {
			t.Errorf("current = %q, count = %d", c.current(), c.count())
		}

		c.setMethods(nil, "")
		if c.current() != "" || c.count() != 0 {
			t.Errorf("current = %q, count = %d", c.current(), c.count())
		}
		if changes == 0 {
			t.Error("changes were not reported")
		}
	})
}

func TestSettingsDialog(t *testing.T) {
	mainthread.Wait(func() {
		defaults := Files{Environments: "environments.json", Requests: "requests.json"}
		current := settings.Settings{Width: 900, Height: 650, ResponseFont: "A Font That Is Not Installed", RequestsFile: "/data/r.json"}

		d := newSettingsDialog(nil, current, defaults)
		if d.environments.Text() != "environments.json" || d.requests.Text() != "/data/r.json" {
			t.Errorf("locations shown as %q and %q", d.environments.Text(), d.requests.Text())
		}

		// Accepting without changing anything changes nothing, even though
		// the font is not one the selector could show.
		if got := d.settings(current); got != current {
			t.Errorf("unchanged dialog gave %+v, want %+v", got, current)
		}

		d.environments.SetText("  /data/e.json ")
		d.requests.SetText("requests.json")
		d.font.SetCurrentFont(qt.QFontDatabase_SystemFont(qt.QFontDatabase__FixedFont))

		got := d.settings(current)
		want := current
		want.EnvironmentsFile = "/data/e.json"
		// A location put back to its default is not recorded.
		want.RequestsFile = ""
		want.ResponseFont = d.font.CurrentFont().Family()
		if got != want || got.ResponseFont == current.ResponseFont {
			t.Errorf("got %+v, want %+v", got, want)
		}

		d.buttons.Button(qt.QDialogButtonBox__Ok).Click()
		if d.Result() != int(qt.QDialog__Accepted) {
			t.Error("OK did not accept the dialog")
		}
	})
}

func TestApplySettings(t *testing.T) {
	files := testFiles(t)
	dir := filepath.Dir(files.Settings)
	movedEnvs := filepath.Join(dir, "moved", "environments.json")
	otherRequests := filepath.Join(dir, "other-requests.json")
	os.Mkdir(filepath.Join(dir, "moved"), 0o755)

	// Another set of requests already exists.
	other := &collection.Store{}
	other.AddFolder("Other").Add(collection.NewREST("Elsewhere", rest.Request{Method: "GET", URL: "other.test"}))
	if err := other.Save(otherRequests); err != nil {
		t.Fatal(err)
	}

	mainthread.Wait(func() {
		w := NewWindow(files)
		w.Show()

		w.environments.add()
		w.editor.name.SetText("Local")
		w.editor.variables.add("host", "example.com")
		w.request.url.SetText("{{host}}/users")
		w.requests.addRequest()

		s := w.config
		s.ResponseFont = "Some Font"
		s.EnvironmentsFile = movedEnvs
		s.RequestsFile = otherRequests
		if err := w.applySettings(s); err != nil {
			t.Fatal(err)
		}

		if got := w.response.body.Font().Families()[0]; got != "Some Font" {
			t.Errorf("response font = %q", got)
		}
		if got := w.grpcResponse.body.Font().Families()[0]; got != "Some Font" {
			t.Errorf("gRPC response font = %q", got)
		}

		// The environments are kept, as their new file did not exist, and
		// later changes go to it.
		if w.environments.list.Count() != 1 || w.environments.store.Environments[0].Name != "Local" {
			t.Errorf("environments = %+v", w.environments.store.Environments)
		}
		w.editor.variables.add("token", "abc")

		// The requests are replaced by those in the file that existed, and
		// the request that was open is closed.
		tree := w.requests.tree
		if tree.TopLevelItemCount() != 1 || tree.TopLevelItem(0).Text(colName) != "Other" || tree.TopLevelItem(0).Child(0).Text(colName) != "Elsewhere" {
			t.Error("requests not loaded from the other file")
		}
		if w.open != nil || w.request.url.Text() != "" {
			t.Errorf("request still open: %+v", w.open)
		}
		tree.SetCurrentItem(tree.TopLevelItem(0).Child(0))
		w.request.url.SetText("changed.test")
	})

	moved, err := env.Load(movedEnvs)
	if err != nil || len(moved.Environments) != 1 || len(moved.Environments[0].Variables) != 2 {
		t.Errorf("moved environments = %+v, %v", moved, err)
	}
	if old, _ := env.Load(files.Environments); len(old.Environments[0].Variables) != 1 {
		t.Errorf("old environments file still written to: %+v", old.Environments)
	}
	if reloaded, _ := collection.Load(otherRequests); reloaded.Folders[0].Requests[0].URL != "changed.test" {
		t.Errorf("other requests = %+v", reloaded.Folders[0].Requests[0].Request)
	}
	if old, _ := collection.Load(files.Requests); len(old.Folders) != 1 || old.Folders[0].Requests[0].URL != "{{host}}/users" {
		t.Errorf("old requests file changed: %+v", old.Folders)
	}

	saved := settings.Load(files.Settings)
	if saved.ResponseFont != "Some Font" || saved.EnvironmentsFile != movedEnvs || saved.RequestsFile != otherRequests {
		t.Errorf("saved settings = %+v", saved)
	}

	// The next run uses the new locations.
	mainthread.Wait(func() {
		w := NewWindow(files)
		if w.environments.list.Count() != 1 || len(w.environments.store.Environments[0].Variables) != 2 {
			t.Errorf("environments = %+v", w.environments.store.Environments)
		}
		if w.requests.tree.TopLevelItem(0).Text(colName) != "Other" {
			t.Error("requests not loaded from the other file")
		}

		// A file that cannot be used is refused and the setting left alone.
		bad := filepath.Join(dir, "bad.json")
		os.WriteFile(bad, []byte("{oops"), 0o644)

		s := w.config
		s.RequestsFile = bad
		s.EnvironmentsFile = filepath.Join(dir, "missing-directory", "e.json")
		err := w.applySettings(s)
		if err == nil || !strings.Contains(err.Error(), "requests were not moved") || !strings.Contains(err.Error(), "environments were not moved") {
			t.Errorf("err = %v", err)
		}
		if w.config.RequestsFile != otherRequests || w.config.EnvironmentsFile != movedEnvs || w.requests.path != otherRequests || w.environments.path != movedEnvs {
			t.Errorf("settings changed: %+v", w.config)
		}

		// Going back to the defaults loads the original files.
		s = w.config
		s.EnvironmentsFile, s.RequestsFile = "", ""
		if err := w.applySettings(s); err != nil {
			t.Fatal(err)
		}
		if len(w.environments.store.Environments[0].Variables) != 1 || w.requests.tree.TopLevelItem(0).Text(colName) != "New Folder" {
			t.Error("default files not loaded")
		}
	})

	if data, _ := os.ReadFile(filepath.Join(dir, "bad.json")); string(data) != "{oops" {
		t.Errorf("bad file was overwritten: %q", data)
	}
}

func TestMenuShortcuts(t *testing.T) {
	mainthread.Wait(func() {
		w := NewWindow(testFiles(t))
		if got := w.about.Shortcut().ToString(); got != "Ctrl+I" {
			t.Errorf("about shortcut = %q", got)
		}
		if got := w.preferences.Shortcut().ToString(); got != "Ctrl+," || w.preferences.Text() != "&Settings…" {
			t.Errorf("settings action = %q %q", w.preferences.Text(), got)
		}
	})
}

func TestStatusBar(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	var w *Window
	mainthread.Wait(func() {
		w = NewWindow(testFiles(t))
		w.Show()

		if !w.StatusBar().IsVisible() || !w.StatusBar().IsSizeGripEnabled() || w.StatusBar().CurrentMessage() != "" {
			t.Errorf("status bar: visible %t, grip %t, message %q", w.StatusBar().IsVisible(), w.StatusBar().IsSizeGripEnabled(), w.StatusBar().CurrentMessage())
		}
		if got := w.response.tabs.TabText(0); got != "Response" {
			t.Errorf("first response tab = %q", got)
		}

		w.request.url.SetText(srv.URL + "/users")
		w.request.send.Click()
		// Requests are not reported in the status bar.
		if got := w.StatusBar().CurrentMessage(); got != "" {
			t.Errorf("status bar = %q", got)
		}
	})

	close(release)
	waitFor(t, "the response", func() bool {
		return fieldValue(w.response, "Code") == "204 No Content"
	})

	// A request that fails is reported in the bar and in the response.
	srv.Close()
	mainthread.Wait(func() { w.request.send.Click() })
	waitFor(t, "the failure", func() bool { return fieldValue(w.response, "Error") != "" })

	mainthread.Wait(func() {
		if w.StatusBar().CurrentMessage() != "" || w.response.body.ToPlainText() != "" || !isFailure(w.response) || fieldValue(w.response, "Time") == "" || fieldValue(w.response, "Code") != "" {
			t.Errorf("response = %q, bar = %+v", w.response.body.ToPlainText(), w.response.fields)
		}
	})
}

func TestIcons(t *testing.T) {
	mainthread.Wait(func() {
		w := NewWindow(testFiles(t))
		if w.WindowIcon().IsNull() || appIcon().IsNull() {
			t.Error("the window has no icon")
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
		waitFor(t, "the response to "+path, func() bool { return fieldValue(w.response, "Code") == code })
	}

	mainthread.Wait(func() {
		w = NewWindow(testFiles(t))
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
		if fieldValue(w.response, "Code") != "404 Not Found" || !isFailure(w.response) || !strings.Contains(fieldValue(w.response, "Time"), "ms") || fieldValue(w.response, "Size") != "24 B" {
			t.Errorf("bar = %+v", w.response.fields)
		}
		// The code is at the left of the bar and the rest at the right.
		left, right := w.response.result.Text(), w.response.details.Text()
		if !strings.Contains(left, "<b>Code:</b>") || strings.Contains(left, "Time") || strings.Contains(left, "Error") {
			t.Errorf("left of the bar = %q", left)
		}
		if !strings.Contains(right, "<b>Time:</b>") || !strings.Contains(right, "<b>Size:</b>") || !strings.Contains(right, "<b>Protocol:</b>") || strings.Contains(right, "Code") {
			t.Errorf("right of the bar = %q", right)
		}
	})

	send("/empty", "500 Internal Server Error")
	mainthread.Wait(func() {
		if w.response.body.ToPlainText() != "" || fieldValue(w.response, "Code") != "500 Internal Server Error" || !isFailure(w.response) {
			t.Errorf("body = %q, bar = %+v", w.response.body.ToPlainText(), w.response.fields)
		}
	})

	// A success has its status in the bar too, but not as a failure.
	send("/ok", "200 OK")
	mainthread.Wait(func() {
		if w.response.body.ToPlainText() != "fine" || fieldValue(w.response, "Code") != "200 OK" || isFailure(w.response) {
			t.Errorf("body = %q, bar = %+v", w.response.body.ToPlainText(), w.response.fields)
		}

		// Starting another request empties the bar.
		w.request.send.Click()
		// Nothing is put in the response while it is awaited either.
		if w.response.result.Text() != "" || w.response.details.Text() != "" || len(w.response.fields) != 0 || w.response.body.PlaceholderText() != "" || w.response.body.ToPlainText() != "" {
			t.Errorf("bar = %q", w.response.result.Text())
		}
	})
	waitFor(t, "the last response", func() bool { return fieldValue(w.response, "Code") == "200 OK" })
}

func TestLayoutRemembered(t *testing.T) {
	files := testFiles(t)

	var w *Window
	mainthread.Wait(func() {
		w = NewWindow(files)
		w.Resize(1000, 700)
		w.Show()
		addGRPCRequest(w)
		w.requests.addRequest()
	})

	// Let the window be laid out before the handles are moved.
	waitFor(t, "the layout", func() bool { return w.split.Sizes()[0] == defaultSidebarWidth })

	var total int
	mainthread.Wait(func() {
		sizes := w.split.Sizes()
		w.split.SetSizes([]int{350, sizes[0] + sizes[1] - 350})

		rest := w.splits[pageRequest]
		sizes = rest.Sizes()
		total = sizes[0] + sizes[1]
		rest.SetSizes([]int{total - 210, 210})

		w.Close()
	})

	saved := settings.Load(files.Settings)
	if saved.SidebarWidth != 350 || saved.ResponseHeight != 210 {
		t.Fatalf("saved %+v", saved)
	}

	mainthread.Wait(func() {
		w = NewWindow(files)
		w.Show()
	})
	waitFor(t, "the sidebar width", func() bool { return w.split.Sizes()[0] == 350 })
	waitFor(t, "the response height", func() bool { return w.splits[pageRequest].Sizes()[1] == 210 })

	// The other page with a response is given the same height when shown.
	mainthread.Wait(func() {
		tree := w.requests.tree
		tree.SetCurrentItem(tree.TopLevelItem(0).Child(0))
		if w.stack.CurrentIndex() != pageGRPC {
			t.Fatal("gRPC request not shown")
		}
	})
	waitFor(t, "the gRPC response height", func() bool { return w.splits[pageGRPC].Sizes()[1] == 210 })

	// Closing from the other page saves the height it has.
	mainthread.Wait(func() {
		split := w.splits[pageGRPC]
		sizes := split.Sizes()
		split.SetSizes([]int{sizes[0] + sizes[1] - 260, 260})
		w.Close()
	})
	if saved := settings.Load(files.Settings); saved.SidebarWidth != 350 || saved.ResponseHeight != 260 {
		t.Errorf("saved %+v", saved)
	}
}

func TestHeader(t *testing.T) {
	files := testFiles(t)

	mainthread.Wait(func() {
		w := NewWindow(files)
		w.Show()

		// The requests are shown first.
		if w.sidebar.CurrentIndex() != 1 || w.sidebar.TabToolTip(1) != "Requests" {
			t.Errorf("sidebar tab = %d", w.sidebar.CurrentIndex())
		}

		// With nothing saved open no request is named, and there are no
		// environments to select.
		h := w.request.header
		if !h.request.IsHidden() || !h.requestLabel.IsHidden() || h.environment.Count() != 1 || h.environment.CurrentText() != "None" {
			t.Errorf("header = %q, %q", h.request.Text(), h.environment.CurrentText())
		}

		w.environments.add()
		w.editor.name.SetText("Local")
		w.editor.variables.add("host", "local.test")
		w.environments.add()
		w.editor.name.SetText("Remote")
		w.editor.variables.add("host", "remote.test")
		w.editor.close.Click()
		w.requests.addRequest()

		if h.request.IsHidden() || h.requestLabel.Text() != "Request:" || h.request.Text() != "New Request" {
			t.Errorf("request = %q", h.request.Text())
		}
		if h.environment.Count() != 3 || h.environment.ItemText(1) != "Local" || h.environment.CurrentText() != "Remote" {
			t.Errorf("environments = %d, selected %q", h.environment.Count(), h.environment.CurrentText())
		}

		// Selecting an environment in the header makes it the active one.
		w.request.url.SetText("{{host}}/users")
		h.environment.SetCurrentIndex(1)
		if w.environments.store.Active != 0 || w.environments.list.CurrentRow() != 0 || w.request.preview.Text() != "→ local.test/users" {
			t.Errorf("active = %d, preview = %q", w.environments.store.Active, w.request.preview.Text())
		}

		// The gRPC request UI has the same header, kept in step.
		addGRPCRequest(w)
		g := w.grpc.header
		if g.request.Text() != "New Request" || g.environment.CurrentText() != "Local" {
			t.Errorf("gRPC header = %q, %q", g.request.Text(), g.environment.CurrentText())
		}
		g.environment.SetCurrentIndex(2)
		if w.environments.store.Active != 1 || h.environment.CurrentText() != "Remote" {
			t.Errorf("active = %d, REST header = %q", w.environments.store.Active, h.environment.CurrentText())
		}

		// It follows the sidebar, and a rename.
		w.environments.list.SetCurrentRow(0)
		if g.environment.CurrentText() != "Local" {
			t.Errorf("environment = %q", g.environment.CurrentText())
		}
		w.environments.edit()
		w.editor.name.SetText("Dev")
		w.editor.close.Click()
		if g.environment.CurrentText() != "Dev" || g.environment.ItemText(1) != "Dev" {
			t.Errorf("environment = %q", g.environment.CurrentText())
		}

		// None can be selected.
		g.environment.SetCurrentIndex(0)
		if w.environments.store.Active != -1 || w.environments.list.CurrentRow() != -1 || h.environment.CurrentText() != "None" {
			t.Errorf("active = %d", w.environments.store.Active)
		}
		g.environment.SetCurrentIndex(2)
	})

	// The selection made in the header is saved.
	if saved, err := env.Load(files.Environments); err != nil || saved.Active != 1 || saved.Environments[0].Name != "Dev" {
		t.Errorf("saved %+v, %v", saved, err)
	}
}

func TestGRPCRequestBarOrder(t *testing.T) {
	var w *Window
	mainthread.Wait(func() {
		w = NewWindow(testFiles(t))
		w.Show()
		addGRPCRequest(w)
	})

	// The positions are only known once the page has been laid out.
	waitFor(t, "the layout", func() bool { return w.grpc.url.Width() > 0 && w.grpc.invoke.X() > 0 })

	mainthread.Wait(func() {
		g := w.grpc
		row := []*qt.QWidget{g.url.QWidget, g.method.QWidget, g.reload.QWidget, g.tls.QWidget, g.invoke.QWidget}

		// The method is given three times the width of the address.
		if ratio := float64(g.method.Width()) / float64(g.url.Width()); ratio < 2.8 || ratio > 3.2 {
			t.Errorf("address is %d wide and the method %d", g.url.Width(), g.method.Width())
		}

		// Everything is on one line, in order from the left.
		for i := 1; i < len(row); i++ {
			if row[i].X() <= row[i-1].X() {
				t.Errorf("widget %d is not to the right of widget %d", i, i-1)
			}
			if overlap := row[i].Geometry().Intersects(row[0].Geometry()); overlap || row[i].Geometry().Bottom() < row[0].Y() || row[i].Y() > row[0].Geometry().Bottom() {
				t.Errorf("widget %d is not on the line of the address", i)
			}
		}
	})
}

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
		w = NewWindow(testFiles(t))
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
		w = NewWindow(testFiles(t))
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

// formatColour returns the colour of a text format, as #rrggbb.
func formatColour(format *qt.QTextCharFormat) string {
	return format.Foreground().Color().Name()
}

func TestJSONColourSettings(t *testing.T) {
	files := testFiles(t)
	defaults := defaultJSONColours()

	mainthread.Wait(func() {
		w := NewWindow(files)
		w.Show()

		// The colours are the defaults until others are chosen.
		if got := formatColour(w.response.formats[tokenKey]); got != defaults[tokenKey] {
			t.Errorf("key colour = %s, want %s", got, defaults[tokenKey])
		}

		d := newSettingsDialog(w.QWidget, w.config, files)
		if got := d.swatches[tokenKey].Text(); got != defaults[tokenKey] {
			t.Errorf("swatch = %q", got)
		}

		d.setColour(tokenKey, "#112233")
		d.setColour(tokenNumber, "#AABBCC")
		// Choosing the default is not recorded, so it still follows the theme.
		d.setColour(tokenString, defaults[tokenString])
		if got := d.swatches[tokenKey].Text(); got != "#112233" || !strings.Contains(d.swatches[tokenKey].StyleSheet(), "background-color: #112233") {
			t.Errorf("swatch = %q, %q", got, d.swatches[tokenKey].StyleSheet())
		}

		want := settings.JSONColours{Key: "#112233", Number: "#aabbcc"}
		chosen := d.settings(w.config)
		if chosen.JSONColours != want {
			t.Errorf("chosen = %+v, want %+v", chosen.JSONColours, want)
		}

		if err := w.applySettings(chosen); err != nil {
			t.Fatal(err)
		}

		// Everything that shows JSON takes the colours at once.
		for name, formats := range map[string]jsonFormats{
			"REST body":     w.request.highlight.json,
			"gRPC message":  w.grpc.highlight.json,
			"REST response": w.response.formats,
			"gRPC response": w.grpcResponse.formats,
		} {
			if formatColour(formats[tokenKey]) != "#112233" || formatColour(formats[tokenNumber]) != "#aabbcc" || formatColour(formats[tokenString]) != defaults[tokenString] {
				t.Errorf("%s: key %s, number %s, string %s", name, formatColour(formats[tokenKey]), formatColour(formats[tokenNumber]), formatColour(formats[tokenString]))
			}
		}

		// Going back to the defaults clears what was chosen.
		d.resetColours()
		if d.settings(w.config).JSONColours != (settings.JSONColours{}) || d.swatches[tokenKey].Text() != defaults[tokenKey] {
			t.Errorf("after reset: %+v", d.settings(w.config).JSONColours)
		}
	})

	if got, want := settings.Load(files.Settings).JSONColours, (settings.JSONColours{Key: "#112233", Number: "#aabbcc"}); got != want {
		t.Errorf("saved %+v, want %+v", got, want)
	}

	// The colours are used from the start of the next run.
	mainthread.Wait(func() {
		w := NewWindow(files)
		if got := formatColour(w.grpcResponse.formats[tokenKey]); got != "#112233" {
			t.Errorf("key colour = %s", got)
		}
		if got := formatColour(w.request.highlight.json[tokenLiteral]); got != defaults[tokenLiteral] {
			t.Errorf("literal colour = %s", got)
		}
	})
}

// treeItem finds an item of the request tree by the names on the way to it
// from the top. It returns nil if there is none.
func treeItem(tree *qt.QTreeWidget, names ...string) *qt.QTreeWidgetItem {
	var item *qt.QTreeWidgetItem
	for depth, name := range names {
		var found *qt.QTreeWidgetItem
		if depth == 0 {
			for i := range tree.TopLevelItemCount() {
				if tree.TopLevelItem(i).Text(colName) == name {
					found = tree.TopLevelItem(i)
				}
			}
		} else {
			for i := range item.ChildCount() {
				if item.Child(i).Text(colName) == name {
					found = item.Child(i)
				}
			}
		}
		if found == nil {
			return nil
		}
		item = found
	}
	return item
}

// renameCurrent gives the selected item of the request tree a name, and puts
// the tree back in order straight away, which otherwise happens a moment
// later. Items found before it must be found again.
func renameCurrent(rt *requestTree, name string) {
	rt.tree.CurrentItem().SetText(colName, name)
	rt.sort()
}

func TestNestedFolders(t *testing.T) {
	files := testFiles(t)

	mainthread.Wait(func() {
		w := NewWindow(files)
		w.Show()
		rt := w.requests
		tree := rt.tree

		// A folder is added at the top level when nothing is selected, and
		// inside the selected folder when one is.
		rt.addFolder()
		renameCurrent(rt, "em-ohip-hotel")
		tree.SetCurrentItem(treeItem(tree, "em-ohip-hotel"))

		rt.addFolder()
		renameCurrent(rt, "ohip_hotel")
		service := treeItem(tree, "em-ohip-hotel")
		if tree.TopLevelItemCount() != 1 || service.ChildCount() != 1 || !service.IsExpanded() || treeItem(tree, "em-ohip-hotel", "ohip_hotel") == nil {
			t.Fatalf("subfolder not added: %d top level, %d children", tree.TopLevelItemCount(), service.ChildCount())
		}

		// A request goes in the selected folder, however deep.
		tree.SetCurrentItem(treeItem(tree, "em-ohip-hotel", "ohip_hotel"))
		addGRPCRequest(w)
		renameCurrent(rt, "GetHotel")
		folders := rt.store.Folders
		if len(folders) != 1 || len(folders[0].Folders) != 1 || len(folders[0].Folders[0].Requests) != 1 {
			t.Fatalf("store = %+v", folders[0])
		}
		get := folders[0].Folders[0].Requests[0]
		if w.open != get || get.Name != "GetHotel" || get.Type != collection.TypeGRPC {
			t.Errorf("open = %+v", w.open)
		}

		// With a request selected, a folder is added beside it, in the
		// folder that holds it, and listed before the requests.
		tree.SetCurrentItem(treeItem(tree, "em-ohip-hotel", "ohip_hotel", "GetHotel"))
		rt.addFolder()
		renameCurrent(rt, "admin")
		inner := treeItem(tree, "em-ohip-hotel", "ohip_hotel")
		if got, want := treeNames(inner), []string{"admin/", "GetHotel"}; !reflect.DeepEqual(got, want) {
			t.Errorf("children = %q, want %q", got, want)
		}

		// The request is still found now that a folder comes before it.
		tree.SetCurrentItem(treeItem(tree, "em-ohip-hotel", "ohip_hotel", "GetHotel"))
		if w.open != get {
			t.Errorf("open = %+v", w.open)
		}
		w.grpc.url.SetText("localhost:1")
		if get.GRPC.URL != "localhost:1" {
			t.Errorf("edit went to %+v", get.GRPC)
		}

		// A REST request three folders down.
		tree.SetCurrentItem(treeItem(tree, "em-ohip-hotel", "ohip_hotel", "admin"))
		rt.addRequest()
		renameCurrent(rt, "Report")
		report := folders[0].Folders[0].Folders[0].Requests[0]
		if w.open != report || report.Name != "Report" || report.Type != collection.TypeREST || w.stack.CurrentIndex() != pageRequest {
			t.Errorf("open = %+v", w.open)
		}

		// A second top level folder needs the selection cleared first.
		rt.deselect()
		rt.addFolder()
		renameCurrent(rt, "other")
		if tree.TopLevelItemCount() != 2 || len(rt.store.Folders) != 2 {
			t.Errorf("top level folders = %d", tree.TopLevelItemCount())
		}

		// Renaming a folder inside another renames the right one.
		if folders[0].Folders[0].Name != "ohip_hotel" || folders[0].Folders[0].Folders[0].Name != "admin" {
			t.Errorf("names = %q, %q", folders[0].Folders[0].Name, folders[0].Folders[0].Folders[0].Name)
		}
	})

	saved, err := collection.Load(files.Requests)
	if err != nil {
		t.Fatal(err)
	}
	if got := saved.Folders[0].Folders[0]; got.Name != "ohip_hotel" || got.Requests[0].Name != "GetHotel" || got.Folders[0].Requests[0].Name != "Report" {
		t.Fatalf("saved %+v", got)
	}

	// A new window restores the whole tree.
	mainthread.Wait(func() {
		w := NewWindow(files)
		w.Show()
		rt := w.requests
		tree := rt.tree

		if tree.TopLevelItemCount() != 2 || treeItem(tree, "em-ohip-hotel", "ohip_hotel", "admin", "Report") == nil || treeItem(tree, "em-ohip-hotel", "ohip_hotel", "GetHotel") == nil {
			t.Fatal("tree not restored")
		}

		tree.SetCurrentItem(treeItem(tree, "em-ohip-hotel", "ohip_hotel", "admin", "Report"))
		report := w.open
		if report == nil || report.Name != "Report" {
			t.Fatalf("open = %+v", report)
		}

		// Removing a folder asks first, saying what is in it.
		var asked string
		rt.confirm = func(question string) bool { asked = question; return false }
		tree.SetCurrentItem(treeItem(tree, "em-ohip-hotel", "ohip_hotel"))
		rt.remove()
		if !strings.Contains(asked, `"ohip_hotel"`) || !strings.Contains(asked, "1 folder and 2 requests") {
			t.Errorf("asked %q", asked)
		}
		if treeItem(tree, "em-ohip-hotel").ChildCount() != 1 || w.open != report {
			t.Error("folder removed without confirmation")
		}

		// It takes everything in it, and closes the request that was open.
		rt.confirm = func(string) bool { return true }
		rt.remove()
		if treeItem(tree, "em-ohip-hotel").ChildCount() != 0 || len(rt.store.Folders[0].Folders) != 0 || w.open != nil {
			t.Errorf("after remove: %d children, open %+v", treeItem(tree, "em-ohip-hotel").ChildCount(), w.open)
		}
	})

	if saved, err := collection.Load(files.Requests); err != nil || len(saved.Folders) != 2 || len(saved.Folders[0].Folders) != 0 {
		t.Errorf("removal not saved: %+v, %v", saved, err)
	}
}

// treeNames lists the names of the children of an item of the request tree,
// with a slash after those of folders.
func treeNames(item *qt.QTreeWidgetItem) []string {
	var names []string
	for i := range item.ChildCount() {
		child := item.Child(i)
		name := child.Text(colName)
		if itemType(child) == "" {
			name += "/"
		}
		names = append(names, name)
	}
	return names
}

func TestDragAndDrop(t *testing.T) {
	files := testFiles(t)

	// other/
	// service/
	//   rest/
	//   rpc/
	//     Get
	//     List
	//   Health
	store := &collection.Store{}
	service := store.AddFolder("service")
	rpcFolder := service.AddFolder("rpc")
	rpcFolder.Add(collection.NewGRPC("Get", rpc.Request{}))
	rpcFolder.Add(collection.NewGRPC("List", rpc.Request{}))
	service.AddFolder("rest")
	service.Add(collection.NewREST("Health", rest.Request{Method: "GET"}))
	store.AddFolder("other")
	if err := store.Save(files.Requests); err != nil {
		t.Fatal(err)
	}

	mainthread.Wait(func() {
		w := NewWindow(files)
		w.Show()
		rt := w.requests
		tree := rt.tree

		// The folders and requests of the window are its own copies, kept
		// in alphabetical order.
		other, service := rt.store.Folders[0], rt.store.Folders[1]
		restFolder, rpcFolder := service.Folders[0], service.Folders[1]
		get, list, health := rpcFolder.Requests[0], rpcFolder.Requests[1], service.Requests[0]
		if other.Name != "other" || restFolder.Name != "rest" || get.Name != "Get" || health.Name != "Health" {
			t.Fatalf("store is not as expected: %q %q %q %q", other.Name, restFolder.Name, get.Name, health.Name)
		}

		check := func(what string, item *qt.QTreeWidgetItem, want ...string) {
			t.Helper()
			if item == nil {
				t.Errorf("%s: no such item", what)
				return
			}
			if got := treeNames(item); !reflect.DeepEqual(got, want) {
				t.Errorf("%s: %q, want %q", what, got, want)
			}
		}

		// Nothing can be dropped onto a request, only beside it.
		if treeItem(tree, "service", "Health").Flags()&qt.ItemIsDropEnabled != 0 || treeItem(tree, "service").Flags()&qt.ItemIsDropEnabled == 0 {
			t.Error("drop flags are wrong")
		}

		// Open a request, and close a folder, to see that both survive.
		tree.SetCurrentItem(treeItem(tree, "service", "Health"))
		if w.open != health {
			t.Fatalf("open = %+v", w.open)
		}
		treeItem(tree, "other").SetExpanded(false)

		// A request dropped on a folder goes inside it, in its place in the
		// alphabetical order.
		if !rt.drop(nil, health, rpcFolder, nil, dropOn) {
			t.Fatal("request not moved")
		}
		check("service", treeItem(tree, "service"), "rest/", "rpc/")
		check("rpc", treeItem(tree, "service", "rpc"), "Get", "Health", "List")
		if tree.CurrentItem().Text(colName) != "Health" || w.open != health {
			t.Errorf("selected %q, open %+v", tree.CurrentItem().Text(colName), w.open)
		}
		if treeItem(tree, "other").IsExpanded() || !treeItem(tree, "service").IsExpanded() {
			t.Error("folders did not stay open or closed")
		}

		// Dropping it beside another request in the same folder leaves the
		// order alphabetical.
		rt.drop(nil, health, rpcFolder, get, dropAbove)
		check("rpc", treeItem(tree, "service", "rpc"), "Get", "Health", "List")

		// Dropped beside a folder it goes in the folder that holds that one,
		// with the requests, which come after the folders.
		rt.drop(nil, list, restFolder, nil, dropAbove)
		check("service", treeItem(tree, "service"), "rest/", "rpc/", "List")

		// A request cannot be put at the top level.
		if rt.drop(nil, list, nil, nil, dropNowhere) || rt.drop(nil, list, service, nil, dropBelow) {
			t.Error("request moved to the top level")
		}
		check("service", treeItem(tree, "service"), "rest/", "rpc/", "List")

		// A folder dropped on a closed folder goes inside it, which opens.
		if !rt.drop(rpcFolder, nil, other, nil, dropOn) {
			t.Fatal("folder not moved")
		}
		check("service", treeItem(tree, "service"), "rest/", "List")
		check("other", treeItem(tree, "other"), "rpc/")
		check("rpc", treeItem(tree, "other", "rpc"), "Get", "Health")
		if !treeItem(tree, "other").IsExpanded() || tree.CurrentItem().Text(colName) != "rpc" {
			t.Errorf("other open %t, selected %q", treeItem(tree, "other").IsExpanded(), tree.CurrentItem().Text(colName))
		}

		// The open request is still found where it now is.
		tree.SetCurrentItem(treeItem(tree, "other", "rpc", "Health"))
		w.request.url.SetText("moved.test")
		if health.URL != "moved.test" {
			t.Errorf("edit went to %+v", health.Request)
		}

		// A folder cannot be put inside itself.
		if rt.drop(other, nil, rpcFolder, nil, dropOn) {
			t.Error("folder moved inside itself")
		}

		// A folder dropped in the empty space goes to the top level, in its
		// place in the order.
		rt.drop(restFolder, nil, nil, nil, dropNowhere)
		var top []string
		for i := range tree.TopLevelItemCount() {
			top = append(top, tree.TopLevelItem(i).Text(colName))
		}
		if want := []string{"other", "rest", "service"}; !reflect.DeepEqual(top, want) {
			t.Errorf("top level = %q, want %q", top, want)
		}

		// A folder dropped beside a request goes with the folders there.
		rt.drop(restFolder, nil, rpcFolder, get, dropBelow)
		check("rpc", treeItem(tree, "other", "rpc"), "rest/", "Get", "Health")
	})

	saved, err := collection.Load(files.Requests)
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.Folders) != 2 || saved.Folders[0].Name != "other" || saved.Folders[0].Folders[0].Folders[0].Name != "rest" || saved.Folders[0].Folders[0].Requests[1].URL != "moved.test" || saved.Folders[1].Requests[0].Name != "List" {
		t.Errorf("saved %+v", saved.Folders)
	}
}

func TestImportOpenAPI(t *testing.T) {
	files := testFiles(t)
	dir := filepath.Dir(files.Requests)

	spec := filepath.Join(dir, "shop.yaml")
	os.WriteFile(spec, []byte(`
openapi: 3.0.3
info:
  title: Shop
servers:
  - url: https://shop.example.com
paths:
  /orders:
    get:
      tags: [orders]
      summary: List orders
      parameters:
        - name: page
          in: query
          required: true
          example: 2
    post:
      tags: [orders]
      summary: Create order
      requestBody:
        content:
          application/json:
            example: {item: tea, quantity: 2}
  /health:
    get:
      summary: Health
`), 0o644)
	bad := filepath.Join(dir, "bad.yaml")
	os.WriteFile(bad, []byte("just: text"), 0o644)

	mainthread.Wait(func() {
		w := NewWindow(files)
		w.Show()
		rt := w.requests
		tree := rt.tree

		var warned []string
		rt.warn = func(message string) { warned = append(warned, message) }

		// There is something already, which is left closed.
		rt.addRequest()
		tree.TopLevelItem(0).SetExpanded(false)

		// Deciding against importing, or against choosing a file, does
		// nothing.
		rt.chooseSource = func() (importSource, bool) { return "", false }
		rt.chooseFile = func(importSource) (string, bool) { t.Error("asked for a file"); return "", false }
		rt.importDocument()

		var asked []importSource
		rt.chooseSource = func() (importSource, bool) { return importOpenAPI, true }
		rt.chooseFile = func(source importSource) (string, bool) { asked = append(asked, source); return "", false }
		rt.importDocument()
		if !reflect.DeepEqual(asked, []importSource{importOpenAPI}) {
			t.Errorf("asked for a file of %q", asked)
		}
		if tree.TopLevelItemCount() != 1 || len(warned) != 0 {
			t.Fatalf("imported without a file: %d folders, warned %q", tree.TopLevelItemCount(), warned)
		}

		rt.chooseFile = func(importSource) (string, bool) { return spec, true }
		rt.importDocument()
		if len(warned) != 0 || tree.TopLevelItemCount() != 2 {
			t.Fatalf("not imported: %d folders, warned %q", tree.TopLevelItemCount(), warned)
		}

		// The folder is added in its place in the order, open and selected.
		shop := tree.TopLevelItem(1)
		if shop.Text(colName) != "Shop" || !shop.IsExpanded() || tree.TopLevelItem(0).IsExpanded() || tree.CurrentItem().Text(colName) != "Shop" {
			t.Errorf("folder = %q, open %t", shop.Text(colName), shop.IsExpanded())
		}
		if got, want := treeNames(shop), []string{"orders/", "Health"}; !reflect.DeepEqual(got, want) {
			t.Errorf("shop = %q, want %q", got, want)
		}
		// The folders inside it start off closed.
		if shop.Child(0).IsExpanded() {
			t.Error("imported subfolder is open")
		}
		if got, want := treeNames(shop.Child(0)), []string{"Create order", "List orders"}; !reflect.DeepEqual(got, want) {
			t.Errorf("orders = %q, want %q", got, want)
		}

		// An imported request opens like any other.
		tree.SetCurrentItem(treeItem(tree, "Shop", "orders", "List orders"))
		if w.open == nil || w.request.url.Text() != "https://shop.example.com/orders?page=2" || w.request.method.CurrentText() != "GET" {
			t.Errorf("open = %+v, url %q", w.open, w.request.url.Text())
		}
		tree.SetCurrentItem(treeItem(tree, "Shop", "orders", "Create order"))
		if w.request.method.CurrentText() != "POST" || !strings.Contains(w.request.bodyRaw.ToPlainText(), `"item": "tea"`) || !w.request.bodyIsJSON() {
			t.Errorf("create: %q %q", w.request.method.CurrentText(), w.request.bodyRaw.ToPlainText())
		}

		// A file that cannot be imported is reported, and adds nothing.
		for _, path := range []string{bad, filepath.Join(dir, "missing.yaml")} {
			rt.chooseFile = func(importSource) (string, bool) { return path, true }
			rt.importDocument()
		}
		if len(warned) != 2 || !strings.Contains(warned[0], "bad.yaml could not be imported") || !strings.Contains(warned[0], "not an OpenAPI document") || tree.TopLevelItemCount() != 2 {
			t.Errorf("warned %q, %d folders", warned, tree.TopLevelItemCount())
		}
	})

	saved, err := collection.Load(files.Requests)
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.Folders) != 2 || saved.Folders[1].Name != "Shop" || saved.Folders[1].Folders[0].Requests[0].Name != "Create order" {
		t.Errorf("saved %+v", saved.Folders)
	}
}

func TestSidebarOrder(t *testing.T) {
	files := testFiles(t)

	var w *Window
	mainthread.Wait(func() {
		w = NewWindow(files)
		w.Show()

		// Environments are listed alphabetically, whatever order they were
		// added in, and the one being edited stays selected as it moves.
		for _, name := range []string{"staging", "Local", "dev"} {
			w.environments.add()
			w.editor.name.SetText(name)
		}
		var names []string
		for i := range w.environments.list.Count() {
			names = append(names, w.environments.list.Item(i).Text())
		}
		if want := []string{"dev", "Local", "staging"}; !reflect.DeepEqual(names, want) {
			t.Errorf("environments = %q, want %q", names, want)
		}
		if current, _ := w.environments.store.Current(); current == nil || current.Name != "dev" || w.environments.list.CurrentRow() != 0 || w.editor.name.Text() != "dev" {
			t.Errorf("active = %+v, row %d", current, w.environments.list.CurrentRow())
		}

		// Renaming one moves it, and the header follows.
		w.editor.name.SetText("zulu")
		if w.environments.list.CurrentRow() != 2 || w.environments.list.Item(2).Text() != "zulu" || w.request.header.environment.CurrentText() != "zulu" {
			t.Errorf("after rename: row %d, header %q", w.environments.list.CurrentRow(), w.request.header.environment.CurrentText())
		}
		w.editor.close.Click()

		// Folders and requests are added in their place in the order.
		rt := w.requests
		rt.addFolder()
		renameCurrent(rt, "zebra")
		rt.deselect()
		rt.addFolder()
		renameCurrent(rt, "Apple")
		if rt.tree.TopLevelItem(0).Text(colName) != "Apple" || rt.tree.TopLevelItem(1).Text(colName) != "zebra" {
			t.Errorf("folders = %q, %q", rt.tree.TopLevelItem(0).Text(colName), rt.tree.TopLevelItem(1).Text(colName))
		}

		rt.tree.SetCurrentItem(treeItem(rt.tree, "Apple"))
		for _, name := range []string{"update", "Create", "list"} {
			rt.addRequest()
			renameCurrent(rt, name)
			rt.tree.SetCurrentItem(treeItem(rt.tree, "Apple"))
		}
		rt.addFolder()
		renameCurrent(rt, "inner")
		if got, want := treeNames(treeItem(rt.tree, "Apple")), []string{"inner/", "Create", "list", "update"}; !reflect.DeepEqual(got, want) {
			t.Errorf("Apple = %q, want %q", got, want)
		}

		// A rename made in the tree is put in order a moment later, once
		// the tree has finished with the edit.
		rt.tree.SetCurrentItem(treeItem(rt.tree, "Apple", "Create"))
		rt.tree.CurrentItem().SetText(colName, "wipe")
	})

	waitFor(t, "the tree to be put in order", func() bool {
		return reflect.DeepEqual(treeNames(treeItem(w.requests.tree, "Apple")), []string{"inner/", "list", "update", "wipe"})
	})

	mainthread.Wait(func() {
		// The renamed request is still the selected and open one.
		if w.requests.tree.CurrentItem().Text(colName) != "wipe" || w.open == nil || w.open.Name != "wipe" {
			t.Errorf("selected %q, open %+v", w.requests.tree.CurrentItem().Text(colName), w.open)
		}
	})

	// Both files are saved in the order shown.
	envs, _ := env.Load(files.Environments)
	if len(envs.Environments) != 3 || envs.Environments[0].Name != "Local" || envs.Environments[2].Name != "zulu" || envs.Active != 2 {
		t.Errorf("saved environments = %+v", envs)
	}
	requests, _ := collection.Load(files.Requests)
	if requests.Folders[0].Name != "Apple" || requests.Folders[0].Requests[2].Name != "wipe" {
		t.Errorf("saved requests = %+v", requests.Folders[0])
	}
}

func TestImportPostman(t *testing.T) {
	files := testFiles(t)
	dir := filepath.Dir(files.Requests)

	exported := filepath.Join(dir, "hotel.postman_collection.json")
	os.WriteFile(exported, []byte(`{
		"info": {"name": "Hotel", "schema": "https://schema.getpostman.com/json/collection/v2.1.0/collection.json"},
		"item": [
			{"name": "Rooms", "item": [
				{"name": "Deep", "item": [
					{"name": "Book room", "request": {
						"method": "POST",
						"auth": {"type": "bearer", "bearer": [{"key": "token", "value": "{{Token}}"}]},
						"header": [{"key": "x-app-key", "value": "{{AppKey}}"}],
						"body": {"mode": "raw", "raw": "{\"room\": 1}", "options": {"raw": {"language": "json"}}},
						"url": {"raw": "{{HostName}}/rooms?dry=1", "query": [{"key": "dry", "value": "1"}, {"key": "debug", "value": "x", "disabled": true}]}
					}}
				]}
			]},
			{"name": "Ping", "request": {"method": "GET", "url": "{{HostName}}/ping"}}
		]
	}`), 0o644)
	spec := filepath.Join(dir, "spec.json")
	os.WriteFile(spec, []byte(`{"openapi": "3.0.0", "paths": {"/a": {"get": {}}}}`), 0o644)

	mainthread.Wait(func() {
		w := NewWindow(files)
		w.Show()
		rt := w.requests
		tree := rt.tree

		var warned []string
		rt.warn = func(message string) { warned = append(warned, message) }

		// The file is read as the kind that was chosen.
		rt.chooseSource = func() (importSource, bool) { return importPostman, true }
		rt.chooseFile = func(source importSource) (string, bool) {
			if source != importPostman {
				t.Errorf("asked for a file of %q", source)
			}
			return exported, true
		}
		rt.importDocument()
		if len(warned) != 0 || tree.TopLevelItemCount() != 1 {
			t.Fatalf("not imported: %d folders, warned %q", tree.TopLevelItemCount(), warned)
		}

		hotel := treeItem(tree, "Hotel")
		if got, want := treeNames(hotel), []string{"Rooms/", "Ping"}; !reflect.DeepEqual(got, want) || !hotel.IsExpanded() || hotel.Child(0).IsExpanded() {
			t.Errorf("hotel = %q, want %q", got, want)
		}

		// A request deep inside it opens with everything it was given.
		tree.SetCurrentItem(treeItem(tree, "Hotel", "Rooms", "Deep", "Book room"))
		got := w.request.request()
		want := rest.Request{
			Method:   "POST",
			URL:      "{{HostName}}/rooms",
			Params:   []rest.KeyValue{{Key: "debug", Value: "x"}, {Key: "dry", Value: "1", Enabled: true}},
			Headers:  []rest.KeyValue{{Key: "x-app-key", Value: "{{AppKey}}", Enabled: true}},
			Auth:     rest.Auth{Type: rest.AuthBearer, Token: "{{Token}}"},
			BodyType: rest.BodyJSON,
			Body:     `{"room": 1}`,
		}
		if !reflect.DeepEqual(got, want) || w.request.url.Text() != "{{HostName}}/rooms?dry=1" {
			t.Errorf("request = %+v\nwant %+v", got, want)
		}

		// A file of the other kind is refused, and adds nothing.
		rt.chooseFile = func(importSource) (string, bool) { return spec, true }
		rt.importDocument()
		if len(warned) != 1 || !strings.Contains(warned[0], "not a Postman collection") || tree.TopLevelItemCount() != 1 {
			t.Errorf("warned %q", warned)
		}
	})
}

func TestImportDialog(t *testing.T) {
	mainthread.Wait(func() {
		w := NewWindow(testFiles(t))

		for source, label := range map[importSource]string{importOpenAPI: "OpenAPI", importPostman: "Postman"} {
			dialog := newImportDialog(w.QWidget)
			dialog.Show()

			button := dialog.buttons[source]
			if button == nil || button.Text() != label {
				t.Fatalf("no button labelled %q", label)
			}

			button.Click()
			if dialog.chosen != source || dialog.Result() != int(qt.QDialog__Accepted) || dialog.IsVisible() {
				t.Errorf("clicking %s chose %q with result %d", label, dialog.chosen, dialog.Result())
			}
		}
	})
}

func TestFoldersStayOpenOrClosed(t *testing.T) {
	files := testFiles(t)

	store := &collection.Store{}
	a := store.AddFolder("a")
	a.AddFolder("inner").Add(collection.NewREST("r", rest.Request{Method: "GET"}))
	store.AddFolder("b")
	if err := store.Save(files.Requests); err != nil {
		t.Fatal(err)
	}

	mainthread.Wait(func() {
		w := NewWindow(files)
		w.Show()
		tree := w.requests.tree

		// Folders are open until they are closed.
		if !treeItem(tree, "a").IsExpanded() || !treeItem(tree, "a", "inner").IsExpanded() {
			t.Error("folders do not start open")
		}
		treeItem(tree, "a", "inner").SetExpanded(false)
		treeItem(tree, "b").SetExpanded(false)
		treeItem(tree, "b").SetExpanded(true)
	})

	saved, err := collection.Load(files.Requests)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Folders[0].Collapsed || !saved.Folders[0].Folders[0].Collapsed || saved.Folders[1].Collapsed {
		t.Errorf("saved %+v, inner %+v", saved.Folders[0], saved.Folders[0].Folders[0])
	}

	// They are as they were left the next time.
	mainthread.Wait(func() {
		w := NewWindow(files)
		w.Show()
		tree := w.requests.tree

		if !treeItem(tree, "a").IsExpanded() || treeItem(tree, "a", "inner").IsExpanded() || !treeItem(tree, "b").IsExpanded() {
			t.Error("folders were not restored as they were left")
		}

		// Adding to a closed folder opens it.
		tree.SetCurrentItem(treeItem(tree, "a", "inner"))
		w.requests.addRequest()
		if !treeItem(tree, "a", "inner").IsExpanded() {
			t.Error("folder not opened for the request added to it")
		}
	})
}

func TestImportEnvironment(t *testing.T) {
	files := testFiles(t)

	store := &env.Store{Active: 0, Environments: []env.Environment{{Name: "Local"}}}
	if err := store.Save(files.Environments); err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	exported := filepath.Join(dir, "staging.postman_environment.json")
	os.WriteFile(exported, []byte(`{
		"name": "Staging",
		"values": [
			{"key": "host", "value": "api.test", "enabled": true},
			{"key": "off", "value": "x", "enabled": false}
		]
	}`), 0o644)
	other := filepath.Join(dir, "collection.json")
	os.WriteFile(other, []byte(`{"info": {"name": "A"}, "item": []}`), 0o644)

	mainthread.Wait(func() {
		w := NewWindow(files)
		w.Show()
		el := w.environments

		var warned []string
		el.warn = func(message string) { warned = append(warned, message) }

		// Nothing is asked for or added if no kind is chosen, or no file.
		el.chooseSource = func() (envImportSource, bool) { return "", false }
		el.chooseFile = func(envImportSource) (string, bool) { t.Error("asked for a file"); return "", false }
		el.importDocument()
		el.chooseSource = func() (envImportSource, bool) { return envImportPostman, true }
		el.chooseFile = func(envImportSource) (string, bool) { return "", false }
		el.importDocument()
		if el.list.Count() != 1 || len(warned) != 0 {
			t.Fatalf("%d environments, warned %q", el.list.Count(), warned)
		}

		// The imported environment is listed in order, selected and saved.
		el.chooseFile = func(envImportSource) (string, bool) { return exported, true }
		el.importDocument()
		if len(warned) != 0 || el.list.Count() != 2 || el.list.CurrentRow() != 1 || el.list.Item(1).Text() != "Staging" {
			t.Fatalf("not imported: %d environments, row %d, warned %q", el.list.Count(), el.list.CurrentRow(), warned)
		}
		if got := w.expand("{{host}}/{{off}}"); got != "api.test/{{off}}" {
			t.Errorf("expanded to %q", got)
		}
		if w.request.header.environment.CurrentText() != "Staging" {
			t.Errorf("header shows %q", w.request.header.environment.CurrentText())
		}

		saved, err := env.Load(files.Environments)
		want := []env.Variable{{Key: "host", Value: "api.test", Enabled: true}, {Key: "off", Value: "x"}}
		if err != nil || saved.Active != 1 || len(saved.Environments) != 2 || !reflect.DeepEqual(saved.Environments[1].Variables, want) {
			t.Errorf("saved %+v, %v", saved, err)
		}

		// A file of another kind is refused, and adds nothing.
		el.chooseFile = func(envImportSource) (string, bool) { return other, true }
		el.importDocument()
		if len(warned) != 1 || !strings.Contains(warned[0], "not a Postman environment") || el.list.Count() != 2 {
			t.Errorf("warned %q", warned)
		}

		// The dialog offers Postman alone.
		dialog := newEnvImportDialog(w.QWidget)
		dialog.Show()
		button := dialog.buttons[envImportPostman]
		if len(dialog.buttons) != 1 || button == nil || button.Text() != "Postman" {
			t.Fatal("no button labelled Postman")
		}
		button.Click()
		if dialog.chosen != envImportPostman || dialog.Result() != int(qt.QDialog__Accepted) {
			t.Errorf("clicking chose %q with result %d", dialog.chosen, dialog.Result())
		}
	})
}

func TestKeyValueOrder(t *testing.T) {
	mainthread.Wait(func() {
		table := newKeyValueTable("Key", "Value")
		changes := 0
		table.onChange = func() { changes++ }

		keys := func() []string {
			var keys []string
			for _, item := range table.items() {
				keys = append(keys, item.Key)
			}
			return keys
		}

		// Pairs are shown in order, ignoring case, and those with the same
		// key stay as they were.
		table.setItems([]rest.KeyValue{
			{Key: "zeta", Value: "1", Enabled: true},
			{Key: "Beta", Value: "first"},
			{Key: "alpha", Value: "3", Enabled: true},
			{Key: "beta", Value: "second", Enabled: true},
		})
		want := []rest.KeyValue{
			{Key: "alpha", Value: "3", Enabled: true},
			{Key: "Beta", Value: "first"},
			{Key: "beta", Value: "second", Enabled: true},
			{Key: "zeta", Value: "1", Enabled: true},
		}
		if got := table.items(); !reflect.DeepEqual(got, want) || changes != 0 {
			t.Fatalf("items = %+v, %d changes", got, changes)
		}

		// A row whose key is edited moves to its place, taking the rest of
		// the row and the current cell with it.
		table.table.SetCurrentCell(0, colValue)
		table.table.Item(0, colKey).SetText("gamma")
		changes = 0
		table.sort()
		if got, want := keys(), []string{"Beta", "beta", "gamma", "zeta"}; !reflect.DeepEqual(got, want) {
			t.Errorf("keys = %q, want %q", got, want)
		}
		if got := table.pair(2); got != (rest.KeyValue{Key: "gamma", Value: "3", Enabled: true}) || table.pair(0).Enabled {
			t.Errorf("moved row = %+v", got)
		}
		if table.table.CurrentRow() != 2 || table.table.CurrentColumn() != colValue || changes != 1 {
			t.Errorf("current cell = %d,%d with %d changes", table.table.CurrentRow(), table.table.CurrentColumn(), changes)
		}

		// Nothing is reported when the rows are already in order.
		table.sort()
		if changes != 1 {
			t.Errorf("%d changes", changes)
		}

		// A new pair goes to its place, leaving the empty row at the bottom.
		table.add("delta", "4")
		if got, want := keys(), []string{"Beta", "beta", "delta", "gamma", "zeta"}; !reflect.DeepEqual(got, want) {
			t.Errorf("keys = %q, want %q", got, want)
		}
		if last := table.table.RowCount() - 1; last != 5 || table.pair(last).Key != "" {
			t.Errorf("last row = %+v of %d", table.pair(last), last+1)
		}
	})
}

func TestParamsOrder(t *testing.T) {
	mainthread.Wait(func() {
		w := NewWindow(testFiles(t))

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

func TestKeyValueOrderAfterEditing(t *testing.T) {
	var table *keyValueTable
	mainthread.Wait(func() {
		table = newKeyValueTable("Key", "Value")
		table.setItems([]rest.KeyValue{{Key: "a", Value: "1", Enabled: true}, {Key: "b", Value: "2", Enabled: true}})
		table.table.Show()

		// The key of the first row is edited and the editor closed.
		item := table.table.Item(0, colKey)
		table.table.SetCurrentItem(item)
		table.table.EditItem(item)
		item.SetText("c")
		table.table.ClosePersistentEditor(item)
		table.table.SetCurrentItem(table.table.Item(0, colKey))
	})

	waitFor(t, "the rows to be put in order", func() bool {
		return table.pair(0).Key == "b" && table.pair(1) == rest.KeyValue{Key: "c", Value: "1", Enabled: true}
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
		w := NewWindow(files)
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
		w = NewWindow(files)
		w.Show()
		w.request.load(rest.Request{Method: "GET", URL: "{{host}}/a", Headers: []rest.KeyValue{{Key: "X-Key", Value: "abc", Enabled: true}}})
		w.request.curl.Click()
		w.request.execute.Click()
	})

	// The response to the command is shown as any other is.
	waitFor(t, "the response", func() bool { return fieldValue(w.response, "Code") != "" })
	mainthread.Wait(func() {
		if got := fieldValue(w.response, "Code"); got != "200 OK" || isFailure(w.response) {
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
	waitFor(t, "the failure", func() bool { return strings.HasPrefix(fieldValue(w.response, "Code"), "404") })
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

func TestSearchRequests(t *testing.T) {
	files := testFiles(t)

	store := &collection.Store{}
	hotel := store.AddFolder("Hotel")
	hotel.Add(collection.NewREST("Ping", rest.Request{Method: "GET"}))
	rooms := hotel.AddFolder("Rooms")
	rooms.Collapsed = true
	rooms.Add(collection.NewREST("Book room", rest.Request{Method: "POST"}))
	rooms.Add(collection.NewREST("List rooms", rest.Request{Method: "GET"}))
	users := store.AddFolder("Users")
	users.Collapsed = true
	users.Add(collection.NewREST("List users", rest.Request{Method: "GET"}))
	store.AddFolder("Empty")
	if err := store.Save(files.Requests); err != nil {
		t.Fatal(err)
	}

	mainthread.Wait(func() {
		w := NewWindow(files)
		w.Show()
		rt := w.requests
		tree := rt.tree

		// shown lists the paths of the items that are not hidden.
		shown := func() []string {
			var paths []string
			var walk func(item *qt.QTreeWidgetItem, path string)
			walk = func(item *qt.QTreeWidgetItem, path string) {
				if item.IsHidden() {
					return
				}
				path += "/" + item.Text(colName)
				paths = append(paths, path)
				for i := range item.ChildCount() {
					walk(item.Child(i), path)
				}
			}
			for i := range tree.TopLevelItemCount() {
				walk(tree.TopLevelItem(i), "")
			}
			return paths
		}
		check := func(query string, want ...string) {
			t.Helper()
			rt.search.SetText(query)
			if got := shown(); !reflect.DeepEqual(got, want) {
				t.Errorf("search %q shows %q, want %q", query, got, want)
			}
		}

		// Requests that match are shown with the folders they are in, which
		// are opened, whatever the case of the letters.
		check("LIST", "/Hotel", "/Hotel/Rooms", "/Hotel/Rooms/List rooms", "/Users", "/Users/List users")
		if !treeItem(tree, "Hotel", "Rooms").IsExpanded() || !treeItem(tree, "Users").IsExpanded() {
			t.Error("the folders of what was found are not open")
		}

		// Every word must match, in the name or the folders above.
		check("list room", "/Hotel", "/Hotel/Rooms", "/Hotel/Rooms/List rooms")
		check("users list", "/Users", "/Users/List users")

		// A folder that matches is shown with everything in it.
		check("rooms", "/Hotel", "/Hotel/Rooms", "/Hotel/Rooms/Book room", "/Hotel/Rooms/List rooms")
		check("empty", "/Empty")
		check("nothing like this")

		// What is found is still the request it was.
		rt.search.SetText("book")
		tree.SetCurrentItem(treeItem(tree, "Hotel", "Rooms", "Book room"))
		if w.open == nil || w.open.Name != "Book room" || w.request.method.CurrentText() != "POST" {
			t.Errorf("opened %+v", w.open)
		}

		// Clearing the search shows everything, with the folders open or
		// closed as they were, and the request still selected.
		check("", "/Empty", "/Hotel", "/Hotel/Rooms", "/Hotel/Rooms/Book room", "/Hotel/Rooms/List rooms", "/Hotel/Ping", "/Users", "/Users/List users")
		// Rooms was closed, but is left open to show the selected request.
		if !treeItem(tree, "Hotel", "Rooms").IsExpanded() || treeItem(tree, "Users").IsExpanded() || !treeItem(tree, "Hotel").IsExpanded() {
			t.Error("the folders are not as they were")
		}
		if item := tree.CurrentItem(); item == nil || item.Text(colName) != "Book room" {
			t.Error("the request is no longer selected")
		}
		if !rooms.Collapsed || !users.Collapsed || hotel.Collapsed {
			t.Error("the search changed which folders are closed")
		}

		// Adding something clears the search, so that it can be seen.
		rt.search.SetText("users")
		tree.SetCurrentItem(treeItem(tree, "Users"))
		rt.addRequest()
		if rt.search.Text() != "" || len(shown()) != 9 || tree.CurrentItem().Text(colName) != "New Request" {
			t.Errorf("after adding: search %q, shown %q", rt.search.Text(), shown())
		}
	})
}

func TestSaveResponse(t *testing.T) {
	dir := t.TempDir()

	mainthread.Wait(func() {
		w := NewWindow(testFiles(t))
		w.Show()

		for _, p := range []*responsePanel{w.response, w.grpcResponse} {
			var offered []string
			var warned []string
			target := filepath.Join(dir, "saved")
			p.chooseSave = func(name string) (string, bool) { offered = append(offered, name); return target, true }
			p.warn = func(message string) { warned = append(warned, message) }

			// There is nothing to save until there is a response.
			if p.save.IsEnabled() || !p.root.IsAncestorOf(p.save.QWidget) {
				t.Fatal("the save button is enabled without a response, or is not in the panel")
			}

			// JSON is saved formatted, as it is shown.
			raw := []byte(`{"a":1}`)
			p.show(&rest.Response{Status: "200 OK", StatusCode: 200, Headers: http.Header{"Content-Type": {"application/json"}}, Body: raw})
			if !p.save.IsEnabled() {
				t.Fatal("the save button is not enabled")
			}
			p.save.Click()
			if got, err := os.ReadFile(target); err != nil || string(got) != "{\n  \"a\": 1\n}" || string(got) != p.body.ToPlainText() || !reflect.DeepEqual(offered, []string{"response.json"}) {
				t.Errorf("saved %q, %v, offered %q", got, err, offered)
			}

			// Anything else is saved as it was received, as is JSON that
			// is not valid.
			for contentType, body := range map[string]string{"application/octet-stream": "\x00\xff {\"a\":1}", "application/json": `{"a":`} {
				p.show(&rest.Response{Status: "200 OK", StatusCode: 200, Headers: http.Header{"Content-Type": {contentType}}, Body: []byte(body)})
				p.save.Click()
				if got, _ := os.ReadFile(target); string(got) != body {
					t.Errorf("saved %q, want %q", got, body)
				}
			}

			// The messages of a gRPC call are saved as they are shown.
			p.showRPC(&rpc.Response{Messages: []string{"{\n  \"a\": 1\n}", "{\n  \"b\": 2\n}"}})
			p.save.Click()
			if got, _ := os.ReadFile(target); string(got) != p.body.ToPlainText() || !strings.Contains(string(got), `"b": 2`) {
				t.Errorf("saved %q", got)
			}

			// Nothing is written if no file is chosen.
			os.Remove(target)
			p.chooseSave = func(string) (string, bool) { return "", false }
			p.save.Click()
			if _, err := os.Stat(target); err == nil {
				t.Error("saved without a file being chosen")
			}

			// A file that cannot be written is reported.
			p.chooseSave = func(string) (string, bool) { return filepath.Join(dir, "missing", "file"), true }
			p.save.Click()
			if len(warned) != 1 || !strings.Contains(warned[0], "could not be saved") {
				t.Errorf("warned %q", warned)
			}

			// A response without a body, an error and a new request leave
			// nothing to save.
			p.show(&rest.Response{Status: "204 No Content", StatusCode: 204, Headers: http.Header{}})
			if p.save.IsEnabled() {
				t.Error("an empty response can be saved")
			}
			p.show(&rest.Response{Status: "200 OK", StatusCode: 200, Headers: http.Header{}, Body: raw})
			p.showError(io.EOF, time.Second)
			if p.save.IsEnabled() {
				t.Error("an error can be saved")
			}
		}

		for contentType, want := range map[string]string{
			"application/json; charset=utf-8": "response.json",
			"text/xml":                        "response.xml",
			"text/html":                       "response.html",
			"text/plain":                      "response.txt",
			"":                                "response.bin",
		} {
			if got := responseFileName(contentType); got != want {
				t.Errorf("file name for %q = %q, want %q", contentType, got, want)
			}
		}
	})
}

package ui

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	qt "github.com/mappu/miqt/qt6"
	"github.com/mappu/miqt/qt6/mainthread"

	"github.com/nomad-software/postie/internal/collection"
	"github.com/nomad-software/postie/internal/env"
	"github.com/nomad-software/postie/internal/rest"
	"github.com/nomad-software/postie/internal/settings"
)

// TestMain runs the Qt event loop on the main thread and the tests on another
// goroutine. Tests must touch widgets through mainthread.Wait.
func TestMain(m *testing.M) {
	os.Setenv("QT_QPA_PLATFORM", "offscreen")
	qt.NewQApplication(os.Args[:1])
	// Tests close windows, which must not end the event loop.
	qt.QGuiApplication_SetQuitOnLastWindowClosed(false)

	code := 0
	go func() {
		code = m.Run()
		mainthread.Start(qt.QCoreApplication_Quit)
	}()

	qt.QApplication_Exec()
	os.Exit(code)
}

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
		w.request.authType.SetCurrentIndex(2) // Bearer Token
		w.request.token.SetText("secret")
		w.request.bodyType.SetCurrentIndex(1) // JSON
		w.request.bodyRaw.SetPlainText(`{"name":"postie"}`)

		w.request.send.Click()
	})

	select {
	case r := <-got:
		want := received{"POST", "page=2", "yes", "Bearer secret", "application/json", `{"name":"postie"}`}
		if r != want {
			t.Errorf("server received %+v, want %+v", r, want)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("request was not sent")
	}

	deadline := time.Now().Add(5 * time.Second)
	for {
		status := mainthread.Wait2(func() string { return w.response.status.Text() })
		if status == "201 Created" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("status = %q", status)
		}
		time.Sleep(10 * time.Millisecond)
	}

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
		if w.StatusBar().CurrentMessage() == "" {
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
		if w.open != list || list.URL != "{{host}}/users" || w.request.title.Text() != "New Request" {
			t.Errorf("open = %+v, title %q", w.open, w.request.title.Text())
		}

		// Renaming updates the title.
		tree.TopLevelItem(0).SetText(0, "Users")
		tree.TopLevelItem(0).Child(0).SetText(0, "List")
		if list.Name != "List" || w.request.title.Text() != "List" || w.requests.store.Folders[0].Name != "Users" {
			t.Errorf("rename: %q, title %q", list.Name, w.request.title.Text())
		}

		// Adding with a request open starts from an empty request in the
		// same folder.
		w.requests.addRequest()
		create := w.requests.store.Folders[0].Requests[1]
		if w.open != create || w.request.url.Text() != "" || tree.TopLevelItem(0).ChildCount() != 2 {
			t.Errorf("second request: open %+v, url %q", w.open, w.request.url.Text())
		}
		tree.TopLevelItem(0).Child(1).SetText(0, "Create")

		// Edits are written to the open request.
		w.request.method.SetCurrentIndex(1) // POST
		w.request.url.SetText("{{host}}/users")
		w.request.params.add("dry", "1")
		w.request.headers.add("X-Test", "yes")
		w.request.authType.SetCurrentIndex(1) // Basic Auth
		w.request.username.SetText("me")
		w.request.password.SetText("pw")
		w.request.bodyType.SetCurrentIndex(1) // JSON
		w.request.bodyRaw.SetPlainText(`{"name":"a"}`)
		w.request.bodyForm.add("f", "1")

		// Switching between requests swaps the contents of the request UI.
		tree.SetCurrentItem(tree.TopLevelItem(0).Child(0))
		if w.open != list || w.request.method.CurrentText() != "GET" || w.request.bodyRaw.ToPlainText() != "" || len(w.request.headers.items()) != 0 || w.request.authType.CurrentIndex() != 0 {
			t.Errorf("first request not shown: %+v", w.request.request())
		}
		if list.Method != "GET" || create.Method != "POST" {
			t.Errorf("switching modified a request: %q %q", list.Method, create.Method)
		}

		// Selecting a folder leaves the open request alone.
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
	if len(saved.Folders) != 2 || saved.Folders[0].Name != "Users" || len(saved.Folders[0].Requests) != 2 {
		t.Fatalf("saved %+v", saved.Folders)
	}
	if got := saved.Folders[0].Requests[1]; got.Name != "Create" || !reflect.DeepEqual(got.Request, want) {
		t.Errorf("saved request %+v, want %+v", got, want)
	}

	// A new window restores the tree, and a request loads back unchanged.
	mainthread.Wait(func() {
		w := NewWindow(files)
		tree := w.requests.tree

		if tree.TopLevelItemCount() != 2 || tree.TopLevelItem(0).Text(0) != "Users" || tree.TopLevelItem(0).Child(1).Text(0) != "Create" {
			t.Fatal("tree not restored")
		}
		if w.open != nil || !w.request.title.IsHidden() {
			t.Error("a request is open at startup")
		}

		tree.SetCurrentItem(tree.TopLevelItem(0).Child(1))
		if got := w.request.request(); !reflect.DeepEqual(got, want) {
			t.Errorf("loaded %+v, want %+v", got, want)
		}

		// Removing the open request opens its neighbour.
		w.requests.remove()
		if folder := w.requests.store.Folders[0]; len(folder.Requests) != 1 || w.open != folder.Requests[0] || w.request.title.Text() != "List" {
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
		if w.open != nil || w.request.url.Text() != "" || !w.request.title.IsHidden() {
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
		if w.StatusBar().CurrentMessage() == "" {
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

	if got, want := settings.Load(files.Settings), (settings.Settings{Width: 900, Height: 650, ResponseFont: "Iosevka Term Curly"}); got != want {
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
	if got, want := settings.Load(files.Settings), (settings.Settings{Width: 900, Height: 650, Maximized: true, ResponseFont: "Iosevka Term Curly"}); got != want {
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
		if w.sidebar.Count() != 2 || w.sidebar.TabText(0) != "Environments" || w.sidebar.TabText(1) != "Requests" {
			t.Errorf("tabs = %d: %q, %q", w.sidebar.Count(), w.sidebar.TabText(0), w.sidebar.TabText(1))
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
		w.Resize(800, 500)
		w.quit.Trigger()
		if w.IsVisible() {
			t.Error("window still open after quit")
		}
	})

	if got := settings.Load(files.Settings); got.Width != 800 || got.Height != 500 {
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
		before := request.Request

		// Changing the environment recolours the fields, which must not
		// alter them or the saved request.
		w.environments.add()
		w.editor.variables.add("host", "example.com")
		w.editor.close.Click()

		if !reflect.DeepEqual(request.Request, before) || !reflect.DeepEqual(w.request.request(), before) {
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

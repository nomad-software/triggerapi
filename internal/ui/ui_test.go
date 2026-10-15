package ui

import (
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"runtime/debug"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf16"

	qt "github.com/mappu/miqt/qt6"
	"github.com/mappu/miqt/qt6/mainthread"

	"github.com/nomad-software/triggerapi/internal/capture"
	"github.com/nomad-software/triggerapi/internal/collection"
	"github.com/nomad-software/triggerapi/internal/crypt"
	"github.com/nomad-software/triggerapi/internal/crypt/crypttest"
	"github.com/nomad-software/triggerapi/internal/env"
	"github.com/nomad-software/triggerapi/internal/rest"
	"github.com/nomad-software/triggerapi/internal/rpc"
	"github.com/nomad-software/triggerapi/internal/rpc/rpctest"
	"github.com/nomad-software/triggerapi/internal/settings"
	"github.com/nomad-software/triggerapi/internal/workspace"
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

	// The dialog which says keys are needed is counted rather than shown.
	adviseKeys = func(*qt.QWidget) { advised++ }

	// Every window is given the keys it needs to read and save anything.
	keyDir, err := os.MkdirTemp("", "triggerapi-keys-")
	if err == nil {
		testPublic, testPrivate, err = crypttest.Write(keyDir, "id", "")
	}
	if err == nil {
		testKeys, err = crypt.Open(testPublic, testPrivate, nil)
	}
	if err != nil {
		panic(err)
	}

	code := 0
	go func() {
		code = m.Run()
		os.RemoveAll(keyDir)
		mainthread.Start(qt.QCoreApplication_Quit)
	}()

	qt.QApplication_Exec()
	os.Exit(code)
}

// selectMethod selects a method of the drop-down by its full name.
func (c *methodCombo) selectMethod(name string) {
	if index := slices.Index(c.names, name); index >= 0 && name != "" {
		c.SetCurrentIndex(index)
	}
}

// count returns the number of methods the drop-down lists.
func (c *methodCombo) count() int {
	return len(slices.DeleteFunc(slices.Clone(c.names), func(name string) bool { return name == "" }))
}

// count returns the number of methods the table lists.
func (e *endpointTable) count() int {
	return len(slices.DeleteFunc(slices.Clone(e.names), func(name string) bool { return name == "" }))
}

// testPublic and testPrivate are the paths of the files of the keys the
// settings of a test window name, and testKeys the keys themselves.
var (
	testPublic, testPrivate string
	testKeys                *crypt.Keys
)

// advised counts the times the user was told that keys are needed.
var advised int

// loadEnvs reads the environments a test window saved.
func loadEnvs(path string) (*env.Store, error) {
	return env.LoadWith(path, testKeys)
}

// loadRequests reads the requests a test window saved.
func loadRequests(path string) (*collection.Store, error) {
	return collection.LoadWith(path, testKeys)
}

// saveTestKeys makes a settings file which names the keys of the tests and
// nothing else, as a window without keys reads and saves nothing.
func saveTestKeys(t *testing.T, path string) {
	t.Helper()
	if err := settings.Prepare(path); err != nil {
		t.Fatal(err)
	}
	if err := (settings.Settings{PublicKey: testPublic, PrivateKey: testPrivate}).Save(path); err != nil {
		t.Fatal(err)
	}
}

// testRequestType is the type given to requests added by a test. An empty
// type stands for the user cancelling the dialog.
var testRequestType = collection.TypeREST

// testPaths are the places a test window keeps its data, with the paths of
// the files of its one workspace.
type testPaths struct {
	Files
	Environments string
	Requests     string
}

// testFiles returns places in a temporary folder for a window to keep its
// data in, with a workspace named Default that has nothing in it yet.
// closeWindows has every window closed when the test ends. A window saves
// its settings when it is closed, which the ones left open would otherwise
// do when the tests are over and the folder of their settings is gone. It is
// called after the folder is made, so that it is still there.
func closeWindows(t *testing.T) {
	t.Cleanup(func() {
		mainthread.Wait(func() {
			for _, widget := range qt.QApplication_TopLevelWidgets() {
				if widget.IsVisible() {
					widget.Close()
				}
			}
		})
	})
}

func testFiles(t *testing.T) testPaths {
	dir := t.TempDir()
	closeWindows(t)
	workspaces := filepath.Join(dir, "workspaces")
	if err := workspace.Create(workspaces, workspace.Default); err != nil {
		t.Fatal(err)
	}
	settingsPath := filepath.Join(dir, "settings.json")
	saveTestKeys(t, settingsPath)
	return testPaths{
		Files:        Files{Settings: filepath.Join(dir, "settings.json"), Workspaces: workspaces},
		Environments: workspace.Environments(workspaces, workspace.Default),
		Requests:     workspace.Requests(workspaces, workspace.Default),
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

		table.remove(0)
		if items := table.items(); len(items) != 1 || items[0].Key != "b" {
			t.Errorf("items after remove = %+v", items)
		}

		// The empty row cannot be removed.
		table.remove(1)
		if table.table.RowCount() != 2 {
			t.Errorf("rows = %d, want 2", table.table.RowCount())
		}
	})
}

func TestEnvironments(t *testing.T) {
	got := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got <- r.URL.Path + " " + r.Header.Get("X-Key")
	}))
	defer srv.Close()

	files := testFiles(t)
	path := files.Environments

	mainthread.Wait(func() {
		w := NewWindow(files.Files)
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
		if got := w.expand(w.request.url.Text()); !strings.Contains(got, "{{id}}") || !strings.Contains(got, "{{host}}") {
			t.Errorf("expanded = %q", got)
		}
		w.environments.list.SetCurrentRow(0)
		if got := w.expand(w.request.url.Text()); !strings.Contains(got, "{{id}}") || strings.Contains(got, "{{host}}") {
			t.Errorf("expanded = %q", got)
		}
		w.request.url.SetText("{{host}}/users")
		if got, want := w.expand(w.request.url.Text()), srv.URL+"/users"; got != want {
			t.Errorf("expanded = %q, want %q", got, want)
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
	saved, err := loadEnvs(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(saved, want) {
		t.Errorf("saved %+v, want %+v", saved, want)
	}

	// A new window restores the environments and the selection.
	mainthread.Wait(func() {
		w := NewWindow(files.Files)

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

		// Nothing is removed unless the user agrees to it.
		var asked string
		w.environments.confirm = func(question string) bool { asked = question; return false }
		w.environments.remove()
		if w.environments.list.Count() != 2 || !strings.Contains(asked, `"Local"`) {
			t.Errorf("%d environments after being asked %q", w.environments.list.Count(), asked)
		}
		w.environments.confirm = func(string) bool { return true }

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
	files := testFiles(t)
	path := files.Environments
	os.WriteFile(path, []byte("{oops"), 0o644)

	mainthread.Wait(func() {
		w := NewWindow(files.Files)
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
		w := NewWindow(files.Files)
		w.Show()
		tree := w.requests.tree

		// Adding a request with nothing open saves what is being composed,
		// creating a folder to hold it.
		w.request.url.SetText("{{host}}/users")
		w.requests.addRequest()
		if topCount(tree) != 1 || topFolder(tree, 0).ChildCount() != 1 {
			t.Fatal("folder and request not created")
		}
		list := w.requests.store.Folders[0].Requests[0]
		if w.open != list || list.URL != "{{host}}/users" || tabTitle(w) != "New Request" {
			t.Errorf("open = %+v, title %q", w.open, tabTitle(w))
		}

		// Renaming updates the title.
		topFolder(tree, 0).SetText(0, "Api")
		topFolder(tree, 0).Child(0).SetText(0, "Fetch")
		if list.Name != "Fetch" || tabTitle(w) != "Fetch" || w.requests.store.Folders[0].Name != "Api" {
			t.Errorf("rename: %q, title %q", list.Name, tabTitle(w))
		}

		// Adding with a request open starts from an empty request in the
		// same folder.
		w.requests.addRequest()
		create := w.requests.store.Folders[0].Requests[1]
		if w.open != create || w.request.url.Text() != "" || topFolder(tree, 0).ChildCount() != 2 {
			t.Errorf("second request: open %+v, url %q", w.open, w.request.url.Text())
		}
		topFolder(tree, 0).Child(1).SetText(0, "Post")

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
		tree.SetCurrentItem(topFolder(tree, 0).Child(0))
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
		if w.open != list || topCount(tree) != 2 {
			t.Errorf("open after adding a folder = %+v", w.open)
		}

		// Selecting a request closes the environment editor.
		w.environments.add()
		if w.stack.CurrentIndex() != pageEditor {
			t.Fatal("editor not open")
		}
		tree.SetCurrentItem(topFolder(tree, 0).Child(1))
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

	saved, err := loadRequests(files.Requests)
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
		w := NewWindow(files.Files)
		tree := w.requests.tree

		if topCount(tree) != 2 || topFolder(tree, 0).Text(0) != "Api" || topFolder(tree, 0).Child(1).Text(0) != "Post" {
			t.Fatal("tree not restored")
		}
		if w.open != nil || tabTitle(w) != untitled {
			t.Error("a request is open at startup")
		}

		tree.SetCurrentItem(topFolder(tree, 0).Child(1))
		if got := w.request.request(); !reflect.DeepEqual(got, want) {
			t.Errorf("loaded %+v, want %+v", got, want)
		}

		// A request is not removed unless the user agrees to it.
		var asked string
		w.requests.confirm = func(question string) bool { asked = question; return false }
		w.requests.remove()
		if folder := w.requests.store.Folders[0]; len(folder.Requests) != 2 || !strings.Contains(asked, "Remove the request") {
			t.Errorf("%d requests after being asked %q", len(folder.Requests), asked)
		}
		w.requests.confirm = func(string) bool { return true }

		// Removing the open request opens its neighbour.
		w.requests.remove()
		if folder := w.requests.store.Folders[0]; len(folder.Requests) != 1 || w.open != folder.Requests[0] || tabTitle(w) != "Fetch" {
			t.Errorf("after remove: open %+v", w.open)
		}

		// Removing a folder that has requests needs confirmation.
		tree.SetCurrentItem(topFolder(tree, 0))
		w.requests.confirm = func(string) bool { return false }
		w.requests.remove()
		if topCount(tree) != 2 {
			t.Error("folder removed without confirmation")
		}

		w.requests.confirm = func(string) bool { return true }
		w.requests.remove()
		if topCount(tree) != 1 || len(w.requests.store.Folders) != 1 {
			t.Error("folder not removed")
		}
		if w.open != nil || w.request.url.Text() != "" || tabTitle(w) != untitled {
			t.Errorf("removed request still open: %+v", w.open)
		}

		// An empty folder is removed without asking.
		w.requests.confirm = func(string) bool { t.Error("asked about an empty folder"); return false }
		tree.SetCurrentItem(topFolder(tree, 0))
		w.requests.remove()
		if len(w.requests.store.Folders) != 0 {
			t.Error("empty folder not removed")
		}
	})

	if saved, err := loadRequests(files.Requests); err != nil || len(saved.Folders) != 0 {
		t.Errorf("removals not saved: %+v, %v", saved, err)
	}
}

func TestRequestsUnreadableFile(t *testing.T) {
	files := testFiles(t)
	os.WriteFile(files.Requests, []byte("{oops"), 0o644)

	mainthread.Wait(func() {
		w := NewWindow(files.Files)
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
		w := NewWindow(files.Files)
		if w.Width() != 1024 || w.Height() != 768 {
			t.Errorf("default size = %dx%d", w.Width(), w.Height())
		}
		w.Show()
		w.Resize(1000, 650)
		w.Close()
	})

	if got, want := withoutLayout(settings.Load(files.Settings)), (settings.Settings{Width: 1000, Height: 650, ResponseFont: "Iosevka Term Curly", Workspace: workspace.Default, PublicKey: testPublic, PrivateKey: testPrivate}); !reflect.DeepEqual(got, want) {
		t.Errorf("saved %+v, want %+v", got, want)
	}

	mainthread.Wait(func() {
		w := NewWindow(files.Files)
		if w.Width() != 1000 || w.Height() != 650 {
			t.Errorf("restored size = %dx%d", w.Width(), w.Height())
		}
	})

	// A maximised window is restored maximised and keeps its normal size.
	settings.Settings{Width: 1000, Height: 650, Maximized: true, PublicKey: testPublic, PrivateKey: testPrivate}.Save(files.Settings)
	mainthread.Wait(func() {
		w := NewWindow(files.Files)
		w.Show()
		if !w.IsMaximized() {
			t.Error("window not maximised")
		}
		w.Close()
	})
	if got, want := withoutLayout(settings.Load(files.Settings)), (settings.Settings{Width: 1000, Height: 650, Maximized: true, ResponseFont: "Iosevka Term Curly", Workspace: workspace.Default, PublicKey: testPublic, PrivateKey: testPrivate}); !reflect.DeepEqual(got, want) {
		t.Errorf("saved %+v, want %+v", got, want)
	}
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

func TestRequestReopened(t *testing.T) {
	mainthread.Wait(func() {
		w := NewWindow(testFiles(t).Files)
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
		w := NewWindow(testFiles(t).Files)
		if w.sidebar.Count() != 3 || w.sidebar.TabToolTip(tabWorkspaces) != "Workspaces" || w.sidebar.TabToolTip(tabEnvironments) != "Environments" || w.sidebar.TabToolTip(tabRequests) != "Requests" {
			t.Errorf("tabs = %d: %q, %q, %q", w.sidebar.Count(), w.sidebar.TabToolTip(0), w.sidebar.TabToolTip(1), w.sidebar.TabToolTip(2))
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
		w := NewWindow(files.Files)
		w.Show()

		if w.about.Text() != "&About" {
			t.Errorf("about action = %q", w.about.Text())
		}

		// Quit closes the window, which saves the settings.
		w.Resize(1000, 600)
		w.quit.Trigger()
		if w.IsVisible() {
			t.Error("window still open after quit")
		}
	})

	if got := settings.Load(files.Settings); got.Width != 1000 || got.Height != 600 {
		t.Errorf("settings not saved on quit: %+v", got)
	}
}

func TestResponseFont(t *testing.T) {
	files := testFiles(t)
	settings.Settings{Width: 800, Height: 600, ResponseFont: "Some Font", PublicKey: testPublic, PrivateKey: testPrivate}.Save(files.Settings)

	mainthread.Wait(func() {
		w := NewWindow(files.Files)
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
	return strings.Contains(p.result.Text(), themeColour(colourError))
}

// itemType returns the type whose icon an item of the request tree has, or
// an empty type if it has the icon of a folder.
func itemType(item *qt.QTreeWidgetItem) collection.Type {
	icon := item.Icon(colName)
	if icon.IsNull() || icon.CacheKey() == folderIcon().CacheKey() {
		return ""
	}
	for _, known := range requestTypes {
		if requestTypeIcon(known.typ).CacheKey() == icon.CacheKey() {
			return known.typ
		}
	}
	return "unknown"
}

// topFolders returns the items of the top level folders in the order they
// are shown: those under each heading, then those on their own.
func topFolders(tree *qt.QTreeWidget) []*qt.QTreeWidgetItem {
	var items []*qt.QTreeWidgetItem
	for i := range tree.TopLevelItemCount() {
		item := tree.TopLevelItem(i)
		if _, ok := headingGroup(item); !ok {
			items = append(items, item)
			continue
		}
		for j := range item.ChildCount() {
			items = append(items, item.Child(j))
		}
	}
	return items
}

// topCount returns the number of top level folders in the tree.
func topCount(tree *qt.QTreeWidget) int {
	return len(topFolders(tree))
}

// topFolder returns the item of a top level folder, by its position among
// them as they are shown, or nil if there is none there.
func topFolder(tree *qt.QTreeWidget, index int) *qt.QTreeWidgetItem {
	if items := topFolders(tree); index < len(items) {
		return items[index]
	}
	return nil
}

// headingGroup reports which heading an item of the top level is, by its
// name, if it is one.
func headingGroup(item *qt.QTreeWidgetItem) (collection.Type, bool) {
	if item.Parent() != nil || !item.Font(colName).Bold() {
		return "", false
	}
	for group, label := range groupLabels {
		if item.Text(colName) == label {
			return group, true
		}
	}
	return "", false
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
		w = NewWindow(files.Files)
		w.Show()

		request = addGRPCRequest(w)
		if request == nil || request.Type != collection.TypeGRPC || request.Request != nil {
			t.Fatalf("request = %+v", request)
		}
		if w.stack.CurrentIndex() != pageGRPC || tabTitle(w) != "New Request" {
			t.Errorf("page = %d, title %q", w.stack.CurrentIndex(), tabTitle(w))
		}
		if got := itemType(topFolder(w.requests.tree, 0).Child(0)); got != collection.TypeGRPC {
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
		if e := w.grpc.endpoints; e.count() != 4 || !slices.Contains(e.names, "test.v1.Echo/Say") {
			t.Errorf("endpoints = %q", e.names)
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
		return fieldValue(w.grpcResponse, "Status") == "0 OK"
	})
	if got := mainthread.Wait2(func() string { return fieldValue(w.grpcResponse, "Protocol") }); got != "gRPC (proto)" {
		t.Errorf("protocol = %q", got)
	}

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

	waitFor(t, "the failure", func() bool { return fieldValue(w.grpcResponse, "Status") == "5 NOT_FOUND" })

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

	saved, err := loadRequests(files.Requests)
	if err != nil {
		t.Fatal(err)
	}
	if got := saved.Folders[0].Requests[0]; got.Type != collection.TypeGRPC || got.Request != nil || !reflect.DeepEqual(*got.GRPC, want) {
		t.Errorf("saved %+v %+v, want %+v", got, got.GRPC, want)
	}

	// A new window restores the request, and loads the methods when it is
	// opened without changing the one that was saved.
	mainthread.Wait(func() {
		w = NewWindow(files.Files)
		w.Show()

		tree := w.requests.tree
		tree.SetCurrentItem(topFolder(tree, 0).Child(0))
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
		w := NewWindow(testFiles(t).Files)
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
		if got := itemType(topFolder(tree, 0).Child(0)); got != collection.TypeREST {
			t.Errorf("type shown as %q", got)
		}
		if tree.ColumnCount() != 1 || itemType(topFolder(tree, 0)) != "" {
			t.Errorf("columns = %d, folder shown as %q", tree.ColumnCount(), itemType(topFolder(tree, 0)))
		}

		// Each type of request opens in its own UI, and leaves the other
		// request alone.
		tree.SetCurrentItem(topFolder(tree, 0).Child(0))
		if w.stack.CurrentIndex() != pageRequest || w.open != restRequest || w.request.url.Text() != "example.com" {
			t.Errorf("REST request not shown: page %d", w.stack.CurrentIndex())
		}
		w.request.url.SetText("changed.test")

		tree.SetCurrentItem(topFolder(tree, 0).Child(1))
		if w.stack.CurrentIndex() != pageGRPC || w.open != grpcRequest || w.grpc.url.Text() != "localhost:1" {
			t.Errorf("gRPC request not shown: page %d", w.stack.CurrentIndex())
		}
		if restRequest.URL != "changed.test" || grpcRequest.GRPC.URL != "localhost:1" || grpcRequest.Request != nil || restRequest.GRPC != nil {
			t.Errorf("requests = %+v, %+v", restRequest.Request, grpcRequest.GRPC)
		}

		// Renaming a gRPC request updates its title.
		topFolder(tree, 0).Child(1).SetText(colName, "Say")
		if grpcRequest.Name != "Say" || tabTitle(w) != "Say" {
			t.Errorf("name = %q, title = %q", grpcRequest.Name, tabTitle(w))
		}

		// Removing the open gRPC request returns to the REST one beside it.
		w.requests.confirm = func(string) bool { return true }
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

func TestGRPCDefinitionError(t *testing.T) {
	srv := rpctest.NewServer(rpctest.ReflectionNone)
	defer srv.Close()

	var w *Window
	mainthread.Wait(func() {
		w = NewWindow(testFiles(t).Files)
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
		w = NewWindow(testFiles(t).Files)
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
	waitFor(t, "the cancel", func() bool { return fieldValue(w.grpcResponse, "Status") == "1 CANCELED" })

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
		w := NewWindow(testFiles(t).Files)

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
		defaults := Files{Workspaces: "workspaces"}
		current := settings.Settings{Width: 900, Height: 650, ResponseFont: "A Font That Is Not Installed"}

		d := newSettingsDialog(nil, current, defaults)
		if d.workspaces.Text() != "workspaces" {
			t.Errorf("location shown as %q", d.workspaces.Text())
		}

		// Accepting without changing anything changes nothing, even though
		// the font is not one the selector could show.
		if got := d.settings(current); !reflect.DeepEqual(got, current) {
			t.Errorf("unchanged dialog gave %+v, want %+v", got, current)
		}

		d.workspaces.SetText("  /data/workspaces ")
		d.font.SetCurrentFont(qt.QFontDatabase_SystemFont(qt.QFontDatabase__FixedFont))

		got := d.settings(current)
		want := current
		want.WorkspacesDir = "/data/workspaces"
		want.ResponseFont = d.font.CurrentFont().Family()
		if !reflect.DeepEqual(got, want) || got.ResponseFont == current.ResponseFont {
			t.Errorf("got %+v, want %+v", got, want)
		}

		// A location put back to its default is not recorded.
		d.workspaces.SetText("workspaces")
		if got := d.settings(got); got.WorkspacesDir != "" {
			t.Errorf("default location recorded as %q", got.WorkspacesDir)
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
	moved := filepath.Join(dir, "moved")
	other := filepath.Join(dir, "other")

	// Another folder already has a workspace.
	elsewhere := &collection.Store{}
	elsewhere.AddFolder("Other").Add(collection.NewREST("Elsewhere", rest.Request{Method: "GET", URL: "other.test"}))
	if err := workspace.Create(other, "Theirs"); err != nil {
		t.Fatal(err)
	}
	if err := elsewhere.Save(workspace.Requests(other, "Theirs")); err != nil {
		t.Fatal(err)
	}

	mainthread.Wait(func() {
		w := NewWindow(files.Files)
		w.Show()

		w.environments.add()
		w.editor.name.SetText("Local")
		w.editor.variables.add("host", "example.com")
		w.request.url.SetText("{{host}}/users")
		w.requests.addRequest()

		s := w.config
		s.ResponseFont = "Some Font"
		s.WorkspacesDir = moved
		if err := w.applySettings(s); err != nil {
			t.Fatal(err)
		}

		if got := w.response.body.Font().Families()[0]; got != "Some Font" {
			t.Errorf("response font = %q", got)
		}
		if got := w.grpcResponse.body.Font().Families()[0]; got != "Some Font" {
			t.Errorf("gRPC response font = %q", got)
		}
		// The request body, the gRPC message and the curl command have it
		// too.
		for name, editor := range map[string]*qt.QPlainTextEdit{"body": w.request.bodyRaw, "message": w.grpc.message, "command": w.request.command} {
			if got := editor.Font().Families()[0]; got != "Some Font" {
				t.Errorf("%s font = %q", name, got)
			}
		}

		// The workspaces are copied to the new folder, as it had none, and
		// later changes go there.
		if w.workspaces.current() != workspace.Default || w.environments.list.Count() != 1 || w.environments.store.Environments[0].Name != "Local" {
			t.Fatalf("workspace %q, environments %+v", w.workspaces.current(), w.environments.store.Environments)
		}
		if w.environments.path != workspace.Environments(moved, workspace.Default) || w.requests.path != workspace.Requests(moved, workspace.Default) {
			t.Errorf("paths = %q, %q", w.environments.path, w.requests.path)
		}
		w.environments.edit()
		w.editor.variables.add("token", "abc")
	})

	copied, err := loadEnvs(workspace.Environments(moved, workspace.Default))
	if err != nil || len(copied.Environments) != 1 || len(copied.Environments[0].Variables) != 2 {
		t.Errorf("moved environments = %+v, %v", copied, err)
	}
	if old, _ := loadEnvs(files.Environments); len(old.Environments) != 1 || len(old.Environments[0].Variables) != 1 {
		t.Errorf("old environments file changed: %+v", old.Environments)
	}
	if old, _ := loadRequests(files.Requests); len(old.Folders) != 1 || old.Folders[0].Requests[0].URL != "{{host}}/users" {
		t.Errorf("old requests file changed: %+v", old.Folders)
	}
	if saved := settings.Load(files.Settings); saved.ResponseFont != "Some Font" || saved.WorkspacesDir != moved || saved.Workspace != workspace.Default {
		t.Errorf("saved settings = %+v", saved)
	}

	// The next run uses the new location.
	mainthread.Wait(func() {
		w := NewWindow(files.Files)
		if w.environments.list.Count() != 1 || len(w.environments.store.Environments[0].Variables) != 2 {
			t.Errorf("environments = %+v", w.environments.store.Environments)
		}

		// A folder that already has workspaces shows those, and nothing is
		// copied into it.
		s := w.config
		s.WorkspacesDir = other
		if err := w.applySettings(s); err != nil {
			t.Fatal(err)
		}
		if got := w.workspaces.names; !reflect.DeepEqual(got, []string{"Theirs"}) || w.workspaces.current() != "Theirs" {
			t.Errorf("workspaces = %q, current %q", got, w.workspaces.current())
		}
		tree := w.requests.tree
		if topCount(tree) != 1 || topFolder(tree, 0).Text(colName) != "Other" || w.environments.list.Count() != 0 {
			t.Error("the workspace of the other folder was not loaded")
		}

		// A location that cannot be used is refused and the setting left
		// alone.
		blocked := filepath.Join(dir, "blocked")
		os.WriteFile(blocked, []byte("a file, not a folder"), 0o644)
		s = w.config
		s.WorkspacesDir = blocked
		if err := w.applySettings(s); err == nil || !strings.Contains(err.Error(), "workspaces were not moved") {
			t.Errorf("err = %v", err)
		}
		if w.config.WorkspacesDir != other || w.workspaces.current() != "Theirs" || w.requests.path != workspace.Requests(other, "Theirs") {
			t.Errorf("settings changed: %+v", w.config)
		}

		// Going back to the default shows the original workspace.
		s = w.config
		s.WorkspacesDir = ""
		if err := w.applySettings(s); err != nil {
			t.Fatal(err)
		}
		if w.workspaces.current() != workspace.Default || len(w.environments.store.Environments[0].Variables) != 1 || topFolder(w.requests.tree, 0).Text(colName) != "New Folder" {
			t.Error("the default workspaces were not loaded")
		}
	})
}

func TestMenuShortcuts(t *testing.T) {
	mainthread.Wait(func() {
		w := NewWindow(testFiles(t).Files)
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
		w = NewWindow(testFiles(t).Files)
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
		return fieldValue(w.response, "Status") == "204 No Content"
	})

	// A request that fails is reported in the bar and in the response.
	srv.Close()
	mainthread.Wait(func() { w.request.send.Click() })
	waitFor(t, "the failure", func() bool { return fieldValue(w.response, "Error") != "" })

	mainthread.Wait(func() {
		if w.StatusBar().CurrentMessage() != "" || w.response.body.ToPlainText() != "" || !isFailure(w.response) || fieldValue(w.response, "Time") == "" || fieldValue(w.response, "Status") != "" {
			t.Errorf("response = %q, bar = %+v", w.response.body.ToPlainText(), w.response.fields)
		}
	})
}

func TestIcons(t *testing.T) {
	mainthread.Wait(func() {
		w := NewWindow(testFiles(t).Files)
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

func TestLayoutRemembered(t *testing.T) {
	files := testFiles(t)

	var w *Window
	mainthread.Wait(func() {
		w = NewWindow(files.Files)
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
		w = NewWindow(files.Files)
		w.Show()
	})
	waitFor(t, "the sidebar width", func() bool { return w.split.Sizes()[0] == 350 })
	waitFor(t, "the response height", func() bool { return w.splits[pageRequest].Sizes()[1] == 210 })

	// The other page with a response is given the same height when shown.
	mainthread.Wait(func() {
		tree := w.requests.tree
		tree.SetCurrentItem(topFolder(tree, 0).Child(0))
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
		w := NewWindow(files.Files)
		w.Show()

		// The requests are shown first.
		if w.sidebar.CurrentIndex() != tabRequests || w.sidebar.TabToolTip(tabRequests) != "Requests" {
			t.Errorf("sidebar tab = %d", w.sidebar.CurrentIndex())
		}

		// With nothing saved open no request is named, and there are no
		// environments to select.
		selector := w.environment.environment
		if tabTitle(w) != untitled || selector.Count() != 1 || selector.CurrentText() != "None" {
			t.Errorf("tab = %q, environment %q", tabTitle(w), selector.CurrentText())
		}
		// The selector is above the tabs, and in neither request UI.
		if w.request.root.IsAncestorOf(selector.QWidget) || w.grpc.root.IsAncestorOf(selector.QWidget) || w.stack.IsAncestorOf(selector.QWidget) || selector.IsHidden() {
			t.Error("the environment selector is inside the request UI")
		}

		w.environments.add()
		w.editor.name.SetText("Local")
		w.editor.variables.add("host", "local.test")
		w.environments.add()
		w.editor.name.SetText("Remote")
		w.editor.variables.add("host", "remote.test")
		w.editor.close.Click()
		w.requests.addRequest()

		if tabTitle(w) != "New Request" {
			t.Errorf("request = %q", tabTitle(w))
		}
		if selector.Count() != 3 || selector.ItemText(1) != "Local" || selector.CurrentText() != "Remote" {
			t.Errorf("environments = %d, selected %q", selector.Count(), selector.CurrentText())
		}

		// Selecting an environment in the header makes it the active one.
		w.request.url.SetText("{{host}}/users")
		selector.SetCurrentIndex(1)
		if w.environments.store.Active != 0 || w.environments.list.CurrentRow() != 0 || w.expand(w.request.url.Text()) != "local.test/users" {
			t.Errorf("active = %d, expanded = %q", w.environments.store.Active, w.expand(w.request.url.Text()))
		}

		// It is the same one when a gRPC request is shown.
		addGRPCRequest(w)
		if tabTitle(w) != "New Request" || selector.CurrentText() != "Local" || selector.IsHidden() {
			t.Errorf("tab = %q, environment %q", tabTitle(w), selector.CurrentText())
		}
		selector.SetCurrentIndex(2)
		if w.environments.store.Active != 1 || w.environments.list.CurrentRow() != 1 {
			t.Errorf("active = %d", w.environments.store.Active)
		}

		// It follows the sidebar, and a rename.
		w.environments.list.SetCurrentRow(0)
		if selector.CurrentText() != "Local" {
			t.Errorf("environment = %q", selector.CurrentText())
		}
		w.environments.edit()
		w.editor.name.SetText("Dev")
		w.editor.close.Click()
		if selector.CurrentText() != "Dev" || selector.ItemText(1) != "Dev" {
			t.Errorf("environment = %q", selector.CurrentText())
		}

		// None can be selected.
		selector.SetCurrentIndex(0)
		if w.environments.store.Active != -1 || w.environments.list.CurrentRow() != -1 || selector.CurrentText() != "None" {
			t.Errorf("active = %d", w.environments.store.Active)
		}
		selector.SetCurrentIndex(2)
	})

	// The selection made in the header is saved.
	if saved, err := loadEnvs(files.Environments); err != nil || saved.Active != 1 || saved.Environments[0].Name != "Dev" {
		t.Errorf("saved %+v, %v", saved, err)
	}
}

func TestWorkspaceSelector(t *testing.T) {
	files := testFiles(t)

	var w *Window
	mainthread.Wait(func() {
		w = NewWindow(files.Files)
		w.Show()
		w.workspaces.warn = func(message string) { t.Error(message) }
		selector := w.environment

		names := func(combo *qt.QComboBox) []string {
			var names []string
			for i := range combo.Count() {
				names = append(names, combo.ItemText(i))
			}
			return names
		}

		// The workspace in use is the one selected, before the environment
		// and on a line above the tabs.
		if !reflect.DeepEqual(names(selector.workspace), []string{"Default"}) || selector.workspace.CurrentText() != "Default" {
			t.Errorf("workspaces = %q, selected %q", names(selector.workspace), selector.workspace.CurrentText())
		}
		if selector.root.IsAncestorOf(w.tabs.bar.QWidget) || w.stack.IsAncestorOf(selector.workspace.QWidget) {
			t.Error("the selector is not apart from the tabs and the request UI")
		}

		// The titles are buttons. With no environment in use, that of the
		// environments shows them in the sidebar.
		selector.environmentTitle.Click()
		if w.sidebar.CurrentIndex() != tabEnvironments || w.stack.CurrentIndex() == pageEditor {
			t.Errorf("with no environment: sidebar tab %d, page %d", w.sidebar.CurrentIndex(), w.stack.CurrentIndex())
		}
		w.environments.add()
		w.editor.name.SetText("Local")
		w.closeEditor()

		// With one, it opens it in the editor.
		w.sidebar.SetCurrentIndex(tabRequests)
		selector.environmentTitle.Click()
		if w.stack.CurrentIndex() != pageEditor || w.editor.name.Text() != "Local" || w.sidebar.CurrentIndex() != tabEnvironments {
			t.Errorf("page = %d, editing %q", w.stack.CurrentIndex(), w.editor.name.Text())
		}
		w.closeEditor()

		// That of the workspaces shows them, and that of the sets shows
		// the sets.
		selector.workspaceTitle.Click()
		if w.sidebar.CurrentIndex() != tabWorkspaces {
			t.Errorf("sidebar tab %d after the workspace title", w.sidebar.CurrentIndex())
		}
		selector.setTitle.Click()
		if w.sidebar.CurrentIndex() != tabRequests || w.requests.views.CurrentIndex() != viewSets {
			t.Errorf("sidebar tab %d, view %d after the set title", w.sidebar.CurrentIndex(), w.requests.views.CurrentIndex())
		}

		// A workspace that is added is listed and selected.
		w.workspaces.add()
		if !reflect.DeepEqual(names(selector.workspace), []string{"Default", "New Workspace"}) || selector.workspace.CurrentText() != "New Workspace" {
			t.Errorf("workspaces = %q, selected %q", names(selector.workspace), selector.workspace.CurrentText())
		}
		if selector.environment.Count() != 1 {
			t.Errorf("the new workspace has %d environments listed", selector.environment.Count())
		}

		// Selecting another makes it the one in use, with its environments.
		selector.workspace.SetCurrentIndex(0)
		if w.workspaces.current() != "Default" || w.config.Workspace != "Default" || selector.workspace.CurrentText() != "Default" {
			t.Errorf("workspace = %q, selected %q", w.workspaces.current(), selector.workspace.CurrentText())
		}
		if selector.environment.CurrentText() != "Local" {
			t.Errorf("environment = %q", selector.environment.CurrentText())
		}

		// A workspace that is renamed is listed by its new name.
		w.workspaces.list.CurrentItem().SetText("Zeta")
	})

	waitFor(t, "the workspace to be renamed", func() bool {
		var ok bool
		mainthread.Wait(func() {
			s := w.environment.workspace
			ok = s.Count() == 2 && s.ItemText(0) == "New Workspace" && s.CurrentText() == "Zeta"
		})
		return ok
	})
}

func TestSelectorWidth(t *testing.T) {
	var w *Window
	var before int
	mainthread.Wait(func() {
		w = NewWindow(testFiles(t).Files)
		w.Resize(1000, 700)
		w.Show()
		before = w.MinimumSizeHint().Width()

		w.environments.add()
		w.editor.name.SetText(strings.Repeat("A very long name ", 40))
		w.closeEditor()
	})

	waitFor(t, "the layout", func() bool {
		return mainthread.Wait2(func() bool { return w.environment.environment.Width() > selectorMinWidth })
	})

	mainthread.Wait(func() {
		combo := w.environment.environment

		// The drop-down would be as wide as the name, and is as wide as
		// the window has room for. The window is not made any wider.
		if combo.SizeHint().Width() <= w.Width() {
			t.Errorf("the name is only %d wide", combo.SizeHint().Width())
		}
		if got := w.MinimumSizeHint().Width(); got != before {
			t.Errorf("least width of the window = %d, was %d", got, before)
		}
		if w.Width() != 1000 || combo.Width() >= w.Width() || w.environment.root.Width() > w.Width() {
			t.Errorf("window = %d wide, selector %d, drop-down %d", w.Width(), w.environment.root.Width(), combo.Width())
		}
		// What is after it is still on show.
		if set := w.environment.set; set.X()+set.Width() > w.environment.root.Width() {
			t.Errorf("the set selector ends at %d of %d", set.X()+set.Width(), w.environment.root.Width())
		}
	})
}

func TestGRPCRequestBarOrder(t *testing.T) {
	var w *Window
	mainthread.Wait(func() {
		w = NewWindow(testFiles(t).Files)
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

// formatColour returns the colour of a text format, as #rrggbb.
func formatColour(format *qt.QTextCharFormat) string {
	return format.Foreground().Color().Name()
}

func TestJSONColourSettings(t *testing.T) {
	files := testFiles(t)
	defaults := defaultJSONColours()

	mainthread.Wait(func() {
		w := NewWindow(files.Files)
		w.Show()

		// The colours are the defaults until others are chosen.
		if got := formatColour(w.response.formats[tokenKey]); got != defaults[tokenKey] {
			t.Errorf("key colour = %s, want %s", got, defaults[tokenKey])
		}

		d := newSettingsDialog(w.QWidget, w.config, files.Files)
		if got := d.json[tokenKey].button.Text(); got != defaults[tokenKey] {
			t.Errorf("swatch = %q", got)
		}

		d.json[tokenKey].set("#112233")
		d.json[tokenNumber].set("#AABBCC")
		// Choosing the default is not recorded, so it still follows the theme.
		d.json[tokenString].set(defaults[tokenString])
		if got := d.json[tokenKey].button.Text(); got != "#112233" || !strings.Contains(d.json[tokenKey].button.StyleSheet(), "background-color: #112233") {
			t.Errorf("swatch = %q, %q", got, d.json[tokenKey].button.StyleSheet())
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
		if d.settings(w.config).JSONColours != (settings.JSONColours{}) || d.json[tokenKey].button.Text() != defaults[tokenKey] {
			t.Errorf("after reset: %+v", d.settings(w.config).JSONColours)
		}
	})

	if got, want := settings.Load(files.Settings).JSONColours, (settings.JSONColours{Key: "#112233", Number: "#aabbcc"}); got != want {
		t.Errorf("saved %+v, want %+v", got, want)
	}

	// The colours are used from the start of the next run.
	mainthread.Wait(func() {
		w := NewWindow(files.Files)
		if got := formatColour(w.grpcResponse.formats[tokenKey]); got != "#112233" {
			t.Errorf("key colour = %s", got)
		}
		if got := formatColour(w.request.highlight.json[tokenLiteral]); got != defaults[tokenLiteral] {
			t.Errorf("literal colour = %s", got)
		}
	})
}

// iconColour returns the colour an icon is drawn on, as #rrggbb.
func iconColour(icon *qt.QIcon) string {
	// The edge of the icon is clear of its symbol. An icon is not made
	// larger than it was drawn, so the picture can be smaller than asked.
	image := icon.Pixmap2(64, 64).ToImage()
	return image.PixelColor(image.Width()/16, image.Height()/2).Name()
}

func TestColourSettings(t *testing.T) {
	files := testFiles(t)

	mainthread.Wait(func() {
		w := NewWindow(files.Files)
		w.Show()
		w.requests.addFolder()
		w.requests.addRequest()
		addGRPCRequest(w)
		w.tabs.toggle(w.tabs.current())

		folder := func() *qt.QTreeWidgetItem { return topFolder(w.requests.tree, 0) }
		shown := func() map[string]string {
			return map[string]string{
				"workspaces tab":   iconColour(w.sidebar.TabIcon(tabWorkspaces)),
				"workspace":        iconColour(w.workspaces.list.Item(0).Icon()),
				"environments tab": iconColour(w.sidebar.TabIcon(tabEnvironments)),
				"requests tab":     iconColour(w.sidebar.TabIcon(tabRequests)),
				"folder":           iconColour(folder().Icon(colName)),
				"first request":    iconColour(folder().Child(0).Icon(colName)),
				"second request":   iconColour(folder().Child(1).Icon(colName)),
				"pinned tab":       iconColour(w.tabs.current().button.Icon()),
				"curl":             iconColour(w.request.curl.Icon()),
				"save":             iconColour(w.response.save.Icon()),
				"gRPC save":        iconColour(w.grpcResponse.save.Icon()),
			}
		}
		// The two requests are a REST and a gRPC one, in the order of their
		// names.
		types := map[collection.Type]colourKind{collection.TypeREST: colourREST, collection.TypeGRPC: colourGRPC}
		first, second := types[itemType(folder().Child(0))], types[itemType(folder().Child(1))]
		check := func(when string, colour func(colourKind) string) {
			t.Helper()
			want := map[string]string{
				"workspaces tab":   colour(colourWorkspaces),
				"workspace":        colour(colourWorkspaces),
				"environments tab": colour(colourEnvironments),
				"requests tab":     colour(colourRequests),
				"folder":           colour(colourRequests),
				"first request":    colour(first),
				"second request":   colour(second),
				"pinned tab":       colour(colourGRPC),
				"curl":             colour(colourAction),
				"save":             colour(colourAction),
				"gRPC save":        colour(colourAction),
			}
			for what, got := range shown() {
				if got != want[what] {
					t.Errorf("%s: %s is %s, want %s", when, what, got, want[what])
				}
			}
		}

		// The colours are the defaults until others are chosen.
		check("at first", defaultColour)

		// A placeholder that is not defined, in the URL of the REST panel,
		// which is filled without it being taken for an edit of the gRPC
		// request that is open.
		w.request.updating = true
		w.request.url.SetText("{{host}}/path")
		w.request.updating = false
		// An error, in the bar beneath the response and in the gRPC panel.
		w.response.setFields(field{label: fieldError, value: "it failed", failure: true})
		placeholders := func() [3]string {
			h := w.request.highlight
			return [3]string{formatColour(h.known), formatColour(h.unknown), w.grpc.status.StyleSheet()}
		}
		if got, want := placeholders(), [3]string{defaultColour(colourFound), defaultColour(colourNotFound), "color: " + defaultColour(colourError)}; got != want {
			t.Errorf("placeholders are %v, want %v", got, want)
		}

		d := newSettingsDialog(w.QWidget, w.config, files.Files)
		if d.tabs.Count() != 3 || d.tabs.TabText(settingsTabRequests) != "Requests" || d.tabs.TabText(settingsTabWorkspaces) != "Workspaces" || d.tabs.TabText(settingsTabInterface) != "User interface" {
			t.Errorf("the dialog has %d tabs", d.tabs.Count())
		}
		custom := map[colourKind]string{
			colourWorkspaces:   "#110000",
			colourEnvironments: "#220000",
			colourRequests:     "#330000",
			colourREST:         "#440000",
			colourGRPC:         "#550000",
			colourRemove:       "#660000",
			colourAction:       "#770000",
			colourSend:         "#aa0000",
			colourFound:        "#880000",
			colourNotFound:     "#990000",
			colourError:        "#bb0000",
			colourCaptured:     "#cc0000",
			colourNotCaptured:  "#dd0000",
		}
		for kind, colour := range custom {
			d.swatches[kind].set(colour)
		}
		if got := d.swatches[colourAction].button.Text(); got != "#770000" {
			t.Errorf("swatch = %q", got)
		}

		selected := w.requests.selection()
		chosen := d.settings(w.config)
		if err := w.applySettings(chosen); err != nil {
			t.Error(err)
		}

		// Every icon takes its colour at once, and nothing else changes.
		check("once chosen", func(kind colourKind) string { return custom[kind] })
		if w.requests.selection() != selected {
			t.Error("the selection changed")
		}
		w.tabs.hover(w.tabs.current())
		if got := iconColour(w.tabs.current().button.Icon()); got != "#660000" {
			t.Errorf("unpin is %s", got)
		}
		w.tabs.hover(nil)
		for name, button := range map[string]*qt.QPushButton{"Send": w.request.send, "Execute": w.request.execute, "Invoke": w.grpc.invoke} {
			if !strings.Contains(button.StyleSheet(), "background-color: #aa0000;") {
				t.Errorf("%s is styled %q", name, button.StyleSheet())
			}
		}
		// A tab that is not pinned has the pin, which is an action button.
		w.tabs.toggle(w.tabs.current())
		if got := iconColour(w.tabs.current().button.Icon()); got != "#770000" {
			t.Errorf("pin is %s", got)
		}
		if got, want := placeholders(), [3]string{"#880000", "#990000", "color: #bb0000"}; got != want {
			t.Errorf("placeholders are %v, want %v", got, want)
		}
		if !strings.Contains(w.response.result.Text(), `color: #bb0000">it failed`) {
			t.Errorf("the error is shown as %q", w.response.result.Text())
		}
		if got := formatColour(w.grpc.highlight.known); got != "#880000" {
			t.Errorf("gRPC placeholders are %s", got)
		}

		// Choosing the default is not recorded.
		d.swatches[colourAction].set(defaultColour(colourAction))
		if got := d.settings(w.config).Colours.Action; got != "" {
			t.Errorf("the default was recorded as %q", got)
		}
		d.resetColours()
		if d.settings(w.config).Colours != (settings.Colours{}) {
			t.Errorf("after reset: %+v", d.settings(w.config).Colours)
		}
	})

	if got := settings.Load(files.Settings).Colours; got.Workspaces != "#110000" || got.Action != "#770000" {
		t.Errorf("saved %+v", got)
	}

	// The colours are used from the start of the next run, and the defaults
	// again once they are given up.
	mainthread.Wait(func() {
		w := NewWindow(files.Files)
		if got := iconColour(w.request.curl.Icon()); got != "#770000" {
			t.Errorf("curl is %s", got)
		}
		if got := iconColour(w.sidebar.TabIcon(tabRequests)); got != "#330000" {
			t.Errorf("requests tab is %s", got)
		}

		s := w.config
		s.Colours = settings.Colours{}
		if err := w.applySettings(s); err != nil {
			t.Error(err)
		}
		if got := iconColour(w.request.curl.Icon()); got != defaultColour(colourAction) {
			t.Errorf("curl is %s after the defaults", got)
		}
	})
}

// pressEscape presses the Escape key in a widget. It must be called on the
// main thread.
func pressEscape(widget *qt.QWidget) {
	event := qt.NewQKeyEvent(qt.QEvent__KeyPress, int(qt.Key_Escape), qt.NoModifier)
	defer event.Delete()
	qt.QCoreApplication_SendEvent(widget.QObject, event.QEvent)
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

// captureMarks returns how capturing is shown to have gone on a response
// panel: "tick", "cross" or "" for the tab, and the same for each row of the
// table by its key, with the tooltips of the rows.
func captureMarks(p *responsePanel) (tab string, rows, tips map[string]string) {
	name := func(icon *qt.QIcon) string {
		switch {
		case icon.IsNull():
			return ""
		case icon.CacheKey() == captureIcon(true).CacheKey():
			return "tick"
		case icon.CacheKey() == captureIcon(false).CacheKey():
			return "cross"
		}
		return "unknown"
	}

	rows, tips = map[string]string{}, map[string]string{}
	table := p.captures.table
	for row := range table.RowCount() - 1 {
		key := table.Item(row, colKey).Text()
		rows[key] = name(table.Item(row, colStatus).Icon())
		tips[key] = table.Item(row, colStatus).ToolTip()
	}
	return name(p.tabs.TabIcon(tabCapture)), rows, tips
}

// variables returns the variables of the active environment by name.
func variables(w *Window) map[string]string {
	values := map[string]string{}
	if e, ok := w.environments.store.Current(); ok {
		for _, v := range e.Variables {
			if v.Enabled {
				values[v.Key] = v.Value
			}
		}
	}
	return values
}

func TestCapture(t *testing.T) {
	status := http.StatusOK
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		io.WriteString(w, `{"data": {"token": "abc123", "items": [{"id": 7}, {"id": 8}]}}`)
	}))
	defer srv.Close()

	files := testFiles(t)
	store := env.Store{Environments: []env.Environment{
		{Name: "Local", Variables: []env.Variable{{Key: "host", Value: srv.URL, Enabled: true}, {Key: "token", Value: "old", Enabled: false}}},
	}}
	if err := store.Save(files.Environments); err != nil {
		t.Fatal(err)
	}

	var w *Window
	var request *collection.Request
	mainthread.Wait(func() {
		w = NewWindow(files.Files)
		w.Show()
		w.requests.addFolder()
		w.requests.addRequest()
		request = w.open

		// The captures are set on a tab of the response.
		for name, tabs := range map[string]*qt.QTabWidget{"REST": w.response.tabs, "gRPC": w.grpcResponse.tabs} {
			if tabs.Count() != 3 || tabs.TabText(2) != "Capture" {
				t.Errorf("%s response has %d tabs", name, tabs.Count())
			}
		}
		if w.response.tabs.Widget(2).IsAncestorOf(w.request.captures.table.QWidget) == false {
			t.Error("the captures are not on the tab")
		}

		w.request.url.SetText("{{host}}/login")
		w.request.captures.add("token", "data.token")
		w.request.captures.add("last", "data.items[-1].id")
		w.request.captures.add("missing", "data.nothing")
		w.request.captures.add("unused", "data.token")
		rules := captureRules(w.request.captures)
		for i := range rules {
			rules[i].Enabled = rules[i].Variable != "unused"
		}
		// A regex picks out part of the value: what it matches, or what
		// its first group does.
		rules = append(rules,
			capture.Rule{Variable: "tokenDigits", Path: "data.token", Regex: `\d+`, Enabled: true},
			capture.Rule{Variable: "tokenLetters", Path: "data.token", Regex: `^([a-z]+)\d`, Enabled: true},
		)
		setCaptureRules(w.request.captures, rules)
		w.request.captures.onChange()

		// The regex has a column of its own, after the path.
		if table := w.request.captures.table; table.IsColumnHidden(colDescription) || table.Item(3, colDescription).Text() != `\d+` || !strings.HasPrefix(table.HorizontalHeaderItem(colDescription).Text(), "Regex") {
			t.Errorf("the regex of row 3 is %q", table.Item(3, colDescription).Text())
		}

		// The captures are part of the request, and are saved with it.
		want := []capture.Rule{
			{Variable: "last", Path: "data.items[-1].id", Enabled: true},
			{Variable: "missing", Path: "data.nothing", Enabled: true},
			{Variable: "token", Path: "data.token", Enabled: true},
			{Variable: "tokenDigits", Path: "data.token", Regex: `\d+`, Enabled: true},
			{Variable: "tokenLetters", Path: "data.token", Regex: `^([a-z]+)\d`, Enabled: true},
			{Variable: "unused", Path: "data.token"},
		}
		if !slices.Equal(request.Request.Captures, want) {
			t.Errorf("captures = %+v", request.Request.Captures)
		}
		if saved, err := loadRequests(files.Requests); err != nil || !slices.Equal(saved.Folders[0].Requests[0].Request.Captures, want) {
			t.Errorf("saved %+v, %v", saved, err)
		}

		w.request.send.Click()
	})
	waitFor(t, "the response", func() bool { return strings.Contains(w.response.result.Text(), "200 OK") })

	mainthread.Wait(func() {
		// The variables are set in the active environment: one that was
		// there is given the value and enabled, and the others are added.
		got := variables(w)
		if got["token"] != "abc123" || got["last"] != "8" || got["tokenDigits"] != "123" || got["tokenLetters"] != "abc" || len(got) != 5 {
			t.Errorf("variables = %v", got)
		}
		if saved, err := loadEnvs(files.Environments); err != nil || !slices.Equal(saved.Environments[0].Variables, w.environments.store.Environments[0].Variables) {
			t.Errorf("saved %+v, %v", saved, err)
		}

		// Each capture is marked with how it went, and the tab with a
		// cross as one of them failed. Nothing is said of it in the bar.
		tab, rows, tips := captureMarks(w.response)
		if want := map[string]string{"last": "tick", "missing": "cross", "token": "tick", "tokenDigits": "tick", "tokenLetters": "tick", "unused": ""}; tab != "cross" || !maps.Equal(rows, want) {
			t.Errorf("tab %q, rows %v", tab, rows)
		}
		if tips["token"] != "Captured" || tips["missing"] != `Not captured: there is no "nothing" in the response` || tips["unused"] != "" {
			t.Errorf("tips = %v", tips)
		}
		if !strings.Contains(w.response.tabs.TabToolTip(tabCapture), `missing: there is no "nothing"`) {
			t.Errorf("tab tip = %q", w.response.tabs.TabToolTip(tabCapture))
		}
		if strings.Contains(w.response.result.Text()+w.response.details.Text(), "aptured") || isFailure(w.response) {
			t.Errorf("bar = %q, %q", w.response.result.Text(), w.response.details.Text())
		}

		// The marks follow the colours of the settings.
		s := w.config
		s.Colours.Captured, s.Colours.NotCaptured = "#001100", "#110000"
		if err := w.applySettings(s); err != nil {
			t.Error(err)
		}
		if got := iconColour(w.response.tabs.TabIcon(tabCapture)); got != "#110000" {
			t.Errorf("the cross is %s", got)
		}
		if got := iconColour(w.request.captures.table.Item(0, colStatus).Icon()); got != "#001100" {
			t.Errorf("the tick is %s", got)
		}

		// Changing the captures takes the marks away, as they are of the
		// captures as they were.
		w.request.captures.remove(1)
		if tab, rows, _ := captureMarks(w.response); tab != "" || rows["token"] != "" || rows["last"] != "" {
			t.Errorf("after a change: tab %q, rows %v", tab, rows)
		}
		w.request.send.Click()
	})
	waitFor(t, "the marks", func() bool { tab, _, _ := captureMarks(w.response); return tab != "" })

	mainthread.Wait(func() {
		// With every value captured the tab has a tick.
		if tab, rows, _ := captureMarks(w.response); tab != "tick" || rows["token"] != "tick" || rows["last"] != "tick" {
			t.Errorf("tab %q, rows %v", tab, rows)
		}

		// A failed response is not captured from.
		status = http.StatusUnauthorized
		w.environments.store.Environments[0].Set("token", "kept")
		w.request.send.Click()
	})
	waitFor(t, "the failure", func() bool { return strings.Contains(w.response.result.Text(), "401") })

	mainthread.Wait(func() {
		if got := variables(w); got["token"] != "kept" {
			t.Errorf("variables = %v", got)
		}
		if tab, rows, _ := captureMarks(w.response); tab != "" || rows["token"] != "" {
			t.Errorf("after a failure: tab %q, rows %v", tab, rows)
		}

		// With no environment selected there is nowhere to keep a value.
		status = http.StatusOK
		w.request.url.SetText(srv.URL)
		w.environments.confirm = func(string) bool { return true }
		w.environments.remove()
		w.request.send.Click()
	})
	waitFor(t, "the response", func() bool { return strings.Contains(w.response.result.Text(), "200 OK") })

	mainthread.Wait(func() {
		tab, rows, tips := captureMarks(w.response)
		if tab != "cross" || rows["token"] != "cross" || tips["token"] != "Not captured: no environment is selected to keep it in" {
			t.Errorf("tab %q, rows %v, tips %v", tab, rows, tips)
		}
	})
}

func TestCaptureGRPC(t *testing.T) {
	srv := rpctest.NewServer(rpctest.ReflectionAll)
	defer srv.Close()

	files := testFiles(t)
	store := env.Store{Environments: []env.Environment{{Name: "Local"}}}
	if err := store.Save(files.Environments); err != nil {
		t.Fatal(err)
	}

	var w *Window
	var request *collection.Request
	mainthread.Wait(func() {
		w = NewWindow(files.Files)
		w.Show()
		w.requests.addFolder()
		request = addGRPCRequest(w)

		w.grpc.url.SetText(srv.URL)
		w.grpc.targetChanged()
	})
	waitFor(t, "the methods", func() bool { return w.grpc.method.count() == 4 })

	mainthread.Wait(func() {
		w.grpc.method.selectMethod("test.v1.Echo/Repeat")
		w.grpc.message.SetPlainText(`{"text": "hello", "count": 3}`)
		w.grpc.captures.add("said", "text")
		w.grpc.captures.add("index", "index")
		if len(request.GRPC.Captures) != 2 {
			t.Errorf("captures = %+v", request.GRPC.Captures)
		}

		w.grpc.invoke.Click()
	})
	waitFor(t, "the response", func() bool { return strings.Contains(w.grpcResponse.result.Text(), "0 OK") })

	mainthread.Wait(func() {
		// The values are those of the last message.
		if got := variables(w); got["said"] != "hello" || got["index"] != "2" {
			t.Errorf("variables = %v", got)
		}
		if tab, rows, _ := captureMarks(w.grpcResponse); tab != "tick" || rows["said"] != "tick" || rows["index"] != "tick" {
			t.Errorf("tab %q, rows %v", tab, rows)
		}

		// A call that fails is not captured from.
		w.environments.store.Environments[0].Set("said", "kept")
		w.grpc.method.selectMethod("test.v1.Echo/Fail")
		w.grpc.invoke.Click()
	})
	waitFor(t, "the failure", func() bool { return strings.Contains(w.grpcResponse.result.Text(), "NOT_FOUND") })

	mainthread.Wait(func() {
		if got := variables(w); got["said"] != "kept" {
			t.Errorf("variables = %v", got)
		}
		if tab, rows, _ := captureMarks(w.grpcResponse); tab != "" || rows["said"] != "" {
			t.Errorf("after a failure: tab %q, rows %v", tab, rows)
		}
	})
}

func TestVariableDescriptions(t *testing.T) {
	files := testFiles(t)
	store := env.Store{Environments: []env.Environment{{Name: "Local", Variables: []env.Variable{
		{Key: "host", Value: "https://staging.test", Description: "Staging"},
		{Key: "token", Value: "abc", Enabled: true},
		{Key: "host", Value: "http://local.test", Enabled: true, Description: "local machine"},
		{Key: "Host", Value: "https://live.test", Description: "Live"},
	}}}}
	if err := store.Save(files.Environments); err != nil {
		t.Fatal(err)
	}

	var w *Window
	mainthread.Wait(func() {
		w = NewWindow(files.Files)
		w.Show()
		w.environments.edit()
		table := w.editor.variables

		// The descriptions are in a column after the values, in text that
		// is half transparent.
		if table.table.IsColumnHidden(colDescription) || colDescription != colValue+1 || table.table.HorizontalHeaderItem(colDescription).Text() != "Description" {
			t.Error("the descriptions are not shown after the values")
		}
		if alpha := table.table.Item(0, colDescription).Foreground().Color().Alpha(); alpha != 127 {
			t.Errorf("alpha = %d", alpha)
		}
		// The other tables have none.
		if !w.request.params.table.IsColumnHidden(colDescription) {
			t.Error("the params have descriptions")
		}

		// The variables are in order of their names, and those with one
		// name in order of their descriptions, both ignoring case.
		descriptions := func() []string {
			var got []string
			for _, p := range table.allDescribed() {
				got = append(got, p.Key+"/"+p.Description)
			}
			return got
		}
		if got, want := descriptions(), []string{"Host/Live", "host/local machine", "host/Staging", "token/"}; !slices.Equal(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}

		// A description that is edited is saved, and the rows put in order.
		table.table.Item(0, colDescription).SetText("Zurich")
		table.sort()
		if got, want := descriptions(), []string{"host/local machine", "host/Staging", "Host/Zurich", "token/"}; !slices.Equal(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})

	saved, err := loadEnvs(files.Environments)
	if err != nil {
		t.Fatal(err)
	}
	want := []env.Variable{
		{Key: "host", Value: "http://local.test", Enabled: true, Description: "local machine"},
		{Key: "host", Value: "https://staging.test", Description: "Staging"},
		{Key: "Host", Value: "https://live.test", Description: "Zurich"},
		{Key: "token", Value: "abc", Enabled: true},
	}
	if !slices.Equal(saved.Environments[0].Variables, want) {
		t.Errorf("saved %+v", saved.Environments[0].Variables)
	}
}

func TestOneVariableOfAName(t *testing.T) {
	files := testFiles(t)
	store := env.Store{Environments: []env.Environment{{Name: "Local", Variables: []env.Variable{
		{Key: "host", Value: "http://local.test", Enabled: true, Description: "a local"},
		{Key: "host", Value: "https://live.test", Description: "b live"},
		{Key: "host", Value: "https://staging.test", Description: "c staging"},
		{Key: "token", Value: "abc", Enabled: true},
	}}}}
	if err := store.Save(files.Environments); err != nil {
		t.Fatal(err)
	}

	mainthread.Wait(func() {
		w := NewWindow(files.Files)
		w.Show()
		w.environments.edit()
		table := w.editor.variables

		enabled := func() []bool {
			var got []bool
			for _, p := range table.allDescribed() {
				got = append(got, p.Enabled)
			}
			return got
		}
		check := func(row int, state qt.CheckState) { table.table.Item(row, colEnabled).SetCheckState(state) }

		// Switching on one of the variables with a name switches off the
		// one that was on, and no others.
		check(2, qt.Checked)
		if got, want := enabled(), []bool{false, false, true, true}; !slices.Equal(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
		if got := w.expand("{{host}} {{token}}"); got != "https://staging.test abc" {
			t.Errorf("expanded to %q", got)
		}
		check(1, qt.Checked)
		if got, want := enabled(), []bool{false, true, false, true}; !slices.Equal(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}

		// They can all be off.
		check(1, qt.Unchecked)
		if got, want := enabled(), []bool{false, false, false, true}; !slices.Equal(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}

		// A variable given the name of one that is on is switched off, and
		// the one in use stays so.
		table.add("other", "xyz")
		if got, want := enabled(), []bool{false, false, false, true, true}; !slices.Equal(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
		table.table.Item(3, colKey).SetText("token")
		if got, want := enabled(), []bool{false, false, false, false, true}; !slices.Equal(got, want) || w.expand("{{token}}") != "abc" {
			t.Errorf("got %v, want %v, token %q", got, want, w.expand("{{token}}"))
		}

		// What is saved is what is shown.
		saved, err := loadEnvs(files.Environments)
		if err != nil || len(saved.Environments[0].Variables) != 5 {
			t.Errorf("saved %+v, %v", saved, err)
			return
		}
		for i, v := range saved.Environments[0].Variables {
			if v.Enabled != enabled()[i] {
				t.Errorf("saved %+v", saved.Environments[0].Variables)
			}
		}

		// The other tables allow it, as a header or a param can be repeated.
		w.request.headers.add("Accept", "a")
		w.request.headers.add("Accept", "b")
		if items := w.request.headers.items(); len(items) != 2 || !items[0].Enabled || !items[1].Enabled {
			t.Errorf("headers = %+v", items)
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
			for i := range topCount(tree) {
				if topFolder(tree, i).Text(colName) == name {
					found = topFolder(tree, i)
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
		w := NewWindow(files.Files)
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
		if topCount(tree) != 1 || service.ChildCount() != 1 || !service.IsExpanded() || treeItem(tree, "em-ohip-hotel", "ohip_hotel") == nil {
			t.Fatalf("subfolder not added: %d top level, %d children", topCount(tree), service.ChildCount())
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
		if topCount(tree) != 2 || len(rt.store.Folders) != 2 {
			t.Errorf("top level folders = %d", topCount(tree))
		}

		// Renaming a folder inside another renames the right one.
		if folders[0].Folders[0].Name != "ohip_hotel" || folders[0].Folders[0].Folders[0].Name != "admin" {
			t.Errorf("names = %q, %q", folders[0].Folders[0].Name, folders[0].Folders[0].Folders[0].Name)
		}
	})

	saved, err := loadRequests(files.Requests)
	if err != nil {
		t.Fatal(err)
	}
	if got := saved.Folders[0].Folders[0]; got.Name != "ohip_hotel" || got.Requests[0].Name != "GetHotel" || got.Folders[0].Requests[0].Name != "Report" {
		t.Fatalf("saved %+v", got)
	}

	// A new window restores the whole tree.
	mainthread.Wait(func() {
		w := NewWindow(files.Files)
		w.Show()
		rt := w.requests
		tree := rt.tree

		if topCount(tree) != 2 || treeItem(tree, "em-ohip-hotel", "ohip_hotel", "admin", "Report") == nil || treeItem(tree, "em-ohip-hotel", "ohip_hotel", "GetHotel") == nil {
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

	if saved, err := loadRequests(files.Requests); err != nil || len(saved.Folders) != 2 || len(saved.Folders[0].Folders) != 0 {
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
		w := NewWindow(files.Files)
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
		for _, f := range rt.store.Folders {
			top = append(top, f.Name)
		}
		if want := []string{"other", "rest", "service"}; !reflect.DeepEqual(top, want) {
			t.Errorf("top level = %q, want %q", top, want)
		}

		// A folder dropped beside a request goes with the folders there.
		rt.drop(restFolder, nil, rpcFolder, get, dropBelow)
		check("rpc", treeItem(tree, "other", "rpc"), "rest/", "Get", "Health")
	})

	saved, err := loadRequests(files.Requests)
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
		w := NewWindow(files.Files)
		w.Show()
		rt := w.requests
		tree := rt.tree

		var warned []string
		rt.warn = func(message string) { warned = append(warned, message) }

		// There is something already, which is left closed.
		rt.addRequest()
		topFolder(tree, 0).SetExpanded(false)

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
		if topCount(tree) != 1 || len(warned) != 0 {
			t.Fatalf("imported without a file: %d folders, warned %q", topCount(tree), warned)
		}

		rt.chooseFile = func(importSource) (string, bool) { return spec, true }
		rt.importDocument()
		if len(warned) != 0 || topCount(tree) != 2 {
			t.Fatalf("not imported: %d folders, warned %q", topCount(tree), warned)
		}

		// The folder is added in its place in the order, open and selected.
		shop := topFolder(tree, 1)
		if shop.Text(colName) != "Shop" || !shop.IsExpanded() || topFolder(tree, 0).IsExpanded() || tree.CurrentItem().Text(colName) != "Shop" {
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
		if len(warned) != 2 || !strings.Contains(warned[0], "bad.yaml could not be imported") || !strings.Contains(warned[0], "not an OpenAPI document") || topCount(tree) != 2 {
			t.Errorf("warned %q, %d folders", warned, topCount(tree))
		}
	})

	saved, err := loadRequests(files.Requests)
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
		w = NewWindow(files.Files)
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
		if w.environments.list.CurrentRow() != 2 || w.environments.list.Item(2).Text() != "zulu" || w.environment.environment.CurrentText() != "zulu" {
			t.Errorf("after rename: row %d, header %q", w.environments.list.CurrentRow(), w.environment.environment.CurrentText())
		}
		w.editor.close.Click()

		// Folders and requests are added in their place in the order.
		rt := w.requests
		rt.addFolder()
		renameCurrent(rt, "zebra")
		rt.deselect()
		rt.addFolder()
		renameCurrent(rt, "Apple")
		if topFolder(rt.tree, 0).Text(colName) != "Apple" || topFolder(rt.tree, 1).Text(colName) != "zebra" {
			t.Errorf("folders = %q, %q", topFolder(rt.tree, 0).Text(colName), topFolder(rt.tree, 1).Text(colName))
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
	envs, _ := loadEnvs(files.Environments)
	if len(envs.Environments) != 3 || envs.Environments[0].Name != "Local" || envs.Environments[2].Name != "zulu" || envs.Active != 2 {
		t.Errorf("saved environments = %+v", envs)
	}
	requests, _ := loadRequests(files.Requests)
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
		w := NewWindow(files.Files)
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
		if len(warned) != 0 || topCount(tree) != 1 {
			t.Fatalf("not imported: %d folders, warned %q", topCount(tree), warned)
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
		if len(warned) != 1 || !strings.Contains(warned[0], "not a Postman collection") || topCount(tree) != 1 {
			t.Errorf("warned %q", warned)
		}
	})
}

func TestImportDialog(t *testing.T) {
	mainthread.Wait(func() {
		w := NewWindow(testFiles(t).Files)

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

func TestFoldersStartClosed(t *testing.T) {
	files := testFiles(t)

	// A file from when whether folders were open was saved.
	old := `{"folders":[{"name":"a","folders":[{"name":"inner","requests":[{"name":"r","method":"GET"}]}]},{"name":"b","collapsed":true}]}`
	if err := os.WriteFile(files.Requests, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}

	for run := range 2 {
		mainthread.Wait(func() {
			w := NewWindow(files.Files)
			w.Show()
			tree := w.requests.tree

			// Every folder is closed at startup, however it was left.
			if treeItem(tree, "a").IsExpanded() || treeItem(tree, "a", "inner").IsExpanded() || treeItem(tree, "b").IsExpanded() {
				t.Errorf("run %d: folders do not start closed", run)
			}

			// Folders opened stay open while the tree is drawn again.
			treeItem(tree, "a").SetExpanded(true)
			w.requests.draw(nil)
			if !treeItem(tree, "a").IsExpanded() || treeItem(tree, "a", "inner").IsExpanded() {
				t.Errorf("run %d: folders not kept open within the run", run)
			}

			// Adding to a closed folder opens it.
			tree.SetCurrentItem(treeItem(tree, "a", "inner"))
			w.requests.addRequest()
			if !treeItem(tree, "a", "inner").IsExpanded() {
				t.Errorf("run %d: folder not opened for the request added to it", run)
			}
			w.Close()
		})
	}

	// Whether a folder is open is never saved.
	if data, err := os.ReadFile(files.Requests); err != nil {
		t.Fatal(err)
	} else if saved, err := loadRequests(files.Requests); err != nil || len(saved.Folders) != 2 || strings.Contains(string(data), "collapsed") {
		t.Errorf("saved %+v, %v", saved, err)
	}
}

func TestOpenedForTab(t *testing.T) {
	files := testFiles(t)
	store := &collection.Store{}
	store.AddFolder("a").AddFolder("inner").Add(collection.NewREST("r", rest.Request{Method: "GET"}))
	store.AddFolder("b").Add(collection.NewREST("s", rest.Request{Method: "GET"}))
	if err := store.Save(files.Requests); err != nil {
		t.Fatal(err)
	}

	mainthread.Wait(func() {
		w := NewWindow(files.Files)
		w.Show()
		tree := w.requests.tree

		// Choosing the tab of a request in closed folders opens them, to
		// show it selected.
		treeItem(tree, "a").SetExpanded(true)
		treeItem(tree, "a", "inner").SetExpanded(true)
		tree.SetCurrentItem(treeItem(tree, "a", "inner", "r"))
		w.tabs.current().button.Click()
		treeItem(tree, "a", "inner").SetExpanded(false)
		treeItem(tree, "a").SetExpanded(false)
		treeItem(tree, "b").SetExpanded(true)
		tree.SetCurrentItem(treeItem(tree, "b", "s"))
		w.tabs.bar.SetCurrentIndex(0)
		if !treeItem(tree, "a").IsExpanded() || !treeItem(tree, "a", "inner").IsExpanded() || tree.CurrentItem().Text(colName) != "r" {
			t.Error("the folders of the request of the tab were not opened")
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
		w := NewWindow(files.Files)
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
		if w.environment.environment.CurrentText() != "Staging" {
			t.Errorf("header shows %q", w.environment.environment.CurrentText())
		}

		saved, err := loadEnvs(files.Environments)
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

func TestRequestHeadings(t *testing.T) {
	files := testFiles(t)

	store := &collection.Store{}
	store.AddFolder("Empty")
	hotel := store.AddFolder("Hotel")
	hotel.Add(collection.NewREST("Ping", rest.Request{Method: "GET"}))
	hotel.AddFolder("Rooms").Add(collection.NewGRPC("Book", rpc.Request{}))
	store.AddFolder("Service").Add(collection.NewGRPC("Get", rpc.Request{}))
	store.AddFolder("Users").Add(collection.NewREST("List", rest.Request{Method: "GET"}))
	if err := store.Save(files.Requests); err != nil {
		t.Fatal(err)
	}

	mainthread.Wait(func() {
		w := NewWindow(files.Files)
		w.Show()
		rt := w.requests
		tree := rt.tree

		// layout lists the top level, with what is under each heading.
		layout := func() []string {
			var got []string
			for i := range tree.TopLevelItemCount() {
				item := tree.TopLevelItem(i)
				if _, ok := headingGroup(item); !ok {
					got = append(got, item.Text(colName))
					continue
				}
				names := make([]string, item.ChildCount())
				for j := range names {
					names[j] = item.Child(j).Text(colName)
				}
				got = append(got, item.Text(colName)+": "+strings.Join(names, ", "))
			}
			return got
		}
		want := []string{"gRPC: Service", "REST: Users", "Mixed: Hotel", "Empty"}
		if got := layout(); !reflect.DeepEqual(got, want) {
			t.Errorf("layout = %q, want %q", got, want)
		}

		// The headings have the icons of their types, and Mixed that of a
		// folder.
		for i, icon := range []*qt.QIcon{requestTypeIcon(collection.TypeGRPC), requestTypeIcon(collection.TypeREST), folderIcon()} {
			if got := tree.TopLevelItem(i).Icon(colName); got.CacheKey() != icon.CacheKey() {
				t.Errorf("heading %d has the wrong icon", i)
			}
		}

		// A heading is not a folder: selecting it opens nothing, and it
		// cannot be renamed, duplicated or removed.
		w.requests.confirm = func(string) bool { t.Error("asked about removing a heading"); return true }
		tree.SetCurrentItem(tree.TopLevelItem(0))
		rt.duplicate()
		rt.remove()
		if w.open != nil || rt.selection() != nil || len(rt.store.Folders) != 4 {
			t.Errorf("heading acted on: open %+v, %d folders", w.open, len(rt.store.Folders))
		}

		// A heading cannot be closed, and has no arrow to close it with.
		heading := tree.TopLevelItem(1)
		heading.SetExpanded(false)
		if !heading.IsExpanded() || tree.RootIsDecorated() {
			t.Error("heading closed like a folder")
		}

		// The keyboard passes over a heading: down from the last request
		// under gRPC goes to the first folder under REST, and up back.
		tree.SetFocus()
		tree.SetCurrentItem(treeItem(tree, "Service", "Get"))
		keyTree := func(key qt.Key) {
			event := qt.NewQKeyEvent(qt.QEvent__KeyPress, int(key), qt.NoModifier)
			defer event.Delete()
			qt.QCoreApplication_SendEvent(tree.QObject, event.QEvent)
		}
		keyTree(qt.Key_Down)
		if item := tree.CurrentItem(); item == nil || item.Text(colName) != "Users" {
			t.Errorf("down went to %q", item.Text(colName))
		}
		keyTree(qt.Key_Up)
		if item := tree.CurrentItem(); item == nil || item.Text(colName) != "Get" {
			t.Errorf("up went to %q", item.Text(colName))
		}

		// Requests have no tooltip.
		if tip := treeItem(tree, "Users", "List").ToolTip(colName); tip != "" {
			t.Errorf("request tooltip %q", tip)
		}

		// Removing the gRPC request from Hotel moves it under REST.
		w.requests.confirm = func(string) bool { return true }
		tree.SetCurrentItem(treeItem(tree, "Hotel", "Rooms", "Book"))
		rt.remove()
		want = []string{"gRPC: Service", "REST: Hotel, Users", "Empty"}
		if got := layout(); !reflect.DeepEqual(got, want) {
			t.Errorf("after remove: layout = %q, want %q", got, want)
		}
		if item := tree.CurrentItem(); item == nil || item.Text(colName) != "Rooms" {
			t.Error("the folder of the removed request is not selected")
		}

		// A request added to the empty folder takes it under a heading.
		testRequestType = collection.TypeGRPC
		defer func() { testRequestType = collection.TypeREST }()
		tree.SetCurrentItem(treeItem(tree, "Empty"))
		rt.addRequest()
		want = []string{"gRPC: Empty, Service", "REST: Hotel, Users"}
		if got := layout(); !reflect.DeepEqual(got, want) {
			t.Errorf("after add: layout = %q, want %q", got, want)
		}
	})

	// The headings are not saved.
	saved, err := loadRequests(files.Requests)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, f := range saved.Folders {
		names = append(names, f.Name)
	}
	if want := []string{"Empty", "Hotel", "Service", "Users"}; !reflect.DeepEqual(names, want) {
		t.Errorf("saved folders %q, want %q", names, want)
	}
}

func TestAddRequestWithNothingSelected(t *testing.T) {
	files := testFiles(t)

	store := &collection.Store{}
	store.AddFolder("Zoo").Add(collection.NewREST("Ping", rest.Request{Method: "GET"}))
	if err := store.Save(files.Requests); err != nil {
		t.Fatal(err)
	}

	mainthread.Wait(func() {
		w := NewWindow(files.Files)
		w.Show()
		rt := w.requests

		// A new folder is made to hold it, even though there is one already.
		rt.deselect()
		rt.addRequest()
		if len(rt.store.Folders) != 2 || rt.store.Folders[0].Name != "New Folder" || len(rt.store.Folders[0].Requests) != 1 ||
			len(rt.store.Folders[1].Requests) != 1 || w.open != rt.store.Folders[0].Requests[0] {
			t.Errorf("folders %+v, open %+v", rt.store.Folders, w.open)
		}
	})
}

func TestSearchRequests(t *testing.T) {
	files := testFiles(t)

	store := &collection.Store{}
	hotel := store.AddFolder("Hotel")
	hotel.Add(collection.NewREST("Ping", rest.Request{Method: "GET"}))
	rooms := hotel.AddFolder("Rooms")
	rooms.Add(collection.NewREST("Book room", rest.Request{Method: "POST"}))
	rooms.Add(collection.NewREST("List rooms", rest.Request{Method: "GET"}))
	users := store.AddFolder("Users")
	users.Add(collection.NewREST("List users", rest.Request{Method: "GET"}))
	store.AddFolder("Empty")
	if err := store.Save(files.Requests); err != nil {
		t.Fatal(err)
	}

	mainthread.Wait(func() {
		w := NewWindow(files.Files)
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
			for i := range topCount(tree) {
				walk(topFolder(tree, i), "")
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

		// Every word must be in the name of the request, in any order.
		check("list room", "/Hotel", "/Hotel/Rooms", "/Hotel/Rooms/List rooms")
		check("users list", "/Users", "/Users/List users")
		check("rooms", "/Hotel", "/Hotel/Rooms", "/Hotel/Rooms/List rooms")

		// The names of folders are not searched, so a folder is only shown
		// to hold what was found.
		check("hotel")
		check("rooms ping")
		check("empty")
		check("nothing like this")

		// What is found is still the request it was.
		rt.search.SetText("book")
		tree.SetCurrentItem(treeItem(tree, "Hotel", "Rooms", "Book room"))
		if w.open == nil || w.open.Name != "Book room" || w.request.method.CurrentText() != "POST" {
			t.Errorf("opened %+v", w.open)
		}

		// Clearing the search shows everything, with the folders open or
		// closed as they were, and the request still selected.
		check("", "/Hotel", "/Hotel/Rooms", "/Hotel/Rooms/Book room", "/Hotel/Rooms/List rooms", "/Hotel/Ping", "/Users", "/Users/List users", "/Empty")
		// The folders were closed, but those of the selected request are
		// left open to show it.
		if !treeItem(tree, "Hotel", "Rooms").IsExpanded() || treeItem(tree, "Users").IsExpanded() || !treeItem(tree, "Hotel").IsExpanded() {
			t.Error("the folders are not as they were")
		}
		if item := tree.CurrentItem(); item == nil || item.Text(colName) != "Book room" {
			t.Error("the request is no longer selected")
		}
		if users := rt.store.Folders[2]; users.Name != "Users" || users.Open {
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
		w := NewWindow(testFiles(t).Files)
		w.Show()

		for _, p := range []*responsePanel{w.response, w.grpcResponse} {
			var offered []string
			var warned []string
			target := filepath.Join(dir, "saved")
			p.chooseSave = func(name string) (string, bool) { offered = append(offered, name); return target, true }
			p.warn = func(message string) { warned = append(warned, message) }
			p.now = func() time.Time { return time.Date(2026, time.October, 11, 17, 30, 5, 0, time.Local) }

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
			if got, err := os.ReadFile(target); err != nil || string(got) != "{\n  \"a\": 1\n}" || string(got) != p.body.ToPlainText() || !reflect.DeepEqual(offered, []string{"untitled_20261011_173005_resp.json"}) {
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

		// The file is named after the request that is open, and the time.
		w.requests.addFolder()
		w.requests.addRequest()
		w.open.Name = "Book Room (v2)"
		for _, p := range []*responsePanel{w.response, w.grpcResponse} {
			var offered string
			p.chooseSave = func(name string) (string, bool) { offered = name; return "", false }
			p.show(&rest.Response{Status: "200 OK", StatusCode: 200, Headers: http.Header{"Content-Type": {"text/xml"}}, Body: []byte("<a/>")})
			p.save.Click()
			if want := "book_room_v2_20261011_173005_resp.xml"; offered != want {
				t.Errorf("offered %q, want %q", offered, want)
			}
		}

		at := time.Date(2026, time.January, 2, 3, 4, 5, 0, time.Local)
		for request, want := range map[string]string{
			"":                         "untitled_20260102_030405_resp.json",
			"  ":                       "untitled_20260102_030405_resp.json",
			"../..":                    "untitled_20260102_030405_resp.json",
			"Ping":                     "ping_20260102_030405_resp.json",
			"  List   Rooms  ":         "list_rooms_20260102_030405_resp.json",
			"GET /users/{id}?a=b&c":    "get_users_id_a_b_c_20260102_030405_resp.json",
			"a.b-c_d:e*f\\g\"h'i|j<k>": "a_b_c_d_e_f_g_h_i_j_k_20260102_030405_resp.json",
			"Café Zürich №5":           "café_zürich_5_20260102_030405_resp.json",
			strings.Repeat("Ab ", 60):  strings.TrimSuffix(strings.Repeat("ab_", 40), "_") + "_20260102_030405_resp.json",
		} {
			if got := saveFileName(request, at, ".json"); got != want {
				t.Errorf("saveFileName(%q) = %q, want %q", request, got, want)
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

func TestKeyValueRemoveButtons(t *testing.T) {
	var table *keyValueTable
	changes := 0
	mainthread.Wait(func() {
		table = newKeyValueTable("Key", "Value")
		table.onChange = func() { changes++ }
		table.setItems([]rest.KeyValue{{Key: "a", Value: "1", Enabled: true}, {Key: "b", Value: "2", Enabled: true}, {Key: "c", Value: "3", Enabled: true}})
		table.table.Resize(500, 300)
		table.table.Show()
	})

	mainthread.Wait(func() {
		keys := func() []string {
			var keys []string
			for _, item := range table.items() {
				keys = append(keys, item.Key)
			}
			return keys
		}
		click := func(row int) {
			qt.UnsafeNewQAbstractButton(table.table.CellWidget(row, colRemove).UnsafePointer()).Click()
		}

		// Every row but the empty one has a button.
		for row, want := range []bool{true, true, true, false} {
			if got := table.table.CellWidget(row, colRemove) != nil; got != want {
				t.Errorf("row %d has a button: %v", row, got)
			}
		}

		// A button removes its own row, without asking.
		click(1)
		if got := keys(); !reflect.DeepEqual(got, []string{"a", "c"}) || changes != 1 {
			t.Errorf("keys = %q with %d changes", got, changes)
		}

		// The buttons move with their rows.
		click(1)
		if got := keys(); !reflect.DeepEqual(got, []string{"a"}) {
			t.Errorf("keys = %q", got)
		}

		// A row that is filled in gets a button.
		table.table.Item(1, colKey).SetText("z")
		if table.table.CellWidget(1, colRemove) == nil || table.table.CellWidget(2, colRemove) != nil {
			t.Error("the new row has no button, or the empty one has")
		}
	})
}

func TestWorkspaces(t *testing.T) {
	files := testFiles(t)
	dir := files.Workspaces

	var w *Window
	var warned []string
	mainthread.Wait(func() {
		w = NewWindow(files.Files)
		w.Show()
		ws := w.workspaces
		ws.warn = func(message string) { warned = append(warned, message) }

		// There is one workspace to begin with, which is the one in use.
		if !reflect.DeepEqual(ws.names, []string{"Default"}) || ws.current() != "Default" || ws.list.CurrentRow() != 0 {
			t.Fatalf("workspaces = %q, current %q", ws.names, ws.current())
		}

		// It is given an environment and a request.
		w.environments.add()
		w.editor.name.SetText("Local")
		w.closeEditor()
		w.request.url.SetText("default.test")
		w.requests.addRequest()

		// The last workspace cannot be removed.
		ws.confirm = func(string) bool { t.Error("asked about the last workspace"); return true }
		ws.remove()
		if len(warned) != 1 || !strings.Contains(warned[0], "last workspace") || len(ws.names) != 1 {
			t.Errorf("warned %q with workspaces %q", warned, ws.names)
		}
		warned = nil

		// A new workspace becomes the one in use, and has nothing in it.
		ws.add()
		if !reflect.DeepEqual(ws.names, []string{"Default", "New Workspace"}) || ws.current() != "New Workspace" || w.config.Workspace != "New Workspace" {
			t.Fatalf("workspaces = %q, current %q", ws.names, ws.current())
		}
		if w.environments.list.Count() != 0 || topCount(w.requests.tree) != 0 || w.open != nil || w.request.url.Text() != "" {
			t.Errorf("new workspace has %d environments and %d folders, open %+v", w.environments.list.Count(), topCount(w.requests.tree), w.open)
		}

		// What is added now goes into it.
		w.environments.add()
		w.editor.name.SetText("Staging")
		w.closeEditor()
		w.request.url.SetText("new.test")
		w.requests.addRequest()

		// It is renamed, which renames its folder and moves it in the list.
		ws.list.CurrentItem().SetText("  Alpha ")
	})

	waitFor(t, "the workspace to be renamed", func() bool {
		return reflect.DeepEqual(w.workspaces.names, []string{"Alpha", "Default"}) && w.workspaces.current() == "Alpha"
	})

	mainthread.Wait(func() {
		ws := w.workspaces
		if w.config.Workspace != "Alpha" || w.environments.path != workspace.Environments(dir, "Alpha") || w.requests.path != workspace.Requests(dir, "Alpha") {
			t.Errorf("after rename: workspace %q, paths %q and %q", w.config.Workspace, w.environments.path, w.requests.path)
		}
		if _, err := os.Stat(filepath.Join(dir, "New Workspace")); err == nil {
			t.Error("the old folder is still there")
		}

		// Later changes are saved under the new name.
		w.environments.edit()
		w.editor.variables.add("host", "alpha.test")
		w.closeEditor()

		// A name that cannot be used is refused.
		ws.list.CurrentItem().SetText("default")
	})

	waitFor(t, "the name to be refused", func() bool { return len(warned) == 1 })

	mainthread.Wait(func() {
		ws := w.workspaces
		if !strings.Contains(warned[0], "already a workspace") || !reflect.DeepEqual(ws.names, []string{"Alpha", "Default"}) || ws.list.Item(0).Text() != "Alpha" {
			t.Errorf("warned %q, workspaces %q, shown %q", warned, ws.names, ws.list.Item(0).Text())
		}
		warned = nil

		// Selecting the other workspace shows what is in it.
		ws.list.SetCurrentRow(1)
		if ws.current() != "Default" || w.environments.list.Count() != 1 || w.environments.list.Item(0).Text() != "Local" {
			t.Fatalf("current %q with %d environments", ws.current(), w.environments.list.Count())
		}
		tree := w.requests.tree
		if topCount(tree) != 1 || w.open != nil {
			t.Fatalf("%d folders, open %+v", topCount(tree), w.open)
		}
		tree.SetCurrentItem(topFolder(tree, 0).Child(0))
		if w.request.url.Text() != "default.test" {
			t.Errorf("url = %q", w.request.url.Text())
		}

		// And back again.
		ws.list.SetCurrentRow(0)
		if w.environments.list.Item(0).Text() != "Staging" || w.expand("{{host}}") != "alpha.test" || w.open != nil {
			t.Errorf("environment %q, host %q", w.environments.list.Item(0).Text(), w.expand("{{host}}"))
		}

		w.Close()
	})

	// Each workspace has its own files.
	alpha, err := loadRequests(workspace.Requests(dir, "Alpha"))
	if err != nil || len(alpha.Folders) != 1 || alpha.Folders[0].Requests[0].URL != "new.test" {
		t.Errorf("requests of Alpha = %+v, %v", alpha, err)
	}
	original, err := loadRequests(files.Requests)
	if err != nil || len(original.Folders) != 1 || original.Folders[0].Requests[0].URL != "default.test" {
		t.Errorf("requests of Default = %+v, %v", original, err)
	}
	if envs, _ := loadEnvs(workspace.Environments(dir, "Alpha")); len(envs.Environments) != 1 || len(envs.Environments[0].Variables) != 1 {
		t.Errorf("environments of Alpha = %+v", envs)
	}
	if saved := settings.Load(files.Settings); saved.Workspace != "Alpha" {
		t.Errorf("saved workspace = %q", saved.Workspace)
	}

	// The next run opens the workspace that was in use.
	mainthread.Wait(func() {
		w = NewWindow(files.Files)
		w.Show()
		ws := w.workspaces
		ws.warn = func(message string) { warned = append(warned, message) }

		if ws.current() != "Alpha" || w.environments.list.Count() != 1 || w.environments.list.Item(0).Text() != "Staging" {
			t.Fatalf("current %q with %d environments", ws.current(), w.environments.list.Count())
		}
		tree := w.requests.tree
		tree.SetCurrentItem(topFolder(tree, 0).Child(0))

		// It is not removed unless the user agrees to it.
		var asked string
		ws.confirm = func(question string) bool { asked = question; return false }
		ws.remove()
		if len(ws.names) != 2 || !strings.Contains(asked, `"Alpha"`) {
			t.Errorf("workspaces %q after being asked %q", ws.names, asked)
		}

		// Removing it deletes its files, and the other becomes the one in
		// use, with the request that was open closed.
		ws.confirm = func(string) bool { return true }
		ws.remove()
		if !reflect.DeepEqual(ws.names, []string{"Default"}) || ws.current() != "Default" || w.config.Workspace != "Default" || len(warned) != 0 {
			t.Fatalf("workspaces = %q, current %q, warned %q", ws.names, ws.current(), warned)
		}
		if w.open != nil || w.environments.list.Item(0).Text() != "Local" || w.requests.path != files.Requests {
			t.Errorf("after remove: open %+v, requests at %q", w.open, w.requests.path)
		}
	})
	if _, err := os.Stat(filepath.Join(dir, "Alpha")); err == nil {
		t.Error("the folder of the removed workspace is still there")
	}

	// A workspace whose folder has other files in it is not removed.
	mainthread.Wait(func() {
		ws := w.workspaces
		ws.add()
		os.WriteFile(filepath.Join(dir, "New Workspace", "notes.txt"), []byte("mine"), 0o644)
		ws.remove()
		if len(warned) != 1 || !strings.Contains(warned[0], "notes.txt") || len(ws.names) != 2 || ws.current() != "New Workspace" {
			t.Errorf("warned %q, workspaces %q", warned, ws.names)
		}
	})
}

func TestFirstRun(t *testing.T) {
	dir := t.TempDir()
	files := Files{
		Settings:   filepath.Join(dir, "settings.json"),
		Workspaces: filepath.Join(dir, "workspaces"),
	}
	saveTestKeys(t, files.Settings)

	// With nothing saved at all there is still a workspace to work in.
	mainthread.Wait(func() {
		w := NewWindow(files)
		if !reflect.DeepEqual(w.workspaces.names, []string{"Default"}) || w.StatusBar().CurrentMessage() != "" {
			t.Fatalf("workspaces = %q, status %q", w.workspaces.names, w.StatusBar().CurrentMessage())
		}
		w.request.url.SetText("first.test")
		w.requests.addRequest()
	})
	if saved, err := loadRequests(workspace.Requests(files.Workspaces, "Default")); err != nil || len(saved.Folders) != 1 {
		t.Errorf("requests = %+v, %v", saved, err)
	}
}

// tabTitle returns the name on the tab that is shown above the request UI.
func tabTitle(w *Window) string {
	return w.tabs.bar.TabText(w.tabs.bar.CurrentIndex())
}

// tabNames lists the tabs above the request UI, with a star after the name
// of each that is pinned.
func tabNames(w *Window) []string {
	var names []string
	for i, tab := range w.tabs.tabs {
		name := w.tabs.bar.TabText(i)
		if tab.pinned {
			name += "*"
		}
		names = append(names, name)
	}
	return names
}

// doubleClick double clicks a button as the pointer does: a press and a
// release, then a double click and a release.
func doubleClick(button *qt.QToolButton) {
	for _, kind := range []qt.QEvent__Type{qt.QEvent__MouseButtonPress, qt.QEvent__MouseButtonRelease, qt.QEvent__MouseButtonDblClick, qt.QEvent__MouseButtonRelease} {
		buttons := qt.LeftButton
		if kind == qt.QEvent__MouseButtonRelease {
			buttons = qt.NoButton
		}
		event := qt.NewQMouseEvent(kind, qt.NewQPointF3(2, 2), qt.LeftButton, buttons, qt.NoModifier)
		qt.QCoreApplication_SendEvent(button.QObject, event.QEvent)
		event.Delete()
	}
}

func TestPinnedRequests(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("pong"))
	}))
	defer srv.Close()

	files := testFiles(t)
	store := &collection.Store{}
	folder := store.AddFolder("Folder")
	folder.Add(collection.NewREST("A", rest.Request{Method: "GET", URL: "a.test"}))
	folder.Add(collection.NewREST("B", rest.Request{Method: "GET", URL: srv.URL}))
	folder.Add(collection.NewGRPC("C", rpc.Request{URL: "c.test:1"}))
	if err := store.Save(files.Requests); err != nil {
		t.Fatal(err)
	}

	var w *Window
	check := func(what string, current int, want ...string) {
		t.Helper()
		if got := tabNames(w); !reflect.DeepEqual(got, want) || w.tabs.bar.CurrentIndex() != current || w.tabs.bar.Count() != len(want)+1 {
			t.Errorf("%s: tabs = %q with %d shown, want %q with %d shown", what, got, w.tabs.bar.CurrentIndex(), want, current)
		}
	}

	mainthread.Wait(func() {
		w = NewWindow(files.Files)
		w.Show()
		tree := w.requests.tree
		pick := func(name string) { tree.SetCurrentItem(treeItem(tree, "Folder", name)) }

		// There is one tab to begin with, which has nothing to pin.
		check("at first", 0, "Untitled")
		if w.tabs.current().button.IsEnabled() {
			t.Error("a request that is not saved can be pinned")
		}

		// It shows whichever request is selected.
		pick("A")
		check("after selecting A", 0, "A")
		pick("B")
		check("after selecting B", 0, "B")
		if w.open == nil || w.open.Name != "B" || !w.tabs.current().button.IsEnabled() {
			t.Fatalf("open = %+v", w.open)
		}

		// Pinning it keeps B there, and the next request selected has a
		// new tab.
		w.tabs.current().button.Click()
		check("after pinning B", 0, "B*")
		pick("C")
		check("after selecting C", 1, "B*", "C")
		if w.open.Name != "C" || w.stack.CurrentIndex() != pageGRPC {
			t.Errorf("open = %+v on page %d", w.open, w.stack.CurrentIndex())
		}
		pick("A")
		check("after selecting A", 1, "B*", "A")

		// Selecting a pinned request goes to its tab.
		pick("B")
		check("after selecting B", 0, "B*", "A")
		if w.open.Name != "B" || w.request.url.Text() != srv.URL {
			t.Errorf("open = %+v", w.open)
		}

		// Choosing a tab shows its request, and selects it in the sidebar.
		w.tabs.bar.SetCurrentIndex(1)
		if w.open.Name != "A" || w.request.url.Text() != "a.test" || tree.CurrentItem().Text(colName) != "A" {
			t.Errorf("open = %+v, selected %q", w.open, tree.CurrentItem().Text(colName))
		}
		w.tabs.current().button.Click()
		pick("C")
		check("after pinning A and selecting C", 2, "B*", "A*", "C")

		// The button of a tab is a pin until the tab is pinned, and then
		// shows the type of its request, but for while the pointer is over
		// the tab, when it is a pin again to unpin it with.
		key := func(tab *requestTab) int64 { return tab.button.Icon().CacheKey() }
		pinned, preview := w.tabs.tabs[0], w.tabs.tabs[2]
		if key(preview) != w.tabs.unpinnedIcon.CacheKey() || key(pinned) != requestTypeIcon(collection.TypeREST).CacheKey() {
			t.Error("the buttons do not show a pin and the type of the request")
		}
		w.tabs.hover(pinned)
		if key(pinned) != w.tabs.pinnedIcon.CacheKey() || pinned.button.ToolTip() != "" || w.tabs.bar.TabToolTip(0) != "" {
			t.Error("the button of a pinned tab is not a pin under the pointer")
		}
		w.tabs.hover(preview)
		if key(pinned) != requestTypeIcon(collection.TypeREST).CacheKey() || key(preview) != w.tabs.unpinnedIcon.CacheKey() {
			t.Error("the button did not go back to the type of the request")
		}
		w.tabs.hover(nil)
		// The tabs have no icon of their own, and no line names the request
		// beneath them.
		if !w.tabs.bar.TabIcon(0).IsNull() {
			t.Error("a tab has an icon beside its button")
		}

		// Renaming a request renames its tab.
		treeItem(tree, "Folder", "A").SetText(colName, "Alpha")
		check("after renaming A", 2, "B*", "Alpha*", "C")

		// A request sent from a tab has its response there when the tab is
		// gone back to.
		w.tabs.bar.SetCurrentIndex(0)
		w.send()
	})

	waitFor(t, "the response", func() bool { return fieldValue(w.response, "Status") == "200 OK" })

	mainthread.Wait(func() {
		tree := w.requests.tree

		w.tabs.bar.SetCurrentIndex(1)
		if fieldValue(w.response, "Status") != "" || w.response.body.ToPlainText() != "" {
			t.Error("the response of one tab is shown in another")
		}
		w.tabs.bar.SetCurrentIndex(0)
		if fieldValue(w.response, "Status") != "200 OK" || w.response.body.ToPlainText() != "pong" {
			t.Errorf("response = %q %q", fieldValue(w.response, "Status"), w.response.body.ToPlainText())
		}

		// A click does not unpin a tab; it takes a double click, which
		// closes a tab that is not shown.
		w.tabs.tabs[1].button.Click()
		check("after clicking Alpha", 0, "B*", "Alpha*", "C")
		doubleClick(w.tabs.tabs[1].button)
		check("after unpinning Alpha", 0, "B*", "C")

		// Unpinning the tab that is shown makes it the one that follows
		// the selection, in place of the other.
		doubleClick(w.tabs.current().button)
		check("after unpinning B", 0, "B")
		if w.open.Name != "B" {
			t.Errorf("open = %+v", w.open)
		}
		tree.SetCurrentItem(treeItem(tree, "Folder", "C"))
		check("after selecting C", 0, "C")

		// B and C are left pinned. Double clicking a tab that is not
		// pinned pins it, and the second click does not unpin it again.
		doubleClick(w.tabs.current().button)
		tree.SetCurrentItem(treeItem(tree, "Folder", "B"))
		w.tabs.current().button.Click()
		check("after pinning C and B", 1, "C*", "B*")

		w.Close()
	})

	// Tabs that are not in a set are not opened again by the next run.
	mainthread.Wait(func() {
		w = NewWindow(files.Files)
		w.Show()
		tree := w.requests.tree
		w.requests.confirm = func(string) bool { return true }

		check("on the next run", 0, "Untitled")
		if w.open != nil {
			t.Errorf("open on the next run: %+v", w.open)
		}
		tree.SetCurrentItem(treeItem(tree, "Folder", "C"))
		w.tabs.current().button.Click()
		tree.SetCurrentItem(treeItem(tree, "Folder", "B"))
		w.tabs.current().button.Click()
		w.tabs.bar.SetCurrentIndex(0)
		check("pinned again", 0, "C*", "B*")
		if w.open == nil || w.open.Name != "C" || w.stack.CurrentIndex() != pageGRPC || tree.CurrentItem() == nil || tree.CurrentItem().Text(colName) != "C" {
			t.Fatalf("open = %+v on page %d", w.open, w.stack.CurrentIndex())
		}

		// The tabs are put away while an environment is edited.
		w.environments.add()
		if w.stack.CurrentIndex() != pageEditor || !w.tabs.bar.IsHidden() {
			t.Error("the tabs are shown above the environment editor")
		}
		w.closeEditor()
		if w.tabs.bar.IsHidden() {
			t.Error("the tabs did not come back")
		}

		// A request being composed is kept while another tab is shown.
		w.stash(w.tabs.current())
		w.showTab(w.tabs.open(nil), false)
		check("with a request that is not saved", 2, "C*", "B*", "Untitled")
		w.request.url.SetText("draft.test")
		w.tabs.bar.SetCurrentIndex(1)
		if w.open == nil || w.open.Name != "B" {
			t.Fatalf("open = %+v", w.open)
		}
		w.tabs.bar.SetCurrentIndex(2)
		if w.open != nil || w.request.url.Text() != "draft.test" {
			t.Errorf("open = %+v, url %q", w.open, w.request.url.Text())
		}

		// Removing a pinned request closes its tab.
		tree.SetCurrentItem(treeItem(tree, "Folder", "B"))
		check("after selecting B", 1, "C*", "B*", "Untitled")
		w.requests.remove()
		if got := tabNames(w); len(got) < 1 || got[0] != "C*" || slices.Contains(got, "B*") {
			t.Errorf("tabs after removing B = %q", got)
		}

		// Pinned tabs that are not in a set are not kept when the workspace
		// is left.
		w.workspaces.warn = func(message string) { t.Error(message) }
		w.workspaces.add()
		check("in a new workspace", 0, "Untitled")
		if w.open != nil {
			t.Errorf("open = %+v", w.open)
		}
		w.workspaces.list.SetCurrentRow(0)
		if got := tabNames(w); !reflect.DeepEqual(got, []string{"Untitled"}) || w.open != nil {
			t.Errorf("tabs back in the first workspace = %q, open %+v", got, w.open)
		}
	})
}

func TestDuplicate(t *testing.T) {
	files := testFiles(t)

	envs := &env.Store{Active: 0, Environments: []env.Environment{{Name: "Local", Variables: []env.Variable{{Key: "host", Value: "local.test", Enabled: true}}}}}
	if err := envs.Save(files.Environments); err != nil {
		t.Fatal(err)
	}
	store := &collection.Store{}
	hotel := store.AddFolder("Hotel")
	hotel.Add(collection.NewREST("Book", rest.Request{Method: "POST", URL: "{{host}}/rooms", Headers: []rest.KeyValue{{Key: "X-Key", Value: "abc", Enabled: true}}}))
	hotel.AddFolder("Inner").Add(collection.NewGRPC("Call", rpc.Request{URL: "c.test:1"}))
	if err := store.Save(files.Requests); err != nil {
		t.Fatal(err)
	}

	mainthread.Wait(func() {
		w := NewWindow(files.Files)
		w.Show()
		tree := w.requests.tree

		// A copy of a request is beside it, is opened, and is its own.
		tree.SetCurrentItem(treeItem(tree, "Hotel", "Book"))
		original := w.open
		w.requests.duplicate()
		if got, want := treeNames(treeItem(tree, "Hotel")), []string{"Inner/", "Book", "Book copy"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("hotel = %q, want %q", got, want)
		}
		if w.open == nil || w.open == original || w.open.Name != "Book copy" || w.request.url.Text() != "{{host}}/rooms" || tree.CurrentItem().Text(colName) != "Book copy" {
			t.Fatalf("open = %+v", w.open)
		}
		w.request.url.SetText("{{host}}/changed")
		if original.URL != "{{host}}/rooms" || len(original.Headers) != 1 {
			t.Errorf("the request copied from was changed: %+v", original.Request)
		}

		// A copy of a folder has a copy of everything in it.
		tree.SetCurrentItem(treeItem(tree, "Hotel"))
		w.requests.duplicate()
		if topCount(tree) != 2 || tree.CurrentItem().Text(colName) != "Hotel copy" {
			t.Fatalf("%d folders, selected %q", topCount(tree), tree.CurrentItem().Text(colName))
		}
		if got, want := treeNames(treeItem(tree, "Hotel copy")), []string{"Inner/", "Book", "Book copy"}; !reflect.DeepEqual(got, want) {
			t.Errorf("copy = %q, want %q", got, want)
		}
		if w.requests.store.Folders[1].Folders[0].Requests[0] == w.requests.store.Folders[0].Folders[0].Requests[0] {
			t.Error("the copy shares a request with the folder it is a copy of")
		}

		// Nothing is duplicated with nothing selected.
		w.requests.deselect()
		w.requests.duplicate()
		if topCount(tree) != 2 {
			t.Error("duplicated with nothing selected")
		}

		// A copy of an environment has its variables, and is the one
		// selected and edited.
		w.environments.duplicate()
		list := w.environments.list
		if list.Count() != 2 || list.CurrentRow() != 1 || list.Item(1).Text() != "Local copy" || w.stack.CurrentIndex() != pageEditor || w.editor.name.Text() != "Local copy" {
			t.Fatalf("%d environments, row %d, editing %q", list.Count(), list.CurrentRow(), w.editor.name.Text())
		}
		w.editor.variables.add("token", "t")
		w.closeEditor()
		if all := w.environments.store.Environments; len(all[0].Variables) != 1 || len(all[1].Variables) != 2 || w.expand("{{host}}") != "local.test" {
			t.Errorf("environments = %+v", all)
		}

		// A copy of a workspace has its environments and requests, and is
		// the one in use.
		w.workspaces.warn = func(message string) { t.Error(message) }
		w.workspaces.duplicate()
		if got, want := w.workspaces.names, []string{"Default", "Default copy"}; !reflect.DeepEqual(got, want) || w.workspaces.current() != "Default copy" {
			t.Fatalf("workspaces = %q, current %q", got, w.workspaces.current())
		}
		if w.environments.list.Count() != 2 || topCount(w.requests.tree) != 2 || w.requests.path != workspace.Requests(files.Workspaces, "Default copy") {
			t.Errorf("copy has %d environments and %d folders, saved to %q", w.environments.list.Count(), topCount(w.requests.tree), w.requests.path)
		}

		// Changing the copy leaves the workspace it is a copy of alone.
		w.requests.confirm = func(string) bool { return true }
		w.requests.tree.SetCurrentItem(treeItem(w.requests.tree, "Hotel copy"))
		w.requests.remove()
	})

	if original, err := loadRequests(files.Requests); err != nil || len(original.Folders) != 2 {
		t.Errorf("requests of the first workspace = %+v, %v", original, err)
	}
	if copied, err := loadRequests(workspace.Requests(files.Workspaces, "Default copy")); err != nil || len(copied.Folders) != 1 {
		t.Errorf("requests of the copy = %+v, %v", copied, err)
	}
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

// The menu of the body of a response offers to capture the value it is
// opened on, which adds a rule with the path of the value.
func TestClickToCapture(t *testing.T) {
	mainthread.Wait(func() {
		w := NewWindow(testFiles(t).Files)
		w.Show()

		// offers returns what the menu at some text in the body offers to
		// capture, and the action which does it.
		offers := func(p *responsePanel, text string, nth int) (string, *qt.QAction) {
			body := p.body.ToPlainText()
			offset := -1
			for range nth {
				offset += 1 + strings.Index(body[offset+1:], text)
			}
			menu := p.contextMenu(len(utf16.Encode([]rune(body[:offset]))) + 1)
			actions := menu.Actions()
			if last := actions[len(actions)-1]; strings.HasPrefix(last.Text(), "Capture") {
				return last.Text(), last
			}
			return "", nil
		}

		// Nothing is offered without a response, or in one that is not
		// JSON.
		p := w.response
		if got, _ := offers(p, "", 1); got != "" {
			t.Errorf("without a response: %q", got)
		}
		p.show(&rest.Response{Status: "200 OK", StatusCode: 200, Headers: http.Header{"Content-Type": {"text/plain"}}, Body: []byte("hello there")})
		if got, _ := offers(p, "there", 1); got != "" {
			t.Errorf("in text: %q", got)
		}

		// A body that is JSON whatever it says it is can be captured
		// from, with the line endings it came with.
		p.show(&rest.Response{Status: "200 OK", StatusCode: 200, Headers: http.Header{"Content-Type": {"text/plain"}}, Body: []byte("{\r\n  \"token\": \"abc\"\r\n}")})
		if got, _ := offers(p, `"abc"`, 1); got != "Capture token" {
			t.Errorf("in JSON sent as text: %q", got)
		}

		p.show(&rest.Response{Status: "200 OK", StatusCode: 200, Headers: http.Header{"Content-Type": {"application/json"}}, Body: []byte(`{"é😀":"x","data":{"items":[{"id":7},{"id":8}],"a&b":1}}`)})
		for text, want := range map[string]string{
			`"data"`: "Capture data",
			`"a&b"`:  "Capture data.a&&b",
			`"x"`:    "Capture é😀",
		} {
			if got, _ := offers(p, text, 1); got != want {
				t.Errorf("at %s: %q, want %q", text, got, want)
			}
		}

		// The rule is added and shown, with its name the current cell.
		got, action := offers(p, `"id"`, 2)
		if got != "Capture data.items[1].id" {
			t.Fatalf("at the second id: %q", got)
		}
		var changes int
		w.request.captures.onChange = func() { changes++ }
		action.Trigger()
		want := []capture.Rule{{Variable: "id", Path: "data.items[1].id", Enabled: true}}
		if rules := captureRules(w.request.captures); !slices.Equal(rules, want) || changes == 0 {
			t.Errorf("rules = %+v after %d changes", rules, changes)
		}
		if table := w.request.captures.table; p.tabs.CurrentIndex() != tabCapture || table.CurrentRow() != 0 || table.CurrentColumn() != colKey {
			t.Errorf("tab %d, cell %d, %d", p.tabs.CurrentIndex(), table.CurrentRow(), table.CurrentColumn())
		}

		// Capturing the same value again adds nothing.
		action.Trigger()
		if rules := captureRules(w.request.captures); len(rules) != 1 {
			t.Errorf("rules = %+v", rules)
		}

		// Of the messages of a gRPC call, values are captured from the
		// last, so they are offered only there.
		p = w.grpcResponse
		p.showRPC(&rpc.Response{Messages: []string{"{\n  \"text\": \"one\"\n}", "{\n  \"text\": \"two\"\n}"}})
		if got, _ := offers(p, `"text"`, 1); got != "" {
			t.Errorf("in the first message: %q", got)
		}
		if got, action := offers(p, `"text"`, 2); got != "Capture text" {
			t.Errorf("in the last message: %q", got)
		} else {
			action.Trigger()
			if rules := captureRules(w.grpc.captures); !slices.Equal(rules, []capture.Rule{{Variable: "text", Path: "text", Enabled: true}}) {
				t.Errorf("rules = %+v", rules)
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

// Text cursors are freed on the GUI thread, not by the garbage collector on
// a thread of its own, which crashed when it came while a document was being
// changed. Here it is made to come all the time.
func TestTextCursorsWhileCollecting(t *testing.T) {
	defer debug.SetGCPercent(debug.SetGCPercent(1))

	const body = "{\n  \"name\": \"a\",\n       \"data\": {\n    \"year\": 2019,\n          \"tags\": [1, 2]\n      }\n   }"

	var w *Window
	mainthread.Wait(func() {
		w = NewWindow(testFiles(t).Files)
		w.Show()
		w.request.bodyType.SetCurrentIndex(1)
		w.response.find.open()
		w.response.find.field.SetText("a")
	})

	var garbage []byte
	for range 1500 {
		mainthread.Wait(func() {
			p := w.request
			p.bodyRaw.SetPlainText(body)
			p.bodyFold.reset()
			p.bodyFold.toggle(2)
			p.bodyFold.lineAt(4)
			garbage = make([]byte, 1<<16)
			p.format.button.Click()

			w.response.setBody(body, true)
			w.response.find.step(1)
		})
	}
	_ = garbage
}

// A variable whose value is a call of a built-in function is given a new
// value for each request, which is the same in every part of that request.
func TestFunctionVariables(t *testing.T) {
	var seen sync.Map
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		seen.Store(requests.Add(1), []string{r.URL.Query().Get("id"), r.Header.Get("X-Id"), string(body), r.URL.Query().Get("day")})
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{}`)
	}))
	defer srv.Close()

	files := testFiles(t)
	store := &env.Store{Active: 0, Environments: []env.Environment{
		{Name: "Local", Variables: []env.Variable{
			{Key: "host", Value: srv.URL, Enabled: true},
			{Key: "id", Value: "random_uuid()", Enabled: true},
			{Key: "day", Value: "iso_date(1)", Enabled: true},
		}},
	}}
	if err := store.Save(files.Environments); err != nil {
		t.Fatal(err)
	}

	var w *Window
	mainthread.Wait(func() {
		w = NewWindow(files.Files)
		w.Show()
		w.request.load(rest.Request{
			Method:   "POST",
			URL:      "{{host}}/a",
			Params:   []rest.KeyValue{{Key: "day", Value: "{{day}}", Enabled: true}, {Key: "id", Value: "{{id}}", Enabled: true}},
			Headers:  []rest.KeyValue{{Key: "X-Id", Value: "{{id}}", Enabled: true}},
			BodyType: rest.BodyText,
			Body:     "{{id}}",
		})
		w.request.send.Click()
	})
	waitFor(t, "the first request", func() bool { return requests.Load() == 1 && fieldValue(w.response, "Status") != "" })
	mainthread.Wait(func() { w.request.send.Click() })
	waitFor(t, "the second request", func() bool { return requests.Load() == 2 })

	first, _ := seen.Load(int32(1))
	second, _ := seen.Load(int32(2))
	a, b := first.([]string), second.([]string)
	if len(a[0]) != 36 || a[0] != a[1] || a[0] != a[2] {
		t.Errorf("the first request had %q", a)
	}
	if len(b[0]) != 36 || b[0] != b[1] || b[0] != b[2] || b[0] == a[0] {
		t.Errorf("the second request had %q after %q", b, a)
	}
	if want := time.Now().AddDate(0, 0, 1).Format(time.DateOnly); a[3] != want {
		t.Errorf("day = %q, want %q", a[3], want)
	}

	// The curl command has one value throughout too, and it is the command
	// as shown that Execute runs.
	mainthread.Wait(func() {
		p := w.request
		p.curl.Click()
		id := p.commanded.Body
		if command := p.command.ToPlainText(); len(id) != 36 || strings.Count(command, id) != 3 || p.commanded.Headers[0].Value != id {
			t.Errorf("command = %s\nmade from %+v", command, p.commanded)
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

// A request, a folder and an environment cannot be given an empty name: one
// that is emptied keeps the name it had.
func TestNamesCannotBeEmpty(t *testing.T) {
	files := testFiles(t)

	mainthread.Wait(func() {
		w := NewWindow(files.Files)
		w.Show()

		w.requests.addFolder()
		folderItem := topFolder(w.requests.tree, 0)
		folderItem.SetText(colName, "Hotel")
		w.requests.addRequest()
		request := w.open
		requestItem := topFolder(w.requests.tree, 0).Child(0)
		requestItem.SetText(colName, "Book room")

		for _, blank := range []string{"", "   ", "\t"} {
			topFolder(w.requests.tree, 0).SetText(colName, blank)
			topFolder(w.requests.tree, 0).Child(0).SetText(colName, blank)
			folder := w.requests.store.Folders[0]
			if folder.Name != "Hotel" || request.Name != "Book room" {
				t.Errorf("after %q: folder %q, request %q", blank, folder.Name, request.Name)
			}
			if got := topFolder(w.requests.tree, 0).Text(colName); got != "Hotel" {
				t.Errorf("after %q the folder is shown as %q", blank, got)
			}
			if got := topFolder(w.requests.tree, 0).Child(0).Text(colName); got != "Book room" {
				t.Errorf("after %q the request is shown as %q", blank, got)
			}
		}
		if got := w.tabs.bar.TabText(0); got != "Book room" {
			t.Errorf("the tab is named %q", got)
		}
		if saved, err := loadRequests(files.Requests); err != nil || saved.Folders[0].Name != "Hotel" || saved.Folders[0].Requests[0].Name != "Book room" {
			t.Errorf("saved %+v, %v", saved, err)
		}

		// The name of an environment can be emptied on the way to typing
		// another, which leaves it with the name it had.
		w.environments.add()
		w.editor.name.SetText("Local")
		w.editor.variables.add("host", "example.com")
		w.editor.name.SetText("")
		if got := w.environments.store.Environments[0].Name; got != "Local" || w.environments.list.Item(0).Text() != "Local" {
			t.Errorf("emptied: the environment is named %q", got)
		}
		// Its variables are still kept while the field is empty.
		w.editor.variables.add("port", "80")
		if e := w.environments.store.Environments[0]; e.Name != "Local" || len(e.Variables) != 2 {
			t.Errorf("edited while empty: %+v", e)
		}
		w.editor.name.SetText("  ")
		w.editor.name.EditingFinished()
		if got := w.editor.name.Text(); got != "Local" || w.environments.store.Environments[0].Name != "Local" {
			t.Errorf("left empty: the field has %q", got)
		}
		w.editor.name.SetText("L")
		if got := w.environments.store.Environments[0].Name; got != "L" {
			t.Errorf("typed: the environment is named %q", got)
		}
		if saved, err := loadEnvs(files.Environments); err != nil || saved.Environments[0].Name != "L" {
			t.Errorf("saved %+v, %v", saved, err)
		}
	})
}

// Ctrl+P goes to the search of the requests, Return goes on from it to the
// tree, where the keyboard moves about without opening anything, and Return
// there opens the request it has got to.
func TestQuickOpen(t *testing.T) {
	files := testFiles(t)
	store := collection.Store{Folders: []*collection.Folder{
		{Name: "Health", Requests: []*collection.Request{collection.NewREST("Ping", rest.Request{Method: "GET"})}},
		{Name: "Hotel",
			Folders: []*collection.Folder{{Name: "Rooms", Requests: []*collection.Request{
				collection.NewREST("Book room", rest.Request{Method: "POST"}),
				collection.NewREST("List rooms", rest.Request{Method: "GET"}),
			}}},
			Requests: []*collection.Request{collection.NewGRPC("Availability", rpc.Request{})},
		},
	}}
	if err := store.Save(files.Requests); err != nil {
		t.Fatal(err)
	}

	mainthread.Wait(func() {
		w := NewWindow(files.Files)
		w.Show()
		tree := w.requests.tree

		press := func(key qt.Key) {
			event := qt.NewQKeyEvent(qt.QEvent__KeyPress, int(key), qt.NoModifier)
			defer event.Delete()
			qt.QCoreApplication_SendEvent(tree.QObject, event.QEvent)
		}
		current := func() string {
			if item := tree.CurrentItem(); item != nil {
				return item.Text(colName)
			}
			return ""
		}
		opened := func() string {
			if w.open != nil {
				return w.open.Name
			}
			return ""
		}

		// The search is gone to from another tab of the sidebar.
		w.sidebar.SetCurrentIndex(tabEnvironments)
		w.quickOpen()
		if w.sidebar.CurrentIndex() != tabRequests {
			t.Error("the requests are not shown")
		}

		// Return goes to the first request that was found, which is not
		// opened by that.
		w.requests.search.SetText("room")
		w.requests.search.ReturnPressed()
		if current() != "Book room" || opened() != "" {
			t.Errorf("after the search: at %q, with %q open", current(), opened())
		}

		// Nor is anything opened by moving about.
		press(qt.Key_Down)
		if current() != "List rooms" || opened() != "" {
			t.Errorf("after down: at %q, with %q open", current(), opened())
		}
		press(qt.Key_Up)
		press(qt.Key_Up)
		if current() != "Rooms" || opened() != "" {
			t.Errorf("after up twice: at %q, with %q open", current(), opened())
		}

		// Left closes a folder and right opens it, as does Return.
		folder := tree.CurrentItem()
		press(qt.Key_Left)
		if folder.IsExpanded() {
			t.Error("left did not close the folder")
		}
		press(qt.Key_Right)
		if !folder.IsExpanded() {
			t.Error("right did not open the folder")
		}
		press(qt.Key_Return)
		if folder.IsExpanded() || opened() != "" {
			t.Errorf("return on a folder: open %v, with %q open", folder.IsExpanded(), opened())
		}
		press(qt.Key_Return)

		// Return opens the request, in the tab that is not pinned.
		press(qt.Key_Down)
		press(qt.Key_Down)
		press(qt.Key_Return)
		if current() != "List rooms" || opened() != "List rooms" || w.tabs.bar.TabText(w.tabs.bar.CurrentIndex()) != "List rooms" {
			t.Errorf("after return: at %q, with %q open", current(), opened())
		}
		w.tabs.toggle(w.tabs.current())

		// One that is pinned is gone to in its own tab.
		press(qt.Key_Up)
		press(qt.Key_Return)
		if opened() != "Book room" || len(w.tabs.tabs) != 2 {
			t.Errorf("another: %q open in %d tabs", opened(), len(w.tabs.tabs))
		}
		press(qt.Key_Down)
		if opened() != "Book room" {
			t.Errorf("moving back opened %q", opened())
		}
		press(qt.Key_Return)
		if opened() != "List rooms" || len(w.tabs.tabs) != 2 || w.tabs.bar.CurrentIndex() != 0 {
			t.Errorf("the pinned one: %q open in %d tabs, tab %d shown", opened(), len(w.tabs.tabs), w.tabs.bar.CurrentIndex())
		}

		// Without a search, Return in the box starts where the selection
		// is, or at the first item if there is none.
		w.requests.search.SetText("")
		w.requests.search.ReturnPressed()
		if current() != "List rooms" {
			t.Errorf("without a search: at %q", current())
		}
		w.requests.deselect()
		w.requests.search.ReturnPressed()
		if current() != "Health" {
			t.Errorf("with nothing selected: at %q", current())
		}

		// A click still opens a request.
		tree.SetCurrentItem(topFolder(tree, 0).Child(0))
		if opened() != "Ping" {
			t.Errorf("selected with the pointer: %q open", opened())
		}
	})
}

func TestEncryption(t *testing.T) {
	files := testFiles(t)
	dir := files.Workspaces
	keyDir := t.TempDir()
	public, private := crypttest.Pair(t, keyDir, "id", "hunter2")
	otherPublic, otherPrivate := crypttest.Pair(t, keyDir, "other", "")

	// The passphrase is given rather than asked for in a dialog.
	asked := 0
	previous := askPassphrase
	askPassphrase = func(*qt.QWidget, string) ([]byte, error) {
		asked++
		return []byte("hunter2"), nil
	}
	t.Cleanup(func() { askPassphrase = previous })

	// Two workspaces saved before there were keys, and settings without
	// any, as on the first run of a version with encryption.
	if err := (settings.Settings{}).Save(files.Settings); err != nil {
		t.Fatal(err)
	}
	if err := workspace.Create(dir, "Other"); err != nil {
		t.Fatal(err)
	}
	envs := &env.Store{Environments: []env.Environment{{Name: "Secret"}}, Active: 0}
	requests := &collection.Store{Folders: []*collection.Folder{{Name: "Hidden"}}}
	paths := []string{
		files.Environments, files.Requests,
		workspace.Environments(dir, "Other"), workspace.Requests(dir, "Other"),
	}
	for _, path := range []string{paths[0], paths[2]} {
		if err := envs.Save(path); err != nil {
			t.Fatal(err)
		}
	}
	for _, path := range []string{paths[1], paths[3]} {
		if err := requests.Save(path); err != nil {
			t.Fatal(err)
		}
	}

	contents := func() []string {
		var all []string
		for _, path := range paths {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Error(err)
			}
			all = append(all, string(data))
		}
		return all
	}
	allEncrypted := func() {
		t.Helper()
		for i, data := range contents() {
			if !crypt.Encrypted([]byte(data)) {
				t.Errorf("%s is not encrypted", paths[i])
			}
		}
	}
	// loaded reports if the window shows the workspace and saves to it.
	loaded := func(w *Window, environments int) bool {
		return w.environments.list.Count() == environments && topCount(w.requests.tree) == 1 &&
			topFolder(w.requests.tree, 0).Text(colName) == "Hidden" &&
			w.environments.path == files.Environments && w.requests.path == files.Requests
	}
	// empty reports if the window shows nothing and saves nowhere.
	empty := func(w *Window) bool {
		return w.environments.list.Count() == 0 && topCount(w.requests.tree) == 0 &&
			w.environments.path == "" && w.requests.path == "" &&
			strings.Contains(w.StatusBar().CurrentMessage(), "needs a pair of SSH keys")
	}

	// Without keys nothing is read or saved, and the user is told so once
	// the window is shown.
	before := contents()
	advised = 0
	var w *Window
	mainthread.Wait(func() {
		w = NewWindow(files.Files)
		w.Show()
		if !empty(w) {
			t.Errorf("without keys: status %q", w.StatusBar().CurrentMessage())
		}
		w.environments.add()
	})
	waitFor(t, "being told that keys are needed", func() bool { return advised == 1 })
	if !reflect.DeepEqual(contents(), before) {
		t.Error("files were written without keys")
	}

	mainthread.Wait(func() {
		// Naming the keys in the settings encrypts the files of every
		// workspace and shows the one in use.
		s := w.config
		s.PublicKey, s.PrivateKey = public, private
		if err := w.applySettings(s); err != nil {
			t.Error(err)
		}
		if asked != 1 {
			t.Errorf("the passphrase was asked for %d times", asked)
		}
		allEncrypted()
		if !loaded(w, 1) || w.StatusBar().CurrentMessage() != "" {
			t.Errorf("the workspace was not loaded: status %q", w.StatusBar().CurrentMessage())
		}

		// What is saved afterwards is encrypted too.
		w.environments.add()
		allEncrypted()
		if !loaded(w, 2) {
			t.Error("an environment was not added")
		}

		// Keys that are not a pair are refused, and the ones there are kept.
		s = w.config
		s.PublicKey = otherPublic
		if err := w.applySettings(s); err == nil || !strings.Contains(err.Error(), "cannot be used") {
			t.Errorf("err = %v", err)
		}
		if w.config.PublicKey != public || w.keys.Err() != nil || !loaded(w, 2) {
			t.Errorf("settings changed: %+v", w.config)
		}
		w.Close()
	})
	if saved := settings.Load(files.Settings); saved.PublicKey != public || saved.PrivateKey != private {
		t.Errorf("saved settings = %+v", saved)
	}

	// The next run asks for the passphrase and reads the files.
	asked, advised = 0, 0
	mainthread.Wait(func() {
		w = NewWindow(files.Files)
		w.Show()
		if asked != 1 {
			t.Errorf("the passphrase was asked for %d times", asked)
		}
		if !loaded(w, 2) {
			t.Error("the encrypted workspace was not loaded")
		}

		// Other keys encrypt every file again.
		s := w.config
		s.PublicKey, s.PrivateKey = otherPublic, otherPrivate
		if err := w.applySettings(s); err != nil {
			t.Error(err)
		}
		allEncrypted()
		other, err := crypt.Open(otherPublic, otherPrivate, nil)
		if err != nil {
			t.Error(err)
			return
		}
		for _, path := range []string{paths[0], paths[2]} {
			if got, err := env.LoadWith(path, other); err != nil || len(got.Environments) == 0 {
				t.Errorf("%s: %+v, %v", path, got, err)
			}
		}
		if advised != 0 {
			t.Errorf("told that keys are needed %d times", advised)
		}

		// Taking the keys away leaves the files as they are, shows
		// nothing, and tells the user.
		before = contents()
		s = w.config
		s.PublicKey, s.PrivateKey = "", ""
		if err := w.applySettings(s); err != nil {
			t.Error(err)
		}
		if advised != 1 || !empty(w) {
			t.Errorf("keys taken away: advised %d times, status %q", advised, w.StatusBar().CurrentMessage())
		}
		w.environments.add()
		if !reflect.DeepEqual(contents(), before) {
			t.Error("files were changed when the keys were taken away")
		}

		// Putting them back shows what is in the files.
		s = w.config
		s.PublicKey, s.PrivateKey = otherPublic, otherPrivate
		if err := w.applySettings(s); err != nil {
			t.Error(err)
		}
		if !loaded(w, 2) {
			t.Error("the workspace was not loaded once the keys were given")
		}
		w.Close()
	})
}

func TestEncryptionSettings(t *testing.T) {
	mainthread.Wait(func() {
		current := settings.Settings{PublicKey: "/keys/id.pub", PrivateKey: "/keys/id"}
		d := newSettingsDialog(nil, current, Files{Workspaces: "workspaces"})
		defer d.DeleteLater()

		if d.publicKey.Text() != "/keys/id.pub" || d.privateKey.Text() != "/keys/id" {
			t.Errorf("keys shown = %q, %q", d.publicKey.Text(), d.privateKey.Text())
		}
		d.publicKey.SetText("  /other/id.pub ")
		d.privateKey.SetText("")
		if got := d.settings(current); got.PublicKey != "/other/id.pub" || got.PrivateKey != "" {
			t.Errorf("settings = %+v", got)
		}
	})
}

func TestExportAndImportWorkspaces(t *testing.T) {
	files := testFiles(t)
	keyDir := t.TempDir()
	theirPublic, theirPrivate := crypttest.Pair(t, keyDir, "theirs", "")
	archive := filepath.Join(t.TempDir(), "export.zip")

	// Someone else has a window of their own, with their own keys.
	theirFiles := testFiles(t)
	if err := (settings.Settings{PublicKey: theirPublic, PrivateKey: theirPrivate}).Save(theirFiles.Settings); err != nil {
		t.Fatal(err)
	}

	mainthread.Wait(func() {
		w := NewWindow(files.Files)
		w.environments.add()
		w.requests.addFolder()

		// The workspaces are exported for their public key alone.
		if err := w.exportWorkspaces(theirPublic, archive); err != nil {
			t.Error(err)
			return
		}
		// They are not encrypted for the keys they came from.
		if _, err := w.importWorkspaces(archive); err == nil {
			t.Error("imported workspaces that were exported for someone else")
		}
		if !reflect.DeepEqual(w.workspaces.names, []string{"Default"}) {
			t.Errorf("workspaces = %q", w.workspaces.names)
		}
		if err := w.exportWorkspaces(filepath.Join(keyDir, "none.pub"), archive); err == nil {
			t.Error("exported for a key that is not there")
		}
	})

	mainthread.Wait(func() {
		w := NewWindow(theirFiles.Files)
		added, err := w.importWorkspaces(archive)
		if err != nil || !reflect.DeepEqual(added, []string{"Default 2"}) {
			t.Errorf("added = %q, %v", added, err)
			return
		}
		// The workspace in use stays so, and the new one is listed.
		if !reflect.DeepEqual(w.workspaces.names, []string{"Default", "Default 2"}) || w.workspaces.current() != "Default" || w.environments.list.Count() != 0 {
			t.Errorf("workspaces = %q, current %q", w.workspaces.names, w.workspaces.current())
		}

		// It can be read with their keys.
		w.workspaces.open("Default 2")
		if w.environments.list.Count() != 1 || topCount(w.requests.tree) != 1 || w.StatusBar().CurrentMessage() != "" {
			t.Errorf("the imported workspace was not loaded: status %q", w.StatusBar().CurrentMessage())
		}
	})
}

func TestSettingsTabs(t *testing.T) {
	mainthread.Wait(func() {
		d := newSettingsDialog(nil, settings.Settings{}, Files{Workspaces: "workspaces"})
		defer d.DeleteLater()

		workspaces, ui := d.tabs.Widget(settingsTabWorkspaces), d.tabs.Widget(settingsTabInterface)
		for _, widget := range []*qt.QWidget{d.workspaces.QWidget, d.publicKey.QWidget, d.privateKey.QWidget, d.exportButton.QWidget, d.importButton.QWidget} {
			if !workspaces.IsAncestorOf(widget) {
				t.Error("something is missing from the Workspaces tab")
			}
		}
		if !ui.IsAncestorOf(d.font.QWidget) || !ui.IsAncestorOf(d.swatches[colourSend].button.QWidget) {
			t.Error("the font or the colours are not on the User interface tab")
		}

		// Every colour has a row of the table, under a heading for its
		// section which is as wide as the table.
		table := d.colourList
		if want := len(colourSections) + 1 + len(colourKinds) + len(tokenKinds); table.RowCount() != want {
			t.Errorf("the table of colours has %d rows, want %d", table.RowCount(), want)
		}
		if table.Item(0, colColourName).Text() != "Sidebar" || table.ColumnSpan(0, colColourName) != 2 || table.Item(1, colColourName).Text() != colourLabel(colourWorkspaces) {
			t.Errorf("first rows = %q, %q", table.Item(0, colColourName).Text(), table.Item(1, colColourName).Text())
		}
		for kind, s := range d.swatches {
			if !table.IsAncestorOf(s.button.QWidget) {
				t.Errorf("the swatch of %v is not in the table", kind)
			}
		}
		for kind, s := range d.json {
			if !table.IsAncestorOf(s.button.QWidget) {
				t.Errorf("the swatch of JSON %v is not in the table", kind)
			}
		}

		// The buttons are given the dialog, to be the parent of what they
		// show.
		exported, imported := 0, 0
		d.onExport = func(parent *qt.QWidget) { exported++ }
		d.onImport = func(parent *qt.QWidget) { imported++ }
		d.exportButton.Click()
		d.importButton.Click()
		if exported != 1 || imported != 1 {
			t.Errorf("exported %d times, imported %d times", exported, imported)
		}
	})
}

func TestF2Renames(t *testing.T) {
	files := testFiles(t)
	f2 := func(widget *qt.QWidget) {
		event := qt.NewQKeyEvent(qt.QEvent__KeyPress, int(qt.Key_F2), qt.NoModifier)
		defer event.Delete()
		qt.QCoreApplication_SendEvent(widget.QObject, event.QEvent)
	}

	mainthread.Wait(func() {
		w := NewWindow(files.Files)
		w.Show()

		// A workspace and a folder have their names opened for editing.
		w.sidebar.SetCurrentIndex(tabWorkspaces)
		f2(w.workspaces.list.QWidget)
		if w.workspaces.list.State() != qt.QAbstractItemView__EditingState {
			t.Error("F2 did not start renaming the workspace")
		}

		w.requests.addFolder()
		w.requests.tree.SetFocus()
		f2(w.requests.tree.QWidget)
		if w.requests.tree.State() != qt.QAbstractItemView__EditingState {
			t.Error("F2 did not start renaming the folder")
		}

		// An environment is opened in the editor, with the keyboard in its
		// name and the name selected.
		w.environments.add()
		w.closeEditor()
		w.sidebar.SetCurrentIndex(tabEnvironments)
		w.environments.list.SetFocus()
		f2(w.environments.list.QWidget)
		if w.stack.CurrentIndex() != pageEditor {
			t.Error("F2 did not open the environment editor")
		}
		// A window that is not on a screen is not the active one, so it is
		// asked which of its widgets the keyboard goes to.
		focused := w.FocusWidget()
		if focused == nil || focused.UnsafePointer() != w.editor.name.QWidget.UnsafePointer() {
			t.Error("the name does not have the keyboard")
		}
		if w.editor.name.SelectedText() != w.editor.name.Text() || w.editor.name.Text() == "" {
			t.Errorf("the name is not selected: %q of %q", w.editor.name.SelectedText(), w.editor.name.Text())
		}
		w.Close()
	})
}

func TestFocusSidebar(t *testing.T) {
	mainthread.Wait(func() {
		w := NewWindow(testFiles(t).Files)
		w.Show()

		// A window that is not on a screen is not the active one, so it is
		// asked which of its widgets the keyboard goes to.
		lists := map[int]*qt.QWidget{
			tabWorkspaces:   w.workspaces.list.QWidget,
			tabEnvironments: w.environments.list.QWidget,
			tabRequests:     w.requests.tree.QWidget,
		}
		for _, tab := range []int{tabWorkspaces, tabEnvironments, tabRequests} {
			w.focusSidebar(tab)
			focused := w.FocusWidget()
			if w.sidebar.CurrentIndex() != tab || focused == nil || focused.UnsafePointer() != lists[tab].UnsafePointer() {
				t.Errorf("tab %d: shown %d, and its list does not have the keyboard", tab, w.sidebar.CurrentIndex())
			}
		}
		if w.workspaces.current() != workspace.Default {
			t.Errorf("the workspace in use changed to %q", w.workspaces.current())
		}
		w.Close()
	})
}

func TestSets(t *testing.T) {
	files := testFiles(t)
	store := &collection.Store{}
	one := store.AddFolder("One")
	one.Add(collection.NewREST("A", rest.Request{Method: "GET", URL: "a.test"}))
	one.Add(collection.NewREST("B", rest.Request{Method: "GET", URL: "b.test"}))
	two := store.AddFolder("Two")
	two.Add(collection.NewREST("C", rest.Request{Method: "GET", URL: "c.test"}))
	two.Add(collection.NewREST("D", rest.Request{Method: "GET", URL: "d.test"}))
	if err := store.Save(files.Requests); err != nil {
		t.Fatal(err)
	}

	var w *Window
	check := func(what string, want ...string) {
		t.Helper()
		if got := tabNames(w); !reflect.DeepEqual(got, want) {
			t.Errorf("%s: tabs = %q, want %q", what, got, want)
		}
	}
	setNames := func() []string {
		var names []string
		for _, set := range w.requests.store.Sets {
			name := set.Name + "["
			for i, r := range set.Requests {
				if i > 0 {
					name += " "
				}
				name += strconv.Quote(r.Name)
			}
			names = append(names, name+"]")
		}
		return names
	}
	// drag moves a tab as dragging it does: pressed, moved, and let go.
	drag := func(from, to int) {
		centre := w.tabs.bar.TabRect(from).Center()
		at := qt.NewQPointF3(float64(centre.X()), float64(centre.Y()))
		press := qt.NewQMouseEvent(qt.QEvent__MouseButtonPress, at, qt.LeftButton, qt.LeftButton, qt.NoModifier)
		defer press.Delete()
		qt.QCoreApplication_SendEvent(w.tabs.bar.QObject, press.QEvent)
		w.tabs.bar.MoveTab(from, to)
		release := qt.NewQMouseEvent(qt.QEvent__MouseButtonRelease, at, qt.LeftButton, qt.NoButton, qt.NoModifier)
		defer release.Delete()
		qt.QCoreApplication_SendEvent(w.tabs.bar.QObject, release.QEvent)
	}

	mainthread.Wait(func() {
		w = NewWindow(files.Files)
		w.Show()
		rt := w.requests
		tree := rt.tree
		pick := func(folder, name string) { tree.SetCurrentItem(treeItem(tree, folder, name)) }
		pin := func(folder, name string) {
			pick(folder, name)
			w.tabs.current().button.Click()
		}

		// The views have icons, and the sets are on a page of their own.
		if rt.views.Count() != 2 || rt.views.TabIcon(viewRequests).IsNull() || rt.views.TabIcon(viewSets).CacheKey() != setIcon().CacheKey() {
			t.Error("the views have no icons")
		}
		rt.views.SetCurrentIndex(viewSets)
		if rt.pages.CurrentIndex() != viewSets {
			t.Error("the sets are not shown")
		}

		// A set added holds the pinned tabs, from any folder, and is the
		// one chosen.
		pin("One", "A")
		pin("Two", "C")
		check("pinned", "A*", "C*")
		rt.sets.add()
		if got := setNames(); !reflect.DeepEqual(got, []string{`New Set["A" "C"]`}) || rt.sets.active != rt.store.Sets[0] {
			t.Errorf("sets = %q", got)
		}
		rt.sets.list.CurrentItem().SetText("Work")
	})
	waitFor(t, "the set to be renamed", func() bool {
		return mainthread.Wait2(func() bool {
			return w.requests.sets.list.Item(0).Text() == "Work" && w.requests.store.Sets[0].Name == "Work"
		})
	})

	mainthread.Wait(func() {
		rt := w.requests
		tree := rt.tree
		pick := func(folder, name string) { tree.SetCurrentItem(treeItem(tree, folder, name)) }

		// A second set, made while the first is chosen, starts with its
		// tabs, and the tabs pinned and unpinned are kept in it.
		rt.sets.add()
		doubleClick(w.tabs.tabs[0].button) // A, which is not shown
		pick("Two", "D")
		w.tabs.current().button.Click()
		check("second set", "C*", "D*")
		if got := setNames(); !reflect.DeepEqual(got, []string{`New Set["C" "D"]`, `Work["A" "C"]`}) {
			t.Errorf("sets = %q", got)
		}

		// Choosing the first replaces the tabs with its own.
		rt.sets.list.SetCurrentItem(rt.sets.list.Item(1))
		check("first set", "A*", "C*")
		if w.open == nil || w.open.Name != "A" || rt.sets.active.Name != "Work" {
			t.Errorf("open %+v", w.open)
		}

		// Deselecting the set takes its tabs away, leaving one empty tab,
		// which belongs to no set: pinning changes none.
		rt.sets.list.ClearSelection()
		if rt.sets.active != nil {
			t.Fatal("a set is chosen with none selected")
		}
		check("deselected", "Untitled")
		if w.open != nil {
			t.Errorf("open after deselecting: %+v", w.open)
		}
		pick("One", "B")
		w.tabs.current().button.Click()
		check("pinned with none chosen", "B*")
		if got := setNames(); !reflect.DeepEqual(got, []string{`New Set["C" "D"]`, `Work["A" "C"]`}) {
			t.Errorf("sets changed while none was chosen: %q", got)
		}
		rt.sets.list.SetCurrentItem(rt.sets.list.Item(1))
		check("first set again", "A*", "C*")

		// Tabs are dragged into another order, which the set keeps.
		drag(0, 1)
		check("moved", "C*", "A*")
		if got := setNames()[1]; got != `Work["C" "A"]` {
			t.Errorf("after the move: %q", got)
		}

		// A tab that is not pinned is pinned by being moved.
		pick("One", "B")
		check("B", "C*", "A*", "B")
		drag(2, 0)
		check("B moved", "B*", "C*", "A*")
		if got := setNames()[1]; got != `Work["B" "C" "A"]` {
			t.Errorf("after pinning by moving: %q", got)
		}

		// A tab dragged past New leaves New at the end.
		drag(0, 3)
		check("B moved to the end", "C*", "A*", "B*")
		if got := setNames()[1]; got != `Work["C" "A" "B"]` || w.tabs.bar.TabText(3) != "New" || w.tabs.newAt != 3 {
			t.Errorf("after moving past New: %q, last tab %q", got, w.tabs.bar.TabText(3))
		}
		drag(2, 0)

		// Renaming a request keeps it in the set, and removing one takes
		// it out of every set.
		treeItem(tree, "Two", "C").SetText(colName, "Charlie")
	})
	waitFor(t, "the rename to be sorted", func() bool {
		return mainthread.Wait2(func() bool { return treeItem(w.requests.tree, "Two", "Charlie") != nil })
	})
	mainthread.Wait(func() {
		rt := w.requests
		rt.confirm = func(string) bool { return true }
		if got := setNames(); !reflect.DeepEqual(got, []string{`New Set["Charlie" "D"]`, `Work["B" "Charlie" "A"]`}) {
			t.Errorf("after the rename: %q", got)
		}
		rt.tree.SetCurrentItem(treeItem(rt.tree, "Two", "Charlie"))
		rt.remove()
		if got := setNames(); !reflect.DeepEqual(got, []string{`New Set["D"]`, `Work["B" "A"]`}) {
			t.Errorf("after the removal: %q", got)
		}
		// What was beside it is opened in the tab that is not pinned.
		check("after the removal", "B*", "A*", "D")

		// The search narrows the sets while they are shown.
		rt.search.SetText("wor")
		if !rt.sets.list.Item(0).IsHidden() || rt.sets.list.Item(1).IsHidden() {
			t.Error("the search does not narrow the sets")
		}
		rt.search.SetText("")
	})

	// The set chosen is chosen again when its workspace is gone back to.
	mainthread.Wait(func() {
		w.workspaces.warn = func(message string) { t.Error(message) }
		w.workspaces.add()
		if w.requests.sets.active != nil {
			t.Errorf("chosen in a new workspace: %+v", w.requests.sets.active)
		}
		w.workspaces.list.SetCurrentRow(0)
		if w.requests.sets.active == nil || w.requests.sets.active.Name != "Work" || w.requests.sets.selected() != w.requests.sets.active {
			t.Errorf("chosen back in the first workspace: %+v", w.requests.sets.active)
		}
		check("back in the first workspace", "B*", "A*")
	})

	// The sets are saved, but none is chosen at the next run, and nothing is
	// selected.
	saved, err := loadRequests(files.Requests)
	if err != nil || len(saved.Sets) != 2 || saved.Sets[1].Name != "Work" || len(saved.Sets[1].Requests) != 2 {
		t.Fatalf("saved %+v", saved.Sets)
	}
	mainthread.Wait(func() {
		w.Close()
		w = NewWindow(files.Files)
		if w.requests.sets.active != nil || w.requests.sets.selected() != nil || w.requests.tree.CurrentItem() != nil || w.open != nil {
			t.Errorf("at the next run: set %+v, open %+v", w.requests.sets.active, w.open)
		}
		check("next run", "Untitled")

		// Removing a set leaves the tabs alone, and none is chosen.
		w.requests.sets.list.SetCurrentItem(w.requests.sets.list.Item(1))
		check("Work chosen", "B*", "A*")
		w.requests.confirm = func(string) bool { return true }
		w.requests.sets.remove()
		if len(w.requests.store.Sets) != 1 || w.requests.sets.active != nil {
			t.Errorf("after removing: %q", setNames())
		}
		check("after removing", "B*", "A*")
	})
}

func TestNewTabAndSave(t *testing.T) {
	files := testFiles(t)

	mainthread.Wait(func() {
		w := NewWindow(files.Files)
		w.Show()
		click := func(index int) {
			centre := w.tabs.bar.TabRect(index).Center()
			at := qt.NewQPointF3(float64(centre.X()), float64(centre.Y()))
			for _, kind := range []qt.QEvent__Type{qt.QEvent__MouseButtonPress, qt.QEvent__MouseButtonRelease} {
				buttons := qt.LeftButton
				if kind == qt.QEvent__MouseButtonRelease {
					buttons = qt.NoButton
				}
				event := qt.NewQMouseEvent(kind, at, qt.LeftButton, buttons, qt.NoModifier)
				qt.QCoreApplication_SendEvent(w.tabs.bar.QObject, event.QEvent)
				event.Delete()
			}
		}

		// Save is beside Send, for the request that is not saved.
		if w.request.save.IsHidden() || w.request.save.Text() != "Save" {
			t.Error("Save is not there for the request that is not saved")
		}
		w.request.url.SetText("example.test/users?page=2")
		w.request.save.Click()
		if w.open == nil || w.open.Name != "New Request" || w.open.URL != "example.test/users" || len(w.open.Params) != 1 {
			t.Fatalf("saved %+v", w.open)
		}
		if got := tabNames(w); !reflect.DeepEqual(got, []string{"New Request"}) || !w.request.save.IsHidden() {
			t.Errorf("after saving: tabs %q, Save shown %t", got, !w.request.save.IsHidden())
		}

		// New is the last tab, and is never shown: clicking it shows a new
		// request of the type chosen, which is not saved until Save is
		// pressed.
		saved := w.open
		w.tabs.current().button.Click()
		if w.tabs.bar.Count() != 2 || w.tabs.bar.TabText(1) != "New" {
			t.Fatalf("tabs %d, last %q", w.tabs.bar.Count(), w.tabs.bar.TabText(1))
		}
		defer func(prev collection.Type) { testRequestType = prev }(testRequestType)
		testRequestType = ""
		click(w.tabs.newAt)
		if w.open != saved || w.tabs.bar.CurrentIndex() != 0 {
			t.Errorf("after deciding against a request: open %+v, tab %d", w.open, w.tabs.bar.CurrentIndex())
		}
		count := func() int {
			n := 0
			for _, f := range w.requests.store.Folders {
				n += len(f.All())
			}
			return n
		}
		testRequestType = collection.TypeGRPC
		click(w.tabs.newAt)
		if w.open != nil || w.stack.CurrentIndex() != pageGRPC || w.grpc.save.IsHidden() || count() != 1 {
			t.Errorf("after New: open %+v, page %d, %d saved", w.open, w.stack.CurrentIndex(), count())
			return
		}
		if got := tabNames(w); !reflect.DeepEqual(got, []string{"New Request*", "Untitled"}) || w.tabs.bar.TabText(2) != "New" {
			t.Errorf("after New: tabs %q", got)
		}

		// It is kept while another tab is shown.
		w.grpc.url.SetText("localhost:1")
		w.tabs.bar.SetCurrentIndex(0)
		if w.open != saved || w.stack.CurrentIndex() != pageRequest {
			t.Errorf("back on the saved tab: open %+v, page %d", w.open, w.stack.CurrentIndex())
		}
		w.tabs.bar.SetCurrentIndex(1)
		if w.open != nil || w.stack.CurrentIndex() != pageGRPC || w.grpc.url.Text() != "localhost:1" || count() != 1 {
			t.Errorf("back on the new tab: open %+v, page %d, address %q", w.open, w.stack.CurrentIndex(), w.grpc.url.Text())
		}

		// Save beside Invoke adds it to the sidebar.
		w.grpc.save.Click()
		if w.open == nil || w.open.Type != collection.TypeGRPC || w.open.GRPC.URL != "localhost:1" || count() != 2 || !w.grpc.save.IsHidden() {
			t.Errorf("after saving: open %+v, %d saved", w.open, count())
		}

		// A REST request from New is not saved either.
		testRequestType = collection.TypeREST
		click(w.tabs.newAt)
		if w.open != nil || w.stack.CurrentIndex() != pageRequest || w.request.url.Text() != "" || count() != 2 {
			t.Errorf("after New: open %+v, page %d, %d saved", w.open, w.stack.CurrentIndex(), count())
		}

		// Choosing New otherwise, as the wheel does, leaves the tab shown.
		w.tabs.bar.SetCurrentIndex(0)
		w.tabs.bar.SetCurrentIndex(w.tabs.newAt)
		if w.tabs.bar.CurrentIndex() != 0 || w.tabs.current() != w.tabs.tabs[0] {
			t.Errorf("New was shown: tab %d", w.tabs.bar.CurrentIndex())
		}
	})
}

func TestSetSelector(t *testing.T) {
	files := testFiles(t)
	store := &collection.Store{}
	folder := store.AddFolder("F")
	a := folder.Add(collection.NewREST("A", rest.Request{Method: "GET"}))
	b := folder.Add(collection.NewREST("B", rest.Request{Method: "GET"}))
	store.AddSet("Work", []*collection.Request{a, b})
	store.AddSet("Play", []*collection.Request{b})
	if err := store.Save(files.Requests); err != nil {
		t.Fatal(err)
	}

	mainthread.Wait(func() {
		w := NewWindow(files.Files)
		w.Show()
		combo := w.environment.set
		items := func() []string {
			var names []string
			for i := range combo.Count() {
				names = append(names, combo.ItemText(i))
			}
			return names
		}

		// The sets are listed with None chosen to begin with.
		if got := items(); !reflect.DeepEqual(got, []string{"None", "Play", "Work"}) || combo.CurrentText() != "None" {
			t.Errorf("sets %q, chosen %q", got, combo.CurrentText())
		}

		// Choosing one there opens its tabs, and selects it in the list.
		combo.SetCurrentIndex(2)
		if got := tabNames(w); !reflect.DeepEqual(got, []string{"A*", "B*"}) || w.requests.sets.active == nil || w.requests.sets.selected() != w.requests.sets.active {
			t.Errorf("after choosing Work: tabs %q", got)
		}

		// Choosing one in the list is shown there too.
		w.requests.sets.list.SetCurrentItem(w.requests.sets.list.Item(0))
		if combo.CurrentText() != "Play" || !reflect.DeepEqual(tabNames(w), []string{"B*"}) {
			t.Errorf("after choosing Play in the list: %q, tabs %q", combo.CurrentText(), tabNames(w))
		}

		// None chooses no set, which takes the tabs away.
		combo.SetCurrentIndex(0)
		if w.requests.sets.active != nil || w.requests.sets.selected() != nil || !reflect.DeepEqual(tabNames(w), []string{"Untitled"}) {
			t.Errorf("after None: tabs %q", tabNames(w))
		}

		// A set added is listed.
		w.requests.sets.add()
		if got := items(); !reflect.DeepEqual(got, []string{"None", "New Set", "Play", "Work"}) || combo.CurrentText() != "New Set" {
			t.Errorf("after adding: %q, chosen %q", got, combo.CurrentText())
		}
	})
}

func TestCopyPasteAcrossWorkspaces(t *testing.T) {
	files := testFiles(t)
	envs := &env.Store{Active: 0, Environments: []env.Environment{{Name: "Local", Variables: []env.Variable{{Key: "token", Value: "secret", Enabled: true}}}}}
	if err := envs.Save(files.Environments); err != nil {
		t.Fatal(err)
	}
	store := &collection.Store{}
	hotel := store.AddFolder("Hotel")
	hotel.Add(collection.NewREST("Book", rest.Request{Method: "POST", URL: "{{host}}/rooms"}))
	hotel.AddFolder("Inner").Add(collection.NewGRPC("Call", rpc.Request{URL: "c.test:1"}))
	if err := store.Save(files.Requests); err != nil {
		t.Fatal(err)
	}

	mainthread.Wait(func() {
		w := NewWindow(files.Files)
		w.Show()
		rt, el := w.requests, w.environments
		tree := rt.tree
		key := func(widget *qt.QWidget, k qt.Key) {
			event := qt.NewQKeyEvent(qt.QEvent__KeyPress, int(k), qt.ControlModifier)
			defer event.Delete()
			qt.QCoreApplication_SendEvent(widget.QObject, event.QEvent)
		}
		clipboard := qt.QGuiApplication_Clipboard().Text()

		// Copy a request, a folder and an environment in one workspace.
		tree.SetCurrentItem(treeItem(tree, "Hotel", "Book"))
		key(tree.QWidget, qt.Key_C)
		book := rt.copied
		tree.SetCurrentItem(treeItem(tree, "Hotel", "Inner"))
		key(tree.QWidget, qt.Key_C)
		el.list.SetCurrentRow(0)
		key(el.list.QWidget, qt.Key_C)
		if _, ok := rt.copied.(*collection.Folder); !ok || book == nil || el.copied == nil || el.copied.Name != "Local" {
			t.Fatalf("copied %+v and %+v", rt.copied, el.copied)
		}
		if qt.QGuiApplication_Clipboard().Text() != clipboard {
			t.Error("the system clipboard was used")
		}

		// Paste them into another.
		w.workspaces.warn = func(message string) { t.Error(message) }
		w.workspaces.add()
		if topCount(tree) != 0 || el.list.Count() != 0 {
			t.Fatalf("the new workspace is not empty")
		}
		key(tree.QWidget, qt.Key_V)
		if topCount(tree) != 1 || treeItem(tree, "Inner", "Call") == nil {
			t.Errorf("folder not pasted at the top level")
		}
		rt.copied = book
		tree.SetCurrentItem(treeItem(tree, "Inner"))
		key(tree.QWidget, qt.Key_V)
		key(tree.QWidget, qt.Key_V)
		if treeItem(tree, "Inner", "Book") == nil || treeItem(tree, "Inner", "Book copy") == nil || w.open == nil || w.open.Name != "Book copy" {
			t.Errorf("requests not pasted into the folder; open %+v", w.open)
		}
		key(el.list.QWidget, qt.Key_V)
		key(el.list.QWidget, qt.Key_V)
		if el.list.Count() != 2 || el.list.Item(0).Text() != "Local" || el.list.Item(1).Text() != "Local copy" || w.expand("{{token}}") != "secret" {
			t.Errorf("environments %d, token %q", el.list.Count(), w.expand("{{token}}"))
		}
		w.Close()
	})

	// The workspace pasted into has them saved, and the one copied from is
	// as it was.
	saved, err := loadRequests(workspace.Requests(filepath.Dir(filepath.Dir(files.Requests)), "New Workspace"))
	if err != nil || len(saved.Folders) != 1 || len(saved.Folders[0].Requests) != 3 || saved.Folders[0].Requests[0].Name != "Book" || saved.Folders[0].Requests[0].URL != "{{host}}/rooms" {
		t.Errorf("saved %+v, %v", saved, err)
	}
	original, err := loadRequests(files.Requests)
	if err != nil || len(original.Folders) != 1 || len(original.Folders[0].Folders[0].Requests) != 1 {
		t.Errorf("original %+v, %v", original, err)
	}
}

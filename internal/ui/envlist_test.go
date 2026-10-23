package ui

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	qt "github.com/mappu/miqt/qt6"
	"github.com/mappu/miqt/qt6/mainthread"

	"github.com/nomad-software/triggerapi/internal/env"
	"github.com/nomad-software/triggerapi/internal/rest"
)

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

	writer.Flush()
	if data, _ := os.ReadFile(path); string(data) != "{oops" {
		t.Errorf("file was overwritten: %q", data)
	}
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

package ui

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	qt "github.com/mappu/miqt/qt6"
	"github.com/mappu/miqt/qt6/mainthread"

	"github.com/nomad-software/triggerapi/internal/collection"
	"github.com/nomad-software/triggerapi/internal/rest"
	"github.com/nomad-software/triggerapi/internal/settings"
	"github.com/nomad-software/triggerapi/internal/workspace"
)

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

func TestHistorySettingsDialog(t *testing.T) {
	mainthread.Wait(func() {
		current := settings.Settings{ResponseFont: "Monospace"}
		d := newSettingsDialog(nil, current, Files{})
		defer d.DeleteLater()

		// The history has a tab of its own, before the User interface one,
		// and is kept and at its default to begin with, which is not
		// recorded.
		page := d.tabs.Widget(settingsTabHistory)
		if d.tabs.TabText(settingsTabHistory) != "History" || settingsTabHistory != settingsTabInterface-1 {
			t.Errorf("tab %d is %q", settingsTabHistory, d.tabs.TabText(settingsTabHistory))
		}
		for _, widget := range []*qt.QWidget{d.history.QWidget, d.historyLimit.QWidget, d.diffCommand.QWidget} {
			if !page.IsAncestorOf(widget) {
				t.Error("something is missing from the History tab")
			}
		}
		if d.tabs.Widget(settingsTabRequests).IsAncestorOf(d.history.QWidget) {
			t.Error("the history is still on the Requests tab")
		}
		if !d.history.IsChecked() || d.historyLimit.Value() != settings.DefaultHistoryLimit || !d.historyLimit.IsEnabled() {
			t.Errorf("at first: kept %v, limit %d", d.history.IsChecked(), d.historyLimit.Value())
		}
		if got := d.settings(current); got.NoHistory || got.HistoryLimit != 0 || got.DiffCommand != "" {
			t.Errorf("unchanged: %+v", got)
		}
		d.diffCommand.SetText("  meld --newtab ")

		d.historyLimit.SetValue(25)
		d.history.SetChecked(false)
		if d.historyLimit.IsEnabled() {
			t.Error("the limit can be changed with no history kept")
		}
		if got := d.settings(current); !got.NoHistory || got.HistoryLimit != 25 || got.DiffCommand != "meld --newtab" {
			t.Errorf("changed: %+v", got)
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
		if d.tabs.Count() != 4 || d.tabs.TabText(settingsTabRequests) != "Requests" || d.tabs.TabText(settingsTabWorkspaces) != "Workspaces" || d.tabs.TabText(settingsTabHistory) != "History" || d.tabs.TabText(settingsTabInterface) != "User interface" {
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

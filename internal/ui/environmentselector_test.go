package ui

import (
	"reflect"
	"strings"
	"testing"

	qt "github.com/mappu/miqt/qt6"
	"github.com/mappu/miqt/qt6/mainthread"

	"github.com/nomad-software/triggerapi/internal/collection"
	"github.com/nomad-software/triggerapi/internal/rest"
)

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

		// With one, it opens it in the editor, leaving the sidebar on the
		// requests if it is showing them, and otherwise showing the
		// environments.
		for _, from := range []int{tabRequests, tabWorkspaces} {
			w.sidebar.SetCurrentIndex(from)
			selector.environmentTitle.Click()
			want := tabEnvironments
			if from == tabRequests {
				want = tabRequests
			}
			if w.stack.CurrentIndex() != pageEditor || w.editor.name.Text() != "Local" || w.sidebar.CurrentIndex() != want {
				t.Errorf("from tab %d: page = %d, editing %q, sidebar tab %d", from, w.stack.CurrentIndex(), w.editor.name.Text(), w.sidebar.CurrentIndex())
			}
			w.closeEditor()
		}

		// Going to another tab of the sidebar closes the editor, and going
		// to that of the environments does not.
		selector.environmentTitle.Click()
		w.sidebar.SetCurrentIndex(tabEnvironments)
		if w.stack.CurrentIndex() != pageEditor {
			t.Errorf("page = %d after going to the environments", w.stack.CurrentIndex())
		}
		w.sidebar.SetCurrentIndex(tabWorkspaces)
		if w.stack.CurrentIndex() != w.requestPage() {
			t.Errorf("page = %d after going to the workspaces", w.stack.CurrentIndex())
		}

		// The button closes the editor it opened, unless the sidebar shows
		// the environments.
		w.sidebar.SetCurrentIndex(tabEnvironments)
		selector.environmentTitle.Click()
		selector.environmentTitle.Click()
		if w.stack.CurrentIndex() != pageEditor {
			t.Errorf("page = %d after the button was clicked again with the environments shown", w.stack.CurrentIndex())
		}
		w.sidebar.SetCurrentIndex(tabRequests)
		selector.environmentTitle.Click()
		selector.environmentTitle.Click()
		if w.stack.CurrentIndex() != w.requestPage() {
			t.Errorf("page = %d after the button was clicked again", w.stack.CurrentIndex())
		}

		// Going between the requests and the sets closes it.
		w.sidebar.SetCurrentIndex(tabRequests)
		for _, view := range []int{viewSets, viewRequests} {
			selector.environmentTitle.Click()
			w.requests.views.SetCurrentIndex(view)
			if w.stack.CurrentIndex() != w.requestPage() {
				t.Errorf("page = %d after going to view %d", w.stack.CurrentIndex(), view)
			}
		}

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

	// The layout is done once the drop-down has been widened and then
	// squeezed into the window, which can take more than one pass.
	waitFor(t, "the layout", func() bool {
		return mainthread.Wait2(func() bool {
			width := w.environment.environment.Width()
			return width > selectorMinWidth && width < w.Width()
		})
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

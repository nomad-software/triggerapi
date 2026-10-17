package ui

import (
	"net/http"
	"net/http/httptest"
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

// withoutLayout returns the settings without the sizes of the sidebar and
// the response, for tests that are not about them.
func withoutLayout(s settings.Settings) settings.Settings {
	s.SidebarWidth, s.ResponseHeight = 0, 0
	return s
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

func TestSidebarTabs(t *testing.T) {
	mainthread.Wait(func() {
		w := NewWindow(testFiles(t).Files)
		if w.sidebar.Count() != 4 || w.sidebar.TabToolTip(tabWorkspaces) != "Workspaces" || w.sidebar.TabToolTip(tabEnvironments) != "Environments" || w.sidebar.TabToolTip(tabRequests) != "Requests" || w.sidebar.TabToolTip(tabHistory) != "History" {
			t.Errorf("tabs = %d: %q, %q, %q, %q", w.sidebar.Count(), w.sidebar.TabToolTip(0), w.sidebar.TabToolTip(1), w.sidebar.TabToolTip(2), w.sidebar.TabToolTip(3))
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

func TestAboutBox(t *testing.T) {
	for _, c := range []struct {
		text  string
		wants []string
	}{
		{mainthread.Wait2(aboutText), []string{"<h2>TriggerAPI</h2>", "Copyright © 2026 Gary Willoughby", "MCP"}},
		// The notice the LGPL asks for, and the libraries it is built with.
		{mainthread.Wait2(licenceText), []string{"Qt version 6.", "LGPLv3", "https://www.gnu.org/licenses/lgpl-3.0.html", "The Qt Company Ltd", "miqt", "MCP Go SDK", "age"}},
	} {
		for _, want := range c.wants {
			if !strings.Contains(c.text, want) {
				t.Errorf("the about box does not mention %q", want)
			}
		}
		// Nothing is given a colour of its own, which is left to the theme.
		if strings.Contains(c.text, "border") || strings.Contains(c.text, "#") {
			t.Errorf("the about box sets colours: %s", c.text)
		}
	}

	mainthread.Wait(func() {
		d := newAboutDialog(nil)
		defer d.DeleteLater()
		d.Show()
		if d.Width() < aboutWidth {
			t.Errorf("the about box is %d wide", d.Width())
		}
		// The notices are in a frame of the style, not one drawn by a style
		// sheet.
		if d.notices.FrameShape() != qt.QFrame__StyledPanel || d.notices.StyleSheet() != "" || d.StyleSheet() != "" {
			t.Error("the notices are not in a frame of the style")
		}
		d.Close()
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
			tabHistory:      w.history.tree.QWidget,
		}
		// The requests are shown even from the sets.
		w.requests.showView(viewSets)
		for _, tab := range []int{tabWorkspaces, tabEnvironments, tabRequests, tabHistory} {
			w.focusSidebar(tab)
			focused := w.FocusWidget()
			if w.sidebar.CurrentIndex() != tab || focused == nil || focused.UnsafePointer() != lists[tab].UnsafePointer() {
				t.Errorf("tab %d: shown %d, and its list does not have the keyboard", tab, w.sidebar.CurrentIndex())
			}
		}
		if w.requests.views.CurrentIndex() != viewRequests {
			t.Error("the sets are still shown")
		}
		if w.workspaces.current() != workspace.Default {
			t.Errorf("the workspace in use changed to %q", w.workspaces.current())
		}
		w.Close()
	})
}

func TestSelectingRequestsClosesEditor(t *testing.T) {
	files := testFiles(t)
	store := &collection.Store{}
	one := store.AddFolder("One")
	a := collection.NewREST("A", rest.Request{Method: "GET", URL: "a.test"})
	one.Add(a)
	store.Sets = append(store.Sets, &collection.Set{Name: "Set", Requests: []*collection.Request{a}})
	if err := store.Save(files.Requests); err != nil {
		t.Fatal(err)
	}

	mainthread.Wait(func() {
		w := NewWindow(files.Files)
		w.Show()
		w.environments.add()
		w.closeEditor()
		w.sidebar.SetCurrentIndex(tabRequests)

		// Each of these, done with the editor open from the requests, closes
		// it.
		for what, select_ := range map[string]func(){
			"selecting a folder": func() { w.requests.tree.SetCurrentItem(topFolder(w.requests.tree, 0)) },
			"choosing a set":     func() { w.requests.sets.chooseAt(0) },
		} {
			w.requests.tree.SetCurrentItem(nil)
			w.requests.sets.chooseAt(-1)
			w.environment.environmentTitle.Click()
			if w.stack.CurrentIndex() != pageEditor || w.sidebar.CurrentIndex() != tabRequests {
				t.Errorf("before %s: page %d, sidebar tab %d", what, w.stack.CurrentIndex(), w.sidebar.CurrentIndex())
			}
			select_()
			if w.stack.CurrentIndex() != w.requestPage() {
				t.Errorf("page = %d after %s", w.stack.CurrentIndex(), what)
			}
		}
		w.Close()
	})
}

// Escape on a request page with nothing open in place of the request
// leaves the page as it is.
func TestEscapeWithNothingOpen(t *testing.T) {
	files := testFiles(t)
	mainthread.Wait(func() {
		w := NewWindow(files.Files)
		w.Show()
		defer func(prev collection.Type) { testRequestType = prev }(testRequestType)
		for _, ui := range w.uis {
			testRequestType = ui.typ
			w.newRequest()
			w.escape()
			if w.stack.CurrentIndex() != ui.page || ui.response.find.isOpen() {
				t.Errorf("%s: page %d after Escape", ui.typ, w.stack.CurrentIndex())
			}
		}
	})
}

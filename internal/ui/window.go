// Package ui contains the Qt user interface.
package ui

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"

	qt "github.com/mappu/miqt/qt6"
	"github.com/mappu/miqt/qt6/mainthread"

	"github.com/nomad-software/postie/internal/collection"
	"github.com/nomad-software/postie/internal/env"
	"github.com/nomad-software/postie/internal/rest"
	"github.com/nomad-software/postie/internal/settings"
)

// Pages of the main stack.
const (
	pageRequest = iota
	pageEditor
)

const aboutText = "<b>Postie</b><p>A desktop client for making REST API calls.</p>"

// Files are the paths of the JSON files the program's data is kept in.
type Files struct {
	Environments string
	Requests     string
	Settings     string
}

// Window is the main application window.
type Window struct {
	*qt.QMainWindow

	// quit and about are the menu bar actions.
	quit  *qt.QAction
	about *qt.QAction

	client *http.Client
	// sidebar has a tab for the environments and one for the requests.
	sidebar      *qt.QTabWidget
	environments *envList
	requests     *requestTree
	// stack shows either the request UI or the environment editor.
	stack    *qt.QStackedWidget
	editor   *envEditor
	request  *requestPanel
	response *responsePanel

	// open is the saved request shown in the request UI, which edits are
	// written back to. It is nil when the request UI holds an unsaved request.
	open *collection.Request

	// cancel aborts the request in flight. It is nil when idle and must only
	// be accessed from the main thread.
	cancel context.CancelFunc
}

// NewWindow creates the main window, loading the saved data from files. It
// must be called on the main thread after the QApplication has been created.
func NewWindow(files Files) *Window {
	w := &Window{
		QMainWindow: qt.NewQMainWindow2(),
		client:      &http.Client{},
		request:     newRequestPanel(),
		editor:      newEnvEditor(),
	}

	w.SetWindowTitle("Postie")

	// Restore the window size, and remember it again when the window closes.
	saved := settings.Load(files.Settings)
	w.response = newResponsePanel(saved.ResponseFont)
	w.Resize(saved.Width, saved.Height)
	if saved.Maximized {
		w.SetWindowState(qt.WindowMaximized)
	}
	w.OnCloseEvent(func(super func(*qt.QCloseEvent), event *qt.QCloseEvent) {
		if err := w.settings(saved).Save(files.Settings); err != nil {
			// The window is going away, so there is nowhere to show this.
			fmt.Fprintln(os.Stderr, "postie: settings could not be saved:", err)
		}
		super(event)
	})

	// A file that could not be read is never saved to, so it is not
	// overwritten.
	var failed []error

	envs, err := env.Load(files.Environments)
	if err != nil {
		files.Environments = ""
		failed = append(failed, err)
	}
	requests, err := collection.Load(files.Requests)
	if err != nil {
		files.Requests = ""
		failed = append(failed, err)
	}
	if len(failed) > 0 {
		w.StatusBar().ShowMessage("Saved data could not be loaded, changes will not be saved: " + errors.Join(failed...).Error())
	}

	saveFailed := func(err error) {
		w.StatusBar().ShowMessage("Changes could not be saved: " + err.Error())
	}

	w.request.highlight.expand = envs.Expand

	w.environments = newEnvList(envs, files.Environments)
	w.environments.onError = saveFailed
	w.environments.onChange = func() {
		// What the placeholders in the request resolve to may have changed.
		w.updatePreview()
		w.request.refreshPlaceholders()
	}
	w.environments.onEdit = w.openEditor
	w.environments.onSelect = func() {
		// The editor follows the selection while it is open.
		if w.stack.CurrentIndex() == pageEditor {
			w.openEditor()
		}
	}

	w.editor.onChange = w.environments.update
	w.editor.close.OnClicked(w.closeEditor)

	w.requests = newRequestTree(requests, files.Requests)
	w.requests.onError = saveFailed
	w.requests.onOpen = w.openRequest
	w.requests.onRemove = func(r *collection.Request) {
		if w.open == r {
			w.show(nil)
		}
	}
	w.requests.onRename = func(r *collection.Request) {
		if w.open == r {
			w.request.setTitle(r.Name)
		}
	}
	w.requests.initial = func() rest.Request {
		// An unsaved request being composed becomes the new request.
		if w.open == nil {
			return w.request.request()
		}
		return rest.Request{Method: http.MethodGet}
	}

	w.request.onChange = func() {
		w.updatePreview()
		if w.open != nil {
			w.open.Request = w.request.request()
			w.requests.save()
		}
	}
	w.request.send.OnClicked(w.send)
	w.request.url.OnReturnPressed(w.send)
	qt.NewQShortcut2(qt.NewQKeySequence2("Ctrl+Return"), w.QObject).OnActivated(w.send)

	main := qt.NewQSplitter3(qt.Vertical)
	main.AddWidget(w.request.root)
	main.AddWidget(w.response.root)
	main.SetChildrenCollapsible(false)
	main.SetStretchFactor(0, 1)
	main.SetStretchFactor(1, 1)

	w.stack = qt.NewQStackedWidget2()
	w.stack.AddWidget(main.QWidget)
	w.stack.AddWidget(w.editor.root)

	w.sidebar = qt.NewQTabWidget2()
	w.sidebar.AddTab(w.environments.root, "Environments")
	w.sidebar.AddTab(w.requests.root, "Requests")

	splitter := qt.NewQSplitter3(qt.Horizontal)
	splitter.AddWidget(w.sidebar.QWidget)
	splitter.AddWidget(w.stack.QWidget)
	splitter.SetStretchFactor(0, 0)
	splitter.SetStretchFactor(1, 1)
	splitter.SetSizes([]int{280, 744})
	w.SetCentralWidget(splitter.QWidget)

	w.newMenuBar()

	w.request.url.SetFocus()

	return w
}

func (w *Window) newMenuBar() {
	file := w.MenuBar().AddMenuWithTitle("&File")
	w.quit = file.AddActionWithText("&Quit")
	w.quit.SetShortcut(qt.NewQKeySequence2("Ctrl+Q"))
	w.quit.SetMenuRole(qt.QAction__QuitRole)
	// Closing the window saves the settings and ends the program.
	w.quit.OnTriggered(func() { w.Close() })

	help := w.MenuBar().AddMenuWithTitle("&Help")
	w.about = help.AddActionWithText("&About")
	w.about.SetMenuRole(qt.QAction__AboutRole)
	w.about.OnTriggered(func() {
		qt.QMessageBox_About(w.QWidget, "About Postie", aboutText)
	})
}

// settings returns the state to remember for the next run. The size of a
// maximised window is that of the window before it was maximised, falling
// back to the previous setting if the platform does not report one.
func (w *Window) settings(previous settings.Settings) settings.Settings {
	s := previous
	s.Width, s.Height, s.Maximized = w.Width(), w.Height(), w.IsMaximized()
	if s.Maximized {
		normal := w.NormalGeometry()
		s.Width, s.Height = normal.Width(), normal.Height()
		if s.Width <= 0 || s.Height <= 0 {
			s.Width, s.Height = previous.Width, previous.Height
		}
	}
	return s
}

func (w *Window) updatePreview() {
	w.request.updatePreview(w.environments.store.Expand)
}

// openEditor shows the selected environment in place of the request UI. The
// editor is closed instead if no environment is selected.
func (w *Window) openEditor() {
	e, ok := w.environments.store.Current()
	if !ok {
		w.closeEditor()
		return
	}
	w.editor.load(e)
	w.stack.SetCurrentIndex(pageEditor)
}

// closeEditor returns to the request UI.
func (w *Window) closeEditor() {
	w.stack.SetCurrentIndex(pageRequest)
}

// openRequest shows a saved request in the request UI.
func (w *Window) openRequest(r *collection.Request) {
	if w.open != r {
		w.show(r)
	}
	w.closeEditor()
}

// show replaces the contents of the request UI with a saved request, or with
// an empty unsaved one if r is nil.
func (w *Window) show(r *collection.Request) {
	if w.cancel != nil {
		w.cancel()
	}

	w.open = r
	if r != nil {
		w.request.load(r.Request)
		w.request.setTitle(r.Name)
	} else {
		w.request.load(rest.Request{Method: http.MethodGet})
		w.request.setTitle("")
	}
	w.updatePreview()
	w.response.clear(responseIdle)
}

// send starts the request, or cancels it if one is already in flight.
func (w *Window) send() {
	if w.cancel != nil {
		w.cancel()
		return
	}

	req := w.request.request().Expand(w.environments.store.Expand)

	ctx, cancel := context.WithCancel(context.Background())
	w.cancel = cancel
	w.request.send.SetText("Cancel")
	w.response.clear("Sending…")

	// Qt widgets may only be touched from the main thread, so the result is
	// handed back to it.
	go func() {
		res, err := rest.Send(ctx, w.client, req)

		mainthread.Start(func() {
			cancel()
			w.cancel = nil
			w.request.send.SetText("Send")

			switch {
			case errors.Is(err, context.Canceled):
				w.response.clear("Cancelled")
			case err != nil:
				w.response.showError(err)
			default:
				w.response.show(res)
			}
		})
	}()
}

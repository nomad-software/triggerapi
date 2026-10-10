// Package ui contains the Qt user interface.
package ui

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	qt "github.com/mappu/miqt/qt6"
	"github.com/mappu/miqt/qt6/mainthread"

	"github.com/nomad-software/triggerapi/internal/collection"
	"github.com/nomad-software/triggerapi/internal/env"
	"github.com/nomad-software/triggerapi/internal/rest"
	"github.com/nomad-software/triggerapi/internal/rpc"
	"github.com/nomad-software/triggerapi/internal/settings"
	"github.com/nomad-software/triggerapi/internal/workspace"
)

// Pages of the main stack.
const (
	pageRequest = iota
	pageEditor
	pageGRPC
)

// sidebarIconSize is the size of the icons on the tabs of the sidebar.
const sidebarIconSize = 56

// defaultSidebarWidth is the width of the sidebar until it is dragged.
const defaultSidebarWidth = 350

// reflectTimeout limits how long a server is given to describe its services.
const reflectTimeout = 15 * time.Second

// askRequestType asks the user what type of request to create. It reports
// false if they decide against creating one. It is a variable so that tests
// can answer without a dialog being shown.
var askRequestType = func(parent *qt.QWidget) (collection.Type, bool) {
	return newRequestTypeDialog(parent).ask()
}

// aboutText returns the contents of the about box, which carries the notice
// the LGPL asks of programs that use Qt.
// Version is the version of the program, shown in the title bar and the
// about box.
const Version = "v0.0.1"

func aboutText() string {
	// The licence notices are set apart from the description of the program
	// in a bordered box of smaller text. Qt's rich text draws a border
	// around a table but not around a paragraph.
	return "<h3>TriggerAPI " + Version + "</h3>" +
		"<p>A desktop client for making REST API and gRPC calls.</p>" +
		"<p>Copyright © 2026 Gary Willoughby. All rights reserved.</p>" +
		"<table style=\"margin-top: 24px; border: 1px solid #888888; border-collapse: collapse;\" cellpadding=\"10\" width=\"100%\">" +
		"<tr><td style=\"font-size: small;\">" +
		"<p><b>Third party licences</b></p>" +
		"<p>This program uses Qt version " + qt.QLibraryInfo_Version().ToString() + ".</p>" +
		"<p>Qt is a C++ toolkit for cross-platform application development, used here under the " +
		"terms of the <a href=\"https://www.gnu.org/licenses/lgpl-3.0.html\">GNU Lesser General " +
		"Public License version 3</a> (LGPLv3). Qt is dynamically linked, so it can be replaced " +
		"with a different or modified build of the library.</p>" +
		"<p>Qt and its source code are available from <a href=\"https://www.qt.io\">www.qt.io</a>. " +
		"See <a href=\"https://www.qt.io/licensing\">www.qt.io/licensing</a> for an overview of Qt " +
		"licensing.</p>" +
		"<p>Copyright (C) The Qt Company Ltd. and other contributors. Qt and the Qt logo are " +
		"trademarks of The Qt Company Ltd.</p>" +
		"<p>The Go bindings for Qt are provided by <a href=\"https://github.com/mappu/miqt\">miqt</a>, " +
		"used under the MIT licence.</p>" +
		"</td></tr></table>"
}

// Files are the places the program's data is kept in.
type Files struct {
	// Settings is the path of the JSON file of the settings.
	Settings string
	// Workspaces is the folder the workspaces are kept in, unless the
	// settings name another.
	Workspaces string
	// Environments and Requests are the paths of the files the
	// environments and requests were kept in before there were workspaces.
	// What is in them is brought into the first workspace.
	Environments string
	Requests     string
}

// The tabs of the sidebar, in order.
const (
	tabWorkspaces = iota
	tabEnvironments
	tabRequests
)

// Window is the main application window.
type Window struct {
	*qt.QMainWindow

	// preferences, quit and about are the menu bar actions.
	preferences *qt.QAction
	quit        *qt.QAction
	about       *qt.QAction

	// files are the default locations of the saved data, and config the
	// settings in effect, which can keep the workspaces elsewhere.
	files  Files
	config settings.Settings

	client *http.Client
	// sidebar has a tab for the workspaces, and one each for the
	// environments and the requests of the workspace in use.
	sidebar      *qt.QTabWidget
	workspaces   *workspaceList
	environments *envList
	requests     *requestTree
	// split divides the sidebar from the rest of the window, and splits
	// divide the request from the response on each page that has them.
	split  *qt.QSplitter
	splits map[int]*qt.QSplitter
	// responseHeight is the height the response was last dragged to, which
	// every page with a response is given. It is zero if it never was.
	responseHeight int
	// laidOut is set once the saved layout has been restored.
	laidOut bool

	// stack shows the REST request UI, the gRPC request UI or the
	// environment editor.
	stack    *qt.QStackedWidget
	editor   *envEditor
	request  *requestPanel
	response *responsePanel
	// grpc and grpcResponse are the gRPC request UI.
	grpc         *grpcPanel
	grpcResponse *responsePanel

	// open is the saved request shown in the request UI for its type, which
	// edits are written back to. It is nil when the REST request UI holds an
	// unsaved request.
	open *collection.Request
	// shown counts the requests that have been shown, so that results which
	// arrive after another request has been shown can be told apart and
	// dropped.
	shown int

	// schemas are the service definitions of the gRPC servers that have been
	// asked for them, by target.
	schemas map[string]*rpc.Schema
	// reflecting counts the times a service definition has been asked for,
	// so that only the answer to the latest is used.
	reflecting int

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
		grpc:        newGRPCPanel(),
		editor:      newEnvEditor(),
		schemas:     map[string]*rpc.Schema{},
	}

	w.SetWindowTitle("TriggerAPI " + Version)
	w.SetWindowIcon(appIcon())
	// Dialogs take their icon from the application.
	qt.QGuiApplication_SetWindowIcon(appIcon())

	// The status bar has a handle at its right to resize the window by.
	w.StatusBar().SetSizeGripEnabled(true)

	// Restore the window size, and remember it again when the window closes.
	w.files = files
	w.config = settings.Load(files.Settings)
	w.response = newResponsePanel(w.config.ResponseFont)
	w.grpcResponse = newResponsePanel(w.config.ResponseFont)
	w.Resize(w.config.Width, w.config.Height)
	if w.config.Maximized {
		w.SetWindowState(qt.WindowMaximized)
	}
	w.OnCloseEvent(func(super func(*qt.QCloseEvent), event *qt.QCloseEvent) {
		if err := w.settings().Save(files.Settings); err != nil {
			// The window is going away, so there is nowhere to show this.
			fmt.Fprintln(os.Stderr, "triggerapi: settings could not be saved:", err)
		}
		super(event)
	})

	// The workspace that was last in use is opened, which is the first
	// there is if this is the first run with workspaces.
	w.workspaces = newWorkspaceList(w.workspacesDir)
	w.migrate()
	name, err := w.workspaces.load(w.config.Workspace)
	if err != nil {
		w.status("The workspaces could not be listed: " + err.Error())
	}
	w.config.Workspace = name
	envs, envPath, requests, requestsPath := w.loadWorkspace(name)

	saveFailed := func(err error) {
		w.status("Changes could not be saved: " + err.Error())
	}

	// The environments can be replaced by ones from another file, so the
	// current store is looked up each time.
	w.request.highlight.expand = w.expand
	w.grpc.highlight.expand = w.expand

	w.environments = newEnvList(envs, envPath)
	w.environments.onError = saveFailed
	w.environments.onChange = w.environmentChanged
	w.environments.onEdit = w.openEditor
	w.environments.onSelect = func() {
		// The editor follows the selection while it is open.
		if w.stack.CurrentIndex() == pageEditor {
			w.openEditor()
		}
	}

	w.editor.onChange = w.environments.update
	w.editor.close.OnClicked(w.closeEditor)

	w.workspaces.onOpen = w.openWorkspace
	w.workspaces.onRename = w.renamedWorkspace

	w.requests = newRequestTree(requests, requestsPath)
	w.requests.onError = saveFailed
	w.requests.onOpen = w.openRequest
	w.requests.onRemove = func(r *collection.Request) {
		if w.open == r {
			w.show(nil)
		}
	}
	w.requests.onRename = func(r *collection.Request) {
		if w.open == r {
			w.updateHeaders()
		}
	}
	w.requests.create = func() (*collection.Request, bool) {
		switch typ, ok := askRequestType(w.QWidget); {
		case !ok:
			return nil, false
		case typ == collection.TypeGRPC:
			return collection.NewGRPC("New Request", rpc.Request{}), true
		case w.open == nil:
			// An unsaved request being composed becomes the new request.
			return collection.NewREST("New Request", w.request.request()), true
		default:
			return collection.NewREST("New Request", rest.Request{Method: http.MethodGet}), true
		}
	}

	w.request.expand = w.expand
	w.request.setCodeFont(w.config.ResponseFont)
	w.grpc.setCodeFont(w.config.ResponseFont)
	w.request.onChange = func() {
		w.updatePreview()
		if w.open != nil && w.open.Type == collection.TypeREST {
			request := w.request.request()
			w.open.Request = &request
			w.requests.save()
		}
	}
	w.request.send.OnClicked(w.send)
	w.request.execute.OnClicked(w.executeCurl)
	w.request.url.OnReturnPressed(w.send)

	w.grpc.onChange = func() {
		if w.open != nil && w.open.Type == collection.TypeGRPC {
			request := w.grpc.request()
			w.open.GRPC = &request
			w.requests.save()
		}
	}
	w.grpc.onTarget = func() { w.reflect(false) }
	w.grpc.reload.OnClicked(func() { w.reflect(true) })
	w.grpc.example.OnClicked(w.useExample)
	w.grpc.invoke.OnClicked(w.invoke)
	w.grpc.url.OnReturnPressed(w.invoke)

	qt.NewQShortcut2(qt.NewQKeySequence2("Ctrl+Return"), w.QObject).OnActivated(func() {
		switch w.stack.CurrentIndex() {
		case pageRequest:
			w.send()
		case pageGRPC:
			w.invoke()
		}
	})

	main := qt.NewQSplitter3(qt.Vertical)
	main.AddWidget(w.request.root)
	main.AddWidget(w.response.root)
	main.SetChildrenCollapsible(false)
	main.SetStretchFactor(0, 1)
	main.SetStretchFactor(1, 1)

	grpc := qt.NewQSplitter3(qt.Vertical)
	grpc.AddWidget(w.grpc.root)
	grpc.AddWidget(w.grpcResponse.root)
	grpc.SetChildrenCollapsible(false)
	grpc.SetStretchFactor(0, 1)
	grpc.SetStretchFactor(1, 1)

	// The order matches the page constants.
	w.stack = qt.NewQStackedWidget2()
	w.stack.AddWidget(main.QWidget)
	w.stack.AddWidget(w.editor.root)
	w.stack.AddWidget(grpc.QWidget)

	// Dragging the response on one page sets its height on the others, which
	// are given it when they are next shown.
	w.responseHeight = w.config.ResponseHeight
	w.splits = map[int]*qt.QSplitter{pageRequest: main, pageGRPC: grpc}
	for _, split := range w.splits {
		split.OnSplitterMoved(func(int, int) { w.responseHeight = split.Sizes()[1] })
	}
	w.stack.OnCurrentChanged(func(page int) {
		if split, ok := w.splits[page]; ok {
			w.later(func() { w.restoreResponseHeight(split) })
		}
	})

	// The tabs are down the left edge and have icons in place of names,
	// which are given as tooltips.
	w.sidebar = qt.NewQTabWidget2()
	w.sidebar.SetTabPosition(qt.QTabWidget__West)
	w.sidebar.SetIconSize(qt.NewQSize2(sidebarIconSize, sidebarIconSize))
	// The order matches the tab constants.
	w.sidebar.AddTab2(w.workspaces.root, drawTabIcon(workspacesColour, drawMonitor), "")
	w.sidebar.AddTab2(w.environments.root, drawTabIcon(environmentsColour, drawGlobe), "")
	w.sidebar.AddTab2(w.requests.root, drawTabIcon(requestsColour, drawFolder), "")
	w.sidebar.SetTabToolTip(tabWorkspaces, "Workspaces")
	w.sidebar.SetTabToolTip(tabEnvironments, "Environments")
	w.sidebar.SetTabToolTip(tabRequests, "Requests")
	// The requests are what is wanted first.
	w.sidebar.SetCurrentIndex(tabRequests)

	w.split = qt.NewQSplitter3(qt.Horizontal)
	w.split.AddWidget(w.sidebar.QWidget)
	w.split.AddWidget(w.stack.QWidget)
	w.split.SetStretchFactor(0, 0)
	w.split.SetStretchFactor(1, 1)
	w.split.SetSizes([]int{defaultSidebarWidth, 1024 - defaultSidebarWidth})
	w.SetCentralWidget(w.split.QWidget)

	// The saved layout is restored the first time the window is shown, as
	// the sizes to divide up are not known before then.
	w.OnShowEvent(func(super func(*qt.QShowEvent), event *qt.QShowEvent) {
		super(event)
		if !w.laidOut {
			w.laidOut = true
			w.later(w.restoreLayout)
		}
	})

	// Selecting an environment above a request selects it in the sidebar,
	// which is what makes it the active one.
	for _, h := range []*header{w.request.header, w.grpc.header} {
		h.onEnvironment = w.environments.list.SetCurrentRow
	}

	w.newMenuBar()
	w.updateHeaders()
	w.applyJSONColours()

	w.request.url.SetFocus()

	return w
}

func (w *Window) newMenuBar() {
	file := w.MenuBar().AddMenuWithTitle("&File")
	w.preferences = file.AddActionWithText("&Settings…")
	w.preferences.SetShortcut(qt.NewQKeySequence2("Ctrl+,"))
	w.preferences.SetMenuRole(qt.QAction__PreferencesRole)
	w.preferences.OnTriggered(w.openSettings)

	file.AddSeparator()
	w.quit = file.AddActionWithText("&Quit")
	w.quit.SetShortcut(qt.NewQKeySequence2("Ctrl+Q"))
	w.quit.SetMenuRole(qt.QAction__QuitRole)
	// Closing the window saves the settings and ends the program.
	w.quit.OnTriggered(func() { w.Close() })

	help := w.MenuBar().AddMenuWithTitle("&Help")
	w.about = help.AddActionWithText("&About")
	w.about.SetShortcut(qt.NewQKeySequence2("Ctrl+I"))
	w.about.SetMenuRole(qt.QAction__AboutRole)
	w.about.OnTriggered(func() {
		qt.QMessageBox_About(w.QWidget, "About TriggerAPI", aboutText())
	})
}

// workspacesDir returns the folder the workspaces are kept in.
func (w *Window) workspacesDir() string {
	if w.config.WorkspacesDir != "" {
		return w.config.WorkspacesDir
	}
	return w.files.Workspaces
}

// migrate makes the first workspace if there are none, and brings into it
// the environments and requests from before there were workspaces. Their
// files are copied and left where they were.
func (w *Window) migrate() {
	dir := w.workspacesDir()
	if names, err := workspace.List(dir); err != nil || len(names) > 0 {
		return
	}

	err := workspace.Create(dir, workspace.Default)
	if err == nil {
		err = workspace.Import(dir, workspace.Default,
			orDefault(w.config.EnvironmentsFile, w.files.Environments),
			orDefault(w.config.RequestsFile, w.files.Requests))
	}
	if err != nil {
		w.status("The first workspace could not be made: " + err.Error())
		return
	}
	w.config.EnvironmentsFile, w.config.RequestsFile = "", ""
}

// loadWorkspace reads the environments and requests of a workspace, and
// returns each with the path it is to be saved to. A file that could not be
// read is never saved to, so it is not overwritten: its path is empty, as
// are both if there is no workspace.
func (w *Window) loadWorkspace(name string) (envs *env.Store, envPath string, requests *collection.Store, requestsPath string) {
	if name == "" {
		w.status("There is no workspace, so changes will not be saved.")
		return &env.Store{Active: -1}, "", &collection.Store{}, ""
	}

	var failed []error
	envPath = workspace.Environments(w.workspacesDir(), name)
	requestsPath = workspace.Requests(w.workspacesDir(), name)

	envs, err := env.Load(envPath)
	if err != nil {
		envPath = ""
		failed = append(failed, err)
	}
	requests, err = collection.Load(requestsPath)
	if err != nil {
		requestsPath = ""
		failed = append(failed, err)
	}

	if len(failed) > 0 {
		w.status("Saved data could not be loaded, changes will not be saved: " + errors.Join(failed...).Error())
	} else {
		w.status("")
	}
	return envs, envPath, requests, requestsPath
}

// openWorkspace makes a workspace the one in use, showing its environments
// and requests in place of those of the one before.
func (w *Window) openWorkspace(name string) {
	envs, envPath, requests, requestsPath := w.loadWorkspace(name)
	w.config.Workspace = name

	// The open request and the environment being edited belong to the
	// workspace being left.
	w.closeEditor()
	w.show(nil)

	w.environments.load(envs, envPath)
	w.requests.clearSearch()
	w.requests.load(requests, requestsPath)
	w.environmentChanged()
}

// renamedWorkspace follows the workspace in use to its new name, which is
// also the name of the folder its files are in.
func (w *Window) renamedWorkspace(name string) {
	w.config.Workspace = name
	if w.environments.path != "" {
		w.environments.path = workspace.Environments(w.workspacesDir(), name)
	}
	if w.requests.path != "" {
		w.requests.path = workspace.Requests(w.workspacesDir(), name)
	}
}

// moveWorkspaces keeps the workspaces in another folder from now on. If
// there are none in it yet, the ones there are now are copied there.
func (w *Window) moveWorkspaces(from string) error {
	to := w.workspacesDir()

	names, err := workspace.List(to)
	if err != nil {
		return err
	}
	if len(names) == 0 {
		if err := workspace.CopyAll(from, to); err != nil {
			return err
		}
	}

	w.workspaces.open(w.config.Workspace)
	return nil
}

// later runs f once the events already waiting have been handled, by which
// time a widget that has just been shown has been given its size.
func (w *Window) later(f func()) {
	mainthread.Start(f)
}

// restoreLayout gives the sidebar and the response their saved sizes.
func (w *Window) restoreLayout() {
	if width, sizes := w.config.SidebarWidth, w.split.Sizes(); width > 0 && sizes[0]+sizes[1] > width {
		w.split.SetSizes([]int{width, sizes[0] + sizes[1] - width})
	}
	if split, ok := w.splits[w.stack.CurrentIndex()]; ok {
		w.restoreResponseHeight(split)
	}
}

// restoreResponseHeight gives the response of a page the height it was last
// dragged to, taking the space from or giving it to the request.
func (w *Window) restoreResponseHeight(split *qt.QSplitter) {
	sizes := split.Sizes()
	if total := sizes[0] + sizes[1]; w.responseHeight > 0 && total > w.responseHeight {
		split.SetSizes([]int{total - w.responseHeight, w.responseHeight})
	}
}

// settings returns the state to remember for the next run. The size of a
// maximised window is that of the window before it was maximised, falling
// back to the previous setting if the platform does not report one.
func (w *Window) settings() settings.Settings {
	s := w.config

	// The layout is only known once the window has been shown. The response
	// is measured on the page in view, as resizing the window changes its
	// height without it being dragged.
	if w.laidOut {
		if width := w.split.Sizes()[0]; width > 0 {
			s.SidebarWidth = width
		}
		s.ResponseHeight = w.responseHeight
		if split, ok := w.splits[w.stack.CurrentIndex()]; ok && split.Sizes()[1] > 0 {
			s.ResponseHeight = split.Sizes()[1]
		}
	}
	s.Width, s.Height, s.Maximized = w.Width(), w.Height(), w.IsMaximized()
	if s.Maximized {
		normal := w.NormalGeometry()
		s.Width, s.Height = normal.Width(), normal.Height()
		if s.Width <= 0 || s.Height <= 0 {
			s.Width, s.Height = w.config.Width, w.config.Height
		}
	}
	return s
}

// openSettings lets the user edit the settings in a dialog.
func (w *Window) openSettings() {
	dialog := newSettingsDialog(w.QWidget, w.config, w.files)
	// A dialog is not destroyed by being closed.
	defer dialog.DeleteLater()

	if dialog.Exec() != int(qt.QDialog__Accepted) {
		return
	}
	if err := w.applySettings(dialog.settings(w.config)); err != nil {
		qt.QMessageBox_Warning(w.QWidget, "Settings", err.Error())
	}
}

// applySettings puts the settings into effect and saves them. A setting that
// cannot be applied is left as it was, and reported in the error.
//
// Moving the workspaces to a folder that has some shows those. Moving them
// to a folder that has none copies the ones there are into it.
func (w *Window) applySettings(s settings.Settings) error {
	var failed []error

	w.config.ResponseFont = s.ResponseFont
	w.response.setBodyFont(s.ResponseFont)
	w.request.setCodeFont(s.ResponseFont)
	w.grpc.setCodeFont(s.ResponseFont)
	w.grpcResponse.setBodyFont(s.ResponseFont)

	w.config.JSONColours = s.JSONColours
	w.applyJSONColours()

	setting, previous := w.config.WorkspacesDir, w.workspacesDir()
	w.config.WorkspacesDir = s.WorkspacesDir
	if dir := w.workspacesDir(); filepath.Clean(dir) != filepath.Clean(previous) {
		if err := w.moveWorkspaces(previous); err != nil {
			w.config.WorkspacesDir = setting
			failed = append(failed, fmt.Errorf("the workspaces were not moved to %s: %w", dir, err))
		}
	}

	if err := w.settings().Save(w.files.Settings); err != nil {
		failed = append(failed, fmt.Errorf("the settings were not saved: %w", err))
	}

	return errors.Join(failed...)
}

// applyJSONColours shows JSON in the colours of the settings, everywhere it
// is highlighted.
func (w *Window) applyJSONColours() {
	colours := jsonColours(w.config.JSONColours)

	w.request.highlight.json.setColours(colours)
	w.request.refreshPlaceholders()
	w.grpc.highlight.json.setColours(colours)
	w.grpc.refreshPlaceholders()
	w.response.setJSONColours(colours)
	w.grpcResponse.setJSONColours(colours)
}

// expand substitutes the variables of the active environment into text.
func (w *Window) expand(text string) string {
	return w.environments.store.Expand(text)
}

// environmentChanged refreshes what depends on the active environment, as
// what the placeholders in the request resolve to may have changed.
func (w *Window) environmentChanged() {
	w.updateHeaders()
	w.updatePreview()
	w.request.refreshPlaceholders()
	w.grpc.refreshPlaceholders()
}

func (w *Window) updatePreview() {
	w.request.updatePreview(w.expand)
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

// requestPage returns the page of the request UI for the open request.
func (w *Window) requestPage() int {
	if w.open != nil && w.open.Type == collection.TypeGRPC {
		return pageGRPC
	}
	return pageRequest
}

// closeEditor returns to the request UI.
func (w *Window) closeEditor() {
	w.stack.SetCurrentIndex(w.requestPage())
}

// openRequest shows a saved request in the request UI.
func (w *Window) openRequest(r *collection.Request) {
	if w.open != r {
		w.show(r)
	}
	w.closeEditor()
}

// show replaces the contents of the request UI with a saved request, or with
// an empty unsaved REST request if r is nil. The environment editor is left
// open if it is.
func (w *Window) show(r *collection.Request) {
	if w.cancel != nil {
		w.cancel()
	}
	w.shown++

	w.open = r
	switch {
	case r == nil:
		w.request.load(rest.Request{Method: http.MethodGet})
	case r.Type == collection.TypeGRPC:
		w.grpc.load(*r.GRPC)
		w.grpcResponse.clear()
		w.reflect(false)
	default:
		w.request.load(*r.Request)
	}

	w.updateHeaders()

	if w.requestPage() == pageRequest {
		w.updatePreview()
		w.response.clear()
	}
	if w.stack.CurrentIndex() != pageEditor {
		w.stack.SetCurrentIndex(w.requestPage())
	}
}

// status shows a message in the status bar until another replaces it. Line
// breaks are removed as the bar has one line. The bar is kept for problems
// with the saved data, which have nowhere else to be shown; requests and
// their responses are not reported in it.
func (w *Window) status(message string) {
	w.StatusBar().ShowMessage(strings.Join(strings.Fields(message), " "))
}

// updateHeaders names the open request above each request UI, and lists the
// environments there with the active one selected.
func (w *Window) updateHeaders() {
	request := ""
	if w.open != nil {
		request = w.open.Name
	}

	store := w.environments.store
	names := make([]string, len(store.Environments))
	for i, e := range store.Environments {
		names[i] = e.Name
	}

	for _, h := range []*header{w.request.header, w.grpc.header} {
		h.setRequest(request)
		h.setEnvironments(names, store.Active)
	}
}

// run does the work of the Send or Invoke button in the background, or
// cancels the work already in flight. The work returns a function to show
// its result, which is called on the main thread.
func (w *Window) run(button *qt.QPushButton, response *responsePanel, work func(context.Context) (show func(), err error)) {
	if w.cancel != nil {
		w.cancel()
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	w.cancel = cancel
	shown := w.shown

	label := button.Text()
	button.SetText("Cancel")
	response.clear()
	start := time.Now()

	// Qt widgets may only be touched from the main thread, so the result is
	// handed back to it.
	go func() {
		show, err := work(ctx)
		elapsed := time.Since(start)

		mainthread.Start(func() {
			cancel()
			w.cancel = nil
			button.SetText(label)

			switch {
			case shown != w.shown:
				// Another request has been shown since, so the result is
				// no longer wanted.
			case errors.Is(err, context.Canceled):
				response.showCancelled(elapsed)
			case err != nil:
				response.showError(err, elapsed)
			default:
				show()
			}
		})
	}()
}

// send starts the REST request, or cancels the request in flight.
func (w *Window) send() {
	req := w.request.request().Expand(w.expand)

	w.run(w.request.send, w.response, func(ctx context.Context) (func(), error) {
		res, err := rest.Send(ctx, w.client, req)
		return func() { w.response.show(res) }, err
	})
}

// executeCurl makes the REST request by running the curl command that is
// shown for it, or cancels the request in flight.
func (w *Window) executeCurl() {
	req := w.request.request().Expand(w.expand)

	w.run(w.request.execute, w.response, func(ctx context.Context) (func(), error) {
		res, err := rest.RunCurl(ctx, req)
		return func() { w.response.show(res) }, err
	})
}

// invoke starts the gRPC request, or cancels the request in flight.
func (w *Window) invoke() {
	req := w.grpc.request().Expand(w.expand)

	// The service definition is loaded by the call if it has not been yet.
	var schema *rpc.Schema
	if target, err := req.Target(); err == nil {
		schema = w.schemas[target]
	}

	w.run(w.grpc.invoke, w.grpcResponse, func(ctx context.Context) (func(), error) {
		res, err := rpc.Invoke(ctx, req, schema)
		return func() { w.grpcResponse.showRPC(res) }, err
	})
}

// reflect loads the service definition of the server the gRPC request is
// for, to offer its methods. One loaded before is used again unless force is
// set.
func (w *Window) reflect(force bool) {
	w.reflecting++
	reflecting := w.reflecting

	req := w.grpc.request().Expand(w.expand)
	target, err := req.Target()
	if err != nil {
		// There is no server to ask yet.
		w.grpc.setStatus("", "")
		return
	}

	if schema, ok := w.schemas[target]; ok && !force {
		w.grpc.setSchema(schema)
		return
	}

	w.grpc.setStatus("", "")

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), reflectTimeout)
		defer cancel()

		schema, err := rpc.Reflect(ctx, req)

		mainthread.Start(func() {
			if err == nil {
				w.schemas[target] = schema
			}
			// A later request for a definition has replaced this one.
			if reflecting != w.reflecting {
				return
			}
			if err != nil {
				w.grpc.setSchemaError(err)
				return
			}
			w.grpc.setSchema(schema)
		})
	}()
}

// useExample replaces the message of the gRPC request with an example for
// the selected method.
func (w *Window) useExample() {
	req := w.grpc.request().Expand(w.expand)

	target, _ := req.Target()
	schema, ok := w.schemas[target]
	if !ok {
		w.grpc.setStatus("An example needs the service definition, which has not been loaded. Enter the server address and press Reload.", statusColour(400))
		return
	}

	example, err := schema.Example(req.Method)
	if err != nil {
		w.grpc.setStatus("An example could not be made: "+err.Error(), statusColour(400))
		return
	}
	w.grpc.message.SetPlainText(example)
}

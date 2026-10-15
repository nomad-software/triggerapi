// Package ui contains the Qt user interface.
package ui

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	qt "github.com/mappu/miqt/qt6"
	"github.com/mappu/miqt/qt6/mainthread"

	"github.com/nomad-software/triggerapi/internal/capture"
	"github.com/nomad-software/triggerapi/internal/collection"
	"github.com/nomad-software/triggerapi/internal/crypt"
	"github.com/nomad-software/triggerapi/internal/env"
	"github.com/nomad-software/triggerapi/internal/jsonfile"
	"github.com/nomad-software/triggerapi/internal/rest"
	"github.com/nomad-software/triggerapi/internal/rpc"
	"github.com/nomad-software/triggerapi/internal/settings"
	"github.com/nomad-software/triggerapi/internal/workspace"
	"github.com/nomad-software/triggerapi/internal/ws"
)

// Pages of the main stack.
const (
	pageRequest = iota
	pageEditor
	pageGRPC
	pageWS
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

// Version is the version of the program, shown in the about box.
const Version = "v0.0.1"

// aboutText returns the contents of the about box, which carries the notice
// the LGPL asks of programs that use Qt.
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
	// unsaved is the type of the request that is not saved, which is on
	// show while open is nil.
	unsaved collection.Type
	// chosenSets are the names of the sets chosen in the workspaces that
	// have been left, by the names of the workspaces, to be chosen again
	// when they are gone back to. They are not kept between runs, which
	// start with no set chosen.
	chosenSets map[string]string
	// keys are the keys of the settings, which the environments and
	// requests are encrypted with. They are nil if the settings have none.
	keys *crypt.Keys

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

	// tabs is the row of tabs above the request UI: one for each pinned
	// request, and one for the request last selected in the sidebar.
	tabs *requestTabs
	// environment selects the workspace and the environment in use, above
	// the tabs.
	environment *environmentSelector
	// stack shows the REST request UI, the gRPC request UI or the
	// environment editor.
	stack    *qt.QStackedWidget
	editor   *envEditor
	request  *requestPanel
	response *responsePanel
	// grpc and grpcResponse are the gRPC request UI.
	grpc         *grpcPanel
	grpcResponse *responsePanel
	// ws and wsResponse are the WebSocket request UI, and socket the
	// connection it has open, or nil.
	ws         *wsPanel
	wsResponse *responsePanel
	socket     *wsSession

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
		ws:          newWSPanel(),
		editor:      newEnvEditor(),
		tabs:        newRequestTabs(),
		environment: newEnvironmentSelector(),
		schemas:     map[string]*rpc.Schema{},
	}

	w.SetWindowTitle("TriggerAPI")
	w.SetWindowIcon(appIcon())
	// Dialogs take their icon from the application.
	qt.QGuiApplication_SetWindowIcon(appIcon())

	// The status bar has a handle at its right to resize the window by.
	w.StatusBar().SetSizeGripEnabled(true)

	// Restore the window size, and remember it again when the window closes.
	w.files = files
	w.config = settings.Load(files.Settings)
	// The icons are drawn in the colours of the settings as they are made.
	setColours(w.config.Colours)
	w.response = newResponsePanel(w.config.ResponseFont)
	w.grpcResponse = newResponsePanel(w.config.ResponseFont)
	w.wsResponse = newResponsePanel(w.config.ResponseFont)
	// What to capture from a response is set beside the response, though
	// it is kept by the request.
	w.response.setCaptureTable(w.request.captures)
	w.grpcResponse.setCaptureTable(w.grpc.captures)
	// A response is saved to a file named after its request.
	for _, response := range []*responsePanel{w.response, w.grpcResponse, w.wsResponse} {
		response.requestName = func() string {
			if w.open == nil {
				return ""
			}
			return w.open.Name
		}
	}
	w.Resize(w.config.Width, w.config.Height)
	if w.config.Maximized {
		w.SetWindowState(qt.WindowMaximized)
	}
	w.OnCloseEvent(func(super func(*qt.QCloseEvent), event *qt.QCloseEvent) {
		if err := w.settings().Save(files.Settings); err != nil {
			// The window is going away, so there is nowhere to show this.
			fmt.Fprintln(os.Stderr, "triggerapi: settings could not be saved:", err)
		}
		// An open connection is closed with the window.
		w.disconnectWS()
		super(event)
	})

	// The workspace that was last in use is opened, which is the first
	// there is if this is the first run with workspaces.
	w.workspaces = newWorkspaceList(w.workspacesDir)
	w.firstWorkspace()
	// A failure to open the keys is reported when the files are loaded.
	w.keys, _ = openKeys(w.QWidget, w.config)
	currentKeys := func() *crypt.Keys { return w.keys }
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
	w.ws.highlight.expand = w.expand

	w.environments = newEnvList(envs, envPath)
	w.environments.onError = saveFailed
	w.environments.keys = currentKeys
	w.environments.onChange = w.environmentChanged
	w.environments.onEdit = func() {
		w.openEditor()
		// Editing starts at the name, ready to be typed over. The editor
		// following the selection does not take the keyboard from the list.
		if w.stack.CurrentIndex() == pageEditor {
			w.editor.name.SetFocus()
			w.editor.name.SelectAll()
		}
	}
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
	w.workspaces.onList = w.updateHeaders

	w.requests = newRequestTree(requests, requestsPath)
	w.requests.onError = saveFailed
	w.requests.keys = currentKeys
	w.requests.onOpen = w.openRequest
	w.requests.onRemove = func(r *collection.Request) {
		// Its tab is closed, which shows another if it was the one shown.
		// The tree is in the middle of removing it, so is left alone.
		w.tabs.remove(r)
		if w.open == r {
			w.showTab(w.tabs.current(), false)
		}
	}
	w.requests.onRename = w.tabs.rename
	// What is in the folders decides which endpoints have requests.
	w.requests.onSave = w.syncEndpoints
	w.tabs.onSelect = func(from, to *requestTab) {
		w.stash(from)
		w.showTab(to, true)
	}
	// The pinned tabs are those of the set chosen, which follows them.
	w.tabs.onPins = func() { w.requests.sets.keep(w.tabs.pinned()) }
	w.requests.sets.pinned = w.tabs.pinned
	w.tabs.onNew = w.newRequest
	w.request.save.OnClicked(w.saveRequest)
	w.grpc.save.OnClicked(w.saveRequest)
	w.ws.save.OnClicked(w.saveRequest)
	w.requests.sets.onChoose = w.chooseSet
	w.requests.create = func() (*collection.Request, bool) {
		switch typ, ok := askRequestType(w.QWidget); {
		case !ok:
			return nil, false
		case w.open == nil && w.unsaved == typ:
			// An unsaved request being composed becomes the new request.
			return w.composed("New Request"), true
		case typ == collection.TypeGRPC:
			return collection.NewGRPC("New Request", rpc.Request{}), true
		case typ == collection.TypeWebSocket:
			return collection.NewWebSocket("New Request", ws.Request{}), true
		default:
			return collection.NewREST("New Request", rest.Request{Method: http.MethodGet}), true
		}
	}

	w.request.expand = w.expand
	w.request.expander = w.expander
	w.request.options = w.options
	w.request.setCodeFont(w.config.ResponseFont)
	w.grpc.setCodeFont(w.config.ResponseFont)
	w.ws.setCodeFont(w.config.ResponseFont)
	w.request.onChange = func() {
		if w.open != nil && w.open.Type == collection.TypeREST {
			request := w.request.request()
			w.open.Request = &request
			w.requests.save()
		}
	}
	w.request.send.OnClicked(w.send)
	w.request.execute.OnClicked(w.executeCurl)

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
	w.grpc.endpoints.onCreate = w.createRequests
	w.grpc.invoke.OnClicked(w.invoke)

	w.ws.onChange = func() {
		if w.open != nil && w.open.Type == collection.TypeWebSocket {
			request := w.ws.request()
			w.open.WebSocket = &request
			w.requests.save()
		}
	}
	w.ws.connect.OnClicked(w.toggleWS)
	w.ws.send.OnClicked(w.sendWS)

	qt.NewQShortcut2(qt.NewQKeySequence2("Ctrl+Return"), w.QObject).OnActivated(func() {
		switch w.stack.CurrentIndex() {
		case pageRequest:
			w.send()
		case pageGRPC:
			w.invoke()
		case pageWS:
			// The message is sent over the connection, which is opened
			// first if it is not.
			if w.socket == nil {
				w.toggleWS()
			} else {
				w.sendWS()
			}
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

	socket := qt.NewQSplitter3(qt.Vertical)
	socket.AddWidget(w.ws.root)
	socket.AddWidget(w.wsResponse.root)
	socket.SetChildrenCollapsible(false)
	socket.SetStretchFactor(0, 1)
	socket.SetStretchFactor(1, 1)

	// The order matches the page constants.
	w.stack = qt.NewQStackedWidget2()
	w.stack.AddWidget(main.QWidget)
	w.stack.AddWidget(w.editor.root)
	w.stack.AddWidget(grpc.QWidget)
	w.stack.AddWidget(socket.QWidget)

	// Dragging the response on one page sets its height on the others, which
	// are given it when they are next shown.
	w.responseHeight = w.config.ResponseHeight
	w.splits = map[int]*qt.QSplitter{pageRequest: main, pageGRPC: grpc, pageWS: socket}
	for _, split := range w.splits {
		split.OnSplitterMoved(func(int, int) { w.responseHeight = split.Sizes()[1] })
	}
	w.stack.OnCurrentChanged(func(page int) {
		// The tabs are of requests, so are put away while an environment
		// is edited in their place.
		w.tabs.bar.SetVisible(page != pageEditor)
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
	w.sidebar.AddTab(plainPage(w.workspaces.root), "")
	w.sidebar.AddTab(plainPage(w.environments.root), "")
	w.sidebar.AddTab(plainPage(w.requests.root), "")
	w.drawSidebarIcons()
	w.sidebar.SetTabToolTip(tabWorkspaces, "Workspaces")
	w.sidebar.SetTabToolTip(tabEnvironments, "Environments")
	w.sidebar.SetTabToolTip(tabRequests, "Requests")
	// The requests are what is wanted first.
	w.sidebar.SetCurrentIndex(tabRequests)

	w.split = qt.NewQSplitter3(qt.Horizontal)
	w.split.AddWidget(w.sidebar.QWidget)
	// The tabs of the requests are above whichever page is shown.
	pages := qt.NewQWidget2()
	pagesLayout := qt.NewQVBoxLayout(pages)
	pagesLayout.SetContentsMargins(0, 0, 0, 0)
	pagesLayout.SetSpacing(0)
	// The workspace and the environment are selected on a line of their
	// own above them, which leaves the tabs the whole width and stays
	// there when the tabs are put away.
	pagesLayout.AddWidget(w.environment.root)
	pagesLayout.AddWidget(w.tabs.bar.QWidget)
	pagesLayout.AddWidget2(w.stack.QWidget, 1)
	w.split.AddWidget(pages)
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
			// A run without keys, as the first is, cannot do anything
			// until it is given them.
			if w.keys == nil {
				w.later(func() { adviseKeys(w.QWidget) })
			}
		}
	})

	// Escape reaches the window when what has the keyboard has no use for
	// it, so it still cancels the editing of a name or a cell.
	w.OnKeyPressEvent(func(super func(*qt.QKeyEvent), event *qt.QKeyEvent) {
		if event.Key() == int(qt.Key_Escape) {
			w.escape()
			return
		}
		super(event)
	})

	qt.NewQShortcut3(qt.QKeySequence__Find, w.QObject).OnActivated(w.find)
	qt.NewQShortcut2(qt.NewQKeySequence2("Ctrl+P"), w.QObject).OnActivated(w.quickOpen)
	// Ctrl+1 to Ctrl+3 go to the tabs of the sidebar, in order.
	for tab := range w.sidebar.Count() {
		qt.NewQShortcut2(qt.NewQKeySequence2(fmt.Sprintf("Ctrl+%d", tab+1)), w.QObject).OnActivated(func() { w.focusSidebar(tab) })
	}

	// Selecting a workspace or an environment above the tabs selects it in
	// the sidebar, which is what makes it the one in use. The button beside
	// the environment does what the edit button of the sidebar does.
	w.environment.onWorkspace = w.workspaces.list.SetCurrentRow
	w.environment.onEnvironment = w.environments.list.SetCurrentRow
	w.environment.workspaceTitle.OnClicked(func() { w.focusSidebar(tabWorkspaces) })
	w.environment.environmentTitle.OnClicked(w.editEnvironment)
	w.environment.setTitle.OnClicked(w.showSets)
	w.environment.onSet = w.requests.sets.chooseAt
	w.requests.sets.onChanged = w.updateSets
	w.updateSets()

	w.newMenuBar()
	w.updateHeaders()
	w.applyJSONColours()
	w.restoreTabs()

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

// firstWorkspace makes the first workspace, named Default, if there are
// none, so that there is always one to work in.
func (w *Window) firstWorkspace() {
	dir := w.workspacesDir()
	if names, err := workspace.List(dir); err != nil || len(names) > 0 {
		return
	}
	if err := workspace.Create(dir, workspace.Default); err != nil {
		w.status("The first workspace could not be made: " + err.Error())
	}
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

	// Nothing is read or saved without keys to encrypt it with.
	if w.keys == nil {
		w.status(needKeys)
		return &env.Store{Active: -1}, "", &collection.Store{}, ""
	}

	var failed []error
	envPath = workspace.Environments(w.workspacesDir(), name)
	requestsPath = workspace.Requests(w.workspacesDir(), name)

	// A file from before there were keys is encrypted once it has been
	// read, which leaves alone one that cannot be.
	envs, err := env.LoadWith(envPath, w.keys)
	if err == nil {
		err = w.encrypt(envPath)
	}
	if err != nil {
		envPath = ""
		failed = append(failed, err)
	}
	requests, err = collection.LoadWith(requestsPath, w.keys)
	if err == nil {
		err = w.encrypt(requestsPath)
	}
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

// encrypt has a file that is not encrypted, as one from before there were
// keys, made so.
func (w *Window) encrypt(path string) error {
	return jsonfile.Convert(path, w.keys, w.keys)
}

// needKeys says what is wrong when the settings have no keys.
const needKeys = "TriggerAPI needs a pair of SSH keys to encrypt its files with. " +
	"Until they are added under Encryption in File > Settings, nothing is loaded or saved."

// adviseKeys tells the user that the program cannot work until the settings
// are given keys. Tests replace it rather than let the dialog open.
var adviseKeys = func(parent *qt.QWidget) {
	qt.QMessageBox_Warning(parent, "Keys Needed", needKeys)
}

// askPassphrase asks for the passphrase of the private key in the file at
// path. Tests replace it rather than let the dialog open.
var askPassphrase = func(parent *qt.QWidget, path string) ([]byte, error) {
	ok := false
	passphrase := qt.QInputDialog_GetText4(parent, "Private Key", "Passphrase of "+path, qt.QLineEdit__Password, "", &ok)
	if !ok {
		return nil, errors.New("no passphrase was given")
	}
	return []byte(passphrase), nil
}

// openKeys returns the keys the settings name, which are nil if they name
// none. Keys that cannot be opened are returned, with the reason, as ones
// that cannot be used, so that nothing is saved without them.
func openKeys(parent *qt.QWidget, s settings.Settings) (*crypt.Keys, error) {
	if s.PublicKey == "" && s.PrivateKey == "" {
		return nil, nil
	}
	keys, err := crypt.Open(s.PublicKey, s.PrivateKey, func() ([]byte, error) {
		return askPassphrase(parent, s.PrivateKey)
	})
	if err != nil {
		err = fmt.Errorf("the keys of the settings cannot be used: %w", err)
		return crypt.Unusable(err), err
	}
	return keys, nil
}

// changeKeys has the environments and requests of every workspace
// encrypted with other keys from now on: each file is decrypted with the
// keys there were and encrypted with those given. The workspace in use is
// read again if it could not be before. Taking the keys away leaves the
// files encrypted as they are, and nothing can be read or saved until there
// are keys again, which the user is told.
func (w *Window) changeKeys(keys *crypt.Keys) error {
	if keys == nil {
		w.keys = nil
		w.workspaces.open(w.config.Workspace)
		adviseKeys(w.QWidget)
		return nil
	}

	dir := w.workspacesDir()
	names, err := workspace.List(dir)

	failed := []error{err}
	for _, name := range names {
		for _, path := range []string{workspace.Environments(dir, name), workspace.Requests(dir, name)} {
			if err := jsonfile.Convert(path, w.keys, keys); err != nil {
				failed = append(failed, fmt.Errorf("%s: %w", path, err))
			}
		}
	}
	w.keys = keys

	if w.environments.path == "" || w.requests.path == "" {
		w.workspaces.open(w.config.Workspace)
	}
	return errors.Join(failed...)
}

// openWorkspace makes a workspace the one in use, showing its environments
// and requests in place of those of the one before.
func (w *Window) openWorkspace(name string) {
	// The pinned requests of the workspace being left are noted while its
	// requests are still there to be found.
	w.keepSet()

	envs, envPath, requests, requestsPath := w.loadWorkspace(name)
	w.config.Workspace = name

	// The environment being edited belongs to the workspace being left, as
	// do the requests in the tabs, which are replaced by those that are
	// pinned in this one.
	w.closeEditor()

	w.environments.load(envs, envPath)
	w.requests.clearSearch()
	w.requests.load(requests, requestsPath)
	w.restoreTabs()
	w.environmentChanged()
}

// keepSet records the set chosen in the workspace in use, for its tabs to be
// opened again when the workspace is gone back to. Tabs that are not in a
// set are not remembered.
func (w *Window) keepSet() {
	if w.chosenSets == nil {
		w.chosenSets = map[string]string{}
	}
	if set := w.requests.sets.active; set != nil {
		w.chosenSets[w.config.Workspace] = set.Name
	} else {
		delete(w.chosenSets, w.config.Workspace)
	}
}

// saveRequest saves the request being composed in the tab that holds no
// saved request, which it shows in that tab, and adds it to the sidebar.
func (w *Window) saveRequest() {
	if w.open != nil {
		return
	}
	w.requests.insert(w.composed("New Request"))
}

// composed returns the request that is not saved, as it is in the request
// UI, with a name. It is not in the store.
func (w *Window) composed(name string) *collection.Request {
	w.request.url.commit()
	switch w.unsaved {
	case collection.TypeGRPC:
		return collection.NewGRPC(name, w.grpc.request())
	case collection.TypeWebSocket:
		return collection.NewWebSocket(name, w.ws.request())
	}
	return collection.NewREST(name, w.request.request())
}

// newRequest asks for the type of a request, and shows an empty request of
// that type in the tab that is not pinned, without saving it: it is added to
// the sidebar only once it is saved.
func (w *Window) newRequest() {
	typ, ok := askRequestType(w.QWidget)
	if !ok {
		return
	}
	draft := collection.NewREST("", rest.Request{Method: http.MethodGet})
	switch typ {
	case collection.TypeGRPC:
		draft = collection.NewGRPC("", rpc.Request{})
	case collection.TypeWebSocket:
		draft = collection.NewWebSocket("", ws.Request{})
	}

	w.stash(w.tabs.current())
	tab := w.tabs.open(nil)
	tab.draft, tab.replay = draft, nil
	w.showTab(tab, true)
	w.closeEditor()
}

// chooseSet replaces the tabs with those of a set, and shows the first.
// Choosing no set, by deselecting the one chosen, takes the tabs away and
// leaves one empty tab.
func (w *Window) chooseSet(set *collection.Set) {
	w.stash(w.tabs.current())
	var requests []*collection.Request
	if set != nil {
		requests = set.Requests
	}
	w.tabs.reset(requests)
	w.showTab(w.tabs.current(), true)
}

// restoreTabs opens a tab for each request of the set that was chosen in
// the workspace in use when it was left, and shows the first. With no set,
// as at startup, there is only a request that is not saved and nothing is
// selected: tabs are only remembered in sets.
func (w *Window) restoreTabs() {
	if name, ok := w.chosenSets[w.config.Workspace]; ok {
		if set := w.requests.store.FindSet(name); set != nil {
			w.requests.sets.load(set)
			w.tabs.reset(set.Requests)
			w.showTab(w.tabs.current(), true)
			return
		}
	}

	w.tabs.reset(nil)
	w.showTab(w.tabs.current(), true)
}

// stash keeps the request being composed in a tab, if it is one that is not
// saved, so that it is still there when the tab is shown again.
func (w *Window) stash(tab *requestTab) {
	if tab != nil && tab.request == nil {
		tab.draft = w.composed("")
	} else {
		w.request.url.commit()
	}
}

// showTab shows the request of a tab in the request UI, with the response
// it was last given. The request is selected in the sidebar too if sync is
// set.
func (w *Window) showTab(tab *requestTab, sync bool) {
	w.show(tab.request)
	if tab.request == nil && tab.draft != nil {
		w.showDraft(tab.draft)
	}
	if tab.replay != nil {
		tab.replay()
	}
	if sync {
		w.requests.selectRequest(tab.request)
	}
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
	dialog.onExport = w.chooseExport
	dialog.onImport = w.chooseImport

	if dialog.Exec() != int(qt.QDialog__Accepted) {
		return
	}
	if err := w.applySettings(dialog.settings(w.config)); err != nil {
		qt.QMessageBox_Warning(w.QWidget, "Settings", err.Error())
	}
}

// exportName is the name offered for the file the workspaces are exported
// to.
const exportName = "triggerapi-workspaces.zip"

// chooseExport asks whose public key the workspaces are to be exported for
// and where to, and exports them.
func (w *Window) chooseExport(parent *qt.QWidget) {
	if w.keys == nil {
		adviseKeys(parent)
		return
	}
	public := qt.QFileDialog_GetOpenFileName3(parent, "Public Key To Export For", filepath.Join(qt.QDir_HomePath(), ".ssh"))
	if public == "" {
		return
	}
	archive := qt.QFileDialog_GetSaveFileName4(parent, "Export Workspaces", filepath.Join(qt.QDir_HomePath(), exportName), "Zip files (*.zip)")
	if archive == "" {
		return
	}

	if err := w.exportWorkspaces(public, archive); err != nil {
		qt.QMessageBox_Warning(parent, "Export Workspaces", fmt.Sprintf("The workspaces were not exported: %v.", err))
		return
	}
	qt.QMessageBox_Information(parent, "Export Workspaces", "The workspaces were exported to "+archive+
		", which can be read only with the private key that goes with "+public+".")
}

// exportWorkspaces writes every workspace to a zip file, encrypted for the
// public key in a file.
func (w *Window) exportWorkspaces(public, archive string) error {
	recipient, err := crypt.Recipient(public)
	if err != nil {
		return err
	}
	return workspace.Export(w.workspacesDir(), archive, w.keys, recipient)
}

// chooseImport asks for a file of exported workspaces and adds them.
func (w *Window) chooseImport(parent *qt.QWidget) {
	if w.keys == nil {
		adviseKeys(parent)
		return
	}
	archive := qt.QFileDialog_GetOpenFileName4(parent, "Import Workspaces", qt.QDir_HomePath(), "Zip files (*.zip);;All files (*)")
	if archive == "" {
		return
	}

	added, err := w.importWorkspaces(archive)
	if err != nil {
		qt.QMessageBox_Warning(parent, "Import Workspaces", fmt.Sprintf("The workspaces were not all imported: %v.", err))
		return
	}
	qt.QMessageBox_Information(parent, "Import Workspaces", "Imported "+strings.Join(added, ", ")+".")
}

// importWorkspaces adds the workspaces of a zip file made by exporting, which
// have to be encrypted for the keys of the settings, and returns the names
// they were given. The workspace in use stays the one in use.
func (w *Window) importWorkspaces(archive string) ([]string, error) {
	added, err := workspace.Unpack(w.workspacesDir(), archive, w.keys)
	if len(added) > 0 {
		if _, listErr := w.workspaces.load(w.config.Workspace); listErr != nil {
			err = errors.Join(err, listErr)
		}
	}
	return added, err
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
	w.ws.setCodeFont(s.ResponseFont)
	w.wsResponse.setBodyFont(s.ResponseFont)

	// The curl command says how long the request can take and whether it
	// follows redirects.
	w.config.Timeout, w.config.NoRedirects = s.Timeout, s.NoRedirects
	w.request.updateCurl()

	w.config.JSONColours = s.JSONColours
	w.applyJSONColours()

	if s.Colours != w.config.Colours {
		w.config.Colours = s.Colours
		w.applyColours()
	}

	// The files are given their new keys where they are, before they are
	// moved.
	// Keys that could not be opened are tried again, as the passphrase may
	// not have been given.
	if s.PublicKey != w.config.PublicKey || s.PrivateKey != w.config.PrivateKey || w.keys.Err() != nil {
		if keys, err := openKeys(w.QWidget, s); err != nil {
			failed = append(failed, err)
		} else {
			w.config.PublicKey, w.config.PrivateKey = s.PublicKey, s.PrivateKey
			if err := w.changeKeys(keys); err != nil {
				failed = append(failed, fmt.Errorf("not every file could be given the keys: %w", err))
			}
		}
	}

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
	w.ws.highlight.json.setColours(colours)
	w.ws.refreshPlaceholders()
	w.response.setJSONColours(colours)
	w.grpcResponse.setJSONColours(colours)
	w.wsResponse.setJSONColours(colours)
}

// drawSidebarIcons gives the tabs of the sidebar their icons.
func (w *Window) drawSidebarIcons() {
	w.sidebar.SetTabIcon(tabWorkspaces, drawTabIcon(themeColour(colourWorkspaces), drawMonitor))
	w.sidebar.SetTabIcon(tabEnvironments, drawTabIcon(themeColour(colourEnvironments), drawGlobe))
	w.sidebar.SetTabIcon(tabRequests, drawTabIcon(themeColour(colourRequests), drawFolder))
}

// applyColours shows the icons, the buttons which send, the placeholders and
// the errors in the colours of the settings, everywhere they are shown.
func (w *Window) applyColours() {
	setColours(w.config.Colours)

	w.drawSidebarIcons()
	w.workspaces.recolour()
	w.environments.recolour()
	w.requests.draw(w.requests.selection())
	w.requests.drawViewIcons()
	w.requests.sets.draw(w.requests.sets.selected())
	w.tabs.recolour()

	for _, button := range []*qt.QPushButton{w.request.send, w.request.execute, w.grpc.invoke, w.ws.connect} {
		makePrimary(button)
	}

	w.request.highlight.setColours()
	w.request.refreshPlaceholders()
	w.grpc.highlight.setColours()
	w.grpc.refreshPlaceholders()
	w.ws.highlight.setColours()
	w.ws.refreshPlaceholders()
	w.ws.styleSend()

	w.grpc.recolour()
	w.response.recolour()
	w.grpcResponse.recolour()
	w.wsResponse.recolour()
}

// expand substitutes the variables of the active environment into text.
func (w *Window) expand(text string) string {
	return w.environments.store.Expand(text)
}

// expander returns a function which substitutes the variables of the active
// environment into each part of one request. A value made by a function,
// such as a random one, is then the same in every part.
func (w *Window) expander() func(string) string {
	return w.environments.store.Expander()
}

// options returns how requests are sent, as the settings say.
func (w *Window) options() rest.Options {
	return rest.Options{Timeout: w.timeout(), NoRedirects: w.config.NoRedirects}
}

// timeout returns how long a request can take, which is zero for no limit.
func (w *Window) timeout() time.Duration {
	return time.Duration(w.config.Timeout) * time.Second
}

// environmentChanged refreshes what depends on the active environment, as
// what the placeholders in the request resolve to may have changed.
func (w *Window) environmentChanged() {
	w.updateHeaders()
	w.request.refreshPlaceholders()
	w.grpc.refreshPlaceholders()
	w.ws.refreshPlaceholders()
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
	if w.open == nil && w.unsaved == collection.TypeGRPC {
		return pageGRPC
	}
	if w.open != nil && w.open.Type == collection.TypeWebSocket || w.open == nil && w.unsaved == collection.TypeWebSocket {
		return pageWS
	}
	return pageRequest
}

// escape closes what is open in place of the request: the environment
// editor, or else the bar text is found in the response with, or else the
// curl command or the preview of the URL of a REST request.
func (w *Window) escape() {
	switch w.stack.CurrentIndex() {
	case pageEditor:
		w.closeEditor()
	case pageRequest:
		if !w.response.find.close() {
			w.request.escape()
		}
	case pageGRPC:
		w.grpcResponse.find.close()
	case pageWS:
		w.wsResponse.find.close()
	}
}

// find opens the bar in which text to find in the response is typed.
func (w *Window) find() {
	switch w.stack.CurrentIndex() {
	case pageRequest:
		w.response.find.open()
	case pageGRPC:
		w.grpcResponse.find.open()
	case pageWS:
		w.wsResponse.find.open()
	}
}

// quickOpen shows the requests in the sidebar with the keyboard in their
// search box, to find a request and open it without the mouse.
func (w *Window) quickOpen() {
	w.sidebar.SetCurrentIndex(tabRequests)
	w.requests.showView(viewRequests)
	w.requests.focusSearch()
}

// focusSidebar shows a tab of the sidebar with the keyboard in its list, to
// move through it and act on what is selected without the mouse. The
// selection is left as it is.
func (w *Window) focusSidebar(tab int) {
	w.sidebar.SetCurrentIndex(tab)
	switch tab {
	case tabWorkspaces:
		w.workspaces.list.SetFocus()
	case tabEnvironments:
		w.environments.list.SetFocus()
	case tabRequests:
		w.requests.focusList()
	}
}

// closeEditor returns to the request UI.
func (w *Window) closeEditor() {
	w.stack.SetCurrentIndex(w.requestPage())
}

// openRequest shows a saved request that was selected in the sidebar: in
// the tab it is pinned in, or else in the tab that is not pinned.
func (w *Window) openRequest(r *collection.Request) {
	if w.open != r {
		w.stash(w.tabs.current())
		w.showTab(w.tabs.open(r), false)
	}
	w.closeEditor()
}

// show replaces the contents of the request UI with a saved request, or with
// an empty unsaved REST request if r is nil. The environment editor is left
// open if it is.
func (w *Window) show(r *collection.Request) {
	// What was typed into the URL is kept by the request it was typed into.
	w.request.url.commit()
	if w.cancel != nil {
		w.cancel()
	}
	// A connection is to the request it was opened for.
	w.disconnectWS()
	w.shown++

	w.open = r
	w.unsaved = collection.TypeREST
	w.request.save.SetVisible(r == nil)
	w.grpc.save.SetVisible(r == nil)
	w.ws.save.SetVisible(r == nil)
	switch {
	case r == nil:
		w.request.load(rest.Request{Method: http.MethodGet})
	case r.Type == collection.TypeGRPC:
		w.grpc.load(*r.GRPC)
		w.grpcResponse.clear()
		w.reflect(false)
	case r.Type == collection.TypeWebSocket:
		w.ws.load(*r.WebSocket)
		w.wsResponse.clear()
	default:
		w.request.load(*r.Request)
	}

	w.updateHeaders()

	if w.requestPage() == pageRequest {
		w.response.clear()
	}
	if w.stack.CurrentIndex() != pageEditor {
		w.stack.SetCurrentIndex(w.requestPage())
	}
}

// showDraft shows a request that is not saved, which show(nil) has made way
// for, in the request UI of its type.
func (w *Window) showDraft(draft *collection.Request) {
	w.unsaved = draft.Type
	switch draft.Type {
	case collection.TypeGRPC:
		w.grpc.load(*draft.GRPC)
		w.grpcResponse.clear()
		w.reflect(false)
	case collection.TypeWebSocket:
		w.ws.load(*draft.WebSocket)
		w.wsResponse.clear()
	default:
		w.request.load(*draft.Request)
	}
	if w.stack.CurrentIndex() != pageEditor {
		w.stack.SetCurrentIndex(w.requestPage())
	}
}

// editEnvironment opens the environment in use in the editor, or shows the
// environments in the sidebar if none is in use.
func (w *Window) editEnvironment() {
	if w.environments.store.Active < 0 {
		w.focusSidebar(tabEnvironments)
		return
	}
	w.sidebar.SetCurrentIndex(tabEnvironments)
	w.environments.edit()
}

// showSets shows the sets in the sidebar, with the keyboard in their list,
// where the one chosen is selected.
func (w *Window) showSets() {
	w.sidebar.SetCurrentIndex(tabRequests)
	w.requests.showView(viewSets)
	w.requests.sets.list.SetFocus()
}

// updateSets lists the sets of requests in the selector above the tabs, with
// the one chosen selected.
func (w *Window) updateSets() {
	store := w.requests.store
	names := make([]string, len(store.Sets))
	for i, set := range store.Sets {
		names[i] = set.Name
	}
	w.environment.setSets(names, slices.Index(store.Sets, w.requests.sets.active))
}

// status shows a message in the status bar until another replaces it. Line
// breaks are removed as the bar has one line. The bar is kept for problems
// with the saved data, which have nowhere else to be shown; requests and
// their responses are not reported in it.
func (w *Window) status(message string) {
	w.StatusBar().ShowMessage(strings.Join(strings.Fields(message), " "))
}

// updateHeaders lists the workspaces and the environments above the tabs,
// with those in use selected.
func (w *Window) updateHeaders() {
	w.environment.setWorkspaces(w.workspaces.names, w.workspaces.list.CurrentRow())

	store := w.environments.store
	names := make([]string, len(store.Environments))
	for i, e := range store.Environments {
		names[i] = e.Name
	}
	w.environment.setEnvironments(names, store.Active)
}

// outcome is what is done, on the main thread, with the result of the work
// of a request that ended without an error.
type outcome struct {
	// capture sets variables of the active environment from the result.
	// It is called once, even if the result is no longer wanted on show.
	capture func()
	// show shows the result. It is called again whenever the tab of the
	// request is gone back to.
	show func()
}

// run does the work of the Send or Invoke button in the background, or
// cancels the work already in flight. The work returns what to do with its
// result, which is done on the main thread.
func (w *Window) run(button *qt.QPushButton, response *responsePanel, work func(context.Context) (outcome, error)) {
	if w.cancel != nil {
		w.cancel()
		return
	}

	// The work is given up on when it has taken as long as the settings
	// allow, which is the same for every kind of request.
	limit := w.config.Timeout
	ctx, cancel := context.WithCancel(context.Background())
	if limit > 0 {
		ctx, cancel = context.WithTimeout(context.Background(), w.timeout())
	}
	w.cancel = cancel
	shown := w.shown
	tab := w.tabs.current()

	label := button.Text()
	button.SetText("Cancel")
	response.clear()
	start := time.Now()

	// Qt widgets may only be touched from the main thread, so the result is
	// handed back to it.
	go func() {
		done, err := work(ctx)
		elapsed := time.Since(start)

		mainthread.Start(func() {
			cancel()
			w.cancel = nil
			button.SetText(label)

			// The request was made for what it captures as much as for
			// what it shows, so that is kept whatever is on show by now.
			if err == nil {
				done.capture()
			}

			var result func()
			switch {
			case shown != w.shown:
				// Another request has been shown since, so the result is
				// no longer wanted.
				return
			case errors.Is(err, context.Canceled):
				result = func() { response.showCancelled(elapsed) }
			case errors.Is(err, context.DeadlineExceeded):
				timedOut := fmt.Errorf("timed out after %s, which is the limit in the settings", plural(limit, "second"))
				result = func() { response.showError(timedOut, elapsed) }
			case err != nil:
				result = func() { response.showError(err, elapsed) }
			default:
				result = done.show
			}

			// The tab keeps the result, to show it again when the tab is
			// gone back to.
			result()
			tab.replay = result
		})
	}()
}

// send starts the REST request, or cancels the request in flight.
func (w *Window) send() {
	w.request.url.commit()
	request := w.request.request()
	req := request.Expand(w.expander())
	req.Options = w.options()

	w.run(w.request.send, w.response, func(ctx context.Context) (outcome, error) {
		res, err := rest.Send(ctx, w.client, req)
		return w.restOutcome(res, request.Captures), err
	})
}

// executeCurl makes the REST request by running the curl command that is
// shown for it, or cancels the request in flight.
func (w *Window) executeCurl() {
	request := w.request.request()
	// What runs is the command as it is shown: a value made by a function
	// would not be the same if it were made again.
	req := w.request.commanded

	w.run(w.request.execute, w.response, func(ctx context.Context) (outcome, error) {
		res, err := rest.RunCurl(ctx, req)
		return w.restOutcome(res, request.Captures), err
	})
}

// restOutcome returns what to do with the response to a REST request, which
// is nil if there was an error instead. Values are captured from a response
// with a status that is a success.
func (w *Window) restOutcome(res *rest.Response, captures []capture.Rule) outcome {
	var captured []captureResult
	return outcome{
		capture: func() {
			if res.StatusCode >= 200 && res.StatusCode < 300 {
				captured = w.capture(captures, string(res.Body))
			}
		},
		show: func() {
			w.response.show(res)
			w.response.showCaptured(captured)
		},
	}
}

// invoke starts the gRPC request, or cancels the request in flight.
func (w *Window) invoke() {
	request := w.grpc.request()
	req := request.Expand(w.expander())

	// The service definition is loaded by the call if it has not been yet.
	var schema *rpc.Schema
	if target, err := req.Target(); err == nil {
		schema = w.schemas[target]
	}

	w.run(w.grpc.invoke, w.grpcResponse, func(ctx context.Context) (outcome, error) {
		res, err := rpc.Invoke(ctx, req, schema)

		// Values are captured from the last message of a call that ended
		// with OK, which is the only message of a unary method.
		var captured []captureResult
		return outcome{
			capture: func() {
				switch {
				case !res.OK():
				case len(res.Messages) == 0:
					captured = failedCaptures(request.Captures, errors.New("there was no response message"))
				default:
					captured = w.capture(request.Captures, res.Messages[len(res.Messages)-1])
				}
			},
			show: func() {
				w.grpcResponse.showRPC(res)
				w.grpcResponse.showCaptured(captured)
			},
		}, err
	})
}

// endpointCollection returns the folder of requests made from endpoints that
// the gRPC request on show is in, or nil if it is in none. That is the
// folder which holds the folder the request is in, when that one is named
// after a service of the server. What the outer folder is called makes no
// difference.
func (w *Window) endpointCollection() *collection.Folder {
	if w.open == nil || w.open.Type != collection.TypeGRPC {
		return nil
	}
	service := w.requests.store.FolderOf(w.open)
	if service == nil || !w.grpc.endpoints.hasService(service.Name) {
		return nil
	}
	outer, _ := w.requests.store.ParentOf(service)
	return outer
}

// syncEndpoints has the endpoints which the collection of the request on
// show has requests for ticked in the Service Definitions tab.
func (w *Window) syncEndpoints() {
	if outer := w.endpointCollection(); outer != nil {
		w.grpc.endpoints.setExisting(endpointsOf(outer), true)
		return
	}
	w.grpc.endpoints.setExisting(nil, false)
}

// createRequests makes a gRPC request for each of the methods, for the
// server of the request on show, and shows them in the sidebar. They are
// added to the collection that request is in, and to no other. If it is in
// none they are a new folder at the top level, named after the server.
func (w *Window) createRequests(methods []string) {
	url := w.grpc.url.Text()
	if outer := w.endpointCollection(); outer != nil {
		addEndpoints(outer, url, methods)
		w.requests.extended(w.open)
		w.sidebar.SetCurrentIndex(tabRequests)
		return
	}

	name := strings.TrimSpace(url)
	if name == "" {
		name = "New Folder"
	}

	w.requests.adopt(endpointFolder(name, url, methods))
	w.sidebar.SetCurrentIndex(tabRequests)
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
		w.grpc.setStatus("")
		return
	}

	if schema, ok := w.schemas[target]; ok && !force {
		w.grpc.setSchema(schema)
		w.syncEndpoints()
		return
	}

	w.grpc.setStatus("")

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
			w.syncEndpoints()
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
		w.grpc.setStatus("An example needs the service definition, which has not been loaded. Enter the server address and press Reload.")
		return
	}

	example, err := schema.Example(req.Method)
	if err != nil {
		w.grpc.setStatus("An example could not be made: " + err.Error())
		return
	}
	w.grpc.message.SetPlainText(example)
}

// Package ui contains the Qt user interface.
package ui

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	qt "github.com/mappu/miqt/qt6"
	"github.com/mappu/miqt/qt6/mainthread"

	"github.com/nomad-software/triggerapi/internal/collection"
	"github.com/nomad-software/triggerapi/internal/crypt"
	"github.com/nomad-software/triggerapi/internal/mcp"
	"github.com/nomad-software/triggerapi/internal/network"
	"github.com/nomad-software/triggerapi/internal/oauth"
	"github.com/nomad-software/triggerapi/internal/rpc"
	"github.com/nomad-software/triggerapi/internal/settings"
)

// Pages of the main stack: one for the UI of each type of request, in the
// order of Window.uis, then the environment editor.
const (
	pageRequest = iota
	pageGRPC
	pageWS
	pageGraphQL
	pageMCP
	pageEditor
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
	tabHistory
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

	// net sends requests as the network settings say, and client is the
	// http.Client with it as its transport. See windownetwork.go.
	net    *network.Client
	client *http.Client
	// netErr is what was wrong with the network settings when they were
	// last loaded, which has them loaded again when the settings are next
	// accepted, as a passphrase may not have been given.
	netErr error
	// secrets are the passphrases and passwords the network settings were
	// given, kept for the session so that each is asked for once.
	secrets map[network.Secret]string
	// sidebar has a tab for the workspaces, and one each for the
	// environments, the requests and the history of the workspace in use.
	sidebar      *qt.QTabWidget
	workspaces   *workspaceList
	environments *envList
	requests     *requestTree
	history      *historyList
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
	// stack shows the UI of a type of request (see uis) or the environment
	// editor.
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
	// graphql and graphqlResponse are the GraphQL request UI.
	graphql         *graphqlPanel
	graphqlResponse *responsePanel
	// mcp and mcpResponse are the MCP request UI, and mcpClient the client
	// of the server of mcpServer (see mcp.Request.Server), or nil.
	mcp         *mcpPanel
	mcpResponse *responsePanel
	mcpClient   *mcp.Client
	mcpServer   string
	// uis are the UIs of the types of request, in the order of their
	// pages. Whatever is the same for every type goes through them.
	uis []*requestUI

	// open is the saved request shown in the request UI for its type, which
	// edits are written back to. It is nil when the request UI of the type
	// unsaved holds a request that is not saved.
	open *collection.Request
	// shown counts the requests that have been shown, so that results which
	// arrive after another request has been shown can be told apart and
	// dropped.
	shown int

	// schemas are the service definitions of the gRPC servers that have been
	// asked for them, by target, graphSchemas the schemas of GraphQL ones,
	// by endpoint, in the schema definition language, and mcpSchemas what
	// MCP servers have, by mcp.Request.Server.
	schemas      serverCache[*rpc.Schema]
	graphSchemas serverCache[string]
	mcpSchemas   serverCache[*mcp.Schema]

	// tokens are the OAuth 2.0 tokens that have been got, which are kept
	// for the session, whatever workspace is in use. See windowoauth.go.
	tokens *oauth.Cache

	// diffDirs are the folders of the files the responses of the history
	// were compared in, which are deleted when the window closes.
	diffDirs []string

	// cancel aborts the request in flight. It is nil when idle and must only
	// be accessed from the main thread.
	cancel context.CancelFunc
}

// NewWindow creates the main window, loading the saved data from files. It
// must be called on the main thread after the QApplication has been created.
func NewWindow(files Files) *Window {
	w := &Window{
		QMainWindow: qt.NewQMainWindow2(),
		request:     newRequestPanel(),
		grpc:        newGRPCPanel(),
		ws:          newWSPanel(),
		graphql:     newGraphQLPanel(),
		mcp:         newMCPPanel(),
		tokens:      oauth.NewCache(),
		editor:      newEnvEditor(),
		tabs:        newRequestTabs(),
		environment: newEnvironmentSelector(),
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
	w.graphqlResponse = newResponsePanel(w.config.ResponseFont)
	w.mcpResponse = newResponsePanel(w.config.ResponseFont)
	w.uis = []*requestUI{
		{typ: collection.TypeREST, page: pageRequest, editor: w.request, response: w.response, send: w.send, timing: true},
		{typ: collection.TypeGRPC, page: pageGRPC, editor: w.grpc, response: w.grpcResponse, send: w.invoke,
			// The methods offered are those of the server.
			shown:     func() { w.reflect(false) },
			endpoints: &endpointSource{grpcEndpoints, w.grpc.endpoints, w.grpc.url.Text}},
		{typ: collection.TypeWebSocket, page: pageWS, editor: w.ws, response: w.wsResponse, send: w.connectAndSendWS},
		{typ: collection.TypeGraphQL, page: pageGraphQL, editor: w.graphql, response: w.graphqlResponse, send: w.sendGraphQL,
			// The schema is that of the server, if it is on show.
			shown: func() { w.graphql.loadSchema(false) }, timing: true},
		{typ: collection.TypeMCP, page: pageMCP, editor: w.mcp, response: w.mcpResponse, send: w.callMCP,
			// What can be called is what the server has.
			shown:     func() { w.discoverMCP(false) },
			endpoints: &endpointSource{mcpEndpoints, w.mcp.endpoints, w.mcp.url.Text}},
	}
	// What to capture from a response is set beside the response, though
	// it is kept by the request.
	w.response.setCaptureTable(w.request.captures)
	w.grpcResponse.setCaptureTable(w.grpc.captures)
	w.graphqlResponse.setCaptureTable(w.graphql.captures)
	w.mcpResponse.setCaptureTable(w.mcp.captures)
	for _, ui := range w.uis {
		if ui.timing {
			ui.response.addTimingTab()
		}
		// A response is saved to a file named after its request.
		ui.response.requestName = func() string {
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
		// An open connection is closed with the window, as is the client of
		// an MCP server. The connection goes in the history, which is then
		// written.
		w.disconnectWS()
		w.closeMCP()
		w.history.flush()
		w.removeDiffFiles()
		super(event)
	})

	// The workspace that was last in use is opened, which is the first
	// there is if this is the first run with workspaces.
	w.workspaces = newWorkspaceList(w.workspacesDir)
	w.firstWorkspace()
	// A failure to open the keys is reported when the files are loaded.
	w.keys, _ = openKeys(w.QWidget, w.config)
	currentKeys := func() *crypt.Keys { return w.keys }
	// Requests to a host whose settings could not be loaded fail with the
	// reason; the others are sent as the settings say.
	client, err := w.loadNetwork(w.config.Network)
	if err != nil {
		w.netErr = err
		w.status(err.Error())
	}
	w.useNetwork(client)
	name, err := w.workspaces.load(w.config.Workspace)
	if err != nil {
		w.status("The workspaces could not be listed: " + err.Error())
	}
	w.config.Workspace = name
	l := w.loadWorkspace(name)

	saveFailed := func(err error) {
		w.status("Changes could not be saved: " + err.Error())
	}

	// The environments can be replaced by ones from another file, so the
	// current store is looked up each time.
	for _, ui := range w.uis {
		ui.editor.highlighter().expand = w.expand
	}

	w.environments = newEnvList(l.envs, l.envPath)
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

	w.requests = newRequestTree(l.requests, l.requestsPath)
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

	w.history = newHistoryList(l.history, l.historyPath)
	w.history.onError = saveFailed
	w.history.keys = currentKeys
	w.history.limit = func() int { return w.config.History() }
	w.history.onOpen = w.openEntry
	w.history.onGoTo = w.goToEntry
	w.history.onDiff = w.diffEntries
	w.tabs.onSelect = func(from, to *requestTab) {
		w.stash(from)
		w.showTab(to, true)
	}
	// The pinned tabs are those of the set chosen, which follows them.
	w.tabs.onPins = func() { w.requests.sets.keep(w.tabs.pinned()) }
	w.requests.sets.pinned = w.tabs.pinned
	w.tabs.onNew = w.newRequest
	w.requests.sets.onChoose = w.chooseSet
	// Anything selected in the requests closes the environment editor, as
	// another tab of the sidebar does.
	w.requests.onSelect = w.closeEditor
	w.requests.create = func() (*collection.Request, bool) {
		switch typ, ok := askRequestType(w.QWidget); {
		case !ok:
			return nil, false
		case w.open == nil && w.unsaved == typ:
			// An unsaved request being composed becomes the new request.
			return w.composed("New Request"), true
		default:
			return collection.New(typ, "New Request"), true
		}
	}

	for _, ui := range w.uis {
		if ui.endpoints != nil {
			ui.endpoints.table.onCreate = func(endpoints []string) { w.createRequests(ui, endpoints) }
		}
		ui.editor.saveButton().OnClicked(w.saveRequest)
		auth := ui.editor.authorization()
		auth.getToken.OnClicked(func() { w.getToken(ui) })
		auth.clearToken.OnClicked(func() { w.clearToken(ui) })
		auth.onEdited = func() { w.showToken(ui) }
		ui.editor.setCodeFont(w.config.ResponseFont)
		// Every edit of a saved request is written back to it and saved.
		ui.editor.setOnChange(func() {
			if w.open != nil && w.open.Type == ui.typ {
				w.open.SetContent(ui.editor.compose(w.open.Name))
				w.requests.save()
			}
		})
	}

	w.request.expand = w.expand
	w.request.expander = w.expander
	w.request.options = w.options
	w.request.authorize = w.keptToken
	w.request.send.OnClicked(w.send)
	w.request.execute.OnClicked(w.executeCurl)

	w.grpc.onTarget = func() { w.reflect(false) }
	w.grpc.reload.OnClicked(func() { w.reflect(true) })
	w.grpc.example.OnClicked(w.useExample)
	w.grpc.invoke.OnClicked(w.invoke)

	w.ws.connect.OnClicked(w.toggleWS)
	w.ws.send.OnClicked(w.sendWS)

	w.graphql.send.OnClicked(w.sendGraphQL)
	w.graphql.onSchema = w.loadGraphQLSchema

	w.mcp.onTarget = func() { w.discoverMCP(false) }
	w.mcp.reload.OnClicked(func() { w.discoverMCP(true) })
	w.mcp.example.OnClicked(w.useMCPExample)
	w.mcp.call.OnClicked(w.callMCP)

	qt.NewQShortcut2(qt.NewQKeySequence2("Ctrl+Return"), w.QObject).OnActivated(func() {
		if ui := w.pageUI(w.stack.CurrentIndex()); ui != nil {
			ui.send()
		}
	})

	// The order matches the page constants. Dragging the response on one
	// page sets its height on the others, which are given it when they are
	// next shown.
	w.stack = qt.NewQStackedWidget2()
	w.responseHeight = w.config.ResponseHeight
	w.splits = map[int]*qt.QSplitter{}
	for _, ui := range w.uis {
		split := newRequestPage(ui)
		split.OnSplitterMoved(func(int, int) { w.responseHeight = split.Sizes()[1] })
		w.splits[w.stack.AddWidget(split.QWidget)] = split
	}
	w.stack.AddWidget(w.editor.root)
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
	w.sidebar.AddTab(plainPage(w.history.root), "")
	w.drawSidebarIcons()
	w.sidebar.SetTabToolTip(tabWorkspaces, "Workspaces")
	w.sidebar.SetTabToolTip(tabEnvironments, "Environments")
	w.sidebar.SetTabToolTip(tabRequests, "Requests")
	w.sidebar.SetTabToolTip(tabHistory, "History")
	// The requests are what is wanted first.
	w.sidebar.SetCurrentIndex(tabRequests)
	// The environment editor belongs to the Environments tab, and is closed
	// for what the request UI shows when another is chosen.
	w.sidebar.OnCurrentChanged(func(tab int) {
		if tab != tabEnvironments && w.stack.CurrentIndex() == pageEditor {
			w.closeEditor()
		}
	})

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
	// Ctrl+W, Ctrl+E, Ctrl+R and Ctrl+H go to the tabs of the sidebar.
	for tab, key := range sidebarKeys {
		qt.NewQShortcut2(qt.NewQKeySequence2(key), w.QObject).OnActivated(func() { w.focusSidebar(tab) })
	}
	// Ctrl+S goes to the sets, in the Requests tab.
	qt.NewQShortcut2(qt.NewQKeySequence2("Ctrl+S"), w.QObject).OnActivated(w.showSets)

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
	w.about.OnTriggered(func() { showAbout(w.QWidget) })
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

// environmentChanged refreshes what depends on the active environment, as
// what the placeholders in the request resolve to may have changed.
func (w *Window) environmentChanged() {
	w.updateHeaders()
	for _, ui := range w.uis {
		ui.editor.refreshPlaceholders()
		w.showToken(ui)
	}
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
	typ := w.unsaved
	if w.open != nil {
		typ = w.open.Type
	}
	return w.requestUI(typ).page
}

// escape closes what is open in place of the request: the environment
// editor, or else the bar text is found in the response with, or else what
// the request panel shows in place of its fields, such as the curl command
// or the preview of the URL of a REST request.
func (w *Window) escape() {
	if w.stack.CurrentIndex() == pageEditor {
		w.closeEditor()
		return
	}
	if ui := w.pageUI(w.stack.CurrentIndex()); ui != nil && !ui.response.find.close() {
		ui.editor.escape()
	}
}

// find opens the bar in which text to find in the response is typed.
func (w *Window) find() {
	if ui := w.pageUI(w.stack.CurrentIndex()); ui != nil {
		ui.response.find.open()
	}
}

// quickOpen shows the requests in the sidebar with the keyboard in their
// search box, to find a request and open it without the mouse.
func (w *Window) quickOpen() {
	w.sidebar.SetCurrentIndex(tabRequests)
	w.requests.showView(viewRequests)
	w.requests.focusSearch()
}

// sidebarKeys are the shortcuts which go to the tabs of the sidebar.
var sidebarKeys = map[int]string{
	tabWorkspaces:   "Ctrl+W",
	tabEnvironments: "Ctrl+E",
	tabRequests:     "Ctrl+R",
	tabHistory:      "Ctrl+H",
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
		// The requests, not the sets, as Ctrl+S goes to those.
		w.requests.showView(viewRequests)
		w.requests.tree.SetFocus()
	case tabHistory:
		w.history.tree.SetFocus()
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
	for _, ui := range w.uis {
		ui.editor.saveButton().SetVisible(r == nil)
	}
	w.request.unsaved = r == nil
	if r == nil {
		// An empty REST request, to be composed.
		w.load(collection.New(collection.TypeREST, ""))
	} else {
		w.load(r)
	}

	w.updateHeaders()
	if w.stack.CurrentIndex() != pageEditor {
		w.stack.SetCurrentIndex(w.requestPage())
	}
}

// load fills the request UI of the type of a request with it, with no
// response.
func (w *Window) load(r *collection.Request) {
	ui := w.requestUI(r.Type)
	ui.editor.show(r)
	ui.response.clear()
	w.showToken(ui)
	if ui.shown != nil {
		ui.shown()
	}
}

// editEnvironment opens the environment in use in the editor, showing the
// environments in the sidebar unless it is showing the requests, or shows the
// environments in the sidebar if none is in use. If the editor is open, it
// closes it, unless the sidebar is showing the environments, where it belongs.
func (w *Window) editEnvironment() {
	if w.stack.CurrentIndex() == pageEditor && w.sidebar.CurrentIndex() != tabEnvironments {
		w.closeEditor()
		return
	}
	if w.environments.store.Active < 0 {
		w.focusSidebar(tabEnvironments)
		return
	}
	if w.sidebar.CurrentIndex() != tabRequests {
		w.sidebar.SetCurrentIndex(tabEnvironments)
	}
	w.environments.edit()
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

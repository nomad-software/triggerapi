package ui

import (
	"github.com/nomad-software/qute/qtcore"
	"github.com/nomad-software/qute/qtgui"
	"github.com/nomad-software/qute/qtwidgets"

	"strings"

	"github.com/nomad-software/triggerapi/internal/collection"
	"github.com/nomad-software/triggerapi/internal/mcp"
)

// mcpHeadings are the headings the tools, resources and prompts of an MCP
// server are listed under, in the drop-down and the table of endpoints. The
// full name of one is its heading and its name, as Tools/echo.
var mcpHeadings = []struct {
	kind    mcp.Kind
	heading string
}{
	{mcp.KindTool, "Tools"},
	{mcp.KindResource, "Resources"},
	{mcp.KindPrompt, "Prompts"},
}

// mcpEndpoint returns the full name of a tool, resource or prompt. A request
// with a name and no kind calls a tool. It is "" if there is no name.
func mcpEndpoint(kind mcp.Kind, name string) string {
	if name == "" {
		return ""
	}
	if kind == "" {
		kind = mcp.KindTool
	}
	for _, h := range mcpHeadings {
		if h.kind == kind {
			return h.heading + "/" + name
		}
	}
	return string(kind) + "/" + name
}

// mcpSplit returns the kind and the name of a full name made by
// mcpEndpoint.
func mcpSplit(full string) (mcp.Kind, string) {
	heading, name, found := strings.Cut(full, "/")
	if !found {
		return "", ""
	}
	for _, h := range mcpHeadings {
		if h.heading == heading {
			return h.kind, name
		}
	}
	return mcp.Kind(heading), name
}

// mcpEndpoints makes MCP requests of the tools, resources and prompts of a
// server, under their headings.
var mcpEndpoints = endpointKind{
	endpoint: func(r *collection.Request) string {
		if r.Type == collection.TypeMCP {
			return mcpEndpoint(r.MCP.Kind, r.MCP.Name)
		}
		return ""
	},
	request: func(url, full string) *collection.Request {
		kind, name := mcpSplit(full)
		return collection.NewMCP(name, mcp.Request{URL: url, Kind: kind, Name: name})
	},
}

// mcpPanel holds the widgets used to compose an MCP request: the server, the
// tool, resource or prompt to call, which come from the server, its
// arguments, and the headers and authorization of the HTTP requests. Only
// the Streamable HTTP transport is offered.
type mcpPanel struct {
	root *qtwidgets.QWidget

	url  *qtwidgets.QLineEdit
	call *qtwidgets.QPushButton
	// save saves a request that is not saved yet.
	save *qtwidgets.QPushButton
	// endpoint is the tool, resource or prompt to call.
	endpoint *methodCombo
	reload   *qtwidgets.QPushButton
	// status reports a failure to load what the server has.
	status statusLine

	// description is what the server says of what is chosen, which scrolls
	// in a frame beside the arguments, as tall as they are.
	description     *qtwidgets.QLabel
	descriptionArea *qtwidgets.QScrollArea
	// argumentsSplit has the arguments and the description side by side.
	argumentsSplit *qtwidgets.QSplitter
	arguments      *qtwidgets.QPlainTextEdit
	// argumentsFold folds the objects and arrays of the arguments.
	argumentsFold *folder
	example       *qtwidgets.QPushButton
	// format sets out the arguments.
	format  *formatButton
	headers *keyValueTable
	auth    *authEditor
	// captures are the variables to set from the response. See
	// requestPanel.captures.
	captures *keyValueTable
	// assertions are what the response should be. Like the captures they
	// are shown beside the response.
	assertions *assertTable
	// endpoints lists what the server has, to make requests of.
	endpoints *endpointTable

	// schema is what the server has, once it has been loaded.
	schema *mcp.Schema
	// transport is that of the request shown, which is kept as it is.
	transport mcp.Transport
	// target is the server last reported by onTarget.
	target string
	// onTarget is called after the user has finished changing the server.
	onTarget func()

	panelState
}

func newMCPPanel() *mcpPanel {
	p := &mcpPanel{root: qtwidgets.NewQWidget(nil), panelState: newPanelState()}

	p.url = qtwidgets.NewQLineEdit(nil)
	p.url.SetPlaceholderText("Server address, e.g. http://localhost:8000/mcp or {{host}}/mcp")
	p.typedLine(p.url)
	p.url.OnEditingFinished(p.targetChanged)
	p.highlight.lineEdit(p.url)

	p.call = qtwidgets.NewQPushButtonWithText("Call", nil)
	makePrimary(p.call)
	// Save is only for a request that is not saved, as one that is saved is
	// saved with every change.
	p.save = qtwidgets.NewQPushButtonWithText("Save", nil)

	p.endpoint = newMethodCombo()
	// The heading says what kind of thing it is while the list is open.
	p.endpoint.closed = func(full string) string {
		_, name := mcpSplit(full)
		return name
	}
	p.endpoint.OnCurrentIndexChanged(func(int) {
		p.describe()
		p.changed()
	})

	p.reload = qtwidgets.NewQPushButtonWithText("Reload", nil)

	// The address takes a quarter of the width left by the buttons and
	// what is called the rest, as in the gRPC panel.
	top := qtwidgets.NewQHBoxLayout(nil)
	top.AddWidgetFull(p.url.AsQWidget(), 1, 0)
	top.AddWidgetFull(p.endpoint.AsQWidget(), 3, 0)
	top.AddWidget(p.reload.AsQWidget())
	top.AddWidget(p.call.AsQWidget())
	top.AddWidget(p.save.AsQWidget())

	p.status = newStatusLine()

	p.description = qtwidgets.NewQLabel(nil)
	p.description.SetTextFormat(qtcore.PlainText)
	p.description.SetWordWrap(true)
	p.description.SetEnabled(false)
	p.description.SetMargin(4)
	p.description.SetAlignment(qtcore.AlignTop | qtcore.AlignLeft)
	p.description.SetBackgroundRole(qtgui.QPaletteBase)

	p.descriptionArea = qtwidgets.NewQScrollArea(nil)
	p.descriptionArea.SetWidget(p.description.AsQWidget())
	p.descriptionArea.SetWidgetResizable(true)
	p.descriptionArea.SetHorizontalScrollBarPolicy(qtcore.ScrollBarAlwaysOff)
	p.descriptionArea.SetFocusPolicy(qtcore.NoFocus)
	p.descriptionArea.SetVisible(false)

	p.arguments = qtwidgets.NewQPlainTextEdit(nil)
	p.arguments.SetFont(qtgui.QFontDatabaseGetSystemFont(qtgui.QFontDatabaseFixedFont))
	p.arguments.SetPlaceholderText(`The arguments as a JSON object, e.g. {"city": "London"}`)
	p.typedText(p.arguments)
	p.highlight.document(p.arguments.Document(), always)
	p.argumentsFold = newFolder(p.arguments, always)

	p.example = qtwidgets.NewQPushButtonWithText("Use Example Arguments", nil)
	p.format = newFormatButton(p.arguments, p.commit)

	exampleBar := qtwidgets.NewQHBoxLayout(nil)
	exampleBar.AddWidget(p.example.AsQWidget())
	exampleBar.AddWidget(p.format.button.AsQWidget())
	exampleBar.AddStretch()

	// The arguments have the room of the description when there is none.
	p.argumentsSplit = qtwidgets.NewQSplitterWithOrientation(qtcore.Horizontal, nil)
	p.argumentsSplit.AddWidget(p.arguments.AsQWidget())
	p.argumentsSplit.AddWidget(p.descriptionArea.AsQWidget())
	p.argumentsSplit.SetChildrenCollapsible(false)
	p.argumentsSplit.SetSizes([]int{1, 1})

	argumentsTab := qtwidgets.NewQWidget(nil)
	argumentsLayout := qtwidgets.NewQVBoxLayout(argumentsTab)
	argumentsLayout.AddWidgetFull(p.argumentsSplit.AsQWidget(), 1, 0)
	argumentsLayout.AddLayout(exampleBar.AsQLayout())

	p.headers = newExclusiveTable("Header", "Value")
	p.headers.onChange = p.changed
	p.highlight.table(p.headers.table)

	p.auth = newAuthEditor(&p.panelState)
	p.auth.onChange = p.changed

	p.captures = newCaptureTable()
	p.captures.onChange = p.changed
	p.assertions = newAssertTable(p.highlight)
	p.assertions.onChange = p.changed

	p.endpoints = newEndpointTable()

	tabs := qtwidgets.NewQTabWidget(nil)
	tabs.AddTab(plainPage(argumentsTab), "Arguments")
	tabs.AddTab(p.headers.widget(), "Headers")
	tabs.AddTab(p.auth.root, "Authorization")
	tabs.AddTab(plainPage(p.endpoints.root), "Capabilities")

	layout := qtwidgets.NewQVBoxLayout(p.root)
	layout.AddLayout(top.AsQLayout())
	layout.AddWidget(p.status.AsQWidget())
	layout.AddWidget(tabs.AsQWidget())

	return p
}

// targetChanged reports that the request is for a different server, unless
// it is the one last reported.
func (p *mcpPanel) targetChanged() {
	if p.updating || p.url.Text() == p.target {
		return
	}
	p.target = p.url.Text()
	if p.onTarget != nil {
		p.onTarget()
	}
}

func (p *mcpPanel) widget() *qtwidgets.QWidget { return p.root }

func (p *mcpPanel) saveButton() *qtwidgets.QPushButton { return p.save }

func (p *mcpPanel) authorization() *authEditor { return p.auth }

// show shows an MCP request. See load.
func (p *mcpPanel) show(r *collection.Request) { p.load(*r.MCP) }

// compose returns the request in the widgets as an MCP request with a name.
func (p *mcpPanel) compose(name string) *collection.Request {
	return collection.NewMCP(name, p.request())
}

// request reads the widgets into an mcp.Request.
func (p *mcpPanel) request() mcp.Request {
	kind, name := mcpSplit(p.endpoint.current())
	return mcp.Request{
		Transport:  p.transport,
		URL:        p.url.Text(),
		Headers:    p.headers.items(),
		Auth:       p.auth.auth(),
		Kind:       kind,
		Name:       name,
		Arguments:  p.arguments.ToPlainText(),
		Captures:   captureRules(p.captures),
		Assertions: p.assertions.checks(),
	}
}

// load shows the request in the widgets without calling onChange or
// onTarget. What it calls is the only thing that can be chosen until what
// the server has has been loaded.
func (p *mcpPanel) load(r mcp.Request) {
	p.quietly(func() {
		p.transport = r.Transport
		p.url.SetText(r.URL)
		p.target = r.URL
		p.schema = nil
		p.endpoint.setMethods(nil, mcpEndpoint(r.Kind, r.Name))
		p.arguments.SetPlainText(r.Arguments)
		p.argumentsFold.reset()
		p.headers.setItems(r.Headers)
		p.auth.load(r.Auth)
		setCaptureRules(p.captures, r.Captures)
		p.assertions.setChecks(r.Assertions)
		p.endpoints.setMethods(nil)
		p.status.set("")
		p.describe()
	})
}

// setSchema offers what the server has to choose from. What the request
// calls stays chosen, and is kept in the list even if the server does not
// have it so that the request is not changed behind the user's back. A
// request that calls nothing takes the first.
func (p *mcpPanel) setSchema(schema *mcp.Schema) {
	names := make([]string, len(schema.Items))
	for i, item := range schema.Items {
		names[i] = mcpEndpoint(item.Kind, item.Name)
	}
	current := p.endpoint.current()

	p.quietly(func() {
		p.schema = schema
		p.endpoint.setMethods(names, current)
		p.endpoints.setMethods(names)
		p.describe()
	})
	p.status.set("")

	if p.endpoint.current() != current {
		p.changed()
	}
}

// setSchemaError reports that what the server has could not be loaded.
func (p *mcpPanel) setSchemaError(err error) {
	p.endpoints.setMethods(nil)
	p.status.set("The capabilities of the server could not be loaded: " + err.Error())
}

// chosen returns what the server says of what is chosen, if it has been
// loaded.
func (p *mcpPanel) chosen() (mcp.Item, bool) {
	if p.schema == nil {
		return mcp.Item{}, false
	}
	return p.schema.Find(mcpSplit(p.endpoint.current()))
}

// describe shows what the server says of what is chosen, if it says
// anything.
func (p *mcpPanel) describe() {
	item, _ := p.chosen()
	p.description.SetText(item.Description)
	p.descriptionArea.SetVisible(item.Description != "")
}

// setCodeFont sets the font the arguments are shown in.
func (p *mcpPanel) setCodeFont(family string) {
	p.arguments.SetFont(fixedFont(family))
}

// recolour shows Call, the placeholders and the status line in the colours
// of the settings.
func (p *mcpPanel) recolour() {
	makePrimary(p.call)
	p.highlight.setColours()
	p.refreshPlaceholders()
	p.status.recolour()
}

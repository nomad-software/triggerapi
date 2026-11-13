package ui

import (
	"github.com/nomad-software/qute/qtgui"
	"github.com/nomad-software/qute/qtwidgets"

	"github.com/nomad-software/triggerapi/internal/collection"
	"github.com/nomad-software/triggerapi/internal/completion"
	"github.com/nomad-software/triggerapi/internal/graphql"
)

// Tabs of the GraphQL panel.
const (
	tabGraphQLQuery = iota
	tabGraphQLVariables
	tabGraphQLHeaders
	tabGraphQLAuthorization
	tabGraphQLSchema
)

// graphqlPanel holds the widgets used to compose a GraphQL request: the
// endpoint, the query and its variables, and the headers and authorization
// of the HTTP request it is sent in. The Schema tab shows the schema of the
// server, which it describes through introspection, and which what can be
// typed in the query is offered from.
type graphqlPanel struct {
	root *qtwidgets.QWidget

	url  *qtwidgets.QLineEdit
	send *qtwidgets.QPushButton
	// save saves a request that is not saved yet.
	save *qtwidgets.QPushButton

	tabs  *qtwidgets.QTabWidget
	query *qtwidgets.QPlainTextEdit
	// queryFold folds the selections and arguments of the query, and
	// completer offers what can be typed in it.
	queryFold *folder
	completer *completer
	// variables are the values of the variables of the query, as JSON.
	variables *qtwidgets.QPlainTextEdit
	// variablesFold folds the objects and arrays of the variables.
	variablesFold *folder
	// format sets out the variables.
	format  *formatButton
	headers *keyValueTable
	auth    *authEditor
	// captures are the variables to set from the response. See
	// requestPanel.captures.
	captures *keyValueTable
	// assertions are what the response should be. Like the captures they
	// are shown beside the response.
	assertions *assertTable

	// described is the schema of the server, or nil if it has not been
	// loaded, and schema shows it in the schema definition language, and
	// reload asks the server for it again.
	described *graphql.Schema
	schema    *qtwidgets.QPlainTextEdit
	// schemaFold folds the definitions of the schema.
	schemaFold *folder
	reload     *qtwidgets.QPushButton
	// status reports a failure to load the schema.
	status statusLine
	// loaded is the endpoint the schema was last asked for, or "" if it
	// has not been asked for since the request was shown.
	loaded string

	// onSchema is called to load the schema of the server: when the Schema
	// tab is shown, when the query is given the keyboard, as what can be
	// typed in it is offered from the schema, when the endpoint has been
	// changed after either, and on Reload, which sets force.
	onSchema func(force bool)

	panelState
}

func newGraphQLPanel() *graphqlPanel {
	p := &graphqlPanel{root: qtwidgets.NewQWidget(nil), panelState: newPanelState()}

	p.url = qtwidgets.NewQLineEdit(nil)
	p.url.SetPlaceholderText("Endpoint, e.g. https://api.example.com/graphql or {{host}}/graphql")
	p.typedLine(p.url)
	p.url.OnEditingFinished(func() { p.loadSchema(false) })
	p.highlight.lineEdit(p.url)

	p.send = qtwidgets.NewQPushButtonWithText("Send", nil)
	makePrimary(p.send)
	// Save is only for a request that is not saved, as one that is saved is
	// saved with every change.
	p.save = qtwidgets.NewQPushButtonWithText("Save", nil)

	top := qtwidgets.NewQHBoxLayout(nil)
	top.AddWidgetFull(p.url.AsQWidget(), 1, 0)
	top.AddWidget(p.send.AsQWidget())
	top.AddWidget(p.save.AsQWidget())

	fixed := qtgui.QFontDatabaseGetSystemFont(qtgui.QFontDatabaseFixedFont)

	p.query = qtwidgets.NewQPlainTextEdit(nil)
	p.query.SetFont(fixed)
	p.query.SetPlaceholderText("The query, e.g. { user(id: 1) { name } }")
	p.typedText(p.query)
	// The query is coloured as GraphQL in the colours of JSON, and folded
	// as JSON is, by its braces.
	p.highlight.graphql(p.query.Document())
	p.queryFold = newFolder(p.query, always)
	p.completer = newCompleter(p.query, p.completeQuery)
	p.query.OverrideFocusInEvent(func(super func(*qtgui.QFocusEvent), event *qtgui.QFocusEvent) {
		super(event)
		p.askSchema(false)
	})

	p.variables = qtwidgets.NewQPlainTextEdit(nil)
	p.variables.SetFont(fixed)
	p.variables.SetPlaceholderText(`The variables of the query as a JSON object, e.g. {"id": 1}`)
	p.typedText(p.variables)
	p.highlight.document(p.variables.Document(), always)
	p.variablesFold = newFolder(p.variables, always)
	p.format = newFormatButton(p.variables, p.commit)

	formatBar := qtwidgets.NewQHBoxLayout(nil)
	formatBar.AddWidget(p.format.button.AsQWidget())
	formatBar.AddStretch()

	variablesTab := qtwidgets.NewQWidget(nil)
	variablesLayout := qtwidgets.NewQVBoxLayout(variablesTab)
	variablesLayout.AddWidgetFull(p.variables.AsQWidget(), 1, 0)
	variablesLayout.AddLayout(formatBar.AsQLayout())

	p.headers = newExclusiveTable("Header", "Value")
	p.headers.onChange = p.changed
	p.highlight.table(p.headers.table)

	p.auth = newAuthEditor(&p.panelState)
	p.auth.onChange = p.changed

	p.captures = newCaptureTable()
	p.captures.onChange = p.changed
	p.assertions = newAssertTable(p.highlight)
	p.assertions.onChange = p.changed

	p.tabs = qtwidgets.NewQTabWidget(nil)
	// The order matches the tab constants.
	p.tabs.AddTab(plainPage(p.newQueryTab()), "Query")
	p.tabs.AddTab(plainPage(variablesTab), "Variables")
	p.tabs.AddTab(p.headers.widget(), "Headers")
	p.tabs.AddTab(p.auth.root, "Authorization")
	p.tabs.AddTab(plainPage(p.newSchemaTab()), "Schema")
	p.tabs.OnCurrentChanged(func(int) { p.loadSchema(false) })

	layout := qtwidgets.NewQVBoxLayout(p.root)
	layout.AddLayout(top.AsQLayout())
	layout.AddWidget(p.tabs.AsQWidget())

	return p
}

// newQueryTab returns the tab the query is typed in.
func (p *graphqlPanel) newQueryTab() *qtwidgets.QWidget {
	tab := qtwidgets.NewQWidget(nil)
	layout := qtwidgets.NewQVBoxLayout(tab)
	layout.AddWidget(p.query.AsQWidget())
	return tab
}

// newSchemaTab returns the tab which shows the schema of the server, with
// what went wrong if it could not be loaded above it.
func (p *graphqlPanel) newSchemaTab() *qtwidgets.QWidget {
	p.status = newStatusLine()

	p.schema = qtwidgets.NewQPlainTextEdit(nil)
	p.schema.SetFont(qtgui.QFontDatabaseGetSystemFont(qtgui.QFontDatabaseFixedFont))
	p.schema.SetReadOnly(true)
	p.schema.SetPlaceholderText("The schema of the server, which it is asked for while this tab is shown.")
	// The schema is GraphQL too.
	p.highlight.graphql(p.schema.Document())
	p.schemaFold = newFolder(p.schema, always)

	p.reload = qtwidgets.NewQPushButtonWithText("Reload", nil)
	p.reload.OnClicked(func(_ bool) { p.loadSchema(true) })

	bar := qtwidgets.NewQHBoxLayout(nil)
	bar.AddWidget(p.reload.AsQWidget())
	bar.AddStretch()

	tab := qtwidgets.NewQWidget(nil)
	layout := qtwidgets.NewQVBoxLayout(tab)
	layout.AddWidget(p.status.AsQWidget())
	layout.AddWidgetFull(p.schema.AsQWidget(), 1, 0)
	layout.AddLayout(bar.AsQLayout())
	return tab
}

func (p *graphqlPanel) widget() *qtwidgets.QWidget { return p.root }

func (p *graphqlPanel) saveButton() *qtwidgets.QPushButton { return p.save }

func (p *graphqlPanel) authorization() *authEditor { return p.auth }

// show shows a GraphQL request. See load.
func (p *graphqlPanel) show(r *collection.Request) { p.load(*r.GraphQL) }

// compose returns the request in the widgets as a GraphQL request with a
// name.
func (p *graphqlPanel) compose(name string) *collection.Request {
	return collection.NewGraphQL(name, p.request())
}

// request reads the widgets into a graphql.Request.
func (p *graphqlPanel) request() graphql.Request {
	return graphql.Request{
		URL:        p.url.Text(),
		Headers:    p.headers.items(),
		Auth:       p.auth.auth(),
		Query:      p.query.ToPlainText(),
		Variables:  p.variables.ToPlainText(),
		Captures:   captureRules(p.captures),
		Assertions: p.assertions.checks(),
	}
}

// load shows the request in the widgets without calling onChange, with no
// schema: it is asked for again if the Schema tab is shown.
func (p *graphqlPanel) load(r graphql.Request) {
	p.quietly(func() {
		p.url.SetText(r.URL)
		p.headers.setItems(r.Headers)
		p.auth.load(r.Auth)
		p.query.SetPlainText(r.Query)
		p.queryFold.reset()
		p.variables.SetPlainText(r.Variables)
		p.variablesFold.reset()
		setCaptureRules(p.captures, r.Captures)
		p.assertions.setChecks(r.Assertions)
	})
	p.loaded = ""
	p.setSchema(nil)
}

// loadSchema asks for the schema of the server where it is wanted: if the
// Schema tab is shown, or it has been asked for since the request was shown,
// as the query is completed from it. See askSchema.
func (p *graphqlPanel) loadSchema(force bool) {
	if p.tabs.CurrentIndex() != tabGraphQLSchema && p.loaded == "" {
		return
	}
	p.askSchema(force)
}

// askSchema asks for the schema of the server, if the endpoint is not the
// one it was last asked for, or always if force is set.
func (p *graphqlPanel) askSchema(force bool) {
	if p.onSchema == nil || !force && p.loaded == p.url.Text() {
		return
	}
	p.loaded = p.url.Text()
	p.onSchema(force)
}

// setSchema shows the schema of the server, which is nil if there is none,
// and has what can be typed in the query offered from it.
func (p *graphqlPanel) setSchema(schema *graphql.Schema) {
	p.described = schema
	sdl := ""
	if schema != nil {
		sdl = schema.SDL()
	}
	p.status.set("")
	p.schema.SetPlainText(sdl)
	p.schemaFold.reset()
}

// completeQuery returns what can be typed at an offset of the query, from
// the schema of the server, which is nothing until it has been loaded.
func (p *graphqlPanel) completeQuery(query string, offset int) completion.Completion {
	if p.described == nil {
		return completion.Completion{}
	}
	return p.described.Complete(query, offset)
}

// setSchemaError reports that the schema could not be loaded.
func (p *graphqlPanel) setSchemaError(err error) {
	p.setSchema(nil)
	p.status.set("The schema could not be loaded: " + err.Error())
}

// setCodeFont sets the font the query, the variables and the schema are
// shown in.
func (p *graphqlPanel) setCodeFont(family string) {
	p.query.SetFont(fixedFont(family))
	p.variables.SetFont(fixedFont(family))
	p.schema.SetFont(fixedFont(family))
}

// recolour shows Send, the placeholders and the status line in the colours
// of the settings.
func (p *graphqlPanel) recolour() {
	makePrimary(p.send)
	p.highlight.setColours()
	p.refreshPlaceholders()
	p.status.recolour()
}

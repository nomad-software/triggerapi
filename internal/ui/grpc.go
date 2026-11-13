package ui

import (
	"github.com/nomad-software/qute/qtgui"
	"github.com/nomad-software/qute/qtwidgets"

	"fmt"

	"github.com/nomad-software/triggerapi/internal/collection"
	"github.com/nomad-software/triggerapi/internal/completion"
	"github.com/nomad-software/triggerapi/internal/rpc"
)

// grpcPanel holds the widgets used to compose a gRPC request. The methods the
// user can choose from come from the server, which describes its services
// through reflection, as do the fields offered as the message is typed.
type grpcPanel struct {
	root *qtwidgets.QWidget

	url    *qtwidgets.QLineEdit
	tls    *qtwidgets.QCheckBox
	invoke *qtwidgets.QPushButton
	// save saves a request that is not saved yet.
	save   *qtwidgets.QPushButton
	method *methodCombo
	reload *qtwidgets.QPushButton
	// status reports a failure to load the service definition.
	status statusLine

	message *qtwidgets.QPlainTextEdit
	// messageFold folds the objects and arrays of the message.
	messageFold *folder
	example     *qtwidgets.QPushButton
	// format sets out the message, or the messages.
	format *formatButton
	// streaming says that the method chosen takes a stream of messages,
	// and how to give several.
	streaming *qtwidgets.QLabel
	auth      *authEditor
	metadata  *keyValueTable
	// captures are the variables to set from the response. See
	// requestPanel.captures.
	captures *keyValueTable
	// assertions are what the response should be. Like the captures they
	// are shown beside the response.
	assertions *assertTable
	// endpoints lists the methods of the server, to make requests of.
	endpoints *endpointTable
	// described is the service definition of the server, or nil if it has
	// not been loaded, which completer offers the fields of the message
	// from.
	described *rpc.Schema
	completer *completer

	panelState
	// target is the server the service definition was last asked of.
	target string

	// onTarget is called after the user has finished changing which server
	// the request is for.
	onTarget func()
}

func newGRPCPanel() *grpcPanel {
	p := &grpcPanel{root: qtwidgets.NewQWidget(nil), panelState: newPanelState()}

	p.url = qtwidgets.NewQLineEdit(nil)
	p.url.SetPlaceholderText("Server address, e.g. localhost:50051")
	p.typedLine(p.url)
	p.url.OnEditingFinished(p.targetChanged)
	p.highlight.lineEdit(p.url)

	p.tls = qtwidgets.NewQCheckBoxWithText("TLS", nil)
	p.tls.OnToggled(func(bool) {
		p.changed()
		p.targetChanged()
	})

	p.invoke = qtwidgets.NewQPushButtonWithText("Invoke", nil)
	makePrimary(p.invoke)
	// Save is only for a request that is not saved, as one that is saved is
	// saved with every change.
	p.save = qtwidgets.NewQPushButtonWithText("Save", nil)

	p.method = newMethodCombo()
	p.method.OnCurrentIndexChanged(func(int) {
		p.showStreaming()
		p.changed()
	})

	p.reload = qtwidgets.NewQPushButtonWithText("Reload", nil)

	// The address takes a quarter of the width left by the buttons and the
	// method the rest.
	top := qtwidgets.NewQHBoxLayout(nil)
	top.AddWidgetFull(p.url.AsQWidget(), 1, 0)
	top.AddWidgetFull(p.method.AsQWidget(), 3, 0)
	top.AddWidget(p.reload.AsQWidget())
	top.AddWidget(p.tls.AsQWidget())
	top.AddWidget(p.invoke.AsQWidget())
	top.AddWidget(p.save.AsQWidget())

	p.status = newStatusLine()

	fixed := qtgui.QFontDatabaseGetSystemFont(qtgui.QFontDatabaseFixedFont)

	p.message = qtwidgets.NewQPlainTextEdit(nil)
	p.message.SetFont(fixed)
	p.message.SetPlaceholderText("The request message as JSON")
	p.typedText(p.message)
	// The message is always JSON.
	p.highlight.document(p.message.Document(), always)
	p.messageFold = newFolder(p.message, always)
	p.completer = newCompleter(p.message, p.completeMessage)

	p.example = qtwidgets.NewQPushButtonWithText("Use Example Message", nil)

	exampleBar := qtwidgets.NewQHBoxLayout(nil)
	exampleBar.AddWidget(p.example.AsQWidget())
	p.format = newFormatButton(p.message, p.commit)
	p.format.several = true
	exampleBar.AddWidget(p.format.button.AsQWidget())
	exampleBar.AddStretch()
	p.streaming = qtwidgets.NewQLabelWithText("This method takes a stream of messages: give them one after another, or in an array.", nil)
	p.streaming.SetEnabled(false)
	p.streaming.SetVisible(false)
	exampleBar.AddWidget(p.streaming.AsQWidget())

	messageTab := qtwidgets.NewQWidget(nil)
	messageLayout := qtwidgets.NewQVBoxLayout(messageTab)
	messageLayout.AddWidgetFull(p.message.AsQWidget(), 1, 0)
	messageLayout.AddLayout(exampleBar.AsQLayout())

	p.auth = newAuthEditor(&p.panelState)
	p.auth.onChange = p.changed

	p.captures = newCaptureTable()
	p.captures.onChange = p.changed
	p.assertions = newAssertTable(p.highlight)
	p.assertions.onChange = p.changed

	p.metadata = newExclusiveTable("Key", "Value")
	p.metadata.onChange = p.changed
	p.highlight.table(p.metadata.table)

	p.endpoints = newEndpointTable()

	tabs := qtwidgets.NewQTabWidget(nil)
	tabs.AddTab(plainPage(messageTab), "Message")
	tabs.AddTab(p.auth.root, "Authorization")
	tabs.AddTab(p.metadata.widget(), "Metadata")
	tabs.AddTab(plainPage(p.endpoints.root), "Service Definitions")

	layout := qtwidgets.NewQVBoxLayout(p.root)
	layout.AddLayout(top.AsQLayout())
	layout.AddWidget(p.status.AsQWidget())
	layout.AddWidget(tabs.AsQWidget())

	return p
}

// targetChanged reports that the request is for a different server, unless it
// is the one last reported.
func (p *grpcPanel) targetChanged() {
	target := p.targetKey()
	if p.updating || target == p.target {
		return
	}
	p.target = target
	if p.onTarget != nil {
		p.onTarget()
	}
}

func (p *grpcPanel) targetKey() string {
	return fmt.Sprintf("%s %t", p.url.Text(), p.tls.IsChecked())
}

func (p *grpcPanel) widget() *qtwidgets.QWidget { return p.root }

func (p *grpcPanel) saveButton() *qtwidgets.QPushButton { return p.save }

func (p *grpcPanel) authorization() *authEditor { return p.auth }

// show shows a gRPC request. See load.
func (p *grpcPanel) show(r *collection.Request) { p.load(*r.GRPC) }

// compose returns the request in the widgets as a gRPC request with a name.
func (p *grpcPanel) compose(name string) *collection.Request {
	return collection.NewGRPC(name, p.request())
}

// request reads the widgets into an rpc.Request.
func (p *grpcPanel) request() rpc.Request {
	return rpc.Request{
		URL:        p.url.Text(),
		TLS:        p.tls.IsChecked(),
		Method:     p.method.current(),
		Message:    p.message.ToPlainText(),
		Metadata:   p.metadata.items(),
		Auth:       p.auth.auth(),
		Captures:   captureRules(p.captures),
		Assertions: p.assertions.checks(),
	}
}

// setCodeFont sets the font the message is shown in.
func (p *grpcPanel) setCodeFont(family string) {
	p.message.SetFont(fixedFont(family))
}

// load shows the request in the widgets without calling onChange or onTarget.
// Its method is the only one that can be chosen until the service definition
// has been loaded.
func (p *grpcPanel) load(r rpc.Request) {
	p.updating = true
	defer func() { p.updating = false }()

	p.url.SetText(r.URL)
	p.tls.SetChecked(r.TLS)
	p.target = p.targetKey()

	p.method.setMethods(nil, r.Method)

	p.message.SetPlainText(r.Message)
	p.messageFold.reset()
	p.metadata.setItems(r.Metadata)
	setCaptureRules(p.captures, r.Captures)
	p.assertions.setChecks(r.Assertions)
	p.auth.load(r.Auth)

	p.endpoints.setMethods(nil)
	p.described = nil
	p.status.set("")
	p.showStreaming()
}

// showStreaming says whether the method chosen takes a stream of messages,
// which is known once the service definition has been loaded.
func (p *grpcPanel) showStreaming() {
	streaming := false
	if p.described != nil {
		m, ok := p.described.Method(p.method.current())
		streaming = ok && m.ClientStreaming
	}
	p.streaming.SetVisible(streaming)
}

// completeMessage returns what can be typed at an offset of the message of
// the method chosen, from the service definition, which is nothing until it
// has been loaded.
func (p *grpcPanel) completeMessage(message string, offset int) completion.Completion {
	if p.described == nil {
		return completion.Completion{}
	}
	return p.described.Complete(p.method.current(), message, offset)
}

// recolour shows Invoke, the placeholders and the status line in the colours
// of the settings.
func (p *grpcPanel) recolour() {
	makePrimary(p.invoke)
	p.highlight.setColours()
	p.refreshPlaceholders()
	p.status.recolour()
}

// setSchema offers the methods of the server to choose from. The method of
// the request stays selected, and is kept in the list even if the server does
// not have it so that the request is not changed behind the user's back. A
// request with no method takes the first one.
func (p *grpcPanel) setSchema(schema *rpc.Schema) {
	p.described = schema
	names := make([]string, len(schema.Methods))
	for i, m := range schema.Methods {
		names[i] = m.Name
	}

	current := p.method.current()

	p.updating = true
	p.method.setMethods(names, current)
	p.endpoints.setMethods(names)
	p.updating = false

	// Only a failure to load the definition is reported.
	p.status.set("")
	p.showStreaming()

	if p.method.current() != current {
		p.changed()
	}
}

// setSchemaError reports that the service definition could not be loaded.
func (p *grpcPanel) setSchemaError(err error) {
	p.described = nil
	p.endpoints.setMethods(nil)
	p.status.set("The service definition could not be loaded: " + err.Error())
	p.showStreaming()
}

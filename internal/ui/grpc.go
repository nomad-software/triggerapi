package ui

import (
	"fmt"

	qt "github.com/mappu/miqt/qt6"

	"github.com/nomad-software/triggerapi/internal/collection"
	"github.com/nomad-software/triggerapi/internal/rpc"
)

// grpcPanel holds the widgets used to compose a gRPC request. The methods the
// user can choose from come from the server, which describes its services
// through reflection.
type grpcPanel struct {
	root *qt.QWidget

	url    *qt.QLineEdit
	tls    *qt.QCheckBox
	invoke *qt.QPushButton
	// save saves a request that is not saved yet.
	save   *qt.QPushButton
	method *methodCombo
	reload *qt.QPushButton
	// status reports a failure to load the service definition.
	status statusLine

	message *qt.QPlainTextEdit
	// messageFold folds the objects and arrays of the message.
	messageFold *folder
	example     *qt.QPushButton
	// format sets out the message.
	format   *formatButton
	auth     *authEditor
	metadata *keyValueTable
	// captures are the variables to set from the response. See
	// requestPanel.captures.
	captures *keyValueTable
	// endpoints lists the methods of the server, to make requests of.
	endpoints *endpointTable

	panelState
	// target is the server the service definition was last asked of.
	target string

	// onTarget is called after the user has finished changing which server
	// the request is for.
	onTarget func()
}

func newGRPCPanel() *grpcPanel {
	p := &grpcPanel{root: qt.NewQWidget2(), panelState: newPanelState()}

	p.url = qt.NewQLineEdit2()
	p.url.SetPlaceholderText("Server address, e.g. localhost:50051")
	p.url.OnTextChanged(func(string) { p.changed() })
	p.url.OnEditingFinished(p.targetChanged)
	p.highlight.lineEdit(p.url)

	p.tls = qt.NewQCheckBox3("TLS")
	p.tls.OnToggled(func(bool) {
		p.changed()
		p.targetChanged()
	})

	p.invoke = qt.NewQPushButton3("Invoke")
	makePrimary(p.invoke)
	// Save is only for a request that is not saved, as one that is saved is
	// saved with every change.
	p.save = qt.NewQPushButton3("Save")

	p.method = newMethodCombo()
	p.method.OnCurrentIndexChanged(func(int) { p.changed() })

	p.reload = qt.NewQPushButton3("Reload")

	// The address takes a quarter of the width left by the buttons and the
	// method the rest.
	top := qt.NewQHBoxLayout2()
	top.AddWidget2(p.url.QWidget, 1)
	top.AddWidget2(p.method.QWidget, 3)
	top.AddWidget(p.reload.QWidget)
	top.AddWidget(p.tls.QWidget)
	top.AddWidget(p.invoke.QWidget)
	top.AddWidget(p.save.QWidget)

	p.status = newStatusLine()

	fixed := qt.QFontDatabase_SystemFont(qt.QFontDatabase__FixedFont)

	p.message = qt.NewQPlainTextEdit2()
	p.message.SetFont(fixed)
	p.message.SetPlaceholderText("The request message as JSON")
	p.message.OnTextChanged(p.changed)
	// The message is always JSON.
	p.highlight.document(p.message.Document(), always)
	p.messageFold = newFolder(p.message, always)

	p.example = qt.NewQPushButton3("Use Example Message")

	exampleBar := qt.NewQHBoxLayout2()
	exampleBar.AddWidget(p.example.QWidget)
	p.format = newFormatButton(p.message)
	exampleBar.AddWidget(p.format.button.QWidget)
	exampleBar.AddStretch()

	messageTab := qt.NewQWidget2()
	messageLayout := qt.NewQVBoxLayout(messageTab)
	messageLayout.AddWidget2(p.message.QWidget, 1)
	messageLayout.AddLayout(exampleBar.QLayout)

	p.auth = newAuthEditor(p.highlight)
	p.auth.onChange = p.changed

	p.captures = newCaptureTable()
	p.captures.onChange = p.changed

	p.metadata = newKeyValueTable("Key", "Value")
	p.metadata.onChange = p.changed
	p.highlight.table(p.metadata.table)

	p.endpoints = newEndpointTable()

	tabs := qt.NewQTabWidget2()
	tabs.AddTab(plainPage(messageTab), "Message")
	tabs.AddTab(p.auth.root, "Authorization")
	tabs.AddTab(p.metadata.widget(), "Metadata")
	tabs.AddTab(plainPage(p.endpoints.root), "Service Definitions")

	layout := qt.NewQVBoxLayout(p.root)
	layout.AddLayout(top.QLayout)
	layout.AddWidget(p.status.QWidget)
	layout.AddWidget(tabs.QWidget)

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

func (p *grpcPanel) widget() *qt.QWidget { return p.root }

func (p *grpcPanel) saveButton() *qt.QPushButton { return p.save }

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
		URL:      p.url.Text(),
		TLS:      p.tls.IsChecked(),
		Method:   p.method.current(),
		Message:  p.message.ToPlainText(),
		Metadata: p.metadata.items(),
		Auth:     p.auth.auth(),
		Captures: captureRules(p.captures),
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
	p.auth.load(r.Auth)

	p.endpoints.setMethods(nil)
	p.status.set("")
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

	if p.method.current() != current {
		p.changed()
	}
}

// setSchemaError reports that the service definition could not be loaded.
func (p *grpcPanel) setSchemaError(err error) {
	p.endpoints.setMethods(nil)
	p.status.set("The service definition could not be loaded: " + err.Error())
}

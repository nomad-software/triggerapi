package ui

import (
	"fmt"

	qt "github.com/mappu/miqt/qt6"

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
	method *methodCombo
	reload *qt.QPushButton
	// status reports a failure to load the service definition.
	status *qt.QLabel

	message *qt.QPlainTextEdit
	// messageFold folds the objects and arrays of the message.
	messageFold *folder
	example     *qt.QPushButton
	auth        *authEditor
	metadata    *keyValueTable
	definition  *qt.QPlainTextEdit

	// highlight colours the placeholders in the fields of the request.
	highlight *placeholderHighlighter

	updating bool
	// target is the server the service definition was last asked of.
	target string

	// onChange is called after the user edits any part of the request.
	onChange func()
	// onTarget is called after the user has finished changing which server
	// the request is for.
	onTarget func()
}

func newGRPCPanel() *grpcPanel {
	p := &grpcPanel{root: qt.NewQWidget2(), highlight: newPlaceholderHighlighter()}

	p.url = qt.NewQLineEdit2()
	p.url.SetPlaceholderText("Server address, e.g. localhost:50051")
	p.url.OnTextChanged(func(string) { p.changed() })
	p.url.OnEditingFinished(p.targetChanged)
	p.highlight.lineEdit(p.url)

	p.tls = qt.NewQCheckBox3("TLS")
	p.tls.SetToolTip("Connect to the server using TLS")
	p.tls.OnToggled(func(bool) {
		p.changed()
		p.targetChanged()
	})

	p.invoke = qt.NewQPushButton3("Invoke")
	makePrimary(p.invoke)
	p.invoke.SetToolTip("Ctrl+Return")

	p.method = newMethodCombo()
	p.method.OnCurrentIndexChanged(func(int) { p.changed() })

	p.reload = qt.NewQPushButton3("Reload")
	p.reload.SetToolTip("Load the service definition from the server again")

	// The address takes a quarter of the width left by the buttons and the
	// method the rest.
	top := qt.NewQHBoxLayout2()
	top.AddWidget2(p.url.QWidget, 1)
	top.AddWidget2(p.method.QWidget, 3)
	top.AddWidget(p.reload.QWidget)
	top.AddWidget(p.tls.QWidget)
	top.AddWidget(p.invoke.QWidget)

	p.status = qt.NewQLabel2()
	p.status.SetTextFormat(qt.PlainText)
	p.status.SetWordWrap(true)
	p.status.SetTextInteractionFlags(qt.TextSelectableByMouse)
	p.status.SetVisible(false)
	p.recolour()

	fixed := qt.QFontDatabase_SystemFont(qt.QFontDatabase__FixedFont)

	p.message = qt.NewQPlainTextEdit2()
	p.message.SetFont(fixed)
	p.message.SetPlaceholderText("The request message as JSON")
	p.message.OnTextChanged(p.changed)
	// The message is always JSON.
	isJSON := func() bool { return true }
	p.highlight.document(p.message.Document(), isJSON)
	p.messageFold = newFolder(p.message, isJSON)

	p.example = qt.NewQPushButton3("Use Example Message")
	p.example.SetToolTip("Replace the message with one that has every field of the method's request")

	exampleBar := qt.NewQHBoxLayout2()
	exampleBar.AddWidget(p.example.QWidget)
	exampleBar.AddStretch()

	messageTab := qt.NewQWidget2()
	messageLayout := qt.NewQVBoxLayout(messageTab)
	messageLayout.AddWidget2(p.message.QWidget, 1)
	messageLayout.AddLayout(exampleBar.QLayout)

	p.auth = newAuthEditor(p.highlight)
	p.auth.onChange = p.changed

	p.metadata = newKeyValueTable("Key", "Value")
	p.metadata.onChange = p.changed
	p.highlight.table(p.metadata.table)

	p.definition = qt.NewQPlainTextEdit2()
	p.definition.SetFont(fixed)
	p.definition.SetReadOnly(true)
	p.definition.SetPlaceholderText("The services of the server are shown here once they have been loaded using server reflection.")

	tabs := qt.NewQTabWidget2()
	tabs.AddTab(messageTab, "Message")
	tabs.AddTab(p.auth.root, "Authorization")
	tabs.AddTab(p.metadata.widget(), "Metadata")
	tabs.AddTab(p.definition.QWidget, "Service Definition")

	layout := qt.NewQVBoxLayout(p.root)
	layout.AddLayout(top.QLayout)
	layout.AddWidget(p.status.QWidget)
	layout.AddWidget(tabs.QWidget)

	return p
}

func (p *grpcPanel) changed() {
	if !p.updating && p.onChange != nil {
		p.onChange()
	}
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

// request reads the widgets into an rpc.Request.
func (p *grpcPanel) request() rpc.Request {
	return rpc.Request{
		URL:      p.url.Text(),
		TLS:      p.tls.IsChecked(),
		Method:   p.method.current(),
		Message:  p.message.ToPlainText(),
		Metadata: p.metadata.items(),
		Auth:     p.auth.auth(),
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
	p.auth.load(r.Auth)

	p.definition.SetPlainText("")
	p.setStatus("")
}

// setStatus shows what went wrong with the service definition, in the colour
// of errors, or hides the message if text is empty.
func (p *grpcPanel) setStatus(text string) {
	p.status.SetText(text)
	p.status.SetVisible(text != "")
}

// recolour gives the status line the colour of errors in the settings.
func (p *grpcPanel) recolour() {
	p.status.SetStyleSheet("color: " + themeColour(colourError))
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
	p.definition.SetPlainText(schema.Describe())
	p.updating = false

	// Only a failure to load the definition is reported.
	p.setStatus("")

	if p.method.current() != current {
		p.changed()
	}
}

// setSchemaError reports that the service definition could not be loaded.
func (p *grpcPanel) setSchemaError(err error) {
	p.definition.SetPlainText("")
	p.setStatus("The service definition could not be loaded: " + err.Error())
}

// refreshPlaceholders recolours the placeholders after the environment has
// changed. Recolouring a document reports its text as changed, which must not
// be taken for an edit.
func (p *grpcPanel) refreshPlaceholders() {
	defer func(prev bool) { p.updating = prev }(p.updating)
	p.updating = true

	p.highlight.refresh()
}

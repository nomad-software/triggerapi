package ui

import (
	"strings"

	qt "github.com/mappu/miqt/qt6"

	"github.com/nomad-software/triggerapi/internal/ws"
)

// wsPanel holds the widgets used to compose a WebSocket request: where to
// connect, the headers and authorization of the handshake, and the message
// to send once connected. Connect opens the connection, which stays open
// until it is closed, and Send sends the message over it as often as wanted.
type wsPanel struct {
	root *qt.QWidget

	url *qt.QLineEdit
	// connect opens the connection, and closes it while it is open.
	connect *qt.QPushButton
	// save saves a request that is not saved yet.
	save *qt.QPushButton

	message *qt.QPlainTextEdit
	// messageFold folds the objects and arrays of a message that is JSON.
	messageFold *folder
	// send sends the message. It is enabled while connected.
	send *qt.QPushButton
	// format sets out a message that is JSON.
	format  *formatButton
	headers *keyValueTable
	auth    *authEditor

	// highlight colours the placeholders in the fields of the request.
	highlight *placeholderHighlighter

	updating bool
	// onChange is called after the user edits any part of the request.
	onChange func()
}

func newWSPanel() *wsPanel {
	p := &wsPanel{root: qt.NewQWidget2(), highlight: newPlaceholderHighlighter()}

	p.url = qt.NewQLineEdit2()
	p.url.SetPlaceholderText("Server address, e.g. ws://localhost:8080/socket or {{host}}/socket")
	p.url.OnTextChanged(func(string) { p.changed() })
	p.highlight.lineEdit(p.url)

	p.connect = qt.NewQPushButton3("Connect")
	makePrimary(p.connect)
	// Save is only for a request that is not saved, as one that is saved is
	// saved with every change.
	p.save = qt.NewQPushButton3("Save")

	top := qt.NewQHBoxLayout2()
	top.AddWidget2(p.url.QWidget, 1)
	top.AddWidget(p.connect.QWidget)
	top.AddWidget(p.save.QWidget)

	p.message = qt.NewQPlainTextEdit2()
	p.message.SetFont(qt.QFontDatabase_SystemFont(qt.QFontDatabase__FixedFont))
	p.message.SetPlaceholderText("The message to send, as text or JSON")
	p.message.OnTextChanged(p.changed)
	// A message can be any text, and is coloured and folded when it looks
	// like JSON.
	isJSON := func() bool {
		text := strings.TrimSpace(p.message.ToPlainText())
		return strings.HasPrefix(text, "{") || strings.HasPrefix(text, "[")
	}
	p.highlight.document(p.message.Document(), isJSON)
	p.messageFold = newFolder(p.message, isJSON)

	// Send is in the colour of the other buttons which send while it can
	// be pressed, and a plain button while it cannot (styleSend).
	p.send = qt.NewQPushButton3("Send")
	p.send.SetEnabled(false)
	p.format = newFormatButton(p.message)

	sendBar := qt.NewQHBoxLayout2()
	sendBar.AddWidget(p.send.QWidget)
	sendBar.AddWidget(p.format.button.QWidget)
	sendBar.AddStretch()

	messageTab := qt.NewQWidget2()
	messageLayout := qt.NewQVBoxLayout(messageTab)
	messageLayout.AddWidget2(p.message.QWidget, 1)
	messageLayout.AddLayout(sendBar.QLayout)

	p.headers = newKeyValueTable("Header", "Value")
	p.headers.onChange = p.changed
	p.highlight.table(p.headers.table)

	p.auth = newAuthEditor(p.highlight)
	p.auth.onChange = p.changed

	tabs := qt.NewQTabWidget2()
	tabs.AddTab(plainPage(messageTab), "Message")
	tabs.AddTab(p.headers.widget(), "Headers")
	tabs.AddTab(p.auth.root, "Authorization")

	layout := qt.NewQVBoxLayout(p.root)
	layout.AddLayout(top.QLayout)
	layout.AddWidget(tabs.QWidget)

	return p
}

func (p *wsPanel) changed() {
	if !p.updating && p.onChange != nil {
		p.onChange()
	}
}

// request reads the widgets into a ws.Request.
func (p *wsPanel) request() ws.Request {
	return ws.Request{
		URL:     p.url.Text(),
		Headers: p.headers.items(),
		Auth:    p.auth.auth(),
		Message: p.message.ToPlainText(),
	}
}

// load shows the request in the widgets without calling onChange.
func (p *wsPanel) load(r ws.Request) {
	p.updating = true
	defer func() { p.updating = false }()

	p.url.SetText(r.URL)
	p.headers.setItems(r.Headers)
	p.auth.load(r.Auth)
	p.message.SetPlainText(r.Message)
	p.messageFold.reset()
}

// setConnected shows whether the connection is open: Connect becomes
// Disconnect, and the message can be sent.
func (p *wsPanel) setConnected(connected bool) {
	p.send.SetEnabled(connected)
	p.styleSend()
	if connected {
		p.connect.SetText("Disconnect")
	} else {
		p.connect.SetText("Connect")
	}
}

// styleSend gives Send the colour of the buttons which send while it is
// enabled, and the look of any other disabled button while it is not. It is
// called again when the colours change.
func (p *wsPanel) styleSend() {
	if p.send.IsEnabled() {
		makePrimary(p.send)
	} else {
		p.send.SetStyleSheet("")
	}
}

// setCodeFont sets the font the message is shown in.
func (p *wsPanel) setCodeFont(family string) {
	p.message.SetFont(fixedFont(family))
}

// refreshPlaceholders recolours the placeholders after the environment has
// changed. Recolouring a document reports its text as changed, which must not
// be taken for an edit.
func (p *wsPanel) refreshPlaceholders() {
	defer func(prev bool) { p.updating = prev }(p.updating)
	p.updating = true

	p.highlight.refresh()
}

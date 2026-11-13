package ui

import (
	"github.com/nomad-software/qute/qtgui"
	"github.com/nomad-software/qute/qtwidgets"

	"strings"

	"github.com/nomad-software/triggerapi/internal/collection"
	"github.com/nomad-software/triggerapi/internal/ws"
)

// wsPanel holds the widgets used to compose a WebSocket request: where to
// connect, the headers and authorization of the handshake, and the message
// to send once connected. Connect opens the connection, which stays open
// until it is closed, and Send sends the message over it as often as wanted.
type wsPanel struct {
	root *qtwidgets.QWidget

	url *qtwidgets.QLineEdit
	// connect opens the connection, and closes it while it is open.
	connect *qtwidgets.QPushButton
	// save saves a request that is not saved yet.
	save *qtwidgets.QPushButton

	message *qtwidgets.QPlainTextEdit
	// messageFold folds the objects and arrays of a message that is JSON.
	messageFold *folder
	// send sends the message. It is enabled while connected.
	send *qtwidgets.QPushButton
	// format sets out a message that is JSON.
	format  *formatButton
	headers *keyValueTable
	auth    *authEditor

	panelState
}

func newWSPanel() *wsPanel {
	p := &wsPanel{root: qtwidgets.NewQWidget(nil), panelState: newPanelState()}

	p.url = qtwidgets.NewQLineEdit(nil)
	p.url.SetPlaceholderText("Server address, e.g. ws://localhost:8080/socket or {{host}}/socket")
	p.typedLine(p.url)
	p.highlight.lineEdit(p.url)

	p.connect = qtwidgets.NewQPushButtonWithText("Connect", nil)
	makePrimary(p.connect)
	// Save is only for a request that is not saved, as one that is saved is
	// saved with every change.
	p.save = qtwidgets.NewQPushButtonWithText("Save", nil)

	top := qtwidgets.NewQHBoxLayout(nil)
	top.AddWidgetFull(p.url.AsQWidget(), 1, 0)
	top.AddWidget(p.connect.AsQWidget())
	top.AddWidget(p.save.AsQWidget())

	p.message = qtwidgets.NewQPlainTextEdit(nil)
	p.message.SetFont(qtgui.QFontDatabaseGetSystemFont(qtgui.QFontDatabaseFixedFont))
	p.message.SetPlaceholderText("The message to send, as text or JSON")
	p.typedText(p.message)
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
	p.send = qtwidgets.NewQPushButtonWithText("Send", nil)
	p.send.SetEnabled(false)
	p.format = newFormatButton(p.message, p.commit)

	sendBar := qtwidgets.NewQHBoxLayout(nil)
	sendBar.AddWidget(p.send.AsQWidget())
	sendBar.AddWidget(p.format.button.AsQWidget())
	sendBar.AddStretch()

	messageTab := qtwidgets.NewQWidget(nil)
	messageLayout := qtwidgets.NewQVBoxLayout(messageTab)
	messageLayout.AddWidgetFull(p.message.AsQWidget(), 1, 0)
	messageLayout.AddLayout(sendBar.AsQLayout())

	p.headers = newExclusiveTable("Header", "Value")
	p.headers.onChange = p.changed
	p.highlight.table(p.headers.table)

	p.auth = newAuthEditor(&p.panelState)
	p.auth.onChange = p.changed

	tabs := qtwidgets.NewQTabWidget(nil)
	tabs.AddTab(plainPage(messageTab), "Message")
	tabs.AddTab(p.headers.widget(), "Headers")
	tabs.AddTab(p.auth.root, "Authorization")

	layout := qtwidgets.NewQVBoxLayout(p.root)
	layout.AddLayout(top.AsQLayout())
	layout.AddWidget(tabs.AsQWidget())

	return p
}

func (p *wsPanel) widget() *qtwidgets.QWidget { return p.root }

func (p *wsPanel) saveButton() *qtwidgets.QPushButton { return p.save }

func (p *wsPanel) authorization() *authEditor { return p.auth }

// show shows a WebSocket request. See load.
func (p *wsPanel) show(r *collection.Request) { p.load(*r.WebSocket) }

// compose returns the request in the widgets as a WebSocket request with a
// name.
func (p *wsPanel) compose(name string) *collection.Request {
	return collection.NewWebSocket(name, p.request())
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

// recolour shows Connect, Send and the placeholders in the colours of the
// settings.
func (p *wsPanel) recolour() {
	makePrimary(p.connect)
	p.highlight.setColours()
	p.refreshPlaceholders()
	p.styleSend()
}

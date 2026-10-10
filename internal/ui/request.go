package ui

import (
	"slices"
	"strings"

	qt "github.com/mappu/miqt/qt6"

	"github.com/nomad-software/triggerapi/internal/env"
	"github.com/nomad-software/triggerapi/internal/rest"
)

// bodyTypes are the entries of the body type selector, in the order they are
// shown.
var bodyTypes = []struct {
	label string
	typ   rest.BodyType
}{
	{"None", rest.BodyNone},
	{"JSON", rest.BodyJSON},
	{"Text", rest.BodyText},
	{"XML", rest.BodyXML},
	{"Form URL-encoded", rest.BodyForm},
}

// Pages of the body stack.
const (
	bodyPageNone = iota
	bodyPageRaw
	bodyPageForm
)

// Pages of the request panel.
const (
	pageFields = iota
	pageCurl
)

// viewIconSize is the size of the icons in the bar at the right of the
// request panel.
const viewIconSize = 32

// curlColour is the colour of the curl icon.
const curlColour = "#0b4f6c"

// requestPanel holds the widgets used to compose a request.
type requestPanel struct {
	root *qt.QWidget

	// header names the saved request being edited and the environment.
	header *header
	method *qt.QComboBox
	url    *qt.QLineEdit
	send   *qt.QPushButton
	// preview shows the URL with environment variables substituted.
	preview *qt.QLabel

	params  *keyValueTable
	headers *keyValueTable

	auth *authEditor

	bodyType *qt.QComboBox
	bodyRaw  *qt.QPlainTextEdit
	bodyForm *keyValueTable
	// bodyFold folds the objects and arrays of a JSON body.
	bodyFold *folder

	// pages shows the fields of the request, or the request as a command
	// (pageFields / pageCurl), as chosen with the curl button in the bar at
	// the right.
	pages *qt.QStackedWidget
	curl  *qt.QToolButton
	// command is the request as a curl command.
	command *qt.QPlainTextEdit
	copy    *qt.QPushButton
	// execute runs the command and shows the response to it.
	execute *qt.QPushButton
	// closeCurl goes back from the command to the fields.
	closeCurl *qt.QPushButton
	// expand substitutes the variables of the active environment into text.
	expand func(string) string

	// highlight colours the placeholders in the fields of the request.
	highlight *placeholderHighlighter

	updating bool
	// syncing is set while the URL and the params table are brought into
	// step, to stop one updating the other in a loop.
	syncing bool

	// onChange is called after the user edits any part of the request.
	onChange func()
}

func newRequestPanel() *requestPanel {
	p := &requestPanel{root: qt.NewQWidget2(), highlight: newPlaceholderHighlighter()}

	p.header = newHeader()

	p.method = qt.NewQComboBox2()
	p.method.AddItems(rest.Methods)
	p.method.OnCurrentIndexChanged(func(int) { p.changed() })

	p.url = qt.NewQLineEdit2()
	p.url.SetPlaceholderText("Enter URL, e.g. https://api.example.com/users or {{host}}/users")
	p.url.OnTextChanged(func(string) { p.urlChanged() })

	p.highlight.lineEdit(p.url)

	p.send = qt.NewQPushButton3("Send")
	makePrimary(p.send)
	p.send.SetToolTip("Ctrl+Return")

	bar := qt.NewQHBoxLayout2()
	bar.AddWidget(p.method.QWidget)
	bar.AddWidget2(p.url.QWidget, 1)
	bar.AddWidget(p.send.QWidget)

	p.preview = qt.NewQLabel2()
	p.preview.SetTextFormat(qt.PlainText)
	p.preview.SetTextInteractionFlags(qt.TextSelectableByMouse)
	// Stop a long URL from forcing the window wider.
	p.preview.SetSizePolicy2(qt.QSizePolicy__Ignored, qt.QSizePolicy__Preferred)
	p.preview.SetVisible(false)

	p.params = newKeyValueTable("Key", "Value")
	p.params.onChange = p.paramsChanged
	p.highlight.table(p.params.table)
	p.auth = newAuthEditor(p.highlight)
	p.auth.onChange = p.changed

	p.headers = newKeyValueTable("Header", "Value")
	p.headers.onChange = p.changed
	p.highlight.table(p.headers.table)

	tabs := qt.NewQTabWidget2()
	tabs.AddTab(p.newBodyTab(), "Body")
	tabs.AddTab(p.params.panel(), "Params")
	tabs.AddTab(p.auth.root, "Authorization")
	tabs.AddTab(p.headers.panel(), "Headers")

	fields := qt.NewQWidget2()
	fieldsLayout := qt.NewQVBoxLayout(fields)
	fieldsLayout.SetContentsMargins(0, 0, 0, 0)
	fieldsLayout.AddLayout(bar.QLayout)
	fieldsLayout.AddWidget(p.preview.QWidget)
	fieldsLayout.AddWidget(tabs.QWidget)

	p.pages = qt.NewQStackedWidget2()
	p.pages.AddWidget(fields)
	p.pages.AddWidget(p.newCurlPage())

	content := qt.NewQHBoxLayout2()
	content.AddWidget2(p.pages.QWidget, 1)
	content.AddWidget(p.newViewBar())

	layout := qt.NewQVBoxLayout(p.root)
	layout.AddWidget(p.header.root)
	layout.AddLayout2(content.QLayout, 1)

	return p
}

// newViewBar returns the slim bar at the right of the panel, with a button
// for each other way of showing the request.
func (p *requestPanel) newViewBar() *qt.QWidget {
	p.curl = qt.NewQToolButton2()
	p.curl.SetIcon(drawIcon(viewIconSize*2, curlColour, drawPrompt))
	p.curl.SetIconSize(qt.NewQSize2(viewIconSize, viewIconSize))
	p.curl.SetToolTip("Show the request as a curl command")
	p.curl.SetCheckable(true)
	p.curl.SetAutoRaise(true)
	p.curl.OnToggled(func(checked bool) {
		if checked {
			p.pages.SetCurrentIndex(pageCurl)
			p.updateCurl()
		} else {
			p.pages.SetCurrentIndex(pageFields)
		}
	})

	// The bar is framed, like the pane of the sidebar at the left. The
	// border is given outright, as not every theme draws one around a frame.
	bar := qt.NewQFrame2()
	bar.SetStyleSheet(".QFrame { border: 1px solid " + borderColour() + "; border-radius: 3px; }")
	layout := qt.NewQVBoxLayout(bar.QWidget)
	layout.SetContentsMargins(4, 4, 4, 4)
	layout.AddWidget(p.curl.QWidget)
	layout.AddStretch()

	return bar.QWidget
}

// newCurlPage returns the page which shows the request as a curl command.
func (p *requestPanel) newCurlPage() *qt.QWidget {
	p.command = qt.NewQPlainTextEdit2()
	p.command.SetReadOnly(true)

	p.copy = qt.NewQPushButton3("Copy")
	p.copy.SetToolTip("Copy the command, to paste into a terminal")
	p.copy.OnClicked(func() {
		qt.QGuiApplication_Clipboard().SetText(p.command.ToPlainText())
	})

	p.closeCurl = qt.NewQPushButton3("Close")
	p.closeCurl.SetToolTip("Go back to the request")
	p.closeCurl.OnClicked(func() { p.curl.SetChecked(false) })

	p.execute = qt.NewQPushButton3("Execute")
	makePrimary(p.execute)
	p.execute.SetToolTip("Run the command with curl and show the response")

	buttons := qt.NewQHBoxLayout2()
	buttons.AddStretch()
	buttons.AddWidget(p.execute.QWidget)
	buttons.AddWidget(p.copy.QWidget)
	buttons.AddWidget(p.closeCurl.QWidget)

	page := qt.NewQWidget2()
	layout := qt.NewQVBoxLayout(page)
	layout.SetContentsMargins(0, 0, 0, 0)
	layout.AddWidget2(p.command.QWidget, 1)
	layout.AddLayout(buttons.QLayout)

	return page
}

// setCodeFont sets the font the curl command is shown in.
func (p *requestPanel) setCodeFont(family string) {
	p.command.SetFont(fixedFont(family))
}

// updateCurl shows the request, with the variables of the active
// environment in place, as a curl command, if the command is on show.
func (p *requestPanel) updateCurl() {
	if p.pages.CurrentIndex() != pageCurl {
		return
	}

	request := p.request()
	if p.expand != nil {
		request = request.Expand(p.expand)
	}

	command, err := request.Curl()
	if err != nil {
		// A comment, so that nothing happens if it is run all the same.
		command = "# The request cannot be made into a command: " + err.Error()
	}
	p.copy.SetEnabled(err == nil)
	p.execute.SetEnabled(err == nil)
	p.command.SetPlainText(command)
}

func (p *requestPanel) newBodyTab() *qt.QWidget {
	p.bodyType = qt.NewQComboBox2()
	for _, t := range bodyTypes {
		p.bodyType.AddItem(t.label)
	}

	p.bodyRaw = qt.NewQPlainTextEdit2()
	p.bodyRaw.SetFont(qt.QFontDatabase_SystemFont(qt.QFontDatabase__FixedFont))
	p.bodyRaw.OnTextChanged(p.changed)
	p.highlight.document(p.bodyRaw.Document(), p.bodyIsJSON)
	p.bodyFold = newFolder(p.bodyRaw, p.bodyIsJSON)

	p.bodyForm = newKeyValueTable("Key", "Value")
	p.bodyForm.onChange = p.changed
	p.highlight.table(p.bodyForm.table)

	stack := qt.NewQStackedWidget2()
	stack.AddWidget(newHint("This request does not have a body."))
	stack.AddWidget(p.bodyRaw.QWidget)
	stack.AddWidget(p.bodyForm.panel())

	p.bodyType.OnCurrentIndexChanged(func(index int) {
		switch bodyTypes[index].typ {
		case rest.BodyNone:
			stack.SetCurrentIndex(bodyPageNone)
		case rest.BodyForm:
			stack.SetCurrentIndex(bodyPageForm)
		default:
			stack.SetCurrentIndex(bodyPageRaw)
		}
		// Only a JSON body is coloured and can be folded.
		p.refreshPlaceholders()
		p.bodyFold.reset()
		p.changed()
	})

	return newSelectorTab("Type", p.bodyType, stack)
}

// newSelectorTab lays out a combo box above the stack of pages it controls.
func newSelectorTab(label string, selector *qt.QComboBox, stack *qt.QStackedWidget) *qt.QWidget {
	tab := qt.NewQWidget2()

	top := qt.NewQHBoxLayout2()
	top.AddWidget(qt.NewQLabel3(label).QWidget)
	top.AddWidget(selector.QWidget)
	top.AddStretch()

	layout := qt.NewQVBoxLayout(tab)
	layout.AddLayout(top.QLayout)
	layout.AddWidget2(stack.QWidget, 1)

	return tab
}

func newHint(text string) *qt.QWidget {
	label := qt.NewQLabel3(text)
	label.SetAlignment(qt.AlignCenter)
	label.SetEnabled(false)
	return label.QWidget
}

// bodyIsJSON reports if the body type is JSON.
func (p *requestPanel) bodyIsJSON() bool {
	return bodyTypes[p.bodyType.CurrentIndex()].typ == rest.BodyJSON
}

func (p *requestPanel) changed() {
	if !p.updating && p.onChange != nil {
		p.onChange()
	}
}

// refreshPlaceholders recolours the placeholders after the environment has
// changed. Recolouring a document reports its text as changed, which must not
// be taken for an edit.
func (p *requestPanel) refreshPlaceholders() {
	defer func(prev bool) { p.updating = prev }(p.updating)
	p.updating = true

	p.highlight.refresh()
	p.updateCurl()
}

// urlChanged copies the query string typed into the URL to the params table.
func (p *requestPanel) urlChanged() {
	if !p.updating && !p.syncing {
		p.syncing = true
		_, typed := rest.SplitURL(p.url.Text())
		p.params.setItems(rest.MergeParams(p.params.items(), typed))
		p.syncing = false
	}
	p.changed()
}

// paramsChanged shows the enabled params as the query string of the URL.
func (p *requestPanel) paramsChanged() {
	if !p.syncing {
		p.syncing = true
		base, _ := rest.SplitURL(p.url.Text())
		p.url.SetText(rest.JoinURL(base, p.params.items()))
		p.syncing = false
	}
	p.changed()
}

// request reads the widgets into a rest.Request. The query string shown in
// the URL field is left out of the URL as the params table holds it.
func (p *requestPanel) request() rest.Request {
	base, _ := rest.SplitURL(p.url.Text())

	return rest.Request{
		Method:   p.method.CurrentText(),
		URL:      base,
		Params:   p.params.items(),
		Headers:  p.headers.items(),
		Auth:     p.auth.auth(),
		BodyType: bodyTypes[p.bodyType.CurrentIndex()].typ,
		Body:     p.bodyRaw.ToPlainText(),
		Form:     p.bodyForm.items(),
	}
}

// load shows the request in the widgets without calling onChange. Unknown
// methods and types fall back to the first entry of their selector.
func (p *requestPanel) load(r rest.Request) {
	p.updating = true
	defer func() { p.updating = false }()

	p.method.SetCurrentIndex(max(0, slices.Index(rest.Methods, r.Method)))
	// A query string in the stored URL is moved into the params.
	base, query := rest.SplitURL(r.URL)
	params := append(query, r.Params...)
	rest.SortPairs(params)
	p.url.SetText(rest.JoinURL(base, params))
	p.params.setItems(params)
	p.headers.setItems(r.Headers)

	p.auth.load(r.Auth)

	bodyIndex := 0
	for i, t := range bodyTypes {
		if t.typ == r.BodyType {
			bodyIndex = i
		}
	}
	p.bodyType.SetCurrentIndex(bodyIndex)
	p.bodyRaw.SetPlainText(r.Body)
	p.bodyFold.reset()
	p.bodyForm.setItems(r.Form)
	p.updateCurl()
}

// updatePreview shows what the URL resolves to when it contains placeholders,
// or which of them are not defined in the active environment.
func (p *requestPanel) updatePreview(expand func(string) string) {
	url := p.url.Text()
	if len(env.Placeholders(url)) == 0 {
		p.preview.SetVisible(false)
		return
	}

	resolved := expand(url)
	if unknown := env.Placeholders(resolved); len(unknown) > 0 {
		p.preview.SetText("Not defined in the selected environment: " + strings.Join(unknown, ", "))
		p.preview.SetStyleSheet("color: " + statusColour(400))
	} else {
		p.preview.SetText("→ " + resolved)
		p.preview.SetStyleSheet("color: " + statusColour(200))
	}
	p.preview.SetVisible(true)
}

package ui

import (
	"slices"
	"strings"

	qt "github.com/mappu/miqt/qt6"

	"github.com/nomad-software/postie/internal/env"
	"github.com/nomad-software/postie/internal/rest"
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
	p.auth = newAuthEditor(p.highlight)
	p.auth.onChange = p.changed

	p.headers = newKeyValueTable("Header", "Value")
	p.headers.onChange = p.changed

	tabs := qt.NewQTabWidget2()
	tabs.AddTab(p.params.panel(), "Params")
	tabs.AddTab(p.auth.root, "Authorization")
	tabs.AddTab(p.headers.panel(), "Headers")
	tabs.AddTab(p.newBodyTab(), "Body")

	layout := qt.NewQVBoxLayout(p.root)
	layout.AddWidget(p.header.root)
	layout.AddLayout(bar.QLayout)
	layout.AddWidget(p.preview.QWidget)
	layout.AddWidget(tabs.QWidget)

	return p
}

func (p *requestPanel) newBodyTab() *qt.QWidget {
	p.bodyType = qt.NewQComboBox2()
	for _, t := range bodyTypes {
		p.bodyType.AddItem(t.label)
	}

	p.bodyRaw = qt.NewQPlainTextEdit2()
	p.bodyRaw.SetFont(qt.QFontDatabase_SystemFont(qt.QFontDatabase__FixedFont))
	p.bodyRaw.OnTextChanged(p.changed)
	p.highlight.document(p.bodyRaw.Document())

	p.bodyForm = newKeyValueTable("Key", "Value")
	p.bodyForm.onChange = p.changed

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
	p.bodyForm.setItems(r.Form)
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

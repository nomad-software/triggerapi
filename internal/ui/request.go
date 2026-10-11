package ui

import (
	"net/http"
	"slices"

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

// Tabs of the request panel.
const (
	tabBody = iota
	tabParams
	tabAuthorization
	tabHeaders
)

// requestPanel holds the widgets used to compose a request.
type requestPanel struct {
	root *qt.QWidget

	method *qt.QComboBox
	// url is the URL field, which wraps a URL too long for it and grows
	// taller to show it all.
	url  *wrapEdit
	send *qt.QPushButton

	// preview, the button between the URL field and Send, shows in place
	// of the field what the URL resolves to.
	preview *qt.QToolButton
	// resolved is the URL with the variables of the active environment in
	// place, and a mark where a placeholder has no value. It cannot be
	// edited.
	resolved *wrapEdit

	params  *keyValueTable
	headers *keyValueTable
	// captures are the variables to set from the response, each with the
	// path of its value in the response. They are part of the request, but
	// are shown as the Capture tab of its response panel, where the
	// response they are about is.
	captures *keyValueTable

	auth *authEditor

	// tabs holds the parts of the request (tabBody, tabParams, ...).
	tabs *qt.QTabWidget

	bodyType *qt.QComboBox
	bodyRaw  *qt.QPlainTextEdit
	bodyForm *keyValueTable
	// bodyFold folds the objects and arrays of a JSON body.
	bodyFold *folder

	// pages shows the fields of the request, or the request as a command
	// (pageFields / pageCurl), as chosen with the curl button beside the
	// tabs.
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

	p.method = qt.NewQComboBox2()
	p.method.AddItems(rest.Methods)
	p.method.OnCurrentIndexChanged(func(int) {
		p.showLikelyTab()
		p.changed()
	})

	p.url = newWrapEdit()
	p.url.SetPlaceholderText("Enter URL, e.g. https://api.example.com/users or {{host}}/users")
	p.url.onChange = func(string) { p.urlChanged() }

	p.highlight.document(p.url.Document(), func() bool { return false })

	p.send = qt.NewQPushButton3("Send")
	makePrimary(p.send)
	p.send.SetToolTip("Ctrl+Return")

	// Only one of the two is on show, and the other takes no room.
	p.resolved = newWrapEdit()
	p.resolved.SetReadOnly(true)
	p.resolved.SetVisible(false)

	p.preview = newIconButton(colourAction, drawEye, "Show the URL with the values of its placeholders")
	p.preview.SetCheckable(true)
	p.preview.OnToggled(func(checked bool) {
		p.url.SetVisible(!checked)
		p.resolved.SetVisible(checked)
		if checked {
			p.updatePreview()
		} else {
			p.url.SetFocus()
		}
	})

	// What is beside the URL stays level with its first line as it grows:
	// each is put in the middle of a line as tall as the tallest of them.
	bar := qt.NewQHBoxLayout2()
	line := max(p.method.SizeHint().Height(), p.preview.SizeHint().Height(), p.send.SizeHint().Height())
	beside := func(widget *qt.QWidget) {
		widget.SetFixedHeight(widget.SizeHint().Height())
		holder := qt.NewQVBoxLayout2()
		holder.SetContentsMargins(0, (line-widget.SizeHint().Height())/2, 0, 0)
		holder.AddWidget(widget)
		holder.AddStretch()
		bar.AddLayout(holder.QLayout)
	}
	urls := qt.NewQVBoxLayout2()
	urls.SetContentsMargins(0, max(0, (line-p.url.Height())/2), 0, 0)
	urls.AddWidget(p.url.QWidget)
	urls.AddWidget(p.resolved.QWidget)
	urls.AddStretch()

	beside(p.method.QWidget)
	bar.AddLayout2(urls.QLayout, 1)
	beside(p.preview.QWidget)
	beside(p.send.QWidget)

	p.params = newKeyValueTable("Key", "Value")
	p.params.onChange = p.paramsChanged
	p.highlight.table(p.params.table)
	p.auth = newAuthEditor(p.highlight)
	p.auth.onChange = p.changed

	p.headers = newKeyValueTable("Header", "Value")
	p.headers.onChange = p.changed
	p.highlight.table(p.headers.table)

	p.captures = newCaptureTable()
	p.captures.onChange = p.changed

	// The curl button is at the right of the line the tabs are on.
	p.curl = newIconButton(colourAction, drawPrompt, "Show the request as a curl command")
	p.curl.SetCheckable(true)
	p.curl.OnToggled(func(checked bool) {
		if checked {
			p.pages.SetCurrentIndex(pageCurl)
			p.updateCurl()
		} else {
			p.pages.SetCurrentIndex(pageFields)
		}
	})

	p.tabs = newButtonTabs(p.curl)
	p.tabs.AddTab(p.newBodyTab(), "Body")
	p.tabs.AddTab(p.params.widget(), "Params")
	p.tabs.AddTab(p.auth.root, "Authorization")
	p.tabs.AddTab(p.headers.widget(), "Headers")

	fields := qt.NewQWidget2()
	fieldsLayout := qt.NewQVBoxLayout(fields)
	fieldsLayout.SetContentsMargins(0, 0, 0, 0)
	fieldsLayout.AddLayout(bar.QLayout)
	// The line of the URL is only as tall as the URL makes it.
	fieldsLayout.AddWidget2(p.tabs.QWidget, 1)

	p.pages = qt.NewQStackedWidget2()
	p.pages.AddWidget(fields)
	p.pages.AddWidget(p.newCurlPage())

	layout := qt.NewQVBoxLayout(p.root)
	layout.AddWidget2(p.pages.QWidget, 1)

	return p
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

// setCodeFont sets the font the body and the curl command are shown in.
func (p *requestPanel) setCodeFont(family string) {
	p.bodyRaw.SetFont(fixedFont(family))
	p.command.SetFont(fixedFont(family))
}

// missingValue is shown in the preview of the URL where a placeholder has no
// value: a black square, for something that is not there.
const missingValue = "\u25a0"

// updatePreview shows the URL as it is with the variables of the active
// environment in place, if the preview is on show. The values are in the
// colour of placeholders that are found, and where the environment does not
// define one, or defines it as nothing, there is a mark, in the colour of
// those that are not.
func (p *requestPanel) updatePreview() {
	if !p.preview.IsChecked() {
		return
	}

	expand := p.expand
	if expand == nil {
		expand = func(text string) string { return text }
	}

	var pieces []textPiece
	url, last := p.url.Text(), 0
	for _, loc := range env.FindPlaceholders(url) {
		pieces = append(pieces, textPiece{text: url[last:loc[0]]})

		match := url[loc[0]:loc[1]]
		// A placeholder that is defined as nothing is as much a gap in
		// the URL as one that is not defined.
		if value := expand(match); value != match && value != "" {
			pieces = append(pieces, textPiece{value, p.highlight.known})
		} else {
			pieces = append(pieces, textPiece{missingValue, p.highlight.unknown})
		}
		last = loc[1]
	}
	pieces = append(pieces, textPiece{text: url[last:]})

	p.resolved.setPieces(pieces)
}

// escape closes what is shown in place of the fields of the request: the
// curl command, or else the preview of the URL. It reports if there was
// anything to close.
func (p *requestPanel) escape() bool {
	switch {
	case p.curl.IsChecked():
		p.curl.SetChecked(false)
	case p.preview.IsChecked():
		p.preview.SetChecked(false)
	default:
		return false
	}
	return true
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
	stack.AddWidget(p.bodyForm.widget())

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

// showLikelyTab selects the tab most likely to be wanted for the method: the
// body for a method that sends one, and otherwise the params.
func (p *requestPanel) showLikelyTab() {
	switch p.method.CurrentText() {
	case http.MethodPost, http.MethodPut, http.MethodPatch:
		p.tabs.SetCurrentIndex(tabBody)
	default:
		p.tabs.SetCurrentIndex(tabParams)
	}
}

func (p *requestPanel) changed() {
	p.updatePreview()
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
	p.updatePreview()
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
		Captures: captureRules(p.captures),
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
	setCaptureRules(p.captures, r.Captures)
	p.showLikelyTab()
	p.updateCurl()
	p.updatePreview()
}

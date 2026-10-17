package ui

import (
	"encoding/json"
	"fmt"
	"html"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"

	qt "github.com/mappu/miqt/qt6"

	"github.com/nomad-software/triggerapi/internal/capture"
	"github.com/nomad-software/triggerapi/internal/graphql"
	"github.com/nomad-software/triggerapi/internal/mcp"
	"github.com/nomad-software/triggerapi/internal/rest"
	"github.com/nomad-software/triggerapi/internal/rpc"
)

// responsePanel displays the result of the last request.
type responsePanel struct {
	root *qt.QWidget

	tabs    *qt.QTabWidget
	body    *qt.QPlainTextEdit
	headers *qt.QTableWidget

	// save writes the body of the response to a file. It is at the right
	// of the line the tabs are on.
	save *qt.QToolButton
	// find is the bar above the body in which text to find in it is typed.
	// Its button is before save.
	find *findBar

	// captures is the table of the Capture tab, which belongs to the
	// request panel, and captured what came of capturing from the response
	// that is shown.
	captures *keyValueTable
	captured []captureResult
	// content is the body as it is saved: JSON formatted as it is shown,
	// and any other HTTP response as it was received. fileName is a name
	// for the file with the extension for what the content is, of which
	// only the extension is used: see saveFileName.
	content  []byte
	fileName string
	// requestName returns the name of the request the response is to,
	// which is empty if it has not been saved, and now the time. The file
	// is named after both.
	requestName func() string
	now         func() time.Time
	// captureDoc is the JSON that values are captured from, which the
	// body ends with as it is shown: all of a REST response, and the last
	// message of a gRPC one. It is empty if there is none.
	captureDoc string
	// chooseSave asks the user where to save a file, offering a name. It
	// reports false if they decide against saving.
	chooseSave func(name string) (string, bool)
	// warn tells the user of a problem.
	warn func(message string)

	// json records that the response is JSON, which is coloured and can have
	// its objects and arrays folded.
	json bool
	fold *folder
	// formats are the colours JSON is shown in, applied by the highlighter.
	formats     jsonFormats
	highlighter *qt.QSyntaxHighlighter

	// result and details make up the bar beneath the response, which shows
	// the fields. The code and any error are in result, at the left, and
	// the rest in details, at the right.
	result  *qt.QLabel
	details *qt.QLabel
	// fields describe the response: its code, the error there was, the time
	// it took and so on.
	fields []field
}

// The labels of the fields shown at the left of the bar.
const (
	fieldCode  = "Status"
	fieldError = "Error"
)

// field is a labelled value in the bar beneath a response.
type field struct {
	label string
	value string
	// failure marks the value as an error, which is shown in the colour of
	// errors.
	failure bool
}

// newResponsePanel creates the panel, showing the response body in the named
// font family.
func newResponsePanel(bodyFont string) *responsePanel {
	p := &responsePanel{root: qt.NewQWidget2()}

	p.body = qt.NewQPlainTextEdit2()
	p.body.SetReadOnly(true)

	isJSON := func() bool { return p.json }
	p.formats = newJSONFormats()
	p.highlighter = qt.NewQSyntaxHighlighter2(p.body.Document())
	p.highlighter.OnHighlightBlock(func(text string) {
		if isJSON() {
			p.formats.apply(p.highlighter, jsonTokens(text))
		}
		// The matches of a search are marked over the colours of JSON.
		if p.find != nil {
			p.find.highlight(text)
		}
	})
	p.fold = newFolder(p.body, isJSON)
	// The menu of the body offers to capture the value it is opened on.
	p.body.OnContextMenuEvent(func(_ func(*qt.QContextMenuEvent), event *qt.QContextMenuEvent) {
		cursor := p.body.CursorForPosition(event.Pos())
		menu := p.contextMenu(cursor.Position())
		release(cursor)
		menu.ExecWithPos(event.GlobalPos())
		menu.DeleteLater()
	})
	p.find = newFindBar(p.body, p.highlighter, p.fold)
	p.find.onShow = func() { p.tabs.SetCurrentIndex(tabResponse) }
	p.setBodyFont(bodyFont)

	p.headers = qt.NewQTableWidget3(0, 2)
	p.headers.SetHorizontalHeaderLabels([]string{"Header", "Value"})
	p.headers.VerticalHeader().SetVisible(false)
	p.headers.HorizontalHeader().SetSectionResizeMode2(0, qt.QHeaderView__ResizeToContents)
	p.headers.HorizontalHeader().SetStretchLastSection(true)
	p.headers.SetEditTriggers(qt.QAbstractItemView__NoEditTriggers)
	p.headers.SetSelectionBehavior(qt.QAbstractItemView__SelectRows)

	p.save = newIconButton(colourAction, drawDisk, "Save the response to a file")
	p.save.SetEnabled(false)
	p.save.OnClicked(p.saveBody)

	// The bar to find text with is above the body it is found in.
	response := qt.NewQWidget2()
	responseLayout := qt.NewQVBoxLayout(response)
	responseLayout.SetContentsMargins(0, 0, 0, 0)
	responseLayout.AddWidget(p.find.root)
	responseLayout.AddWidget2(p.body.QWidget, 1)

	p.tabs = newButtonTabs(p.find.button, p.save)
	p.tabs.AddTab(response, "Response")
	p.tabs.AddTab(p.headers.QWidget, "Headers")

	p.now = time.Now
	p.chooseSave = func(name string) (string, bool) {
		path := qt.QFileDialog_GetSaveFileName3(p.root, "Save Response", name)
		return path, path != ""
	}
	p.warn = func(message string) {
		qt.QMessageBox_Warning(p.root, "TriggerAPI", message)
	}
	layout := qt.NewQVBoxLayout(p.root)
	p.result = qt.NewQLabel2()
	p.result.SetTextFormat(qt.RichText)
	p.result.SetWordWrap(true)
	p.result.SetTextInteractionFlags(qt.TextSelectableByMouse)
	p.details = qt.NewQLabel2()
	p.details.SetTextFormat(qt.RichText)

	fields := qt.NewQHBoxLayout2()
	fields.AddWidget2(p.result.QWidget, 1)
	fields.AddWidget3(p.details.QWidget, 0, qt.AlignRight|qt.AlignTop)

	layout.AddWidget2(p.tabs.QWidget, 1)
	layout.AddLayout(fields.QLayout)

	return p
}

// setContent keeps the body of the response to be saved, and the name to
// offer for its file. Without a body there is nothing to save.
func (p *responsePanel) setContent(content []byte, fileName string) {
	p.content = content
	p.fileName = fileName
	p.save.SetEnabled(len(content) > 0)
}

// saveBody asks where to save the body of the response, and saves it.
func (p *responsePanel) saveBody() {
	if len(p.content) == 0 {
		return
	}
	request := ""
	if p.requestName != nil {
		request = p.requestName()
	}
	path, ok := p.chooseSave(saveFileName(request, p.now(), filepath.Ext(p.fileName)))
	if !ok {
		return
	}
	if err := os.WriteFile(path, p.content, 0o644); err != nil {
		p.warn(fmt.Sprintf("The response could not be saved: %v.", err))
	}
}

// maxFileStem is the most characters of the name of a request that go into
// the name of a file.
const maxFileStem = 80

// saveFileName returns the name to offer for the file a response is saved
// to: the name of its request, the time and resp, which says that it is a
// response, with underscores between them, and the extension, as in
// book_room_20261011_173005_resp.json. A request with no name is called
// untitled, as its tab is.
//
// The name is one that is no trouble on any system or in a shell. Letters
// are made lower case, and anything that is not a letter or a digit, such as
// a space, a slash or a dot, becomes an underscore, of which there is never
// more than one together or any at either end.
func saveFileName(request string, at time.Time, extension string) string {
	var stem strings.Builder
	count, gap := 0, false
	for _, r := range strings.ToLower(request) {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			gap = true
			continue
		}
		if count == maxFileStem {
			break
		}
		if gap && count > 0 {
			stem.WriteByte('_')
		}
		gap = false
		stem.WriteRune(r)
		count++
	}
	if count == 0 {
		stem.WriteString("untitled")
	}
	return stem.String() + "_" + at.Format("20060102_150405") + "_resp" + extension
}

// responseFileName returns a name for the file a response is saved to which
// has the extension for the type of content it says it is.
func responseFileName(contentType string) string {
	contentType = strings.ToLower(contentType)
	for _, kind := range []struct{ part, extension string }{
		{"json", "json"}, {"xml", "xml"}, {"html", "html"}, {"csv", "csv"}, {"pdf", "pdf"},
		{"png", "png"}, {"jpeg", "jpg"}, {"gif", "gif"}, {"zip", "zip"}, {"text/", "txt"},
	} {
		if strings.Contains(contentType, kind.part) {
			return "response." + kind.extension
		}
	}
	return "response.bin"
}

// setJSONColours changes the colours JSON is shown in.
func (p *responsePanel) setJSONColours(colours map[tokenKind]string) {
	p.formats.setColours(colours)
	p.highlighter.Rehighlight()
}

// setBodyFont shows the response body in the named font family. The system's
// fixed width font is the fallback if the family is not installed, and
// supplies the size.
func (p *responsePanel) setBodyFont(family string) {
	p.body.SetFont(fixedFont(family))
}

// fixedFont returns the font of a family, falling back to the fixed width
// font of the system where it is not installed.
func fixedFont(family string) *qt.QFont {
	font := qt.QFontDatabase_SystemFont(qt.QFontDatabase__FixedFont)
	font.SetFamilies(append([]string{family}, font.Families()...))
	return font
}

// clear empties the panel.
func (p *responsePanel) clear() {
	p.setBody("", false)
	p.setContent(nil, "")
	p.headers.SetRowCount(0)
	p.tabs.SetTabText(tabResponseHeaders, "Headers")
	p.setFields()
	p.showCaptured(nil)
}

// maxHighlight is the size of response above which it is not treated as
// JSON, as colouring it line by line would take too long.
const maxHighlight = 2 << 20

// setBody shows text as the response. JSON is coloured and can be folded.
func (p *responsePanel) setBody(text string, isJSON bool) {
	p.json = isJSON && len(text) <= maxHighlight
	p.captureDoc = ""
	p.body.SetPlainText(text)
	p.fold.reset()
	// What is being looked for is looked for in the new body.
	p.find.search()
}

// capturePath returns the path of the value at a position in the body, for
// a rule which captures it, and a name to call it by. There is none if the
// position is not in JSON that values are captured from.
func (p *responsePanel) capturePath(position int) (path, name string, ok bool) {
	if p.captures == nil || p.captureDoc == "" {
		return "", "", false
	}
	text := p.body.ToPlainText()
	if !strings.HasSuffix(text, p.captureDoc) {
		return "", "", false
	}

	// The position is in UTF-16 code units, and the offset in bytes.
	offset := 0
	for _, r := range text {
		if position <= 0 {
			break
		}
		position -= utf16.RuneLen(r)
		offset += utf8.RuneLen(r)
	}

	offset -= len(text) - len(p.captureDoc)
	if offset < 0 {
		return "", "", false
	}
	return capture.PathAt(p.captureDoc, offset)
}

// contextMenu returns the menu of the body for a position in it: the usual
// one of an editor and, if there is a value there that can be captured, the
// offer to capture it.
func (p *responsePanel) contextMenu(position int) *qt.QMenu {
	menu := p.body.CreateStandardContextMenu()
	if path, name, ok := p.capturePath(position); ok {
		label := "Capture the whole response"
		if path != "" {
			// An & would otherwise underline the letter after it.
			label = "Capture " + strings.ReplaceAll(path, "&", "&&")
		}
		menu.AddSeparator()
		menu.AddActionWithText(label).OnTriggered(func() { p.addCapture(path, name) })
	}
	return menu
}

// addCapture adds a rule which captures the value at a path, and shows it
// on the Capture tab with the name of its variable ready to be changed.
func (p *responsePanel) addCapture(path, name string) {
	p.tabs.SetCurrentIndex(tabCapture)
	p.captures.addToEdit(name, path)
}

// Tabs of a response panel. The last is the one on which what to capture
// from the response is set.
const (
	tabResponse = iota
	tabResponseHeaders
	tabCapture
)

// setCaptureTable adds the Capture tab, which shows the table of what the
// request captures from its response.
func (p *responsePanel) setCaptureTable(table *keyValueTable) {
	p.captures = table
	p.tabs.AddTab(table.widget(), "Capture")
	// The marks are of the rows as they were when the response came.
	table.onStale = func() { p.showCaptured(nil) }
}

// showCaptured marks the Capture tab with how capturing from the response
// went: a tick if every value was captured and a cross if any was not, with
// the same against each row of the table, and the reason one failed as the
// tooltip of its mark. With no results the marks are taken away.
func (p *responsePanel) showCaptured(results []captureResult) {
	p.captured = results
	if p.captures == nil {
		return
	}

	statuses := map[string]rowStatus{}
	var failed []string
	for _, r := range results {
		if r.err != nil {
			statuses[r.variable] = rowStatus{captureIcon(false), "Not captured: " + r.err.Error()}
			failed = append(failed, r.variable+": "+r.err.Error())
		} else {
			statuses[r.variable] = rowStatus{captureIcon(true), "Captured"}
		}
	}
	p.captures.showStatuses(statuses)

	switch {
	case len(results) == 0:
		none := qt.NewQIcon()
		defer none.Delete()
		p.tabs.SetTabIcon(tabCapture, none)
		p.tabs.SetTabToolTip(tabCapture, "")
	case len(failed) > 0:
		p.tabs.SetTabIcon(tabCapture, captureIcon(false))
		p.tabs.SetTabToolTip(tabCapture, "Not captured:\n"+strings.Join(failed, "\n"))
	default:
		p.tabs.SetTabIcon(tabCapture, captureIcon(true))
		p.tabs.SetTabToolTip(tabCapture, "Every value was captured")
	}
}

// recolour shows the bar beneath the response and the marks of what was
// captured again, after the colours have changed.
func (p *responsePanel) recolour() {
	p.setFields(p.fields...)
	p.showCaptured(p.captured)
	p.find.recolour()
}

// setFields fills the bar beneath the response with labelled values. Fields
// with no value are left out.
func (p *responsePanel) setFields(fields ...field) {
	p.fields = slices.DeleteFunc(fields, func(f field) bool { return f.value == "" })

	// The fields at the left are the cells of a table, so that an error too
	// long for its line carries on beneath itself and not beneath the
	// status before it. Nothing else is wrapped.
	const nowrap = `white-space: nowrap; `
	// The labels are fainter than the values they name.
	faded := fadedTextColour()
	var left strings.Builder
	var right []string
	// The last cell takes the width that is left over, which would
	// otherwise be shared out and leave gaps between the others. It is the
	// error if there is one.
	filled := false
	for _, f := range p.fields {
		value := html.EscapeString(f.value)
		if f.failure {
			value = fmt.Sprintf(`<span style="color: %s">%s</span>`, themeColour(colourError), value)
		}
		label := fmt.Sprintf(`<b style="color: %s">%s:</b>`, faded, html.EscapeString(f.label))

		switch f.label {
		case fieldCode:
			fmt.Fprintf(&left, `<td style="%spadding-right: 5px">%s</td><td style="%spadding-right: 20px">%s</td>`, nowrap, label, nowrap, value)
		case fieldError:
			fmt.Fprintf(&left, `<td style="%spadding-right: 5px">%s</td><td width="100%%">%s</td>`, nowrap, label, value)
			filled = true
		default:
			right = append(right, label+" "+value)
		}
	}

	result := ""
	if left.Len() > 0 {
		if !filled {
			left.WriteString(`<td width="100%"></td>`)
		}
		result = `<table width="100%" cellspacing="0" cellpadding="0"><tr>` + left.String() + `</tr></table>`
	}
	p.result.SetText(result)
	p.details.SetText(strings.Join(right, "&nbsp;&nbsp;&nbsp;&nbsp; "))
}

// showCancelled shows that the request was cancelled before it finished.
func (p *responsePanel) showCancelled(elapsed time.Duration) {
	p.clear()
	p.setFields(field{fieldError, "Cancelled", true}, field{label: "Time", value: formatDuration(elapsed)})
}

// showError shows an error that prevented there being a response, and how
// long it took to fail.
func (p *responsePanel) showError(err error, elapsed time.Duration) {
	p.clear()
	p.setFields(field{fieldError, err.Error(), true}, field{label: "Time", value: formatDuration(elapsed)})
}

// show shows the response to a REST request. Only its body goes in the
// response; its status goes in the bar beneath, as a failure if it is an
// error status.
func (p *responsePanel) show(res *rest.Response) {
	text := res.Text()
	p.setBody(text, res.IsJSON())
	// Values are captured from any body that is JSON, whatever it says it
	// is. It is taken as it is shown, as the editor has its own line
	// endings.
	if json.Valid(res.Body) {
		p.captureDoc = p.body.ToPlainText()
	}
	// JSON is saved formatted, as it is shown. Anything else is saved as
	// it was received, which keeps a file that is not text intact.
	content := res.Body
	if res.IsJSON() {
		content = []byte(text)
	}
	p.setContent(content, responseFileName(res.Headers.Get("Content-Type")))
	p.setHeaders(headerRows(res.Headers, ""))
	p.setFields(
		field{fieldCode, res.Status, res.StatusCode >= 400},
		field{label: "Time", value: formatDuration(res.Duration)},
		field{label: "Size", value: formatSize(len(res.Body))},
		field{label: "Protocol", value: res.Proto},
	)
}

// showGraphQL shows the response to a GraphQL request as that to a REST
// request, with the errors it reports as the Error, as they can come with
// any status.
func (p *responsePanel) showGraphQL(res *rest.Response) {
	p.show(res)
	if message := graphql.Errors(res.Body); message != "" {
		fields := slices.Clone(p.fields)
		at := 0
		if len(fields) > 0 && fields[0].label == fieldCode {
			at = 1
		}
		p.setFields(slices.Insert(fields, at, field{fieldError, message, true})...)
	}
}

// showRPC shows the result of a gRPC call. Only its messages go in the
// response; its status goes in the bar beneath, as a failure if it is not
// OK. The trailers of the call are listed after its headers.
func (p *responsePanel) showRPC(res *rpc.Response) {
	p.setBody(strings.Join(res.Messages, "\n\n"), true)
	if len(res.Messages) > 0 {
		p.captureDoc = res.Messages[len(res.Messages)-1]
	}
	p.setContent([]byte(p.body.ToPlainText()), "response.json")
	p.setHeaders(append(headerRows(res.Headers, ""), headerRows(res.Trailers, " (trailer)")...))
	p.setFields(
		field{fieldCode, fmt.Sprintf("%d %s", res.Code, res.CodeName), !res.OK()},
		field{fieldError, res.Status, true},
		field{label: "Time", value: formatDuration(res.Duration)},
		field{label: "Messages", value: strconv.Itoa(len(res.Messages))},
		field{label: "Protocol", value: res.Protocol},
	)
}

// showMCP shows the result of a call to an MCP server as JSON, or the error
// the server answered with. Values are captured from the result. The bar
// beneath says how it went, as a failure if the server answered with an
// error or the tool reported one.
func (p *responsePanel) showMCP(res *mcp.Response) {
	status, problem, failed := res.Outcome()
	p.setBody(res.Text(), true)
	if res.Error == nil {
		p.captureDoc = p.body.ToPlainText()
	}
	p.setContent([]byte(p.body.ToPlainText()), "response.json")
	p.setHeaders(headerRows(res.Headers, ""))
	p.setFields(
		field{fieldCode, status, failed},
		field{fieldError, problem, true},
		field{label: "Time", value: formatDuration(res.Duration)},
		field{label: "Server", value: res.Server},
		field{label: "Protocol", value: res.Protocol},
	)
}

// noteOmitted says in the bar beneath a response from the history that its
// body, of a size, was too large to be kept there, and gives that as the
// Size of an HTTP response, which would otherwise be that of no body.
func (p *responsePanel) noteOmitted(size int) {
	fields := slices.Clone(p.fields)
	for i := range fields {
		if fields[i].label == "Size" {
			fields[i].value = formatSize(size)
		}
	}
	fields = append(fields, field{label: "Body", value: "not kept in the history"})
	p.setFields(fields...)
}

// headerRows returns the headers as name and value pairs sorted by name, with
// the suffix added to each name.
func headerRows(headers http.Header, suffix string) [][2]string {
	var rows [][2]string
	for _, key := range slices.Sorted(maps.Keys(headers)) {
		for _, value := range headers[key] {
			rows = append(rows, [2]string{key + suffix, value})
		}
	}
	return rows
}

func (p *responsePanel) setHeaders(rows [][2]string) {
	p.headers.SetRowCount(0)
	for _, pair := range rows {
		row := p.headers.RowCount()
		p.headers.InsertRow(row)
		p.headers.SetItem(row, 0, qt.NewQTableWidgetItem2(pair[0]))
		p.headers.SetItem(row, 1, qt.NewQTableWidgetItem2(pair[1]))
	}
	p.tabs.SetTabText(tabResponseHeaders, fmt.Sprintf("Headers (%d)", len(rows)))
}

func formatDuration(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%d ms", d.Milliseconds())
	}
	return fmt.Sprintf("%.2f s", d.Seconds())
}

func formatSize(bytes int) string {
	switch {
	case bytes < 1024:
		return fmt.Sprintf("%d B", bytes)
	case bytes < 1024*1024:
		return fmt.Sprintf("%.1f KB", float64(bytes)/1024)
	default:
		return fmt.Sprintf("%.1f MB", float64(bytes)/(1024*1024))
	}
}

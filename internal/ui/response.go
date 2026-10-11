package ui

import (
	"fmt"
	"html"
	"maps"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	qt "github.com/mappu/miqt/qt6"

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
	// content is the body as it is saved: JSON formatted as it is shown,
	// and any other HTTP response as it was received. fileName is the name
	// offered for the file.
	content  []byte
	fileName string
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
			p.formats.apply(p.highlighter, text)
		}
	})
	p.fold = newFolder(p.body, isJSON)
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

	p.tabs = newButtonTabs(p.save)
	p.tabs.AddTab(p.body.QWidget, "Response")
	p.tabs.AddTab(p.headers.QWidget, "Headers")

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
	path, ok := p.chooseSave(p.fileName)
	if !ok {
		return
	}
	if err := os.WriteFile(path, p.content, 0o644); err != nil {
		p.warn(fmt.Sprintf("The response could not be saved: %v.", err))
	}
}

// responseFileName returns a name to offer for the file a response is saved
// to, from the type of content it says it is.
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
	p.tabs.SetTabText(1, "Headers")
	p.setFields()
}

// maxHighlight is the size of response above which it is not treated as
// JSON, as colouring it line by line would take too long.
const maxHighlight = 2 << 20

// setBody shows text as the response. JSON is coloured and can be folded.
func (p *responsePanel) setBody(text string, isJSON bool) {
	p.json = isJSON && len(text) <= maxHighlight
	p.body.SetPlainText(text)
	p.fold.reset()
}

// recolour shows the bar beneath the response again, after the colour of
// errors has changed.
func (p *responsePanel) recolour() {
	p.setFields(p.fields...)
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
	p.setBody(res.Text(), res.IsJSON())
	// JSON is saved formatted, as it is shown. Anything else is saved as
	// it was received, which keeps a file that is not text intact.
	content := res.Body
	if res.IsJSON() {
		content = []byte(res.Text())
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

// showRPC shows the result of a gRPC call. Only its messages go in the
// response; its status goes in the bar beneath, as a failure if it is not
// OK. The trailers of the call are listed after its headers.
func (p *responsePanel) showRPC(res *rpc.Response) {
	p.setBody(strings.Join(res.Messages, "\n\n"), true)
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
	p.tabs.SetTabText(1, fmt.Sprintf("Headers (%d)", len(rows)))
}

// failureColour is the colour errors are shown in, until another is chosen
// in the settings (colourError).
const failureColour = "#ef5350"

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

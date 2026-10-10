package ui

import (
	"fmt"
	"html"
	"maps"
	"net/http"
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
	fieldCode  = "Code"
	fieldError = "Error"
)

// field is a labelled value in the bar beneath a response.
type field struct {
	label string
	value string
	// failure marks the value as an error, which is shown in red.
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

	p.tabs = qt.NewQTabWidget2()
	p.tabs.AddTab(p.body.QWidget, "Response")
	p.tabs.AddTab(p.headers.QWidget, "Headers")

	layout := qt.NewQVBoxLayout(p.root)
	p.result = qt.NewQLabel2()
	p.result.SetTextFormat(qt.RichText)
	p.result.SetWordWrap(true)
	p.result.SetTextInteractionFlags(qt.TextSelectableByMouse)
	p.details = qt.NewQLabel2()
	p.details.SetTextFormat(qt.RichText)

	bar := qt.NewQHBoxLayout2()
	bar.AddWidget2(p.result.QWidget, 1)
	bar.AddWidget3(p.details.QWidget, 0, qt.AlignRight|qt.AlignTop)

	layout.AddWidget2(p.tabs.QWidget, 1)
	layout.AddLayout(bar.QLayout)

	return p
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

// setFields fills the bar beneath the response with labelled values. Fields
// with no value are left out.
func (p *responsePanel) setFields(fields ...field) {
	p.fields = slices.DeleteFunc(fields, func(f field) bool { return f.value == "" })

	var left, right []string
	for _, f := range p.fields {
		value := html.EscapeString(f.value)
		if f.failure {
			value = fmt.Sprintf(`<span style="color: %s">%s</span>`, failureColour, value)
		}
		text := fmt.Sprintf("<b>%s:</b> %s", html.EscapeString(f.label), value)

		if f.label == fieldCode || f.label == fieldError {
			left = append(left, text)
		} else {
			right = append(right, text)
		}
	}

	const gap = "&nbsp;&nbsp;&nbsp;&nbsp; "
	p.result.SetText(strings.Join(left, gap))
	p.details.SetText(strings.Join(right, gap))
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
	p.setHeaders(append(headerRows(res.Headers, ""), headerRows(res.Trailers, " (trailer)")...))
	p.setFields(
		field{fieldCode, fmt.Sprintf("%d %s", res.Code, res.CodeName), !res.OK()},
		field{fieldError, res.Status, true},
		field{label: "Time", value: formatDuration(res.Duration)},
		field{label: "Messages", value: strconv.Itoa(len(res.Messages))},
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

// failureColour is the colour errors are shown in.
const failureColour = "#ef5350"

func statusColour(code int) string {
	switch {
	case code >= 500:
		return failureColour
	case code >= 400:
		return "#d97706"
	case code >= 300:
		return "#2f7fd1"
	default:
		return "#2e9e5b"
	}
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

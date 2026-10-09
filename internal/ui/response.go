package ui

import (
	"fmt"
	"slices"
	"time"

	qt "github.com/mappu/miqt/qt6"

	"github.com/nomad-software/postie/internal/rest"
)

const responseIdle = "Send a request to see the response."

// responsePanel displays the result of the last request.
type responsePanel struct {
	root *qt.QWidget

	status  *qt.QLabel
	stats   *qt.QLabel
	tabs    *qt.QTabWidget
	body    *qt.QPlainTextEdit
	headers *qt.QTableWidget
}

// newResponsePanel creates the panel, showing the response body in the named
// font family.
func newResponsePanel(bodyFont string) *responsePanel {
	p := &responsePanel{root: qt.NewQWidget2()}

	p.status = qt.NewQLabel3(responseIdle)
	p.status.SetTextInteractionFlags(qt.TextSelectableByMouse)
	p.stats = qt.NewQLabel2()

	bar := qt.NewQHBoxLayout2()
	bar.AddWidget2(p.status.QWidget, 1)
	bar.AddWidget(p.stats.QWidget)

	p.body = qt.NewQPlainTextEdit2()
	p.body.SetReadOnly(true)
	// The system's fixed width font is the fallback if the family is not
	// installed, and supplies the size.
	font := qt.QFontDatabase_SystemFont(qt.QFontDatabase__FixedFont)
	font.SetFamilies(append([]string{bodyFont}, font.Families()...))
	p.body.SetFont(font)

	p.headers = qt.NewQTableWidget3(0, 2)
	p.headers.SetHorizontalHeaderLabels([]string{"Header", "Value"})
	p.headers.VerticalHeader().SetVisible(false)
	p.headers.HorizontalHeader().SetSectionResizeMode2(0, qt.QHeaderView__ResizeToContents)
	p.headers.HorizontalHeader().SetStretchLastSection(true)
	p.headers.SetEditTriggers(qt.QAbstractItemView__NoEditTriggers)
	p.headers.SetSelectionBehavior(qt.QAbstractItemView__SelectRows)

	p.tabs = qt.NewQTabWidget2()
	p.tabs.AddTab(p.body.QWidget, "Body")
	p.tabs.AddTab(p.headers.QWidget, "Headers")

	layout := qt.NewQVBoxLayout(p.root)
	layout.AddLayout(bar.QLayout)
	layout.AddWidget2(p.tabs.QWidget, 1)

	return p
}

func (p *responsePanel) clear(status string) {
	p.setStatus(status, "")
	p.stats.SetText("")
	p.body.SetPlainText("")
	p.headers.SetRowCount(0)
	p.tabs.SetTabText(1, "Headers")
}

func (p *responsePanel) setStatus(text, colour string) {
	p.status.SetText(text)
	if colour == "" {
		p.status.SetStyleSheet("")
	} else {
		p.status.SetStyleSheet("font-weight: bold; color: " + colour)
	}
}

func (p *responsePanel) showError(err error) {
	p.clear("Error")
	p.setStatus("Error", statusColour(500))
	p.body.SetPlainText(err.Error())
}

func (p *responsePanel) show(res *rest.Response) {
	p.setStatus(res.Status, statusColour(res.StatusCode))
	p.stats.SetText(fmt.Sprintf("%s    %s    %s", res.Proto, formatDuration(res.Duration), formatSize(len(res.Body))))
	p.body.SetPlainText(res.Text())

	keys := make([]string, 0, len(res.Headers))
	for key := range res.Headers {
		keys = append(keys, key)
	}
	slices.Sort(keys)

	p.headers.SetRowCount(0)
	for _, key := range keys {
		for _, value := range res.Headers[key] {
			row := p.headers.RowCount()
			p.headers.InsertRow(row)
			p.headers.SetItem(row, 0, qt.NewQTableWidgetItem2(key))
			p.headers.SetItem(row, 1, qt.NewQTableWidgetItem2(value))
		}
	}
	p.tabs.SetTabText(1, fmt.Sprintf("Headers (%d)", p.headers.RowCount()))
}

func statusColour(code int) string {
	switch {
	case code >= 500:
		return "#d9363e"
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

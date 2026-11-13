package ui

import (
	"github.com/nomad-software/qute/qtcore"
	"github.com/nomad-software/qute/qtgui"
	"github.com/nomad-software/qute/qtwidgets"

	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/nomad-software/triggerapi/internal/rest"
)

// stageLabels name the stages of a request in the Timing tab.
var stageLabels = map[rest.Stage]string{
	rest.StageRedirects: "Redirects",
	rest.StageDNS:       "DNS lookup",
	rest.StageConnect:   "TCP connect",
	rest.StageTLS:       "TLS handshake",
	rest.StageSend:      "Request sent",
	rest.StageWait:      "Waiting (TTFB)",
	rest.StageDownload:  "Download",
}

// The pages of the Timing tab.
const (
	timingPageHint = iota
	timingPageTable
)

// The columns of the table of the Timing tab.
const (
	colTimingStage = iota
	colTimingTime
	colTimingBar
)

// timingTab is the Timing tab of a response panel, which says where the time
// a request took went: a row for each stage there was, in the order of
// rest.Stages, with how long it took and a bar showing when on the time of
// the whole, which is the last row, and beneath it what is known of the
// connection. A response with no timing has a hint in its place.
type timingTab struct {
	root  *qtwidgets.QWidget
	pages *qtwidgets.QStackedWidget
	table *qtwidgets.QTableWidget
	// bars are those of the rows, the last being that of the total.
	bars []*timingBar
	// details says what the connection was.
	details *qtwidgets.QLabel
	// timing is what is shown, which is nil for none.
	timing *rest.Timing
}

func newTimingTab() *timingTab {
	t := &timingTab{}

	t.table = qtwidgets.NewQTableWidgetWithRows(len(rest.Stages)+1, 3, nil)
	t.table.SetHorizontalHeaderLabels([]string{"Stage", "Time", "Timeline"})
	t.table.VerticalHeader().SetVisible(false)
	t.table.HorizontalHeader().SetSectionResizeMode(colTimingStage, qtwidgets.QHeaderViewResizeToContents)
	t.table.HorizontalHeader().SetSectionResizeMode(colTimingTime, qtwidgets.QHeaderViewResizeToContents)
	t.table.HorizontalHeader().SetStretchLastSection(true)
	t.table.SetEditTriggers(qtwidgets.QAbstractItemViewNoEditTriggers)
	// The rows are only to be read, and a selected one would put its bar on
	// the colour of the selection.
	t.table.SetSelectionMode(qtwidgets.QAbstractItemViewNoSelection)

	// The rows are made once, and those of stages there were not are
	// hidden.
	for row := range t.table.RowCount() {
		label := "Total"
		if row < len(rest.Stages) {
			label = stageLabels[rest.Stages[row]]
		}
		stage := qtwidgets.NewQTableWidgetItemWithText(label)
		duration := qtwidgets.NewQTableWidgetItemWithText("")
		duration.SetTextAlignment(qtcore.AlignRight | qtcore.AlignVCenter)
		if row == len(rest.Stages) {
			for _, item := range []*qtwidgets.QTableWidgetItem{stage, duration} {
				font := item.Font()
				font.SetBold(true)
				item.SetFont(font)
			}
		}
		t.table.SetItem(row, colTimingStage, stage)
		t.table.SetItem(row, colTimingTime, duration)

		bar := newTimingBar()
		t.bars = append(t.bars, bar)
		t.table.SetCellWidget(row, colTimingBar, bar.widget)
	}

	t.details = qtwidgets.NewQLabel(nil)
	t.details.SetTextFormat(qtcore.RichText)
	t.details.SetWordWrap(true)
	t.details.SetTextInteractionFlags(qtcore.TextSelectableByMouse)

	// The details are in a panel, as the table is, with room about them.
	// A frame of the style's own is not drawn by every style.
	details := qtwidgets.NewQWidget(nil)
	detailsLayout := qtwidgets.NewQVBoxLayout(details)
	detailsLayout.SetContentsMargins(qtcore.QMargins{Left: 10, Top: 8, Right: 10, Bottom: 8})
	detailsLayout.AddWidget(t.details.AsQWidget())

	page := qtwidgets.NewQWidget(nil)
	layout := qtwidgets.NewQVBoxLayout(page)
	layout.SetContentsMargins(qtcore.QMargins{Left: 0, Top: 0, Right: 0, Bottom: 0})
	layout.SetSpacing(8)
	layout.AddWidgetFull(t.table.AsQWidget(), 1, 0)
	layout.AddWidget(newPanel(details))

	t.pages = qtwidgets.NewQStackedWidget(nil)
	t.pages.AddWidget(newHint("Where the time of a response went is shown here."))
	t.pages.AddWidget(plainPage(page))
	t.root = t.pages.AsQWidget()

	t.show(nil, 0)
	return t
}

// show shows the timing of a response which took the time given, or the
// hint if it is nil.
func (t *timingTab) show(timing *rest.Timing, total time.Duration) {
	if timing == nil {
		t.pages.SetCurrentIndex(timingPageHint)
		t.details.SetText("")
		return
	}
	t.pages.SetCurrentIndex(timingPageTable)

	// The bars are drawn on the time of the whole, which the last phase can
	// end after where the time was measured by someone else, as curl's is.
	scale := total
	for _, p := range timing.Phases {
		scale = max(scale, p.End)
	}
	at := func(d time.Duration) float64 {
		if scale <= 0 {
			return 0
		}
		return float64(d) / float64(scale)
	}

	for row, stage := range rest.Stages {
		p, ok := timing.Phase(stage)
		t.table.SetRowHidden(row, !ok)
		label := stageLabels[stage]
		if stage == rest.StageRedirects && timing.Redirects > 0 {
			label = fmt.Sprintf("%s (%d)", label, timing.Redirects)
		}
		t.table.Item(row, colTimingStage).SetText(label)
		t.table.Item(row, colTimingTime).SetText(formatTiming(p.Duration()))
		t.bars[row].set(at(p.Start), at(p.End))
	}
	last := len(rest.Stages)
	t.table.Item(last, colTimingTime).SetText(formatTiming(total))
	t.bars[last].set(0, at(total))

	t.timing = timing
	t.details.SetText(timingDetails(timing))
}

// recolour draws the bars and the details again, after the colours have
// changed.
func (t *timingTab) recolour() {
	if t.timing != nil && t.pages.CurrentIndex() == timingPageTable {
		t.details.SetText(timingDetails(t.timing))
	}
	for _, bar := range t.bars {
		bar.widget.Update()
	}
}

// timingDetails describes the connection a request was made on, with the
// labels fainter than the values, as in the bar beneath a response.
func timingDetails(timing *rest.Timing) string {
	connection := "New"
	if timing.Reused {
		connection = "Reused, so there was no lookup, connecting or handshake"
	}
	certificate := ""
	if timing.Unverified {
		certificate = "Not checked, as the network settings say for this host"
	}
	faded := fadedTextColour()
	var parts []string
	for _, f := range []field{
		{label: "Address", value: timing.Address},
		{label: "Proxy", value: timing.Proxy},
		{label: "Connection", value: connection},
		{label: "TLS", value: timing.TLS},
		{label: "Certificate", value: certificate, failure: true},
	} {
		if f.value != "" {
			label, value := f.html(faded)
			parts = append(parts, label+" "+value)
		}
	}
	return strings.Join(parts, "&nbsp;&nbsp;&nbsp;&nbsp; ")
}

// formatTiming gives a duration to as many places as tell short stages
// apart, which formatDuration's whole milliseconds would not.
func formatTiming(d time.Duration) string {
	ms := float64(d) / float64(time.Millisecond)
	switch {
	case d >= time.Second:
		return formatDuration(d)
	case ms < 10:
		return strconv.FormatFloat(ms, 'f', 2, 64) + " ms"
	default:
		return strconv.FormatFloat(ms, 'f', 1, 64) + " ms"
	}
}

// timingBar draws when a stage was on the time of the whole request: a bar
// from where it started to where it ended, as fractions of the width, in
// the colour of the timing bars.
type timingBar struct {
	widget   *qtwidgets.QWidget
	from, to float64
}

func newTimingBar() *timingBar {
	b := &timingBar{widget: qtwidgets.NewQWidget(nil)}
	b.widget.OverridePaintEvent(func(super func(*qtgui.QPaintEvent), event *qtgui.QPaintEvent) {
		super(event)
		b.paint()
	})
	return b
}

// set has the bar drawn from one fraction of the width to another.
func (b *timingBar) set(from, to float64) {
	b.from, b.to = from, to
	b.widget.Update()
}

func (b *timingBar) paint() {
	// The bar is kept off the edges of its cell.
	const padding = 4
	width, height := b.widget.Width()-2*padding, b.widget.Height()
	if width <= 0 {
		return
	}
	// A stage too short to see is a line, so that it is seen.
	x := int(b.from * float64(width))
	w := max(1, int((b.to-b.from)*float64(width)))
	margin := height / 4

	painter := qtgui.NewQPainterOn(b.widget.AsQPaintDevice())
	defer painter.Delete()
	defer painter.End()
	colour := qcolour(themeColour(colourTiming))
	painter.FillRect(qtcore.QRect{X: padding + x, Y: margin, Width: min(w, width-x), Height: height - 2*margin}, colour)
}

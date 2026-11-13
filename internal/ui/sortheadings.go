package ui

import (
	"github.com/nomad-software/qute/qtcore"
	"github.com/nomad-software/qute/qtgui"
	"github.com/nomad-software/qute/qtwidgets"

	"cmp"
	"slices"
	"strconv"
	"strings"
)

// sortKey is one of the columns rows are shown sorted by.
type sortKey struct {
	column     int
	descending bool
}

// sortColumns are the columns rows can be sorted by.
var sortColumns = []int{colKey, colValue, colDescription}

// nextSort returns the sort after a heading is clicked. A click sorts by its
// column alone, ascending, then descending, then not at all. A click that
// adds (with Shift) sorts by its column after the others, ascending, then
// descending, then no longer by it.
func nextSort(keys []sortKey, column int, add bool) []sortKey {
	at := slices.IndexFunc(keys, func(k sortKey) bool { return k.column == column })
	if !add {
		switch {
		case len(keys) == 1 && at == 0 && !keys[0].descending:
			return []sortKey{{column, true}}
		case len(keys) == 1 && at == 0:
			return nil
		}
		return []sortKey{{column, false}}
	}
	keys = slices.Clone(keys)
	switch {
	case at < 0:
		return append(keys, sortKey{column, false})
	case !keys[at].descending:
		keys[at].descending = true
		return keys
	}
	return slices.Delete(keys, at, at+1)
}

// sortByHeadings has the pairs kept in the order they were added, and shown
// sorted by the key, value or description as the user chooses with the
// headings (nextSort). Each heading sorted by is marked (paintSortMark).
func (t *keyValueTable) sortByHeadings() {
	t.sortable = true

	// The header is one of the table's own making, as the modifiers of a
	// click can only be had from its events, and its sections are painted
	// with their marks.
	header := &sortHeader{table: t}
	qtwidgets.NewQHeaderViewWith(header, qtcore.Horizontal, nil)
	header.SetSectionsClickable(true)
	header.SetHighlightSections(true)
	header.OnSectionClicked(func(column int) {
		if !slices.Contains(sortColumns, column) {
			return
		}
		t.setSort(nextSort(t.sortKeys, column, t.adding))
		if t.onSort != nil {
			t.onSort(slices.Clone(t.sortKeys))
		}
	})
	// The table takes the header, and deletes the one it had.
	t.table.SetHorizontalHeader(header)
	t.sizeColumns()
}

// sortHeader is the header of a table sorted by its headings, which reads
// the modifiers of a click and paints the marks of the columns sorted by.
type sortHeader struct {
	qtwidgets.QHeaderViewBase
	table *keyValueTable
}

// MouseReleaseEvent notes whether Shift is held as a heading is clicked,
// which adds its column to those sorted by.
func (h *sortHeader) MouseReleaseEvent(event *qtgui.QMouseEvent) {
	h.table.adding = event.Modifiers()&qtcore.ShiftModifier != 0
	h.Super().MouseReleaseEvent(event)
	h.table.adding = false
}

// PaintSection paints a heading with its mark.
func (h *sortHeader) PaintSection(painter *qtgui.QPainter, rect qtcore.QRect, column int) {
	h.Super().PaintSection(painter, rect, column)
	h.table.paintSortMark(h.QHeaderView, painter, rect, column)
}

// setSort shows the rows sorted by the columns given, or in the order they
// are kept in for none, without calling onSort.
func (t *keyValueTable) setSort(keys []sortKey) {
	t.sortKeys = keys
	t.table.HorizontalHeader().Viewport().Update()
	t.sort()
}

// compareSorted gives the order rows are shown in when they are sorted by
// the headings: by each column chosen in turn, and then by their places.
func (t *keyValueTable) compareSorted(a describedPair, aPlace int, b describedPair, bPlace int) int {
	for _, k := range t.sortKeys {
		c := compareText(a.column(k.column), b.column(k.column))
		if k.descending {
			c = -c
		}
		if c != 0 {
			return c
		}
	}
	return cmp.Compare(aPlace, bPlace)
}

// column returns what a pair has in a column of text.
func (p describedPair) column(column int) string {
	switch column {
	case colKey:
		return p.Key
	case colValue:
		return p.Value
	case colDescription:
		return p.Description
	}
	return ""
}

// compareText orders text alphabetically, ignoring case, so that what
// differs only in case is left to the next column sorted by.
func compareText(a, b string) int {
	return strings.Compare(strings.ToLower(a), strings.ToLower(b))
}

// box is a rectangle, in pixels that need not be whole.
type box struct{ x, y, w, h float64 }

// sortMarkLayout places the mark of a heading in its section: a triangle size
// wide and three fifths of that tall, and after it, half its width away, the
// number, numberWidth wide (none for 0), ending margin from the right of the
// section, both centred on its height. The number's box is as tall as the
// section, for it to be drawn centred in.
func sortMarkLayout(section box, size, numberWidth, margin float64) (triangle, number box) {
	right := section.x + section.w - margin
	if numberWidth > 0 {
		number = box{right - numberWidth, section.y, numberWidth, section.h}
		right = number.x - size/2
	}
	height := size * 3 / 5
	triangle = box{right - size, section.y + (section.h-height)/2, size, height}
	return triangle, number
}

// paintSortMark marks the heading of a column the rows are sorted by, in the
// colour of its text at half opacity: a triangle pointing up for ascending
// or down for descending, and its place among the columns when there are
// several, at the right of the heading.
func (t *keyValueTable) paintSortMark(header *qtwidgets.QHeaderView, painter *qtgui.QPainter, rect qtcore.QRect, column int) {
	at := slices.IndexFunc(t.sortKeys, func(k sortKey) bool { return k.column == column })
	if at < 0 {
		return
	}
	key := t.sortKeys[at]

	// The number is a little smaller than the heading.
	font := header.Font()
	font.SetPointSizeF(font.PointSizeF() * 0.85)
	number, numberWidth := "", 0.0
	if len(t.sortKeys) > 1 {
		number = strconv.Itoa(at + 1)
		numberWidth = float64(qtgui.NewQFontMetricsWithFont(font).HorizontalAdvance(number))
	}
	size := float64(header.FontMetrics().Height()) / 2
	margin := float64(header.Style().PixelMetricFull(qtwidgets.QStylePM_HeaderMargin, nil, header.AsQWidget()))
	section := box{float64(rect.X), float64(rect.Y), float64(rect.Width), float64(rect.Height)}
	triangle, place := sortMarkLayout(section, size, numberWidth, margin)

	painter.Save()
	defer painter.Restore()
	painter.SetRenderHint(qtgui.QPainterAntialiasing)
	painter.SetOpacity(0.5)
	colour := header.Palette().Color(qtgui.QPaletteButtonText)

	path := qtgui.NewQPainterPath()
	defer path.Delete()
	top, bottom := triangle.y, triangle.y+triangle.h
	if key.descending {
		top, bottom = bottom, top
	}
	path.MoveTo(qtcore.QPointF{X: triangle.x, Y: bottom})
	path.LineTo(qtcore.QPointF{X: triangle.x + triangle.w, Y: bottom})
	path.LineTo(qtcore.QPointF{X: triangle.x + triangle.w/2, Y: top})
	path.CloseSubpath()
	brush := qtgui.NewQBrushWithColor(colour)
	defer brush.Delete()
	painter.FillPath(path, brush)

	if number != "" {
		painter.SetFont(font)
		painter.SetPenColor(colour)
		area := qtcore.QRectF{X: place.x, Y: place.y, Width: place.w, Height: place.h}
		painter.DrawTextInF(area, int(qtcore.AlignLeft|qtcore.AlignVCenter), number)
	}
}

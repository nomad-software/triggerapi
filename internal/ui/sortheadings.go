package ui

import (
	"cmp"
	"slices"
	"strconv"
	"strings"

	qt "github.com/mappu/miqt/qt6"
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
	header := qt.NewQHeaderView(qt.Horizontal)
	header.SetSectionsClickable(true)
	header.SetHighlightSections(true)
	header.OnMouseReleaseEvent(func(super func(*qt.QMouseEvent), event *qt.QMouseEvent) {
		t.adding = event.Modifiers()&qt.ShiftModifier != 0
		super(event)
		t.adding = false
	})
	header.OnSectionClicked(func(column int) {
		if !slices.Contains(sortColumns, column) {
			return
		}
		t.setSort(nextSort(t.sortKeys, column, t.adding))
		if t.onSort != nil {
			t.onSort(slices.Clone(t.sortKeys))
		}
	})
	header.OnPaintSection(func(super func(*qt.QPainter, *qt.QRect, int), painter *qt.QPainter, rect *qt.QRect, column int) {
		super(painter, rect, column)
		t.paintSortMark(header, painter, rect, column)
	})
	// The table takes the header, and deletes the one it had.
	t.table.SetHorizontalHeader(header)
	t.sizeColumns()
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
func (t *keyValueTable) paintSortMark(header *qt.QHeaderView, painter *qt.QPainter, rect *qt.QRect, column int) {
	at := slices.IndexFunc(t.sortKeys, func(k sortKey) bool { return k.column == column })
	if at < 0 {
		return
	}
	key := t.sortKeys[at]

	// The number is a little smaller than the heading.
	font := qt.NewQFont5(header.Font())
	defer font.Delete()
	font.SetPointSizeF(font.PointSizeF() * 0.85)
	number, numberWidth := "", 0.0
	if len(t.sortKeys) > 1 {
		number = strconv.Itoa(at + 1)
		metrics := qt.NewQFontMetrics(font)
		numberWidth = float64(metrics.HorizontalAdvance(number))
		metrics.Delete()
	}
	size := float64(header.FontMetrics().Height()) / 2
	margin := float64(header.Style().PixelMetric(qt.QStyle__PM_HeaderMargin, nil, header.QWidget))
	section := box{float64(rect.X()), float64(rect.Y()), float64(rect.Width()), float64(rect.Height())}
	triangle, place := sortMarkLayout(section, size, numberWidth, margin)

	painter.Save()
	defer painter.Restore()
	painter.SetRenderHint(qt.QPainter__Antialiasing)
	painter.SetOpacity(0.5)
	colour := header.Palette().ColorWithCr(qt.QPalette__ButtonText)

	path := qt.NewQPainterPath()
	defer path.Delete()
	top, bottom := triangle.y, triangle.y+triangle.h
	if key.descending {
		top, bottom = bottom, top
	}
	path.MoveTo2(triangle.x, bottom)
	path.LineTo2(triangle.x+triangle.w, bottom)
	path.LineTo2(triangle.x+triangle.w/2, top)
	path.CloseSubpath()
	brush := qt.NewQBrush3(colour)
	defer brush.Delete()
	painter.FillPath(path, brush)

	if number != "" {
		painter.SetFont(font)
		painter.SetPen(colour)
		area := qt.NewQRectF4(place.x, place.y, place.w, place.h)
		defer area.Delete()
		painter.DrawText5(area, int(qt.AlignLeft|qt.AlignVCenter), number)
	}
}

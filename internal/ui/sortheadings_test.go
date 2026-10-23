package ui

import (
	"math"
	"slices"
	"testing"

	qt "github.com/mappu/miqt/qt6"
	"github.com/mappu/miqt/qt6/mainthread"
)

// A click on a heading sorts by its column alone, and one with Shift adds it
// to the columns sorted by; each goes ascending, descending, then off.
func TestNextSort(t *testing.T) {
	var keys []sortKey
	for _, step := range []struct {
		column int
		add    bool
		want   []sortKey
	}{
		{colKey, false, []sortKey{{colKey, false}}},
		{colKey, false, []sortKey{{colKey, true}}},
		{colKey, false, nil},
		{colKey, true, []sortKey{{colKey, false}}},
		{colValue, true, []sortKey{{colKey, false}, {colValue, false}}},
		{colDescription, true, []sortKey{{colKey, false}, {colValue, false}, {colDescription, false}}},
		{colValue, true, []sortKey{{colKey, false}, {colValue, true}, {colDescription, false}}},
		{colValue, true, []sortKey{{colKey, false}, {colDescription, false}}},
		{colKey, true, []sortKey{{colKey, true}, {colDescription, false}}},
		// Without Shift, a column already among several starts again alone.
		{colDescription, false, []sortKey{{colDescription, false}}},
	} {
		before := slices.Clone(keys)
		next := nextSort(keys, step.column, step.add)
		if !slices.Equal(next, step.want) {
			t.Errorf("after %v with %v: %v, want %v", before, step, next, step.want)
		}
		// What it was given is left as it was.
		if !slices.Equal(keys, before) {
			t.Errorf("%v was changed to %v", before, keys)
		}
		keys = next
	}
}

// The mark of a heading ends at the margin of its right edge, with the
// number last, and is centred on the height of the heading.
func TestSortMarkLayout(t *testing.T) {
	section := box{x: 100, y: 0, w: 200, h: 30}

	triangle, number := sortMarkLayout(section, 10, 0, 4)
	if triangle.x+triangle.w != 296 || triangle.w != 10 || triangle.h != 6 {
		t.Errorf("triangle = %+v", triangle)
	}
	if mid := triangle.y + triangle.h/2; mid != 15 {
		t.Errorf("the triangle is centred at %v of 30", mid)
	}
	if number != (box{}) {
		t.Errorf("number = %+v", number)
	}

	triangle, number = sortMarkLayout(section, 10, 8, 4)
	if number.x+number.w != 296 || number.y != 0 || number.h != 30 {
		t.Errorf("number = %+v", number)
	}
	if triangle.x+triangle.w != number.x-5 || triangle.y+triangle.h/2 != 15 {
		t.Errorf("triangle = %+v, number %+v", triangle, number)
	}
}

// The mark is painted in the colour of the heading's text at half opacity.
func TestSortMarkPainted(t *testing.T) {
	mainthread.Wait(func() {
		table := newKeyValueTable("Key", "Value")
		defer table.table.Delete()
		table.sortByHeadings()
		table.table.Resize(600, 200)
		table.table.Show()
		defer table.table.Hide()
		header := table.table.HorizontalHeader()

		pixel := func(image *qt.QImage, x, y float64) *qt.QColor {
			return image.PixelColor(int(math.Round(x)), int(math.Round(y)))
		}
		grab := func() *qt.QImage { return header.Grab().ToImage() }

		size := float64(header.FontMetrics().Height()) / 2
		margin := float64(header.Style().PixelMetric(qt.QStyle__PM_HeaderMargin, nil, header.QWidget))
		section := box{float64(header.SectionViewportPosition(colKey)), 0, float64(header.SectionSize(colKey)), float64(header.Height())}
		triangle, _ := sortMarkLayout(section, size, 0, margin)

		// The middle of the bottom of the triangle, with and without it.
		x, y := triangle.x+triangle.w/2, triangle.y+triangle.h*0.8
		background := pixel(grab(), x, y)
		table.setSort([]sortKey{{colKey, false}})
		mark := pixel(grab(), x, y)
		text := header.Palette().ColorWithCr(qt.QPalette__ButtonText)
		for _, c := range []struct {
			name                   string
			mark, background, text int
		}{
			{"red", mark.Red(), background.Red(), text.Red()},
			{"green", mark.Green(), background.Green(), text.Green()},
			{"blue", mark.Blue(), background.Blue(), text.Blue()},
		} {
			if want := (c.background + c.text) / 2; c.mark < want-12 || c.mark > want+12 {
				t.Errorf("%s of the mark = %d, want about %d (text %d on %d)", c.name, c.mark, want, c.text, c.background)
			}
		}

		// Unsorted, the heading has no mark.
		table.setSort(nil)
		if got := pixel(grab(), x, y); got.Rgb() != background.Rgb() {
			t.Errorf("a mark is left: %v", got.Name())
		}
	})
}

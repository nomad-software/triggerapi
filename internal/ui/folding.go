package ui

import (
	"maps"

	qt "github.com/mappu/miqt/qt6"
	"github.com/mappu/miqt/qt6/mainthread"
)

// gutterWidth is the width of the margin the fold markers are drawn in.
const gutterWidth = 14

// folder adds folding to an editor that holds JSON. A marker is drawn in a
// margin at the left of each line that opens an object or array which closes
// on a later line. Clicking it hides the lines in between, and clicking it
// again shows them.
type folder struct {
	edit   *qt.QPlainTextEdit
	gutter *qt.QWidget
	marker *qt.QBrush

	// enabled reports if the editor holds JSON at the moment.
	enabled func() bool

	// text is the text the regions were found in.
	text string
	// regions maps the line each foldable object or array opens on to the
	// line it closes on.
	regions map[int]int
	// folded holds the regions that are folded, in the same form.
	folded map[int]int
	// hidden records that some lines are hidden.
	hidden bool
	// pending records that a check for changed text is on its way.
	pending bool
}

func newFolder(edit *qt.QPlainTextEdit, enabled func() bool) *folder {
	f := &folder{
		edit:    edit,
		gutter:  qt.NewQWidget(edit.QWidget),
		marker:  qt.NewQBrush3(qt.NewQColor6("#888888")),
		enabled: enabled,
		folded:  map[int]int{},
	}

	edit.SetViewportMargins(gutterWidth, 0, 0, 0)
	edit.OnResizeEvent(func(super func(*qt.QResizeEvent), event *qt.QResizeEvent) {
		super(event)
		f.place()
	})
	f.place()

	// The markers move with the text as it scrolls.
	edit.OnUpdateRequest(func(*qt.QRect, int) { f.gutter.Update() })

	// The text is looked at once the change has been made, as the lines must
	// not be hidden or shown while the document is in the middle of it.
	edit.Document().OnContentsChange(func(int, int, int) {
		if !f.pending {
			f.pending = true
			mainthread.Start(func() {
				f.pending = false
				f.sync()
			})
		}
	})

	f.gutter.SetCursor(qt.NewQCursor2(qt.PointingHandCursor))
	f.gutter.OnPaintEvent(func(super func(*qt.QPaintEvent), event *qt.QPaintEvent) {
		super(event)
		f.paint()
	})
	f.gutter.OnMousePressEvent(func(_ func(*qt.QMouseEvent), event *qt.QMouseEvent) {
		if line, ok := f.lineAt(event.Pos().Y()); ok {
			f.toggle(line)
		}
	})

	return f
}

// place puts the margin at the left edge of the editor.
func (f *folder) place() {
	area := f.edit.ContentsRect()
	f.gutter.SetGeometry(area.Left(), area.Top(), gutterWidth, area.Height())
}

// reset unfolds everything, for when the editor has been given other text or
// what it holds is no longer JSON.
func (f *folder) reset() {
	clear(f.folded)
	f.text = f.edit.ToPlainText()
	f.find()
	f.apply()
}

// sync looks for a change to the text made by the user. A fold is kept only
// if its object or array is where it was.
func (f *folder) sync() {
	text := f.edit.ToPlainText()
	if text == f.text {
		return
	}
	f.text = text
	f.find()

	maps.DeleteFunc(f.folded, func(start, end int) bool { return f.regions[start] != end })
	f.apply()
}

// find works out what can be folded in the text.
func (f *folder) find() {
	f.regions = nil
	if f.enabled() {
		f.regions = foldRegions(f.text)
	}
}

// foldable reports if a marker is shown for a line. An object or array that
// closes on the line after it opens has nothing in between to hide.
func (f *folder) foldable(line int) bool {
	end, ok := f.regions[line]
	return ok && end > line+1
}

// toggle folds or unfolds the object or array that opens on a line.
func (f *folder) toggle(line int) {
	if !f.foldable(line) {
		return
	}

	if _, ok := f.folded[line]; ok {
		delete(f.folded, line)
	} else {
		f.folded[line] = f.regions[line]
	}
	f.apply()
}

// reveal unfolds whatever a line is folded away in.
func (f *folder) reveal(line int) {
	before := len(f.folded)
	maps.DeleteFunc(f.folded, func(start, end int) bool { return start < line && line < end })
	if len(f.folded) != before {
		f.apply()
	}
}

// apply hides the lines inside the folded regions and shows the rest.
func (f *folder) apply() {
	defer f.gutter.Update()

	if len(f.folded) == 0 && !f.hidden {
		return
	}

	doc := f.edit.Document()
	hide := make([]bool, doc.BlockCount())
	for start, end := range f.folded {
		for line := start + 1; line < end && line < len(hide); line++ {
			hide[line] = true
		}
	}

	f.hidden = false
	changed := false
	for block := doc.FirstBlock(); block.IsValid(); block = block.Next() {
		line := block.BlockNumber()
		visible := line >= len(hide) || !hide[line]
		f.hidden = f.hidden || !visible

		if block.IsVisible() != visible {
			block.SetVisible(visible)
			changed = true
		}
	}

	if !changed {
		return
	}

	// The text cursor must not be left on a line that cannot be seen.
	if cursor := f.edit.TextCursor(); cursor.BlockNumber() < len(hide) && hide[cursor.BlockNumber()] {
		line := cursor.BlockNumber()
		for line > 0 && hide[line] {
			line--
		}
		cursor.SetPosition(doc.FindBlockByNumber(line).Position())
		f.edit.SetTextCursor(cursor)
	}

	// Lay the text out again now that lines have come or gone.
	doc.MarkContentsDirty(0, doc.CharacterCount())
	f.edit.Viewport().Update()
}

// visibleLines calls each for the lines in view, with the position of their
// top edge in the margin and their height, until it returns false.
func (f *folder) visibleLines(each func(line, top, height int) bool) {
	origin := qt.NewQPoint2(0, 0)
	defer origin.Delete()

	height := f.gutter.Height()
	for block := f.edit.CursorForPosition(origin).Block(); block.IsValid(); block = block.Next() {
		if !block.IsVisible() {
			continue
		}

		cursor := qt.NewQTextCursor4(block)
		rect := f.edit.CursorRect(cursor)
		cursor.Delete()

		if rect.Top() > height || !each(block.BlockNumber(), rect.Top(), rect.Height()) {
			return
		}
	}
}

// lineAt returns the line at a vertical position in the margin.
func (f *folder) lineAt(y int) (int, bool) {
	found, ok := 0, false
	f.visibleLines(func(line, top, height int) bool {
		if y >= top && y < top+height {
			found, ok = line, true
		}
		return !ok
	})
	return found, ok
}

// paint draws a marker beside each line that can be folded: a triangle that
// points down if the lines beneath are showing and right if they are hidden.
func (f *folder) paint() {
	if len(f.regions) == 0 {
		return
	}

	painter := qt.NewQPainter2(f.gutter.QPaintDevice)
	defer painter.Delete()
	defer painter.End()
	painter.SetRenderHint(qt.QPainter__Antialiasing)

	f.visibleLines(func(line, top, height int) bool {
		if !f.foldable(line) {
			return true
		}

		// The triangle is drawn about the middle of the line.
		x, y := float64(gutterWidth)/2, float64(top)+float64(height)/2
		path := qt.NewQPainterPath()
		defer path.Delete()

		if _, folded := f.folded[line]; folded {
			path.MoveTo2(x-2, y-4)
			path.LineTo2(x+3, y)
			path.LineTo2(x-2, y+4)
		} else {
			path.MoveTo2(x-4, y-2)
			path.LineTo2(x+4, y-2)
			path.LineTo2(x, y+3)
		}
		path.CloseSubpath()
		painter.FillPath(path, f.marker)

		return true
	})
}

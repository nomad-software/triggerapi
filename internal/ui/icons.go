package ui

import (
	"github.com/nomad-software/qute/qtcore"
	"github.com/nomad-software/qute/qtgui"

	"math"

	"github.com/nomad-software/triggerapi/internal/appicon"
)

// iconSize is the size request type icons are shown at. They are drawn at
// twice that so they stay sharp on high density displays.
const iconSize = 64

// glyphSize is the size of the square that the symbol of an icon is drawn
// on, whatever size the icon itself is.
const glyphSize = 128

// drawIcon returns an icon of a symbol on a rounded square of a colour, size
// pixels wide. The glyph draws the symbol in white on a square of glyphSize.
// Icons are drawn like this rather than loaded from files so that the program
// needs nothing beside its binary.
func drawIcon(size int, colour string, glyph func(*qtgui.QPainter)) *qtgui.QIcon {
	pixmap := drawPixmap(size, colour, glyph)
	// The icon keeps a copy of the picture.
	defer pixmap.Delete()
	icon := qtgui.NewQIconWithPixmap(pixmap)
	// The icon of a selected item is otherwise tinted with the colour of
	// the selection.
	icon.AddPixmapFull(pixmap, qtgui.QIconSelected, qtgui.QIconOff)
	return icon
}

// drawPixmap draws the picture of an icon. See drawIcon.
func drawPixmap(size int, colour string, glyph func(*qtgui.QPainter)) *qtgui.QPixmap {
	pixmap := qtgui.NewQPixmapWithW(size, size)
	transparent := qtgui.QColor{}
	pixmap.FillFull(transparent)

	// The painter keeps a copy of the brush.
	background := qcolour(colour)
	brush := qtgui.NewQBrushWithColor(background)
	defer brush.Delete()

	painter := qtgui.NewQPainterOn(pixmap.AsQPaintDevice())
	painter.SetRenderHint(qtgui.QPainterAntialiasing)
	painter.Scale(float64(size)/glyphSize, float64(size)/glyphSize)
	painter.SetPenStyle(qtcore.NoPen)
	painter.SetBrush(brush)
	painter.DrawRoundedRect(qtcore.QRect{X: 4, Y: 4, Width: glyphSize - 8, Height: glyphSize - 8}, 24, 24)
	glyph(painter)
	// Painting must be finished before the pixmap is used.
	painter.End()
	painter.Delete()

	return pixmap
}

// drawTabIcon returns an icon for a tab at the left of a tab widget. Tabs
// there are drawn turned a quarter turn anticlockwise, icon and all, so the
// picture is turned the other way beforehand to end up upright.
func drawTabIcon(colour string, glyph func(*qtgui.QPainter)) *qtgui.QIcon {
	pixmap := drawPixmap(iconSize*2, colour, glyph)
	defer pixmap.Delete()
	turn := qtgui.NewQTransform()
	defer turn.Delete()
	turn.Rotate(90)
	// The turned picture is freed by the garbage collector, and the icon
	// keeps a copy of it.
	return qtgui.NewQIconWithPixmap(pixmap.TransformedFull(turn, qtcore.SmoothTransformation))
}

// drawGlobe draws a globe, for the environments requests are sent to.
func drawGlobe(painter *qtgui.QPainter) {
	painter.SetPen(whitePen())
	painter.SetBrushStyle(qtcore.NoBrush)

	// The outline, a meridian and the equator.
	painter.DrawEllipse(qtcore.QRect{X: 28, Y: 28, Width: 72, Height: 72})
	painter.DrawEllipse(qtcore.QRect{X: 49, Y: 28, Width: 30, Height: 72})
	painter.DrawLine(28, 64, 100, 64)
}

// drawRequests draws two arrows going opposite ways, a request going out and
// its response coming back, for requests and the folders they are kept in.
func drawRequests(painter *qtgui.QPainter) {
	outline(painter)
	path := qtgui.NewQPainterPath()
	exchange(path, 28, 100, 46, 82, 14)
	painter.DrawPath(path)
}

// exchange adds to a path an arrow from left to right at the height top and
// one from right to left beneath it at bottom, with heads of the size given.
func exchange(path *qtgui.QPainterPath, left, right, top, bottom, head float64) {
	path.MoveTo(qtcore.QPointF{X: left, Y: top})
	path.LineTo(qtcore.QPointF{X: right, Y: top})
	path.MoveTo(qtcore.QPointF{X: right - head, Y: top - head})
	path.LineTo(qtcore.QPointF{X: right, Y: top})
	path.LineTo(qtcore.QPointF{X: right - head, Y: top + head})

	path.MoveTo(qtcore.QPointF{X: right, Y: bottom})
	path.LineTo(qtcore.QPointF{X: left, Y: bottom})
	path.MoveTo(qtcore.QPointF{X: left + head, Y: bottom - head})
	path.LineTo(qtcore.QPointF{X: left, Y: bottom})
	path.LineTo(qtcore.QPointF{X: left + head, Y: bottom + head})
}

// drawDocument draws a page with a folded corner and lines of text, for a
// document that describes an API.
func drawDocument(painter *qtgui.QPainter) {
	painter.SetPen(whitePen())
	painter.SetBrushStyle(qtcore.NoBrush)

	path := qtgui.NewQPainterPath()
	// The page, and the fold at its top right corner.
	path.MoveTo(qtcore.QPointF{X: 38, Y: 28})
	path.LineTo(qtcore.QPointF{X: 72, Y: 28})
	path.LineTo(qtcore.QPointF{X: 90, Y: 46})
	path.LineTo(qtcore.QPointF{X: 90, Y: 100})
	path.LineTo(qtcore.QPointF{X: 38, Y: 100})
	path.CloseSubpath()
	path.MoveTo(qtcore.QPointF{X: 72, Y: 28})
	path.LineTo(qtcore.QPointF{X: 72, Y: 46})
	path.LineTo(qtcore.QPointF{X: 90, Y: 46})
	// The lines of text.
	path.MoveTo(qtcore.QPointF{X: 51, Y: 64})
	path.LineTo(qtcore.QPointF{X: 77, Y: 64})
	path.MoveTo(qtcore.QPointF{X: 51, Y: 82})
	path.LineTo(qtcore.QPointF{X: 77, Y: 82})
	painter.DrawPath(path)
}

// drawPlane draws a paper plane, for something that sends requests.
func drawPlane(painter *qtgui.QPainter) {
	painter.SetPen(whitePen())
	painter.SetBrushStyle(qtcore.NoBrush)

	path := qtgui.NewQPainterPath()
	// The outline of the plane, and the crease along its middle.
	path.MoveTo(qtcore.QPointF{X: 26, Y: 62})
	path.LineTo(qtcore.QPointF{X: 102, Y: 30})
	path.LineTo(qtcore.QPointF{X: 78, Y: 100})
	path.LineTo(qtcore.QPointF{X: 60, Y: 74})
	path.CloseSubpath()
	path.MoveTo(qtcore.QPointF{X: 60, Y: 74})
	path.LineTo(qtcore.QPointF{X: 102, Y: 30})
	painter.DrawPath(path)
}

// drawPrompt draws the prompt of a terminal, for a command to run in one.
func drawPrompt(painter *qtgui.QPainter) {
	painter.SetPen(whitePen())
	painter.SetBrushStyle(qtcore.NoBrush)

	path := qtgui.NewQPainterPath()
	// The chevron, and the cursor after it.
	path.MoveTo(qtcore.QPointF{X: 30, Y: 42})
	path.LineTo(qtcore.QPointF{X: 56, Y: 64})
	path.LineTo(qtcore.QPointF{X: 30, Y: 86})
	path.MoveTo(qtcore.QPointF{X: 66, Y: 88})
	path.LineTo(qtcore.QPointF{X: 98, Y: 88})
	painter.DrawPath(path)
}

// drawEye draws an eye, for looking at how something will turn out.
func drawEye(painter *qtgui.QPainter) {
	painter.SetPen(whitePen())
	painter.SetBrushStyle(qtcore.NoBrush)

	path := qtgui.NewQPainterPath()
	// The lids, from corner to corner.
	path.MoveTo(qtcore.QPointF{X: 24, Y: 64})
	path.QuadTo(qtcore.QPointF{X: 64, Y: 20}, qtcore.QPointF{X: 104, Y: 64})
	path.QuadTo(qtcore.QPointF{X: 64, Y: 108}, qtcore.QPointF{X: 24, Y: 64})
	painter.DrawPath(path)

	// The pupil.
	painter.SetBrush(qtgui.NewQBrushWithColor(qcolour("#ffffff")))
	painter.DrawEllipse(qtcore.QRect{X: 54, Y: 54, Width: 20, Height: 20})
}

// drawMagnifier draws a magnifying glass, for finding something.
func drawMagnifier(painter *qtgui.QPainter) {
	painter.SetPen(whitePen())
	painter.SetBrushStyle(qtcore.NoBrush)

	painter.DrawEllipse(qtcore.QRect{X: 28, Y: 28, Width: 50, Height: 50})

	// The handle.
	path := qtgui.NewQPainterPath()
	path.MoveTo(qtcore.QPointF{X: 72, Y: 72})
	path.LineTo(qtcore.QPointF{X: 100, Y: 100})
	painter.DrawPath(path)
}

// drawTick draws a tick, for something that went well.
func drawTick(painter *qtgui.QPainter) {
	pen := whitePen()
	pen.SetWidth(14)
	painter.SetPen(pen)
	painter.SetBrushStyle(qtcore.NoBrush)

	path := qtgui.NewQPainterPath()
	path.MoveTo(qtcore.QPointF{X: 32, Y: 68})
	path.LineTo(qtcore.QPointF{X: 54, Y: 90})
	path.LineTo(qtcore.QPointF{X: 98, Y: 40})
	painter.DrawPath(path)
}

// drawCross draws a cross, for something that did not.
func drawCross(painter *qtgui.QPainter) {
	pen := whitePen()
	pen.SetWidth(14)
	painter.SetPen(pen)
	painter.SetBrushStyle(qtcore.NoBrush)

	path := qtgui.NewQPainterPath()
	path.MoveTo(qtcore.QPointF{X: 38, Y: 38})
	path.LineTo(qtcore.QPointF{X: 90, Y: 90})
	path.MoveTo(qtcore.QPointF{X: 90, Y: 38})
	path.LineTo(qtcore.QPointF{X: 38, Y: 90})
	painter.DrawPath(path)
}

// drawDisk draws a floppy disk, for saving something to a file.
func drawDisk(painter *qtgui.QPainter) {
	painter.SetPen(whitePen())
	painter.SetBrushStyle(qtcore.NoBrush)

	path := qtgui.NewQPainterPath()
	// The disk, with its top right corner cut off.
	path.MoveTo(qtcore.QPointF{X: 30, Y: 30})
	path.LineTo(qtcore.QPointF{X: 84, Y: 30})
	path.LineTo(qtcore.QPointF{X: 98, Y: 44})
	path.LineTo(qtcore.QPointF{X: 98, Y: 98})
	path.LineTo(qtcore.QPointF{X: 30, Y: 98})
	path.CloseSubpath()
	// The shutter at the top, and the label at the bottom.
	path.MoveTo(qtcore.QPointF{X: 46, Y: 30})
	path.LineTo(qtcore.QPointF{X: 46, Y: 50})
	path.LineTo(qtcore.QPointF{X: 74, Y: 50})
	path.LineTo(qtcore.QPointF{X: 74, Y: 30})
	path.MoveTo(qtcore.QPointF{X: 46, Y: 98})
	path.LineTo(qtcore.QPointF{X: 46, Y: 72})
	path.LineTo(qtcore.QPointF{X: 82, Y: 72})
	path.LineTo(qtcore.QPointF{X: 82, Y: 98})
	painter.DrawPath(path)
}

// outline sets a painter up to draw the lines of a symbol.
func outline(painter *qtgui.QPainter) {
	painter.SetPen(whitePen())
	painter.SetBrushStyle(qtcore.NoBrush)
}

// plus adds a plus sign to a path, centred on a point.
func plus(path *qtgui.QPainterPath, x, y, arm float64) {
	path.MoveTo(qtcore.QPointF{X: x - arm, Y: y})
	path.LineTo(qtcore.QPointF{X: x + arm, Y: y})
	path.MoveTo(qtcore.QPointF{X: x, Y: y - arm})
	path.LineTo(qtcore.QPointF{X: x, Y: y + arm})
}

// drawPlus draws a plus sign, for adding something.
func drawPlus(painter *qtgui.QPainter) {
	outline(painter)
	path := qtgui.NewQPainterPath()
	plus(path, 64, 64, 30)
	painter.DrawPath(path)
}

// checklist adds rows of a checklist to a path, each a tick followed by a
// line, with the first at top and the others gap below one another.
func checklist(path *qtgui.QPainterPath, top, gap float64, rows int) {
	for i := range rows {
		y := top + gap*float64(i)
		path.MoveTo(qtcore.QPointF{X: 30, Y: y})
		path.LineTo(qtcore.QPointF{X: 37, Y: y + 7})
		path.LineTo(qtcore.QPointF{X: 49, Y: y - 7})
		path.MoveTo(qtcore.QPointF{X: 60, Y: y})
		path.LineTo(qtcore.QPointF{X: 98, Y: y})
	}
}

// drawSets draws a checklist, for a set: a set of requests worked on
// together.
func drawSets(painter *qtgui.QPainter) {
	outline(painter)
	path := qtgui.NewQPainterPath()
	checklist(path, 40, 24, 3)
	painter.DrawPath(path)
}

// setIcon returns the icon of a set, in the colour of the sets.
func setIcon() *qtgui.QIcon {
	return cachedIcon("set", func() *qtgui.QIcon { return drawIcon(iconSize*2, themeColour(colourSets), drawSets) })
}

// drawRequestsPlus draws the arrows of drawRequests with a plus sign beside
// them, for adding a folder.
func drawRequestsPlus(painter *qtgui.QPainter) {
	outline(painter)
	path := qtgui.NewQPainterPath()
	exchange(path, 24, 76, 44, 84, 12)
	plus(path, 96, 64, 10)
	painter.DrawPath(path)
}

// drawCopy draws one page in front of another, for making a copy of
// something.
func drawCopy(painter *qtgui.QPainter) {
	outline(painter)
	// The page in front.
	painter.DrawRoundedRect(qtcore.QRect{X: 46, Y: 44, Width: 54, Height: 58}, 6, 6)

	// What can be seen of the page behind it.
	path := qtgui.NewQPainterPath()
	path.MoveTo(qtcore.QPointF{X: 30, Y: 82})
	path.LineTo(qtcore.QPointF{X: 30, Y: 32})
	path.QuadTo(qtcore.QPointF{X: 30, Y: 26}, qtcore.QPointF{X: 36, Y: 26})
	path.LineTo(qtcore.QPointF{X: 82, Y: 26})
	painter.DrawPath(path)
}

// drawImport draws an arrow going into a page from outside it, pointing
// left, for bringing something in from a file.
func drawImport(painter *qtgui.QPainter) {
	outline(painter)
	path := qtgui.NewQPainterPath()
	// The page, open at the right where the arrow goes in, with the fold at
	// its top left corner.
	path.MoveTo(qtcore.QPointF{X: 76, Y: 44})
	path.LineTo(qtcore.QPointF{X: 76, Y: 28})
	path.LineTo(qtcore.QPointF{X: 46, Y: 28})
	path.LineTo(qtcore.QPointF{X: 28, Y: 46})
	path.LineTo(qtcore.QPointF{X: 28, Y: 100})
	path.LineTo(qtcore.QPointF{X: 76, Y: 100})
	path.LineTo(qtcore.QPointF{X: 76, Y: 84})
	path.MoveTo(qtcore.QPointF{X: 46, Y: 28})
	path.LineTo(qtcore.QPointF{X: 46, Y: 46})
	path.LineTo(qtcore.QPointF{X: 28, Y: 46})
	// The arrow.
	path.MoveTo(qtcore.QPointF{X: 104, Y: 64})
	path.LineTo(qtcore.QPointF{X: 52, Y: 64})
	path.MoveTo(qtcore.QPointF{X: 68, Y: 48})
	path.LineTo(qtcore.QPointF{X: 52, Y: 64})
	path.LineTo(qtcore.QPointF{X: 68, Y: 80})
	painter.DrawPath(path)
}

// drawPencil draws a pencil, for editing something.
func drawPencil(painter *qtgui.QPainter) {
	outline(painter)
	path := qtgui.NewQPainterPath()
	// The pencil, from its point at the bottom left.
	path.MoveTo(qtcore.QPointF{X: 30, Y: 98})
	path.LineTo(qtcore.QPointF{X: 36, Y: 74})
	path.LineTo(qtcore.QPointF{X: 80, Y: 30})
	path.LineTo(qtcore.QPointF{X: 98, Y: 48})
	path.LineTo(qtcore.QPointF{X: 54, Y: 92})
	path.CloseSubpath()
	// The band beneath the rubber.
	path.MoveTo(qtcore.QPointF{X: 68, Y: 42})
	path.LineTo(qtcore.QPointF{X: 86, Y: 60})
	painter.DrawPath(path)
}

// drawLetters draws a capital and a small letter with the cursor that text
// is typed at, for renaming something.
func drawLetters(painter *qtgui.QPainter) {
	font := qtgui.NewQFont()
	font.SetPixelSize(56)
	font.SetBold(true)

	painter.SetFont(font)
	painter.SetPen(whitePen())
	painter.DrawTextIn(qtcore.QRect{X: 14, Y: 0, Width: 80, Height: glyphSize - 6}, int(qtcore.AlignCenter), "Aa")

	path := qtgui.NewQPainterPath()
	path.MoveTo(qtcore.QPointF{X: 104, Y: 34})
	path.LineTo(qtcore.QPointF{X: 104, Y: 94})
	painter.DrawPath(path)
}

// drawBin draws a rubbish bin, for removing something.
func drawBin(painter *qtgui.QPainter) {
	outline(painter)
	path := qtgui.NewQPainterPath()
	// The lid and its handle.
	path.MoveTo(qtcore.QPointF{X: 30, Y: 42})
	path.LineTo(qtcore.QPointF{X: 98, Y: 42})
	path.MoveTo(qtcore.QPointF{X: 52, Y: 42})
	path.LineTo(qtcore.QPointF{X: 52, Y: 28})
	path.LineTo(qtcore.QPointF{X: 76, Y: 28})
	path.LineTo(qtcore.QPointF{X: 76, Y: 42})
	// The bin.
	path.MoveTo(qtcore.QPointF{X: 40, Y: 42})
	path.LineTo(qtcore.QPointF{X: 46, Y: 102})
	path.LineTo(qtcore.QPointF{X: 82, Y: 102})
	path.LineTo(qtcore.QPointF{X: 88, Y: 42})
	// The ribs down its side.
	path.MoveTo(qtcore.QPointF{X: 56, Y: 58})
	path.LineTo(qtcore.QPointF{X: 57, Y: 86})
	path.MoveTo(qtcore.QPointF{X: 72, Y: 58})
	path.LineTo(qtcore.QPointF{X: 71, Y: 86})
	painter.DrawPath(path)
}

// drawPin draws a drawing pin, for keeping something where it is.
func drawPin(painter *qtgui.QPainter) {
	pen := whitePen()
	pen.SetWidth(12)
	painter.SetPen(pen)
	painter.SetBrushStyle(qtcore.NoBrush)

	path := qtgui.NewQPainterPath()
	// The head, the body, the collar and the point.
	path.MoveTo(qtcore.QPointF{X: 44, Y: 26})
	path.LineTo(qtcore.QPointF{X: 84, Y: 26})
	path.MoveTo(qtcore.QPointF{X: 52, Y: 26})
	path.LineTo(qtcore.QPointF{X: 50, Y: 62})
	path.MoveTo(qtcore.QPointF{X: 76, Y: 26})
	path.LineTo(qtcore.QPointF{X: 78, Y: 62})
	path.MoveTo(qtcore.QPointF{X: 34, Y: 64})
	path.LineTo(qtcore.QPointF{X: 94, Y: 64})
	path.MoveTo(qtcore.QPointF{X: 64, Y: 64})
	path.LineTo(qtcore.QPointF{X: 64, Y: 104})
	painter.DrawPath(path)
}

// drawMonitor draws the monitor of a desk, on its stand, for a workspace.
func drawMonitor(painter *qtgui.QPainter) {
	outline(painter)
	painter.DrawRoundedRect(qtcore.QRect{X: 22, Y: 28, Width: 84, Height: 54}, 6, 6)

	path := qtgui.NewQPainterPath()
	// The stand and its foot.
	path.MoveTo(qtcore.QPointF{X: 64, Y: 82})
	path.LineTo(qtcore.QPointF{X: 64, Y: 100})
	path.MoveTo(qtcore.QPointF{X: 44, Y: 100})
	path.LineTo(qtcore.QPointF{X: 84, Y: 100})
	painter.DrawPath(path)
}

// drawClock draws a clock, for the history of the requests sent.
func drawClock(painter *qtgui.QPainter) {
	outline(painter)
	painter.DrawEllipse(qtcore.QRect{X: 26, Y: 26, Width: 76, Height: 76})

	path := qtgui.NewQPainterPath()
	defer path.Delete()
	// The hands, one up to twelve and one down to four.
	path.MoveTo(qtcore.QPointF{X: 64, Y: 40})
	path.LineTo(qtcore.QPointF{X: 64, Y: 64})
	path.LineTo(qtcore.QPointF{X: 82, Y: 74})
	painter.DrawPath(path)
}

// drawDiff draws two pages side by side, whose lines differ, for comparing
// two things.
func drawDiff(painter *qtgui.QPainter) {
	outline(painter)
	painter.DrawRoundedRect(qtcore.QRect{X: 22, Y: 28, Width: 36, Height: 72}, 5, 5)
	painter.DrawRoundedRect(qtcore.QRect{X: 70, Y: 28, Width: 36, Height: 72}, 5, 5)

	path := qtgui.NewQPainterPath()
	defer path.Delete()
	// The lines of each page, the same but for the middle one.
	for _, y := range []float64{46, 64, 82} {
		path.MoveTo(qtcore.QPointF{X: 32, Y: y})
		path.LineTo(qtcore.QPointF{X: 48, Y: y})
		path.MoveTo(qtcore.QPointF{X: 80, Y: y})
		if y == 64 {
			path.LineTo(qtcore.QPointF{X: 88, Y: y})
		} else {
			path.LineTo(qtcore.QPointF{X: 96, Y: y})
		}
	}
	painter.DrawPath(path)
}

// whitePen returns a pen for drawing the lines of a symbol.
func whitePen() *qtgui.QPen {
	pen := qtgui.NewQPenWithColor(qcolour("#ffffff"))
	pen.SetWidth(9)
	pen.SetCapStyle(qtcore.RoundCap)
	pen.SetJoinStyle(qtcore.RoundJoin)
	return pen
}

// drawBraces draws a pair of braces, as around a JSON document.
func drawBraces(painter *qtgui.QPainter) {
	font := qtgui.NewQFont()
	font.SetPixelSize(64)
	font.SetBold(true)

	painter.SetFont(font)
	painter.SetPen(whitePen())
	painter.DrawTextIn(qtcore.QRect{X: 0, Y: 0, Width: glyphSize, Height: glyphSize - 8}, int(qtcore.AlignCenter), "{ }")
}

// drawArrows draws two arrows pointing in opposite directions, for a call
// and its reply.
func drawArrows(painter *qtgui.QPainter) {
	painter.SetPen(whitePen())
	painter.SetBrushStyle(qtcore.NoBrush)

	path := qtgui.NewQPainterPath()
	// The upper arrow points right.
	path.MoveTo(qtcore.QPointF{X: 34, Y: 48})
	path.LineTo(qtcore.QPointF{X: 94, Y: 48})
	path.MoveTo(qtcore.QPointF{X: 78, Y: 32})
	path.LineTo(qtcore.QPointF{X: 94, Y: 48})
	path.LineTo(qtcore.QPointF{X: 78, Y: 64})
	// The lower arrow points left.
	path.MoveTo(qtcore.QPointF{X: 94, Y: 82})
	path.LineTo(qtcore.QPointF{X: 34, Y: 82})
	path.MoveTo(qtcore.QPointF{X: 50, Y: 66})
	path.LineTo(qtcore.QPointF{X: 34, Y: 82})
	path.LineTo(qtcore.QPointF{X: 50, Y: 98})

	painter.DrawPath(path)
}

// drawPulse draws the line of a pulse, for a WebSocket: a connection that
// stays open, with messages going both ways along it.
func drawPulse(painter *qtgui.QPainter) {
	painter.SetPen(whitePen())
	painter.SetBrushStyle(qtcore.NoBrush)

	path := qtgui.NewQPainterPath()
	path.MoveTo(qtcore.QPointF{X: 26, Y: 66})
	path.LineTo(qtcore.QPointF{X: 46, Y: 66})
	path.LineTo(qtcore.QPointF{X: 56, Y: 38})
	path.LineTo(qtcore.QPointF{X: 72, Y: 92})
	path.LineTo(qtcore.QPointF{X: 82, Y: 66})
	path.LineTo(qtcore.QPointF{X: 102, Y: 66})
	painter.DrawPath(path)
}

// drawGraph draws a graph: six nodes around a hexagon, each joined to its
// neighbours, with a triangle across it, for GraphQL, which queries a graph.
func drawGraph(painter *qtgui.QPainter) {
	pen := whitePen()
	pen.SetWidth(6)
	painter.SetPen(pen)
	painter.SetBrushStyle(qtcore.NoBrush)

	// The nodes, from the top round to the right.
	var nodes [6][2]float64
	for i := range nodes {
		angle := math.Pi/3*float64(i) - math.Pi/2
		nodes[i] = [2]float64{64 + 36*math.Cos(angle), 66 + 36*math.Sin(angle)}
	}

	path := qtgui.NewQPainterPath()
	path.MoveTo(qtcore.QPointF{X: nodes[5][0], Y: nodes[5][1]})
	for _, n := range nodes {
		path.LineTo(qtcore.QPointF{X: n[0], Y: n[1]})
	}
	path.MoveTo(qtcore.QPointF{X: nodes[0][0], Y: nodes[0][1]})
	path.LineTo(qtcore.QPointF{X: nodes[2][0], Y: nodes[2][1]})
	path.LineTo(qtcore.QPointF{X: nodes[4][0], Y: nodes[4][1]})
	path.CloseSubpath()
	painter.DrawPath(path)

	white := qcolour("#ffffff")
	brush := qtgui.NewQBrushWithColor(white)
	defer brush.Delete()
	painter.SetPenStyle(qtcore.NoPen)
	painter.SetBrush(brush)
	for _, n := range nodes {
		center := qtcore.QPointF{X: n[0], Y: n[1]}
		painter.DrawEllipseAtF(center, 8, 8)
	}
}

// drawPlug draws a plug with its lead, for MCP, which plugs tools and data
// into a model.
func drawPlug(painter *qtgui.QPainter) {
	painter.SetPen(whitePen())
	painter.SetBrushStyle(qtcore.NoBrush)

	path := qtgui.NewQPainterPath()
	// The pins.
	path.MoveTo(qtcore.QPointF{X: 52, Y: 26})
	path.LineTo(qtcore.QPointF{X: 52, Y: 44})
	path.MoveTo(qtcore.QPointF{X: 76, Y: 26})
	path.LineTo(qtcore.QPointF{X: 76, Y: 44})
	// The body, narrowing to the lead.
	path.MoveTo(qtcore.QPointF{X: 38, Y: 44})
	path.LineTo(qtcore.QPointF{X: 90, Y: 44})
	path.LineTo(qtcore.QPointF{X: 90, Y: 62})
	path.CubicTo(qtcore.QPointF{X: 90, Y: 76}, qtcore.QPointF{X: 78, Y: 84}, qtcore.QPointF{X: 64, Y: 84})
	path.CubicTo(qtcore.QPointF{X: 50, Y: 84}, qtcore.QPointF{X: 38, Y: 76}, qtcore.QPointF{X: 38, Y: 62})
	path.CloseSubpath()
	// The lead.
	path.MoveTo(qtcore.QPointF{X: 64, Y: 84})
	path.LineTo(qtcore.QPointF{X: 64, Y: 104})
	painter.DrawPath(path)
}

// drawRevolver draws a revolver seen from the side, pointing right: the
// program is named after its trigger.
func drawRevolver(painter *qtgui.QPainter) {
	white := qcolour("#ffffff")
	painter.SetPenStyle(qtcore.NoPen)
	painter.SetBrush(qtgui.NewQBrushWithColor(white))

	// The barrel, with the front sight on top and the rod beneath it.
	painter.DrawRoundedRect(qtcore.QRect{X: 72, Y: 30, Width: 44, Height: 14}, 3, 3)
	painter.DrawRect(qtcore.QRect{X: 107, Y: 23, Width: 5, Height: 9})
	painter.DrawRoundedRect(qtcore.QRect{X: 78, Y: 47, Width: 24, Height: 5}, 2, 2)

	// The frame: a strap above the cylinder and one below it, joined
	// behind it and in front of it.
	painter.DrawRect(qtcore.QRect{X: 32, Y: 30, Width: 44, Height: 5})
	painter.DrawRect(qtcore.QRect{X: 34, Y: 67, Width: 44, Height: 5})
	painter.DrawRect(qtcore.QRect{X: 30, Y: 30, Width: 9, Height: 42})
	painter.DrawRect(qtcore.QRect{X: 71, Y: 30, Width: 7, Height: 42})

	// The cylinder, clear of the frame all round.
	painter.DrawRoundedRect(qtcore.QRect{X: 42, Y: 38, Width: 26, Height: 26}, 5, 5)

	// The hammer, cocked back above the grip.
	hammer := qtgui.NewQPainterPath()
	hammer.MoveTo(qtcore.QPointF{X: 36, Y: 31})
	hammer.LineTo(qtcore.QPointF{X: 27, Y: 19})
	hammer.LineTo(qtcore.QPointF{X: 21, Y: 24})
	hammer.LineTo(qtcore.QPointF{X: 28, Y: 33})
	hammer.CloseSubpath()
	painter.DrawPath(hammer)

	// The grip, curving down and back from the frame.
	grip := qtgui.NewQPainterPath()
	grip.MoveTo(qtcore.QPointF{X: 30, Y: 42})
	grip.LineTo(qtcore.QPointF{X: 47, Y: 70})
	grip.QuadTo(qtcore.QPointF{X: 40, Y: 86}, qtcore.QPointF{X: 42, Y: 100})
	grip.QuadTo(qtcore.QPointF{X: 42, Y: 108}, qtcore.QPointF{X: 34, Y: 108})
	grip.LineTo(qtcore.QPointF{X: 22, Y: 108})
	grip.QuadTo(qtcore.QPointF{X: 14, Y: 108}, qtcore.QPointF{X: 15, Y: 100})
	grip.QuadTo(qtcore.QPointF{X: 18, Y: 70}, qtcore.QPointF{X: 30, Y: 42})
	grip.CloseSubpath()
	painter.DrawPath(grip)

	// The trigger guard, and the trigger inside it.
	pen := qtgui.NewQPenWithColor(white)
	pen.SetWidth(6)
	pen.SetCapStyle(qtcore.RoundCap)
	pen.SetJoinStyle(qtcore.RoundJoin)
	painter.SetPen(pen)
	painter.SetBrushStyle(qtcore.NoBrush)

	guard := qtgui.NewQPainterPath()
	guard.MoveTo(qtcore.QPointF{X: 45, Y: 90})
	guard.LineTo(qtcore.QPointF{X: 62, Y: 90})
	guard.QuadTo(qtcore.QPointF{X: 75, Y: 90}, qtcore.QPointF{X: 75, Y: 73})
	guard.MoveTo(qtcore.QPointF{X: 60, Y: 73})
	guard.QuadTo(qtcore.QPointF{X: 63, Y: 79}, qtcore.QPointF{X: 59, Y: 83})
	painter.DrawPath(guard)
}

// appIconColour is the colour of the program's icon.
const appIconColour = "#e8590c"

var appIconCache *qtgui.QIcon

// appIcon returns the icon of the program, from the PNG built into it.
func appIcon() *qtgui.QIcon {
	if appIconCache == nil {
		pixmap := qtgui.NewQPixmap()
		// The icon keeps a copy of the picture.
		defer pixmap.Delete()
		pixmap.LoadFromData(appicon.PNG)
		appIconCache = qtgui.NewQIconWithPixmap(pixmap)
	}
	return appIconCache
}

// appIconPNG draws the program's icon as a PNG of a size, which make icons
// writes into internal/appicon/appicon.png, or nil if it could not be made.
func appIconPNG(size int) []byte {
	icon := drawPixmap(size, appIconColour, drawRevolver)
	defer icon.Delete()

	buffer := qtcore.NewQBuffer(nil)
	defer buffer.Delete()
	if !buffer.Open(qtcore.QIODeviceBaseWriteOnly) {
		return nil
	}
	if icon.SaveToFull(buffer, "PNG", -1) != nil {
		return nil
	}
	return buffer.Data()
}

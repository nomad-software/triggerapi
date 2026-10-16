package ui

import (
	"math"

	qt "github.com/mappu/miqt/qt6"
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
func drawIcon(size int, colour string, glyph func(*qt.QPainter)) *qt.QIcon {
	pixmap := drawPixmap(size, colour, glyph)
	// The icon keeps a copy of the picture.
	defer pixmap.Delete()
	icon := qt.NewQIcon2(pixmap)
	// The icon of a selected item is otherwise tinted with the colour of
	// the selection.
	icon.AddPixmap2(pixmap, qt.QIcon__Selected)
	return icon
}

// drawPixmap draws the picture of an icon. See drawIcon.
func drawPixmap(size int, colour string, glyph func(*qt.QPainter)) *qt.QPixmap {
	pixmap := qt.NewQPixmap2(size, size)
	transparent := qt.NewQColor2(qt.Transparent)
	defer transparent.Delete()
	pixmap.FillWithFillColor(transparent)

	// The painter keeps a copy of the brush.
	background := qt.NewQColor6(colour)
	defer background.Delete()
	brush := qt.NewQBrush3(background)
	defer brush.Delete()

	painter := qt.NewQPainter2(pixmap.QPaintDevice)
	painter.SetRenderHint(qt.QPainter__Antialiasing)
	painter.Scale(float64(size)/glyphSize, float64(size)/glyphSize)
	painter.SetPenWithStyle(qt.NoPen)
	painter.SetBrush(brush)
	painter.DrawRoundedRect2(4, 4, glyphSize-8, glyphSize-8, 24, 24)
	glyph(painter)
	// Painting must be finished before the pixmap is used.
	painter.End()
	painter.Delete()

	return pixmap
}

// drawTabIcon returns an icon for a tab at the left of a tab widget. Tabs
// there are drawn turned a quarter turn anticlockwise, icon and all, so the
// picture is turned the other way beforehand to end up upright.
func drawTabIcon(colour string, glyph func(*qt.QPainter)) *qt.QIcon {
	pixmap := drawPixmap(iconSize*2, colour, glyph)
	defer pixmap.Delete()
	turn := qt.NewQTransform2()
	defer turn.Delete()
	turn.Rotate(90)
	// The turned picture is freed by the garbage collector, and the icon
	// keeps a copy of it.
	return qt.NewQIcon2(pixmap.Transformed2(turn, qt.SmoothTransformation))
}

// drawGlobe draws a globe, for the environments requests are sent to.
func drawGlobe(painter *qt.QPainter) {
	painter.SetPenWithPen(whitePen())
	painter.SetBrushWithStyle(qt.NoBrush)

	// The outline, a meridian and the equator.
	painter.DrawEllipse2(28, 28, 72, 72)
	painter.DrawEllipse2(49, 28, 30, 72)
	painter.DrawLine2(28, 64, 100, 64)
}

// drawFolder draws a folder, for the requests kept in folders.
func drawFolder(painter *qt.QPainter) {
	painter.SetPenWithPen(whitePen())
	painter.SetBrushWithStyle(qt.NoBrush)

	// A folder with its tab at the top left.
	path := qt.NewQPainterPath()
	path.MoveTo2(28, 40)
	path.LineTo2(54, 40)
	path.LineTo2(64, 52)
	path.LineTo2(100, 52)
	path.LineTo2(100, 92)
	path.LineTo2(28, 92)
	path.CloseSubpath()
	painter.DrawPath(path)
}

// drawDocument draws a page with a folded corner and lines of text, for a
// document that describes an API.
func drawDocument(painter *qt.QPainter) {
	painter.SetPenWithPen(whitePen())
	painter.SetBrushWithStyle(qt.NoBrush)

	path := qt.NewQPainterPath()
	// The page, and the fold at its top right corner.
	path.MoveTo2(38, 28)
	path.LineTo2(72, 28)
	path.LineTo2(90, 46)
	path.LineTo2(90, 100)
	path.LineTo2(38, 100)
	path.CloseSubpath()
	path.MoveTo2(72, 28)
	path.LineTo2(72, 46)
	path.LineTo2(90, 46)
	// The lines of text.
	path.MoveTo2(51, 64)
	path.LineTo2(77, 64)
	path.MoveTo2(51, 82)
	path.LineTo2(77, 82)
	painter.DrawPath(path)
}

// drawPlane draws a paper plane, for something that sends requests.
func drawPlane(painter *qt.QPainter) {
	painter.SetPenWithPen(whitePen())
	painter.SetBrushWithStyle(qt.NoBrush)

	path := qt.NewQPainterPath()
	// The outline of the plane, and the crease along its middle.
	path.MoveTo2(26, 62)
	path.LineTo2(102, 30)
	path.LineTo2(78, 100)
	path.LineTo2(60, 74)
	path.CloseSubpath()
	path.MoveTo2(60, 74)
	path.LineTo2(102, 30)
	painter.DrawPath(path)
}

// drawPrompt draws the prompt of a terminal, for a command to run in one.
func drawPrompt(painter *qt.QPainter) {
	painter.SetPenWithPen(whitePen())
	painter.SetBrushWithStyle(qt.NoBrush)

	path := qt.NewQPainterPath()
	// The chevron, and the cursor after it.
	path.MoveTo2(30, 42)
	path.LineTo2(56, 64)
	path.LineTo2(30, 86)
	path.MoveTo2(66, 88)
	path.LineTo2(98, 88)
	painter.DrawPath(path)
}

// drawEye draws an eye, for looking at how something will turn out.
func drawEye(painter *qt.QPainter) {
	painter.SetPenWithPen(whitePen())
	painter.SetBrushWithStyle(qt.NoBrush)

	path := qt.NewQPainterPath()
	// The lids, from corner to corner.
	path.MoveTo2(24, 64)
	path.QuadTo2(64, 20, 104, 64)
	path.QuadTo2(64, 108, 24, 64)
	painter.DrawPath(path)

	// The pupil.
	painter.SetBrush(qt.NewQBrush3(qt.NewQColor6("#ffffff")))
	painter.DrawEllipse2(54, 54, 20, 20)
}

// drawMagnifier draws a magnifying glass, for finding something.
func drawMagnifier(painter *qt.QPainter) {
	painter.SetPenWithPen(whitePen())
	painter.SetBrushWithStyle(qt.NoBrush)

	painter.DrawEllipse2(28, 28, 50, 50)

	// The handle.
	path := qt.NewQPainterPath()
	path.MoveTo2(72, 72)
	path.LineTo2(100, 100)
	painter.DrawPath(path)
}

// drawTick draws a tick, for something that went well.
func drawTick(painter *qt.QPainter) {
	pen := whitePen()
	pen.SetWidth(14)
	painter.SetPenWithPen(pen)
	painter.SetBrushWithStyle(qt.NoBrush)

	path := qt.NewQPainterPath()
	path.MoveTo2(32, 68)
	path.LineTo2(54, 90)
	path.LineTo2(98, 40)
	painter.DrawPath(path)
}

// drawCross draws a cross, for something that did not.
func drawCross(painter *qt.QPainter) {
	pen := whitePen()
	pen.SetWidth(14)
	painter.SetPenWithPen(pen)
	painter.SetBrushWithStyle(qt.NoBrush)

	path := qt.NewQPainterPath()
	path.MoveTo2(38, 38)
	path.LineTo2(90, 90)
	path.MoveTo2(90, 38)
	path.LineTo2(38, 90)
	painter.DrawPath(path)
}

// drawDisk draws a floppy disk, for saving something to a file.
func drawDisk(painter *qt.QPainter) {
	painter.SetPenWithPen(whitePen())
	painter.SetBrushWithStyle(qt.NoBrush)

	path := qt.NewQPainterPath()
	// The disk, with its top right corner cut off.
	path.MoveTo2(30, 30)
	path.LineTo2(84, 30)
	path.LineTo2(98, 44)
	path.LineTo2(98, 98)
	path.LineTo2(30, 98)
	path.CloseSubpath()
	// The shutter at the top, and the label at the bottom.
	path.MoveTo2(46, 30)
	path.LineTo2(46, 50)
	path.LineTo2(74, 50)
	path.LineTo2(74, 30)
	path.MoveTo2(46, 98)
	path.LineTo2(46, 72)
	path.LineTo2(82, 72)
	path.LineTo2(82, 98)
	painter.DrawPath(path)
}

// outline sets a painter up to draw the lines of a symbol.
func outline(painter *qt.QPainter) {
	painter.SetPenWithPen(whitePen())
	painter.SetBrushWithStyle(qt.NoBrush)
}

// plus adds a plus sign to a path, centred on a point.
func plus(path *qt.QPainterPath, x, y, arm float64) {
	path.MoveTo2(x-arm, y)
	path.LineTo2(x+arm, y)
	path.MoveTo2(x, y-arm)
	path.LineTo2(x, y+arm)
}

// drawPlus draws a plus sign, for adding something.
func drawPlus(painter *qt.QPainter) {
	outline(painter)
	path := qt.NewQPainterPath()
	plus(path, 64, 64, 30)
	painter.DrawPath(path)
}

// checklist adds rows of a checklist to a path, each a tick followed by a
// line, with the first at top and the others gap below one another.
func checklist(path *qt.QPainterPath, top, gap float64, rows int) {
	for i := range rows {
		y := top + gap*float64(i)
		path.MoveTo2(30, y)
		path.LineTo2(37, y+7)
		path.LineTo2(49, y-7)
		path.MoveTo2(60, y)
		path.LineTo2(98, y)
	}
}

// drawSets draws a checklist, for a set: a set of requests worked on
// together.
func drawSets(painter *qt.QPainter) {
	outline(painter)
	path := qt.NewQPainterPath()
	checklist(path, 40, 24, 3)
	painter.DrawPath(path)
}

// setIcon returns the icon of a set, in the colour of the sets.
func setIcon() *qt.QIcon {
	return cachedIcon("set", func() *qt.QIcon { return drawIcon(iconSize*2, themeColour(colourSets), drawSets) })
}

// drawFolderPlus draws a folder with a plus sign on it, for adding a folder.
func drawFolderPlus(painter *qt.QPainter) {
	drawFolder(painter)
	path := qt.NewQPainterPath()
	plus(path, 64, 72, 11)
	painter.DrawPath(path)
}

// drawCopy draws one page in front of another, for making a copy of
// something.
func drawCopy(painter *qt.QPainter) {
	outline(painter)
	// The page in front.
	painter.DrawRoundedRect2(46, 44, 54, 58, 6, 6)

	// What can be seen of the page behind it.
	path := qt.NewQPainterPath()
	path.MoveTo2(30, 82)
	path.LineTo2(30, 32)
	path.QuadTo2(30, 26, 36, 26)
	path.LineTo2(82, 26)
	painter.DrawPath(path)
}

// drawImport draws an arrow going into a page from outside it, pointing
// left, for bringing something in from a file.
func drawImport(painter *qt.QPainter) {
	outline(painter)
	path := qt.NewQPainterPath()
	// The page, open at the right where the arrow goes in, with the fold at
	// its top left corner.
	path.MoveTo2(76, 44)
	path.LineTo2(76, 28)
	path.LineTo2(46, 28)
	path.LineTo2(28, 46)
	path.LineTo2(28, 100)
	path.LineTo2(76, 100)
	path.LineTo2(76, 84)
	path.MoveTo2(46, 28)
	path.LineTo2(46, 46)
	path.LineTo2(28, 46)
	// The arrow.
	path.MoveTo2(104, 64)
	path.LineTo2(52, 64)
	path.MoveTo2(68, 48)
	path.LineTo2(52, 64)
	path.LineTo2(68, 80)
	painter.DrawPath(path)
}

// drawPencil draws a pencil, for editing something.
func drawPencil(painter *qt.QPainter) {
	outline(painter)
	path := qt.NewQPainterPath()
	// The pencil, from its point at the bottom left.
	path.MoveTo2(30, 98)
	path.LineTo2(36, 74)
	path.LineTo2(80, 30)
	path.LineTo2(98, 48)
	path.LineTo2(54, 92)
	path.CloseSubpath()
	// The band beneath the rubber.
	path.MoveTo2(68, 42)
	path.LineTo2(86, 60)
	painter.DrawPath(path)
}

// drawLetters draws a capital and a small letter with the cursor that text
// is typed at, for renaming something.
func drawLetters(painter *qt.QPainter) {
	font := qt.NewQFont()
	font.SetPixelSize(56)
	font.SetBold(true)

	painter.SetFont(font)
	painter.SetPenWithPen(whitePen())
	painter.DrawText7(14, 0, 80, glyphSize-6, int(qt.AlignCenter), "Aa")

	path := qt.NewQPainterPath()
	path.MoveTo2(104, 34)
	path.LineTo2(104, 94)
	painter.DrawPath(path)
}

// drawBin draws a rubbish bin, for removing something.
func drawBin(painter *qt.QPainter) {
	outline(painter)
	path := qt.NewQPainterPath()
	// The lid and its handle.
	path.MoveTo2(30, 42)
	path.LineTo2(98, 42)
	path.MoveTo2(52, 42)
	path.LineTo2(52, 28)
	path.LineTo2(76, 28)
	path.LineTo2(76, 42)
	// The bin.
	path.MoveTo2(40, 42)
	path.LineTo2(46, 102)
	path.LineTo2(82, 102)
	path.LineTo2(88, 42)
	// The ribs down its side.
	path.MoveTo2(56, 58)
	path.LineTo2(57, 86)
	path.MoveTo2(72, 58)
	path.LineTo2(71, 86)
	painter.DrawPath(path)
}

// drawPin draws a drawing pin, for keeping something where it is.
func drawPin(painter *qt.QPainter) {
	pen := whitePen()
	pen.SetWidth(12)
	painter.SetPenWithPen(pen)
	painter.SetBrushWithStyle(qt.NoBrush)

	path := qt.NewQPainterPath()
	// The head, the body, the collar and the point.
	path.MoveTo2(44, 26)
	path.LineTo2(84, 26)
	path.MoveTo2(52, 26)
	path.LineTo2(50, 62)
	path.MoveTo2(76, 26)
	path.LineTo2(78, 62)
	path.MoveTo2(34, 64)
	path.LineTo2(94, 64)
	path.MoveTo2(64, 64)
	path.LineTo2(64, 104)
	painter.DrawPath(path)
}

// drawMonitor draws the monitor of a desk, on its stand, for a workspace.
func drawMonitor(painter *qt.QPainter) {
	outline(painter)
	painter.DrawRoundedRect2(22, 28, 84, 54, 6, 6)

	path := qt.NewQPainterPath()
	// The stand and its foot.
	path.MoveTo2(64, 82)
	path.LineTo2(64, 100)
	path.MoveTo2(44, 100)
	path.LineTo2(84, 100)
	painter.DrawPath(path)
}

// whitePen returns a pen for drawing the lines of a symbol.
func whitePen() *qt.QPen {
	pen := qt.NewQPen3(qt.NewQColor6("#ffffff"))
	pen.SetWidth(9)
	pen.SetCapStyle(qt.RoundCap)
	pen.SetJoinStyle(qt.RoundJoin)
	return pen
}

// drawBraces draws a pair of braces, as around a JSON document.
func drawBraces(painter *qt.QPainter) {
	font := qt.NewQFont()
	font.SetPixelSize(64)
	font.SetBold(true)

	painter.SetFont(font)
	painter.SetPenWithPen(whitePen())
	painter.DrawText7(0, 0, glyphSize, glyphSize-8, int(qt.AlignCenter), "{ }")
}

// drawArrows draws two arrows pointing in opposite directions, for a call
// and its reply.
func drawArrows(painter *qt.QPainter) {
	painter.SetPenWithPen(whitePen())
	painter.SetBrushWithStyle(qt.NoBrush)

	path := qt.NewQPainterPath()
	// The upper arrow points right.
	path.MoveTo2(34, 48)
	path.LineTo2(94, 48)
	path.MoveTo2(78, 32)
	path.LineTo2(94, 48)
	path.LineTo2(78, 64)
	// The lower arrow points left.
	path.MoveTo2(94, 82)
	path.LineTo2(34, 82)
	path.MoveTo2(50, 66)
	path.LineTo2(34, 82)
	path.LineTo2(50, 98)

	painter.DrawPath(path)
}

// drawPulse draws the line of a pulse, for a WebSocket: a connection that
// stays open, with messages going both ways along it.
func drawPulse(painter *qt.QPainter) {
	painter.SetPenWithPen(whitePen())
	painter.SetBrushWithStyle(qt.NoBrush)

	path := qt.NewQPainterPath()
	path.MoveTo2(26, 66)
	path.LineTo2(46, 66)
	path.LineTo2(56, 38)
	path.LineTo2(72, 92)
	path.LineTo2(82, 66)
	path.LineTo2(102, 66)
	painter.DrawPath(path)
}

// drawGraph draws a graph: six nodes around a hexagon, each joined to its
// neighbours, with a triangle across it, for GraphQL, which queries a graph.
func drawGraph(painter *qt.QPainter) {
	pen := whitePen()
	pen.SetWidth(6)
	painter.SetPenWithPen(pen)
	painter.SetBrushWithStyle(qt.NoBrush)

	// The nodes, from the top round to the right.
	var nodes [6][2]float64
	for i := range nodes {
		angle := math.Pi/3*float64(i) - math.Pi/2
		nodes[i] = [2]float64{64 + 36*math.Cos(angle), 66 + 36*math.Sin(angle)}
	}

	path := qt.NewQPainterPath()
	path.MoveTo2(nodes[5][0], nodes[5][1])
	for _, n := range nodes {
		path.LineTo2(n[0], n[1])
	}
	path.MoveTo2(nodes[0][0], nodes[0][1])
	path.LineTo2(nodes[2][0], nodes[2][1])
	path.LineTo2(nodes[4][0], nodes[4][1])
	path.CloseSubpath()
	painter.DrawPath(path)

	white := qt.NewQColor6("#ffffff")
	defer white.Delete()
	brush := qt.NewQBrush3(white)
	defer brush.Delete()
	painter.SetPenWithStyle(qt.NoPen)
	painter.SetBrush(brush)
	for _, n := range nodes {
		center := qt.NewQPointF3(n[0], n[1])
		painter.DrawEllipse3(center, 8, 8)
		center.Delete()
	}
}

// drawRevolver draws a revolver seen from the side, pointing right: the
// program is named after its trigger.
func drawRevolver(painter *qt.QPainter) {
	white := qt.NewQColor6("#ffffff")
	painter.SetPenWithStyle(qt.NoPen)
	painter.SetBrush(qt.NewQBrush3(white))

	// The barrel, with the front sight on top and the rod beneath it.
	painter.DrawRoundedRect2(72, 30, 44, 14, 3, 3)
	painter.DrawRect2(107, 23, 5, 9)
	painter.DrawRoundedRect2(78, 47, 24, 5, 2, 2)

	// The frame: a strap above the cylinder and one below it, joined
	// behind it and in front of it.
	painter.DrawRect2(32, 30, 44, 5)
	painter.DrawRect2(34, 67, 44, 5)
	painter.DrawRect2(30, 30, 9, 42)
	painter.DrawRect2(71, 30, 7, 42)

	// The cylinder, clear of the frame all round.
	painter.DrawRoundedRect2(42, 38, 26, 26, 5, 5)

	// The hammer, cocked back above the grip.
	hammer := qt.NewQPainterPath()
	hammer.MoveTo2(36, 31)
	hammer.LineTo2(27, 19)
	hammer.LineTo2(21, 24)
	hammer.LineTo2(28, 33)
	hammer.CloseSubpath()
	painter.DrawPath(hammer)

	// The grip, curving down and back from the frame.
	grip := qt.NewQPainterPath()
	grip.MoveTo2(30, 42)
	grip.LineTo2(47, 70)
	grip.QuadTo2(40, 86, 42, 100)
	grip.QuadTo2(42, 108, 34, 108)
	grip.LineTo2(22, 108)
	grip.QuadTo2(14, 108, 15, 100)
	grip.QuadTo2(18, 70, 30, 42)
	grip.CloseSubpath()
	painter.DrawPath(grip)

	// The trigger guard, and the trigger inside it.
	pen := qt.NewQPen3(white)
	pen.SetWidth(6)
	pen.SetCapStyle(qt.RoundCap)
	pen.SetJoinStyle(qt.RoundJoin)
	painter.SetPenWithPen(pen)
	painter.SetBrushWithStyle(qt.NoBrush)

	guard := qt.NewQPainterPath()
	guard.MoveTo2(45, 90)
	guard.LineTo2(62, 90)
	guard.QuadTo2(75, 90, 75, 73)
	guard.MoveTo2(60, 73)
	guard.QuadTo2(63, 79, 59, 83)
	painter.DrawPath(guard)
}

// appIconColour is the colour of the program's icon.
const appIconColour = "#e8590c"

var appIconCache *qt.QIcon

// appIcon returns the icon of the program.
func appIcon() *qt.QIcon {
	if appIconCache == nil {
		appIconCache = drawIcon(256, appIconColour, drawRevolver)
	}
	return appIconCache
}

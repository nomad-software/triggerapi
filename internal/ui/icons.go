package ui

import (
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
	return qt.NewQIcon2(drawPixmap(size, colour, glyph))
}

// drawPixmap draws the picture of an icon. See drawIcon.
func drawPixmap(size int, colour string, glyph func(*qt.QPainter)) *qt.QPixmap {
	pixmap := qt.NewQPixmap2(size, size)
	pixmap.FillWithFillColor(qt.NewQColor2(qt.Transparent))

	painter := qt.NewQPainter2(pixmap.QPaintDevice)
	painter.SetRenderHint(qt.QPainter__Antialiasing)
	painter.Scale(float64(size)/glyphSize, float64(size)/glyphSize)
	painter.SetPenWithStyle(qt.NoPen)
	painter.SetBrush(qt.NewQBrush3(qt.NewQColor6(colour)))
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
	return qt.NewQIcon2(pixmap.Transformed2(qt.NewQTransform2().Rotate(90), qt.SmoothTransformation))
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

// drawFolderPlus draws a folder with a plus sign on it, for adding a folder.
func drawFolderPlus(painter *qt.QPainter) {
	drawFolder(painter)
	path := qt.NewQPainterPath()
	plus(path, 64, 72, 11)
	painter.DrawPath(path)
}

// drawPagePlus draws a page with a plus sign on it, for adding a request.
func drawPagePlus(painter *qt.QPainter) {
	outline(painter)
	path := qt.NewQPainterPath()
	// The page, and the fold at its top right corner.
	path.MoveTo2(36, 26)
	path.LineTo2(72, 26)
	path.LineTo2(92, 46)
	path.LineTo2(92, 102)
	path.LineTo2(36, 102)
	path.CloseSubpath()
	path.MoveTo2(72, 26)
	path.LineTo2(72, 46)
	path.LineTo2(92, 46)
	plus(path, 64, 74, 12)
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

// The colours of the sidebar tab icons.
const (
	environmentsColour = "#8e6fd8"
	requestsColour     = "#17807c"
)

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

// drawBolt draws a bolt of lightning, for a request that is fired off.
func drawBolt(painter *qt.QPainter) {
	white := qt.NewQColor6("#ffffff")
	pen := qt.NewQPen3(white)
	pen.SetWidth(6)
	pen.SetJoinStyle(qt.RoundJoin)
	painter.SetPenWithPen(pen)
	painter.SetBrush(qt.NewQBrush3(white))

	path := qt.NewQPainterPath()
	path.MoveTo2(74, 22)
	path.LineTo2(38, 72)
	path.LineTo2(61, 72)
	path.LineTo2(54, 106)
	path.LineTo2(91, 55)
	path.LineTo2(68, 55)
	path.CloseSubpath()
	painter.DrawPath(path)
}

// appIconColour is the colour of the program's icon.
const appIconColour = "#e8590c"

var appIconCache *qt.QIcon

// appIcon returns the icon of the program.
func appIcon() *qt.QIcon {
	if appIconCache == nil {
		appIconCache = drawIcon(256, appIconColour, drawBolt)
	}
	return appIconCache
}

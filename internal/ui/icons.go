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

	return qt.NewQIcon2(pixmap)
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

// drawEnvelope draws an envelope, as delivered by a postie.
func drawEnvelope(painter *qt.QPainter) {
	painter.SetPenWithPen(whitePen())
	painter.SetBrushWithStyle(qt.NoBrush)
	painter.DrawRoundedRect2(26, 38, 76, 52, 6, 6)

	// The flap.
	path := qt.NewQPainterPath()
	path.MoveTo2(29, 42)
	path.LineTo2(64, 68)
	path.LineTo2(99, 42)
	painter.DrawPath(path)
}

// appIconColour is the colour of the program's icon.
const appIconColour = "#e8590c"

var appIconCache *qt.QIcon

// appIcon returns the icon of the program.
func appIcon() *qt.QIcon {
	if appIconCache == nil {
		appIconCache = drawIcon(256, appIconColour, drawEnvelope)
	}
	return appIconCache
}

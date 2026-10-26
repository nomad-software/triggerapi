package ui

import (
	"runtime"

	qt "github.com/mappu/miqt/qt6"

	"github.com/nomad-software/triggerapi/internal/settings"
)

// newThemeStyle returns the style of the light and dark themes: Fusion,
// with the boxes of check boxes edged more clearly. Fusion edges them in a
// shade a little darker than the window, which in the light theme is barely
// seen between the white of the box and the page it is on, and in the dark
// theme is as dark as the box. The edge is drawn again in a colour half way
// between the text and the box, which shows in either, and fades with the
// text where the box is disabled. Qt keeps the style, and deletes it when
// another is put in its place.
func newThemeStyle() *qt.QStyle {
	s := qt.NewQProxyStyle2(fusion)
	s.OnDrawPrimitive(func(super func(qt.QStyle__PrimitiveElement, *qt.QStyleOption, *qt.QPainter, *qt.QWidget), element qt.QStyle__PrimitiveElement, option *qt.QStyleOption, painter *qt.QPainter, widget *qt.QWidget) {
		super(element, option, painter, widget)
		// A box the keyboard has brought the focus to keeps the edge
		// Fusion gives it, in the colour of the selection.
		focus := qt.QStyle__State_HasFocus | qt.QStyle__State_KeyboardFocusChange
		if element == qt.QStyle__PE_IndicatorCheckBox && option.State()&focus != focus {
			edgeBox(option, painter)
		}
	})
	return s.QStyle
}

// edgeBox draws the edge of the box of a check box, in a colour half way
// between that of text and that of the box.
func edgeBox(option *qt.QStyleOption, painter *qt.QPainter) {
	palette := option.Palette()
	text, base := palette.ColorWithCr(qt.QPalette__Text), palette.ColorWithCr(qt.QPalette__Base)
	edge := qt.NewQColor3((text.Red()+base.Red())/2, (text.Green()+base.Green())/2, (text.Blue()+base.Blue())/2)
	runtime.KeepAlive(palette)
	r := option.Rect()
	// An outline drawn without antialiasing covers the pixel below and to
	// the right of each of its lines, so it is one less each way.
	rect := qt.NewQRectF4(float64(r.X()), float64(r.Y()), float64(r.Width()-1), float64(r.Height()-1))
	runtime.KeepAlive(r)
	painter.Save()
	painter.SetRenderHint2(qt.QPainter__Antialiasing, false)
	painter.SetPen(edge)
	painter.SetBrushWithStyle(qt.NoBrush)
	painter.DrawRect(rect)
	painter.Restore()
	edge.Delete()
	rect.Delete()
}

// fusion is the style the light and dark themes are drawn in.
const fusion = "Fusion"

// theme is a theme that can be chosen: the name the settings keep it by,
// what the settings dialog calls it and the colours of its palette, which
// are nil for the system's.
type theme struct {
	name, label string
	colours     *colours
}

// themes are the themes that can be chosen, in the order they are offered:
// the system's, which is "", and light and dark ones.
var themes = []theme{
	{"", "Follow the system", nil},
	{settings.ThemeLight, "Light", &lightColours},
	{settings.ThemeDark, "Dark", &darkColours},
}

// themeIndex returns the index in themes of the theme with the name, or of
// the system's for a name that is not known.
func themeIndex(name string) int {
	return selectorIndex(themes, func(t theme) bool { return t.name == name })
}

// systemStyle is the name of the style the desktop gave the program, which
// following the system goes back to.
var systemStyle string

// rememberSystemStyle keeps the name of the style the program started with,
// before a theme of its own replaces it.
func rememberSystemStyle() {
	if systemStyle == "" {
		systemStyle = qt.QApplication_Style().Name()
	}
}

// applyTheme puts the theme with the name into effect, in the windows that
// are open as in those opened later. The light and dark themes are the
// Fusion style, which draws in whatever palette it is given on every
// desktop, with a palette of their own; the system's is the style and the
// palette the desktop gives, which Qt keeps up to date as the desktop's
// theme changes.
func applyTheme(name string) {
	rememberSystemStyle()
	t := themes[themeIndex(name)]
	var p *qt.QPalette
	if t.colours != nil {
		qt.QApplication_SetStyle(newThemeStyle())
		p = makePalette(*t.colours)
	} else {
		qt.QApplication_SetStyleWithStyle(systemStyle)
		// A palette that sets nothing is the desktop's.
		p = qt.NewQPalette()
	}
	qt.QApplication_SetPalette(p)
	p.Delete()
	themeName = t.name
	// The lists of the table of assertions have a style of their own, over
	// the one in effect.
	if paddedStyle != nil {
		paddedStyle.SetBaseStyle(qt.QStyleFactory_Create(themeStyleName()))
	}
}

// themeName is the name of the theme in effect, "" for the system's.
var themeName string

// themeStyleName returns the name of the style the theme in effect is drawn
// in: Fusion for the light and dark themes, and the desktop's for the
// system's.
func themeStyleName() string {
	if themeName != "" {
		return fusion
	}
	rememberSystemStyle()
	return systemStyle
}

// colours are the colours of a palette: those of each role, and those of
// some roles where what they are in is disabled.
type colours struct {
	roles, disabled map[qt.QPalette__ColorRole]string
}

// lightColours and darkColours are the palettes of the light and dark
// themes. Each sets every colour Fusion draws with, as what is left out
// would be the desktop's: Fusion's own standard palette, too, is dark where
// the desktop's colour scheme is.
var (
	lightColours = colours{
		roles: map[qt.QPalette__ColorRole]string{
			qt.QPalette__Window:          "#efefef",
			qt.QPalette__WindowText:      "#1a1a1a",
			qt.QPalette__Base:            "#ffffff",
			qt.QPalette__AlternateBase:   "#f5f5f5",
			qt.QPalette__Text:            "#1a1a1a",
			qt.QPalette__PlaceholderText: "#808080",
			qt.QPalette__Button:          "#efefef",
			qt.QPalette__ButtonText:      "#1a1a1a",
			qt.QPalette__BrightText:      "#ffffff",
			qt.QPalette__ToolTipBase:     "#ffffdc",
			qt.QPalette__ToolTipText:     "#1a1a1a",
			qt.QPalette__Highlight:       "#308cc6",
			qt.QPalette__HighlightedText: "#ffffff",
			qt.QPalette__Link:            "#0b57d0",
			qt.QPalette__LinkVisited:     "#6a1b9a",
			// The shades Fusion draws the edges of buttons and frames
			// with.
			qt.QPalette__Light:    "#ffffff",
			qt.QPalette__Midlight: "#e3e3e3",
			qt.QPalette__Mid:      "#b8b8b8",
			qt.QPalette__Dark:     "#9f9f9f",
			qt.QPalette__Shadow:   "#767676",
		},
		disabled: map[qt.QPalette__ColorRole]string{
			qt.QPalette__WindowText:      "#a0a0a0",
			qt.QPalette__Text:            "#a0a0a0",
			qt.QPalette__ButtonText:      "#a0a0a0",
			qt.QPalette__Highlight:       "#919191",
			qt.QPalette__HighlightedText: "#ffffff",
		},
	}
	darkColours = colours{
		roles: map[qt.QPalette__ColorRole]string{
			qt.QPalette__Window:          "#2d2d2d",
			qt.QPalette__WindowText:      "#e6e6e6",
			qt.QPalette__Base:            "#1e1e1e",
			qt.QPalette__AlternateBase:   "#262626",
			qt.QPalette__Text:            "#e6e6e6",
			qt.QPalette__PlaceholderText: "#8c8c8c",
			qt.QPalette__Button:          "#353535",
			qt.QPalette__ButtonText:      "#e6e6e6",
			qt.QPalette__BrightText:      "#ff6060",
			qt.QPalette__ToolTipBase:     "#353535",
			qt.QPalette__ToolTipText:     "#e6e6e6",
			qt.QPalette__Highlight:       "#2f6fb5",
			qt.QPalette__HighlightedText: "#ffffff",
			qt.QPalette__Link:            "#6aa9ff",
			qt.QPalette__LinkVisited:     "#b48cff",
			qt.QPalette__Light:           "#4a4a4a",
			qt.QPalette__Midlight:        "#3e3e3e",
			qt.QPalette__Mid:             "#262626",
			qt.QPalette__Dark:            "#1a1a1a",
			qt.QPalette__Shadow:          "#101010",
		},
		disabled: map[qt.QPalette__ColorRole]string{
			qt.QPalette__WindowText:      "#7a7a7a",
			qt.QPalette__Text:            "#7a7a7a",
			qt.QPalette__ButtonText:      "#7a7a7a",
			qt.QPalette__Highlight:       "#4a4a4a",
			qt.QPalette__HighlightedText: "#b0b0b0",
		},
	}
)

// makePalette returns a palette of the colours.
func makePalette(cs colours) *qt.QPalette {
	p := qt.NewQPalette()
	// The palette keeps a copy of each colour.
	for role, colour := range cs.roles {
		c := qt.NewQColor6(colour)
		p.SetColor2(role, c)
		c.Delete()
	}
	for role, colour := range cs.disabled {
		c := qt.NewQColor6(colour)
		p.SetColor(qt.QPalette__Disabled, role, c)
		c.Delete()
	}
	return p
}

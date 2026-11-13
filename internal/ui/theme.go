package ui

import (
	"github.com/nomad-software/qute/qtcore"
	"github.com/nomad-software/qute/qtgui"
	"github.com/nomad-software/qute/qtwidgets"

	"github.com/nomad-software/triggerapi/internal/settings"
)

// useThemeStyle puts the style of the light and dark themes into effect: Fusion,
// with the boxes of check boxes edged more clearly. Fusion edges them in a
// shade a little darker than the window, which in the light theme is barely
// seen between the white of the box and the page it is on, and in the dark
// theme is as dark as the box. The edge is drawn again in a colour half way
// between the text and the box, which shows in either, and fades with the
// text where the box is disabled. The application keeps the style, and
// deletes it when another is put in its place.
func useThemeStyle() {
	s := &themeStyle{}
	qtwidgets.NewQProxyStyleWithKeyWith(s, fusion)
	qtwidgets.QApplicationSetStyle(s)
}

// themeStyle is the style of the light and dark themes. See useThemeStyle.
type themeStyle struct {
	qtwidgets.QProxyStyleBase
}

// DrawPrimitive draws as Fusion does, and edges the box of a check box.
func (s *themeStyle) DrawPrimitive(element qtwidgets.QStylePrimitiveElement, option *qtwidgets.QStyleOption, painter *qtgui.QPainter, widget *qtwidgets.QWidget) {
	s.Super().DrawPrimitive(element, option, painter, widget)
	// A box the keyboard has brought the focus to keeps the edge Fusion
	// gives it, in the colour of the selection.
	focus := qtwidgets.QStyleState_HasFocus | qtwidgets.QStyleState_KeyboardFocusChange
	if element == qtwidgets.QStylePE_IndicatorCheckBox && option.State()&focus != focus {
		edgeBox(option, painter)
	}
}

// edgeBox draws the edge of the box of a check box, in a colour half way
// between that of text and that of the box.
func edgeBox(option *qtwidgets.QStyleOption, painter *qtgui.QPainter) {
	palette := option.Palette()
	text, base := palette.Color(qtgui.QPaletteText), palette.Color(qtgui.QPaletteBase)
	edge := qtgui.RGB(uint8((int(text.R)+int(base.R))/2), uint8((int(text.G)+int(base.G))/2), uint8((int(text.B)+int(base.B))/2))
	r := option.Rect()
	// An outline drawn without antialiasing covers the pixel below and to
	// the right of each of its lines, so it is one less each way.
	rect := qtcore.QRectF{X: float64(r.X), Y: float64(r.Y), Width: float64(r.Width - 1), Height: float64(r.Height - 1)}
	painter.Save()
	painter.SetRenderHintFull(qtgui.QPainterAntialiasing, false)
	painter.SetPenColor(edge)
	painter.SetBrushStyle(qtcore.NoBrush)
	painter.DrawRectF(rect)
	painter.Restore()
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
		systemStyle = qtwidgets.QApplicationStyle().Name()
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
	var p *qtgui.QPalette
	if t.colours != nil {
		useThemeStyle()
		p = makePalette(*t.colours)
	} else {
		qtwidgets.QApplicationSetStyleName(systemStyle)
		// A palette that sets nothing is the desktop's.
		p = qtgui.NewQPalette()
	}
	qtwidgets.QApplicationSetPalette(p)
	p.Delete()
	themeName = t.name
	// The lists of the table of assertions have a style of their own, over
	// the one in effect.
	if paddedStyle != nil {
		paddedStyle.SetBaseStyle(qtwidgets.QStyleFactoryCreate(themeStyleName()))
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
	roles, disabled map[qtgui.QPaletteColorRole]string
}

// lightColours and darkColours are the palettes of the light and dark
// themes. Each sets every colour Fusion draws with, as what is left out
// would be the desktop's: Fusion's own standard palette, too, is dark where
// the desktop's colour scheme is.
var (
	lightColours = colours{
		roles: map[qtgui.QPaletteColorRole]string{
			qtgui.QPaletteWindow:          "#efefef",
			qtgui.QPaletteWindowText:      "#1a1a1a",
			qtgui.QPaletteBase:            "#ffffff",
			qtgui.QPaletteAlternateBase:   "#f5f5f5",
			qtgui.QPaletteText:            "#1a1a1a",
			qtgui.QPalettePlaceholderText: "#808080",
			qtgui.QPaletteButton:          "#efefef",
			qtgui.QPaletteButtonText:      "#1a1a1a",
			qtgui.QPaletteBrightText:      "#ffffff",
			qtgui.QPaletteToolTipBase:     "#ffffdc",
			qtgui.QPaletteToolTipText:     "#1a1a1a",
			qtgui.QPaletteHighlight:       "#308cc6",
			qtgui.QPaletteHighlightedText: "#ffffff",
			qtgui.QPaletteLink:            "#0b57d0",
			qtgui.QPaletteLinkVisited:     "#6a1b9a",
			// The shades Fusion draws the edges of buttons and frames
			// with.
			qtgui.QPaletteLight:    "#ffffff",
			qtgui.QPaletteMidlight: "#e3e3e3",
			qtgui.QPaletteMid:      "#b8b8b8",
			qtgui.QPaletteDark:     "#9f9f9f",
			qtgui.QPaletteShadow:   "#767676",
		},
		disabled: map[qtgui.QPaletteColorRole]string{
			qtgui.QPaletteWindowText:      "#a0a0a0",
			qtgui.QPaletteText:            "#a0a0a0",
			qtgui.QPaletteButtonText:      "#a0a0a0",
			qtgui.QPaletteHighlight:       "#919191",
			qtgui.QPaletteHighlightedText: "#ffffff",
		},
	}
	darkColours = colours{
		roles: map[qtgui.QPaletteColorRole]string{
			qtgui.QPaletteWindow:          "#2d2d2d",
			qtgui.QPaletteWindowText:      "#e6e6e6",
			qtgui.QPaletteBase:            "#1e1e1e",
			qtgui.QPaletteAlternateBase:   "#262626",
			qtgui.QPaletteText:            "#e6e6e6",
			qtgui.QPalettePlaceholderText: "#8c8c8c",
			qtgui.QPaletteButton:          "#353535",
			qtgui.QPaletteButtonText:      "#e6e6e6",
			qtgui.QPaletteBrightText:      "#ff6060",
			qtgui.QPaletteToolTipBase:     "#353535",
			qtgui.QPaletteToolTipText:     "#e6e6e6",
			qtgui.QPaletteHighlight:       "#2f6fb5",
			qtgui.QPaletteHighlightedText: "#ffffff",
			qtgui.QPaletteLink:            "#6aa9ff",
			qtgui.QPaletteLinkVisited:     "#b48cff",
			qtgui.QPaletteLight:           "#4a4a4a",
			qtgui.QPaletteMidlight:        "#3e3e3e",
			qtgui.QPaletteMid:             "#262626",
			qtgui.QPaletteDark:            "#1a1a1a",
			qtgui.QPaletteShadow:          "#101010",
		},
		disabled: map[qtgui.QPaletteColorRole]string{
			qtgui.QPaletteWindowText:      "#7a7a7a",
			qtgui.QPaletteText:            "#7a7a7a",
			qtgui.QPaletteButtonText:      "#7a7a7a",
			qtgui.QPaletteHighlight:       "#4a4a4a",
			qtgui.QPaletteHighlightedText: "#b0b0b0",
		},
	}
)

// makePalette returns a palette of the colours.
func makePalette(cs colours) *qtgui.QPalette {
	p := qtgui.NewQPalette()
	// The palette keeps a copy of each colour.
	for role, colour := range cs.roles {
		c := qcolour(colour)
		p.SetColor(role, c)
	}
	for role, colour := range cs.disabled {
		c := qcolour(colour)
		p.SetGroupColor(qtgui.QPaletteDisabled, role, c)
	}
	return p
}

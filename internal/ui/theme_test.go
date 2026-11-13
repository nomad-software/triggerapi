package ui

import (
	"github.com/nomad-software/qute"
	"github.com/nomad-software/qute/qtcore"
	"github.com/nomad-software/qute/qtgui"
	"github.com/nomad-software/qute/qtwidgets"

	"runtime"
	"testing"

	"github.com/nomad-software/triggerapi/internal/settings"
)

// windowColour returns the colour of windows in the palette of the
// application.
func windowColour() string {
	palette := qtgui.QGuiApplicationPalette()
	defer runtime.KeepAlive(palette)
	return palette.GroupColor(qtgui.QPaletteActive, qtgui.QPaletteWindow).Name()
}

// styleBase returns the name of the style a style is: that of the style a
// proxy style is over, or its own.
func styleBase(style *qtwidgets.QStyle) string {
	if style.Inherits("QProxyStyle") {
		return qtwidgets.QProxyStyleCast(style).BaseStyle().Name()
	}
	return style.Name()
}

// useTheme puts the theme into effect until the test ends, when the
// system's is put back.
func useTheme(t *testing.T, name string) {
	t.Helper()
	qute.PostSync(func() { applyTheme(name) })
	t.Cleanup(func() { qute.PostSync(func() { applyTheme("") }) })
}

// boxEdge returns how much the edge of the box of a check box that is not
// checked stands out from the box: the most that the lightness of a pixel
// of the box, edge and all, differs from that of the box within it.
func boxEdge() int {
	box := qtwidgets.NewQCheckBoxWithText("Ticked", nil)
	defer box.Delete()
	box.AdjustSize()
	pixmap := box.Grab()
	img := pixmap.ToImage()
	runtime.KeepAlive(pixmap)
	palette := qtgui.QGuiApplicationPalette()
	base := palette.GroupColor(qtgui.QPaletteActive, qtgui.QPaletteBase).Lightness()
	runtime.KeepAlive(palette)
	// The box is at the left, before the space and the text.
	width := box.Style().PixelMetricFull(qtwidgets.QStylePM_IndicatorWidth, nil, box.AsQWidget())
	most := 0
	for y := range img.Height() {
		for x := range width {
			most = max(most, abs(img.PixelColor(x, y).Lightness()-base))
		}
	}
	return most
}

// abs returns the size of n.
func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// TestCheckBoxEdges checks that the boxes of check boxes have an edge that
// shows in the light and dark themes, which Fusion's own barely does.
func TestCheckBoxEdges(t *testing.T) {
	for _, theme := range []string{settings.ThemeLight, settings.ThemeDark} {
		useTheme(t, theme)
		qute.PostSync(func() {
			if got := boxEdge(); got < 80 {
				t.Errorf("%s: the edge of a check box stands out from it by %d of 255", theme, got)
			}
		})
	}
}

// TestThemes checks the light and dark themes, and that the system's is put
// back as it was.
func TestThemes(t *testing.T) {
	var system string
	qute.PostSync(func() { system = windowColour() })

	useTheme(t, settings.ThemeDark)
	qute.PostSync(func() {
		if got := styleBase(qtwidgets.QApplicationStyle()); got != "fusion" {
			t.Errorf("style = %q", got)
		}
		if got := windowColour(); got != "#2d2d2d" {
			t.Errorf("dark window = %s", got)
		}
		if got := defaultJSONColours()[tokenKey]; got != jsonDark[tokenKey] {
			t.Errorf("JSON keys in the dark theme are %s", got)
		}
	})

	qute.PostSync(func() { applyTheme(settings.ThemeLight) })
	qute.PostSync(func() {
		// The light theme's own colours, not Fusion's standard
		// palette, which is dark where the desktop's colour scheme is.
		if got := windowColour(); got != "#efefef" {
			t.Errorf("light window = %s", got)
		}
		if got := defaultJSONColours()[tokenKey]; got != jsonLight[tokenKey] {
			t.Errorf("JSON keys in the light theme are %s", got)
		}
	})

	qute.PostSync(func() { applyTheme("") })
	qute.PostSync(func() {
		if got := qtwidgets.QApplicationStyle().Name(); got != systemStyle {
			t.Errorf("style = %q, not the system's %q", got, systemStyle)
		}
		if got := windowColour(); got != system {
			t.Errorf("system window = %s, not %s as it was", got, system)
		}
	})
}

// TestThemesAreWhole checks that each theme sets every colour Fusion draws
// with, light ones in the light theme and dark in the dark, so that none is
// left to the desktop, whose colour scheme may be the other.
func TestThemesAreWhole(t *testing.T) {
	roles := []qtgui.QPaletteColorRole{
		qtgui.QPaletteWindow, qtgui.QPaletteWindowText, qtgui.QPaletteBase, qtgui.QPaletteAlternateBase,
		qtgui.QPaletteText, qtgui.QPalettePlaceholderText, qtgui.QPaletteButton, qtgui.QPaletteButtonText,
		qtgui.QPaletteBrightText, qtgui.QPaletteToolTipBase, qtgui.QPaletteToolTipText, qtgui.QPaletteHighlight,
		qtgui.QPaletteHighlightedText, qtgui.QPaletteLink, qtgui.QPaletteLinkVisited,
		qtgui.QPaletteLight, qtgui.QPaletteMidlight, qtgui.QPaletteMid, qtgui.QPaletteDark, qtgui.QPaletteShadow,
	}
	lightnessOf := func(colour string) int {
		return qcolour(colour).Lightness()
	}
	for name, cs := range map[string]colours{"light": lightColours, "dark": darkColours} {
		for _, role := range roles {
			if _, ok := cs.roles[role]; !ok {
				t.Errorf("%s does not set role %d", name, role)
			}
		}
		// The page and the window are light in the light theme and dark
		// in the dark, with text that stands out on them.
		qute.PostSync(func() {
			for _, pair := range [][2]qtgui.QPaletteColorRole{
				{qtgui.QPaletteWindow, qtgui.QPaletteWindowText},
				{qtgui.QPaletteBase, qtgui.QPaletteText},
				{qtgui.QPaletteButton, qtgui.QPaletteButtonText},
			} {
				back, text := lightnessOf(cs.roles[pair[0]]), lightnessOf(cs.roles[pair[1]])
				if light := back > 128; light != (name == "light") {
					t.Errorf("%s: background %s is the wrong way", name, cs.roles[pair[0]])
				}
				if d := back - text; d < 128 && d > -128 {
					t.Errorf("%s: text %s does not stand out on %s", name, cs.roles[pair[1]], cs.roles[pair[0]])
				}
			}
		})
	}
}

// TestThemeSetting checks that the theme is offered on the User interface
// tab of the settings, is put into effect at once, with what is drawn in
// colours made from the palette drawn again, is kept, and is put into
// effect when the program starts.
func TestThemeSetting(t *testing.T) {
	t.Cleanup(func() { qute.PostSync(func() { applyTheme("") }) })
	files := testFiles(t)

	qute.PostSync(func() {
		w := NewWindow(files.Files)
		w.Show()

		d := newSettingsDialog(nil, w.config, files.Files)
		defer d.DeleteLater()
		// The system's is offered first, and is the one in effect.
		if d.theme.CurrentText() != "Follow the system" || d.theme.Count() != len(themes) {
			t.Errorf("theme = %q of %d", d.theme.CurrentText(), d.theme.Count())
		}
		if !d.tabs.Widget(settingsTabInterface).IsAncestorOf(d.theme.AsQWidget()) {
			t.Error("the theme is not on the User interface tab")
		}

		d.theme.SetCurrentIndex(themeIndex(settings.ThemeDark))
		if err := w.applySettings(d.settings(w.config)); err != nil {
			t.Fatal(err)
		}
		if got := windowColour(); got != "#2d2d2d" {
			t.Errorf("window = %s", got)
		}
		if got := formatColour(w.response.formats[tokenKey]); got != jsonDark[tokenKey] {
			t.Errorf("JSON keys of a response are %s", got)
		}
		// The palettes of the widgets follow once the change, which is
		// posted to them, is delivered.
		qtcore.QCoreApplicationProcessEvents()
		// The selection of a response looks the same without the keyboard
		// as with it, in the new palette.
		palette := w.response.body.Palette()
		active, inactive := palette.GroupColor(qtgui.QPaletteActive, qtgui.QPaletteHighlight).Name(), palette.GroupColor(qtgui.QPaletteInactive, qtgui.QPaletteHighlight).Name()
		runtime.KeepAlive(palette)
		if active != darkColours.roles[qtgui.QPaletteHighlight] || inactive != active {
			t.Errorf("the selection of a response is %s, and %s without the keyboard", active, inactive)
		}
		// The lists of the table of assertions are in the theme's style.
		if got := styleBase(paddedComboStyle()); got != "fusion" {
			t.Errorf("the lists of assertions are in %q", got)
		}
		if saved := settings.Load(files.Settings); saved.Theme != settings.ThemeDark {
			t.Errorf("saved %q", saved.Theme)
		}
		again := newSettingsDialog(nil, w.config, files.Files)
		defer again.DeleteLater()
		if again.theme.CurrentText() != "Dark" {
			t.Errorf("theme = %q", again.theme.CurrentText())
		}
	})

	// A window started with the setting is dark from the start.
	qute.PostSync(func() { applyTheme("") })
	qute.PostSync(func() {
		NewWindow(files.Files)
		if got := windowColour(); got != "#2d2d2d" {
			t.Errorf("a window started dark has a window of %s", got)
		}
	})
}

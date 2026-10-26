package ui

import (
	"runtime"
	"testing"

	qt "github.com/mappu/miqt/qt6"
	"github.com/mappu/miqt/qt6/mainthread"

	"github.com/nomad-software/triggerapi/internal/settings"
)

// windowColour returns the colour of windows in the palette of the
// application.
func windowColour() string {
	palette := qt.QGuiApplication_Palette()
	defer runtime.KeepAlive(palette)
	return palette.Color(qt.QPalette__Active, qt.QPalette__Window).Name()
}

// styleBase returns the name of the style a style is: that of the style a
// proxy style is over, or its own.
func styleBase(style *qt.QStyle) string {
	if style.Inherits("QProxyStyle") {
		return qt.UnsafeNewQProxyStyle(style.UnsafePointer()).BaseStyle().Name()
	}
	return style.Name()
}

// useTheme puts the theme into effect until the test ends, when the
// system's is put back.
func useTheme(t *testing.T, name string) {
	t.Helper()
	mainthread.Wait(func() { applyTheme(name) })
	t.Cleanup(func() { mainthread.Wait(func() { applyTheme("") }) })
}

// boxEdge returns how much the edge of the box of a check box that is not
// checked stands out from the box: the most that the lightness of a pixel
// of the box, edge and all, differs from that of the box within it.
func boxEdge() int {
	box := qt.NewQCheckBox3("Ticked")
	defer box.Delete()
	box.AdjustSize()
	pixmap := box.Grab()
	img := pixmap.ToImage()
	runtime.KeepAlive(pixmap)
	palette := qt.QGuiApplication_Palette()
	base := palette.Color(qt.QPalette__Active, qt.QPalette__Base).Lightness()
	runtime.KeepAlive(palette)
	// The box is at the left, before the space and the text.
	width := box.Style().PixelMetric(qt.QStyle__PM_IndicatorWidth, nil, box.QWidget)
	most := 0
	for y := range img.Height() {
		for x := range width {
			c := qt.NewQColor4(img.Pixel(x, y))
			most = max(most, abs(c.Lightness()-base))
			c.Delete()
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
		mainthread.Wait(func() {
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
	mainthread.Wait(func() { system = windowColour() })

	useTheme(t, settings.ThemeDark)
	mainthread.Wait(func() {
		if got := styleBase(qt.QApplication_Style()); got != "fusion" {
			t.Errorf("style = %q", got)
		}
		if got := windowColour(); got != "#2d2d2d" {
			t.Errorf("dark window = %s", got)
		}
		if got := defaultJSONColours()[tokenKey]; got != jsonDark[tokenKey] {
			t.Errorf("JSON keys in the dark theme are %s", got)
		}
	})

	mainthread.Wait(func() { applyTheme(settings.ThemeLight) })
	mainthread.Wait(func() {
		// The light theme's own colours, not Fusion's standard
		// palette, which is dark where the desktop's colour scheme is.
		if got := windowColour(); got != "#efefef" {
			t.Errorf("light window = %s", got)
		}
		if got := defaultJSONColours()[tokenKey]; got != jsonLight[tokenKey] {
			t.Errorf("JSON keys in the light theme are %s", got)
		}
	})

	mainthread.Wait(func() { applyTheme("") })
	mainthread.Wait(func() {
		if got := qt.QApplication_Style().Name(); got != systemStyle {
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
	roles := []qt.QPalette__ColorRole{
		qt.QPalette__Window, qt.QPalette__WindowText, qt.QPalette__Base, qt.QPalette__AlternateBase,
		qt.QPalette__Text, qt.QPalette__PlaceholderText, qt.QPalette__Button, qt.QPalette__ButtonText,
		qt.QPalette__BrightText, qt.QPalette__ToolTipBase, qt.QPalette__ToolTipText, qt.QPalette__Highlight,
		qt.QPalette__HighlightedText, qt.QPalette__Link, qt.QPalette__LinkVisited,
		qt.QPalette__Light, qt.QPalette__Midlight, qt.QPalette__Mid, qt.QPalette__Dark, qt.QPalette__Shadow,
	}
	lightness := func(colour string) int {
		c := qt.NewQColor6(colour)
		defer c.Delete()
		return c.Lightness()
	}
	for name, cs := range map[string]colours{"light": lightColours, "dark": darkColours} {
		for _, role := range roles {
			if _, ok := cs.roles[role]; !ok {
				t.Errorf("%s does not set role %d", name, role)
			}
		}
		// The page and the window are light in the light theme and dark
		// in the dark, with text that stands out on them.
		mainthread.Wait(func() {
			for _, pair := range [][2]qt.QPalette__ColorRole{
				{qt.QPalette__Window, qt.QPalette__WindowText},
				{qt.QPalette__Base, qt.QPalette__Text},
				{qt.QPalette__Button, qt.QPalette__ButtonText},
			} {
				back, text := lightness(cs.roles[pair[0]]), lightness(cs.roles[pair[1]])
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
	t.Cleanup(func() { mainthread.Wait(func() { applyTheme("") }) })
	files := testFiles(t)

	mainthread.Wait(func() {
		w := NewWindow(files.Files)
		w.Show()

		d := newSettingsDialog(nil, w.config, files.Files)
		defer d.DeleteLater()
		// The system's is offered first, and is the one in effect.
		if d.theme.CurrentText() != "Follow the system" || d.theme.Count() != len(themes) {
			t.Errorf("theme = %q of %d", d.theme.CurrentText(), d.theme.Count())
		}
		if !d.tabs.Widget(settingsTabInterface).IsAncestorOf(d.theme.QWidget) {
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
		qt.QCoreApplication_ProcessEvents()
		// The selection of a response looks the same without the keyboard
		// as with it, in the new palette.
		palette := w.response.body.Palette()
		active, inactive := palette.Color(qt.QPalette__Active, qt.QPalette__Highlight).Name(), palette.Color(qt.QPalette__Inactive, qt.QPalette__Highlight).Name()
		runtime.KeepAlive(palette)
		if active != darkColours.roles[qt.QPalette__Highlight] || inactive != active {
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
	mainthread.Wait(func() { applyTheme("") })
	mainthread.Wait(func() {
		NewWindow(files.Files)
		if got := windowColour(); got != "#2d2d2d" {
			t.Errorf("a window started dark has a window of %s", got)
		}
	})
}

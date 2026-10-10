package ui

import (
	"fmt"
	"strings"

	qt "github.com/mappu/miqt/qt6"

	"github.com/nomad-software/triggerapi/internal/settings"
)

// settingsDialog edits the settings the user can choose.
type settingsDialog struct {
	*qt.QDialog

	font       *qt.QFontComboBox
	workspaces *qt.QLineEdit
	buttons    *qt.QDialogButtonBox

	// colours are the JSON colours the user has chosen. One that is empty
	// is the default, which is what its swatch shows.
	colours  settings.JSONColours
	swatches map[tokenKind]*qt.QPushButton

	// initialFont is the family the font selector started on. It can differ
	// from the setting if that font is not installed.
	initialFont string
	defaults    Files
}

// newSettingsDialog creates the dialog showing the current settings. The
// defaults are the locations used when the settings do not name one.
func newSettingsDialog(parent *qt.QWidget, current settings.Settings, defaults Files) *settingsDialog {
	d := &settingsDialog{
		QDialog:    qt.NewQDialog(parent),
		font:       qt.NewQFontComboBox2(),
		workspaces: qt.NewQLineEdit2(),
		buttons:    qt.NewQDialogButtonBox4(qt.QDialogButtonBox__Ok | qt.QDialogButtonBox__Cancel),
		defaults:   defaults,
		colours:    current.JSONColours,
		swatches:   map[tokenKind]*qt.QPushButton{},
	}
	d.SetWindowTitle("Settings")
	d.SetMinimumWidth(560)

	d.font.SetCurrentFont(qt.NewQFont2(current.ResponseFont))
	d.initialFont = d.font.CurrentFont().Family()

	d.workspaces.SetText(orDefault(current.WorkspacesDir, defaults.Workspaces))

	note := qt.NewQLabel3("Each workspace has a folder of its own in this one, holding its environments and requests. " +
		"Choosing a folder that already has workspaces shows those. " +
		"Choosing one that has none copies the current workspaces there. " +
		"A relative path is relative to the directory TriggerAPI is run from.")
	note.SetWordWrap(true)
	note.SetEnabled(false)

	form := qt.NewQFormLayout2()
	form.AddRow3("Response font", d.font.QWidget)
	form.AddRow4("Workspaces folder", d.folderRow(d.workspaces, "Workspaces Folder"))
	// The note is about the folder above it.
	form.AddRowWithWidget(note.QWidget)

	for _, t := range tokenKinds {
		swatch := qt.NewQPushButton2()
		swatch.SetToolTip("Choose the colour")
		swatch.OnClicked(func() { d.pickColour(t.kind, t.label) })
		d.swatches[t.kind] = swatch
		form.AddRow3("JSON "+strings.ToLower(t.label[:1])+t.label[1:], swatch.QWidget)
		d.showColour(t.kind)
	}

	reset := qt.NewQPushButton3("Use Default Colours")
	reset.OnClicked(d.resetColours)
	resetRow := qt.NewQHBoxLayout2()
	resetRow.AddWidget(reset.QWidget)
	resetRow.AddStretch()
	form.AddRow4("", resetRow.QLayout)

	d.buttons.OnAccepted(d.Accept)
	d.buttons.OnRejected(d.Reject)

	layout := qt.NewQVBoxLayout(d.QWidget)
	layout.AddLayout(form.QLayout)
	layout.AddStretch()
	layout.AddWidget(d.buttons.QWidget)

	return d
}

// folderRow lays out a path field beside a button to browse for the folder.
func (d *settingsDialog) folderRow(edit *qt.QLineEdit, caption string) *qt.QLayout {
	browse := qt.NewQPushButton3("Browse…")
	browse.OnClicked(func() {
		if dir := qt.QFileDialog_GetExistingDirectory3(d.QWidget, caption, edit.Text()); dir != "" {
			edit.SetText(dir)
		}
	})

	row := qt.NewQHBoxLayout2()
	row.AddWidget2(edit.QWidget, 1)
	row.AddWidget(browse.QWidget)
	return row.QLayout
}

// colour returns the colour shown for a kind of token: the one chosen, or
// the default if none has been.
func (d *settingsDialog) colour(kind tokenKind) string {
	return jsonColours(d.colours)[kind]
}

// showColour fills the swatch of a kind of token with its colour and names
// the colour on it, in black or white, whichever can be read against it.
func (d *settingsDialog) showColour(kind tokenKind) {
	colour := d.colour(kind)

	text := "#ffffff"
	if qt.NewQColor6(colour).Lightness() > 140 {
		text = "#000000"
	}

	swatch := d.swatches[kind]
	swatch.SetText(colour)
	swatch.SetStyleSheet(fmt.Sprintf(
		"QPushButton { background-color: %s; color: %s; border: 1px solid #888888; border-radius: 3px; padding: 4px 12px; }",
		colour, text,
	))
}

// setColour chooses the colour for a kind of token. Choosing the default
// colour is the same as not choosing, so that it still follows the theme.
func (d *settingsDialog) setColour(kind tokenKind, colour string) {
	colour = strings.ToLower(colour)
	if colour == defaultJSONColours()[kind] {
		colour = ""
	}
	*customColour(&d.colours, kind) = colour
	d.showColour(kind)
}

// pickColour lets the user choose the colour for a kind of token.
func (d *settingsDialog) pickColour(kind tokenKind, label string) {
	chosen := qt.QColorDialog_GetColor3(qt.NewQColor6(d.colour(kind)), d.QWidget, "Colour of "+label)
	if chosen.IsValid() {
		d.setColour(kind, chosen.Name())
	}
}

// resetColours goes back to the default colour for every kind of token.
func (d *settingsDialog) resetColours() {
	d.colours = settings.JSONColours{}
	for kind := range d.swatches {
		d.showColour(kind)
	}
}

func orDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

// settings returns base with the values in the dialog. A location left empty
// or at its default is not recorded, so it keeps following the default. The
// font is only changed if another was picked, so that a font which is not
// installed is not replaced just by opening the dialog.
func (d *settingsDialog) settings(base settings.Settings) settings.Settings {
	if family := d.font.CurrentFont().Family(); family != d.initialFont {
		base.ResponseFont = family
	}

	location := func(edit *qt.QLineEdit, fallback string) string {
		if path := strings.TrimSpace(edit.Text()); path != fallback {
			return path
		}
		return ""
	}
	base.WorkspacesDir = location(d.workspaces, d.defaults.Workspaces)
	base.JSONColours = d.colours

	return base
}

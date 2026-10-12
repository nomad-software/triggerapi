package ui

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	qt "github.com/mappu/miqt/qt6"

	"github.com/nomad-software/triggerapi/internal/settings"
)

// settingsDialog edits the settings the user can choose, on a tab for each
// sort: General (settingsTabGeneral), Requests (settingsTabRequests) and
// Colours (settingsTabColours).
type settingsDialog struct {
	*qt.QDialog

	tabs *qt.QTabWidget
	font *qt.QFontComboBox
	// timeout is how long a request can take, in seconds, which is zero
	// for no limit, and redirects whether a REST request follows them.
	timeout    *qt.QSpinBox
	redirects  *qt.QCheckBox
	workspaces *qt.QLineEdit
	// publicKey and privateKey are the paths of the files of the SSH keys
	// the saved files are encrypted with.
	publicKey  *qt.QLineEdit
	privateKey *qt.QLineEdit
	buttons    *qt.QDialogButtonBox

	// colours and jsonColours are the colours the user has chosen. One
	// that is empty is the default, which is what its swatch shows.
	colours     settings.Colours
	jsonColours settings.JSONColours
	swatches    map[colourKind]*swatch
	json        map[tokenKind]*swatch

	// initialFont is the family the font selector started on. It can differ
	// from the setting if that font is not installed.
	initialFont string
	defaults    Files
}

// The tabs of the settings dialog.
const (
	settingsTabGeneral = iota
	settingsTabRequests
	settingsTabColours
)

// tabMargin is the space between the edge of a tab of the settings dialog
// and what is on it.
const tabMargin = 20

// newSettingsDialog creates the dialog showing the current settings. The
// defaults are the locations used when the settings do not name one.
func newSettingsDialog(parent *qt.QWidget, current settings.Settings, defaults Files) *settingsDialog {
	d := &settingsDialog{
		QDialog:     qt.NewQDialog(parent),
		tabs:        qt.NewQTabWidget2(),
		font:        qt.NewQFontComboBox2(),
		timeout:     qt.NewQSpinBox2(),
		redirects:   qt.NewQCheckBox3("Follow redirects"),
		workspaces:  qt.NewQLineEdit2(),
		publicKey:   qt.NewQLineEdit2(),
		privateKey:  qt.NewQLineEdit2(),
		buttons:     qt.NewQDialogButtonBox4(qt.QDialogButtonBox__Ok | qt.QDialogButtonBox__Cancel),
		defaults:    defaults,
		colours:     current.Colours,
		jsonColours: current.JSONColours,
		swatches:    map[colourKind]*swatch{},
		json:        map[tokenKind]*swatch{},
	}
	d.SetWindowTitle("Settings")
	d.SetMinimumWidth(620)

	d.font.SetCurrentFont(qt.NewQFont2(current.ResponseFont))
	d.initialFont = d.font.CurrentFont().Family()

	d.timeout.SetRange(0, settings.MaxTimeout)
	d.timeout.SetSuffix(" s")
	// Zero is shown as what it means.
	d.timeout.SetSpecialValueText("No limit")
	d.timeout.SetValue(current.Timeout)
	d.timeout.SetToolTip("How long a request can take before it is given up on")
	d.redirects.SetChecked(!current.NoRedirects)
	d.redirects.SetToolTip("Send a REST request again to the address a response redirects it to. " +
		"Without this, the response which redirects is the one shown.")

	d.workspaces.SetText(orDefault(current.WorkspacesDir, defaults.Workspaces))

	note := qt.NewQLabel3("Each workspace has a folder of its own in this one, holding its environments and requests. " +
		"Choosing a folder that already has workspaces shows those. " +
		"Choosing one that has none copies the current workspaces there. " +
		"A relative path is relative to the directory TriggerAPI is run from.")
	note.SetWordWrap(true)
	note.SetEnabled(false)

	d.publicKey.SetText(current.PublicKey)
	d.publicKey.SetPlaceholderText("Such as ~/.ssh/id_ed25519.pub")
	d.privateKey.SetText(current.PrivateKey)
	d.privateKey.SetPlaceholderText("Such as ~/.ssh/id_ed25519")

	keysNote := qt.NewQLabel3("The environments and requests of every workspace are encrypted with this pair of SSH keys, " +
		"which can be Ed25519 or RSA. TriggerAPI needs them to work: without them nothing is loaded or saved. " +
		"Changing the keys encrypts every file again with the new ones. " +
		"The passphrase of a private key that has one is asked for when TriggerAPI starts.")
	keysNote.SetWordWrap(true)
	keysNote.SetEnabled(false)

	encryption := sectionHeading("Encryption")
	encryption.SetContentsMargins(0, 12, 0, 0)

	general := qt.NewQWidget2()
	form := qt.NewQFormLayout(general)
	form.SetContentsMargins(tabMargin, tabMargin, tabMargin, tabMargin)
	form.AddRow3("Code font", d.font.QWidget)
	form.AddRow4("Workspaces folder", d.folderRow(d.workspaces, "Workspaces Folder"))
	// The note is about the folder above it.
	form.AddRowWithWidget(note.QWidget)
	form.AddRowWithWidget(encryption.QWidget)
	form.AddRow4("Public key", d.fileRow(d.publicKey, "Public Key"))
	form.AddRow4("Private key", d.fileRow(d.privateKey, "Private Key"))
	form.AddRowWithWidget(keysNote.QWidget)

	requests := qt.NewQWidget2()
	requestsForm := qt.NewQFormLayout(requests)
	requestsForm.SetContentsMargins(tabMargin, tabMargin, tabMargin, tabMargin)
	requestsForm.AddRow3("Timeout", d.timeout.QWidget)
	requestsForm.AddRow3("", d.redirects.QWidget)

	colours := qt.NewQWidget2()
	coloursLayout := qt.NewQVBoxLayout(colours)
	coloursLayout.SetContentsMargins(tabMargin, tabMargin, tabMargin, tabMargin)
	coloursLayout.AddLayout(d.colourGrid().QLayout)
	coloursLayout.AddStretch()

	reset := qt.NewQPushButton3("Use Default Colours")
	reset.OnClicked(d.resetColours)
	resetRow := qt.NewQHBoxLayout2()
	resetRow.AddStretch()
	resetRow.AddWidget(reset.QWidget)
	coloursLayout.AddLayout(resetRow.QLayout)

	// The order matches the tab constants.
	d.tabs.AddTab(general, "General")
	d.tabs.AddTab(requests, "Requests")
	d.tabs.AddTab(colours, "Colours")

	d.buttons.OnAccepted(d.Accept)
	d.buttons.OnRejected(d.Reject)

	layout := qt.NewQVBoxLayout(d.QWidget)
	layout.AddWidget2(d.tabs.QWidget, 1)
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

// fileRow lays out a path field beside a button to browse for the file,
// which starts in the folder of the path there is, or where SSH keys are
// kept.
func (d *settingsDialog) fileRow(edit *qt.QLineEdit, caption string) *qt.QLayout {
	browse := qt.NewQPushButton3("Browse…")
	browse.OnClicked(func() {
		start := strings.TrimSpace(edit.Text())
		if start == "" {
			start = filepath.Join(qt.QDir_HomePath(), ".ssh")
		}
		if path := qt.QFileDialog_GetOpenFileName3(d.QWidget, caption, start); path != "" {
			edit.SetText(path)
		}
	})

	row := qt.NewQHBoxLayout2()
	row.AddWidget2(edit.QWidget, 1)
	row.AddWidget(browse.QWidget)
	return row.QLayout
}

// sectionHeading returns the heading of a section of a tab.
func sectionHeading(title string) *qt.QLabel {
	label := qt.NewQLabel3(title)
	font := label.Font()
	font.SetBold(true)
	label.SetFont(font)
	return label
}

// colourSections are the colours as they are set out on the Colours tab: in
// two columns of sections, each a heading above the colours of one sort.
// Those of JSON are a section of their own, after the last of these.
var colourSections = [2][]struct {
	title string
	kinds []colourKind
}{
	{
		{"Sidebar", []colourKind{colourWorkspaces, colourEnvironments, colourRequests}},
		{"Request types", []colourKind{colourREST, colourGRPC}},
		{"Buttons", []colourKind{colourSend, colourAction, colourRemove}},
	},
	{
		{"Text", []colourKind{colourFound, colourNotFound, colourError, colourMatch}},
		{"Capture", []colourKind{colourCaptured, colourNotCaptured}},
	},
}

// swatchWidth is the width of every swatch, so that they line up.
const swatchWidth = 110

// colourGrid lays out the swatches of every colour. Each of the two columns
// of sections is a column of names and one of swatches, with a gap between
// the two.
func (d *settingsDialog) colourGrid() *qt.QGridLayout {
	const gap = 2

	grid := qt.NewQGridLayout2()
	grid.SetHorizontalSpacing(12)
	grid.SetVerticalSpacing(6)
	// The names are as wide as they need to be, and what width is left
	// over goes between the two columns of sections.
	grid.SetColumnMinimumWidth(gap, 32)
	grid.SetColumnStretch(gap, 1)

	// rows counts the rows used in each of the two columns of sections.
	var rows [2]int
	heading := func(side int, title string) {
		label := sectionHeading(title)
		if rows[side] > 0 {
			// A section stands apart from the one above it.
			label.SetContentsMargins(0, 12, 0, 0)
		}
		grid.AddWidget3(label.QWidget, rows[side], side*(gap+1), 1, 2)
		rows[side]++
	}
	row := func(side int, name string, s *swatch) {
		label := qt.NewQLabel3(name)
		// The names are set in from the heading they are under.
		label.SetContentsMargins(12, 0, 0, 0)
		grid.AddWidget2(label.QWidget, rows[side], side*(gap+1))
		grid.AddWidget2(s.button.QWidget, rows[side], side*(gap+1)+1)
		rows[side]++
	}

	for side, sections := range colourSections {
		for _, section := range sections {
			heading(side, section.title)
			for _, k := range colourKinds {
				if !slices.Contains(section.kinds, k.kind) {
					continue
				}
				s := d.newSwatch(k.label, customIconColour(&d.colours, k.kind), func() string { return k.fallback })
				d.swatches[k.kind] = s
			}
			// The colours are in the order the section gives them.
			for _, kind := range section.kinds {
				row(side, colourLabel(kind), d.swatches[kind])
			}
		}
	}

	heading(1, "JSON")
	for _, t := range tokenKinds {
		s := d.newSwatch("JSON "+strings.ToLower(t.label[:1])+t.label[1:], customColour(&d.jsonColours, t.kind), func() string { return defaultJSONColours()[t.kind] })
		d.json[t.kind] = s
		row(1, t.label, s)
	}

	return grid
}

// swatch is a button which shows one of the colours and lets the user choose
// another for it.
type swatch struct {
	button *qt.QPushButton
	// chosen is where the colour the user has chosen is kept, which is
	// empty for the default.
	chosen *string
	// fallback returns the default colour.
	fallback func() string
}

// newSwatch returns a swatch for the colour kept at chosen, which is called
// label.
func (d *settingsDialog) newSwatch(label string, chosen *string, fallback func() string) *swatch {
	s := &swatch{button: qt.NewQPushButton2(), chosen: chosen, fallback: fallback}
	s.button.SetFixedWidth(swatchWidth)
	s.button.SetToolTip("Choose the colour")
	s.button.OnClicked(func() {
		picked := qt.QColorDialog_GetColor3(qt.NewQColor6(s.colour()), d.QWidget, "Colour of "+label)
		if picked.IsValid() {
			s.set(picked.Name())
		}
	})
	s.show()
	return s
}

// colour returns the colour shown: the one chosen, or the default if none
// has been.
func (s *swatch) colour() string {
	return orDefault(*s.chosen, s.fallback())
}

// show fills the swatch with its colour and names the colour on it, in
// black or white, whichever can be read against it.
func (s *swatch) show() {
	colour := s.colour()

	text := "#ffffff"
	if qt.NewQColor6(colour).Lightness() > 140 {
		text = "#000000"
	}

	s.button.SetText(colour)
	s.button.SetStyleSheet(fmt.Sprintf(
		"QPushButton { background-color: %s; color: %s; border: 1px solid #888888; border-radius: 3px; padding: 4px 0; }",
		colour, text,
	))
}

// set chooses the colour. Choosing the default colour is the same as not
// choosing, so that it still follows the default, which for JSON depends on
// the theme.
func (s *swatch) set(colour string) {
	colour = strings.ToLower(colour)
	if colour == s.fallback() {
		colour = ""
	}
	*s.chosen = colour
	s.show()
}

// resetColours goes back to the default for every colour.
func (d *settingsDialog) resetColours() {
	d.colours = settings.Colours{}
	d.jsonColours = settings.JSONColours{}
	for _, s := range d.swatches {
		s.show()
	}
	for _, s := range d.json {
		s.show()
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
	base.Timeout = d.timeout.Value()
	base.NoRedirects = !d.redirects.IsChecked()
	base.WorkspacesDir = location(d.workspaces, d.defaults.Workspaces)
	base.PublicKey = strings.TrimSpace(d.publicKey.Text())
	base.PrivateKey = strings.TrimSpace(d.privateKey.Text())
	base.JSONColours = d.jsonColours
	base.Colours = d.colours

	return base
}

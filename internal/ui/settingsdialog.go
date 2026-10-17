package ui

import (
	"fmt"
	"path/filepath"
	"strings"

	qt "github.com/mappu/miqt/qt6"

	"github.com/nomad-software/triggerapi/internal/settings"
)

// settingsDialog edits the settings the user can choose, on a tab for each
// sort: Workspaces (settingsTabWorkspaces), Requests (settingsTabRequests),
// History (settingsTabHistory) and User interface (settingsTabInterface).
type settingsDialog struct {
	*qt.QDialog

	tabs *qt.QTabWidget
	font *qt.QFontComboBox
	// timeout is how long a request can take, in seconds, which is zero
	// for no limit, and redirects whether a REST request follows them.
	timeout   *qt.QSpinBox
	redirects *qt.QCheckBox
	// history is whether the requests sent are kept in the history of the
	// workspace, and historyLimit how many are.
	history      *qt.QCheckBox
	historyLimit *qt.QSpinBox
	// diffCommand is the command the responses of the history are compared
	// with.
	diffCommand *qt.QLineEdit
	workspaces  *qt.QLineEdit
	// publicKey and privateKey are the paths of the files of the SSH keys
	// the saved files are encrypted with.
	publicKey  *qt.QLineEdit
	privateKey *qt.QLineEdit
	// exportButton and importButton write the workspaces to an archive
	// for someone else and add those of an archive, by calling onExport
	// and onImport, which are given the dialog to be the parent of theirs.
	exportButton *qt.QPushButton
	importButton *qt.QPushButton
	onExport     func(parent *qt.QWidget)
	onImport     func(parent *qt.QWidget)
	buttons      *qt.QDialogButtonBox

	// colours and jsonColours are the colours the user has chosen. One
	// that is empty is the default, which is what its swatch shows.
	colours     settings.Colours
	jsonColours settings.JSONColours
	swatches    map[colourKind]*swatch
	json        map[tokenKind]*swatch
	// colourList is the table the swatches are listed in.
	colourList *qt.QTableWidget

	// initialFont is the family the font selector started on. It can differ
	// from the setting if that font is not installed.
	initialFont string
	defaults    Files
}

// The tabs of the settings dialog.
const (
	settingsTabWorkspaces = iota
	settingsTabRequests
	settingsTabHistory
	settingsTabInterface
)

// tabMargin is the space between the edge of a tab of the settings dialog
// and what is on it.
const tabMargin = 20

// colourTableHeight is the least height of the table of colours, which
// scrolls to show the rest.
const colourTableHeight = 300

// newSettingsDialog creates the dialog showing the current settings. The
// defaults are the locations used when the settings do not name one.
func newSettingsDialog(parent *qt.QWidget, current settings.Settings, defaults Files) *settingsDialog {
	d := &settingsDialog{
		QDialog:      qt.NewQDialog(parent),
		tabs:         qt.NewQTabWidget2(),
		font:         qt.NewQFontComboBox2(),
		timeout:      qt.NewQSpinBox2(),
		redirects:    qt.NewQCheckBox3("Follow redirects"),
		history:      qt.NewQCheckBox3("Enabled"),
		historyLimit: qt.NewQSpinBox2(),
		diffCommand:  qt.NewQLineEdit2(),
		workspaces:   qt.NewQLineEdit2(),
		publicKey:    qt.NewQLineEdit2(),
		privateKey:   qt.NewQLineEdit2(),
		exportButton: qt.NewQPushButton3("Export…"),
		importButton: qt.NewQPushButton3("Import…"),
		buttons:      qt.NewQDialogButtonBox4(qt.QDialogButtonBox__Ok | qt.QDialogButtonBox__Cancel),
		defaults:     defaults,
		colours:      current.Colours,
		jsonColours:  current.JSONColours,
		swatches:     map[colourKind]*swatch{},
		json:         map[tokenKind]*swatch{},
	}
	d.SetWindowTitle("Settings")
	d.SetMinimumWidth(680)

	d.font.SetCurrentFont(qt.NewQFont2(current.ResponseFont))
	d.initialFont = d.font.CurrentFont().Family()

	d.timeout.SetRange(0, settings.MaxTimeout)
	d.timeout.SetSuffix(" s")
	// Zero is shown as what it means.
	d.timeout.SetSpecialValueText("No limit")
	d.timeout.SetValue(current.Timeout)
	d.redirects.SetChecked(!current.NoRedirects)
	d.history.SetChecked(!current.NoHistory)
	d.historyLimit.SetRange(1, settings.MaxHistoryLimit)
	d.historyLimit.SetSuffix(" requests")
	d.historyLimit.SetValue(current.History())
	d.historyLimit.SetEnabled(d.history.IsChecked())
	d.history.OnToggled(d.historyLimit.SetEnabled)
	d.diffCommand.SetText(current.DiffCommand)
	d.diffCommand.SetPlaceholderText("Such as meld, kdiff3 or code --diff --wait")

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

	sharing := sectionHeading("Import and export")
	sharing.SetContentsMargins(0, 12, 0, 0)

	d.exportButton.OnClicked(func() {
		if d.onExport != nil {
			d.onExport(d.QWidget)
		}
	})
	d.importButton.OnClicked(func() {
		if d.onImport != nil {
			d.onImport(d.QWidget)
		}
	})
	sharingRow := qt.NewQHBoxLayout2()
	sharingRow.AddWidget(d.exportButton.QWidget)
	sharingRow.AddWidget(d.importButton.QWidget)
	sharingRow.AddStretch()

	sharingNote := qt.NewQLabel3("Export asks for the public SSH key of whoever the workspaces are for, " +
		"and writes every workspace to a zip file encrypted so that only their private key can read it. " +
		"Your own files are left as they are. " +
		"Import adds the workspaces of such a file that was exported for your public key. " +
		"Both use the keys and the folder in effect, not changes made here that have yet to be accepted.")
	sharingNote.SetWordWrap(true)
	sharingNote.SetEnabled(false)

	general := qt.NewQWidget2()
	form := qt.NewQFormLayout(general)
	form.SetContentsMargins(tabMargin, tabMargin, tabMargin, tabMargin)
	form.AddRow4("Workspaces folder", d.folderRow(d.workspaces, "Workspaces Folder"))
	// The note is about the folder above it.
	form.AddRowWithWidget(note.QWidget)
	form.AddRowWithWidget(encryption.QWidget)
	form.AddRow4("Public key", d.fileRow(d.publicKey, "Public Key"))
	form.AddRow4("Private key", d.fileRow(d.privateKey, "Private Key"))
	form.AddRowWithWidget(keysNote.QWidget)
	form.AddRowWithWidget(sharing.QWidget)
	form.AddRowWithLayout(sharingRow.QLayout)
	form.AddRowWithWidget(sharingNote.QWidget)

	requests := qt.NewQWidget2()
	requestsForm := qt.NewQFormLayout(requests)
	requestsForm.SetContentsMargins(tabMargin, tabMargin, tabMargin, tabMargin)
	requestsForm.AddRow3("Timeout", d.timeout.QWidget)
	requestsForm.AddRow3("", d.redirects.QWidget)

	historyNote := qt.NewQLabel3("Each workspace keeps the requests sent from it, newest first, with their responses, " +
		"encrypted like its requests, and written when the workspace is left or TriggerAPI closes. " +
		"The oldest are dropped beyond this number, and a response body larger than 1 MB is not kept.")
	historyNote.SetWordWrap(true)
	historyNote.SetEnabled(false)
	diffHeading := sectionHeading("Comparing responses")
	diffHeading.SetContentsMargins(0, 12, 0, 0)
	diffNote := qt.NewQLabel3("The responses of the entries ticked in the history are compared with this command. " +
		"Each is written to a file of its own, oldest first, and the paths of the files are added to the end of the command, " +
		"which is run as it is written, not by a shell; quote a word with spaces in it. " +
		"The files are deleted when TriggerAPI closes.")
	diffNote.SetWordWrap(true)
	diffNote.SetEnabled(false)

	historyPage := qt.NewQWidget2()
	historyForm := qt.NewQFormLayout(historyPage)
	historyForm.SetContentsMargins(tabMargin, tabMargin, tabMargin, tabMargin)
	historyForm.AddRow3("", d.history.QWidget)
	historyForm.AddRow3("Keep", d.historyLimit.QWidget)
	historyForm.AddRowWithWidget(historyNote.QWidget)
	historyForm.AddRowWithWidget(diffHeading.QWidget)
	historyForm.AddRow3("Diff tool", d.diffCommand.QWidget)
	historyForm.AddRowWithWidget(diffNote.QWidget)

	colours := qt.NewQWidget2()
	coloursLayout := qt.NewQVBoxLayout(colours)
	coloursLayout.SetContentsMargins(tabMargin, tabMargin, tabMargin, tabMargin)
	// The font has the whole width, as the names of fonts are long.
	fontRow := qt.NewQHBoxLayout2()
	fontRow.SetContentsMargins(12, 0, 0, 0)
	fontRow.SetSpacing(16)
	fontRow.AddWidget(qt.NewQLabel3("Code").QWidget)
	fontRow.AddWidget2(d.font.QWidget, 1)
	coloursLayout.AddWidget(sectionHeading("Font").QWidget)
	coloursLayout.AddLayout(fontRow.QLayout)
	coloursLayout.AddSpacing(14)
	coloursLayout.AddWidget(sectionHeading("Colours").QWidget)
	// The table has what height the tab has left, and scrolls.
	d.colourList = d.colourTable()
	d.colourList.SetMinimumHeight(colourTableHeight)
	coloursLayout.AddWidget2(d.colourList.QWidget, 1)

	reset := qt.NewQPushButton3("Use Default Colours")
	reset.OnClicked(d.resetColours)
	resetRow := qt.NewQHBoxLayout2()
	resetRow.AddStretch()
	resetRow.AddWidget(reset.QWidget)
	coloursLayout.AddLayout(resetRow.QLayout)

	// The order matches the tab constants.
	d.tabs.AddTab(plainPage(general), "Workspaces")
	d.tabs.AddTab(plainPage(requests), "Requests")
	d.tabs.AddTab(plainPage(historyPage), "History")
	d.tabs.AddTab(plainPage(colours), "User interface")

	d.buttons.OnAccepted(d.Accept)
	d.buttons.OnRejected(d.Reject)

	layout := qt.NewQVBoxLayout(d.QWidget)
	layout.AddWidget2(d.tabs.QWidget, 1)
	layout.AddWidget(d.buttons.QWidget)

	return d
}

// folderRow lays out a path field beside a button to browse for the folder.
func (d *settingsDialog) folderRow(edit *qt.QLineEdit, caption string) *qt.QLayout {
	return browseRow(edit, func() string {
		return qt.QFileDialog_GetExistingDirectory3(d.QWidget, caption, edit.Text())
	})
}

// fileRow lays out a path field beside a button to browse for the file,
// which starts in the folder of the path there is, or where SSH keys are
// kept.
func (d *settingsDialog) fileRow(edit *qt.QLineEdit, caption string) *qt.QLayout {
	return browseRow(edit, func() string {
		start := strings.TrimSpace(edit.Text())
		if start == "" {
			start = filepath.Join(qt.QDir_HomePath(), ".ssh")
		}
		return qt.QFileDialog_GetOpenFileName3(d.QWidget, caption, start)
	})
}

// browseRow lays out a path field beside a button to browse for the path
// with, which puts what choose returns in the field unless it is empty.
func browseRow(edit *qt.QLineEdit, choose func() string) *qt.QLayout {
	browse := qt.NewQPushButton3("Browse…")
	browse.OnClicked(func() {
		if path := choose(); path != "" {
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

// colourSections are the colours as they are listed on the User interface
// tab: in sections, each a heading above the colours of one sort. Those of
// JSON are a section of their own, after the last of these.
var colourSections = []struct {
	title string
	kinds []colourKind
}{
	{"Sidebar", []colourKind{colourWorkspaces, colourEnvironments, colourRequests, colourSets, colourHistory}},
	{"Request types", []colourKind{colourREST, colourGRPC, colourWebSocket, colourGraphQL, colourMCP}},
	{"Buttons", []colourKind{colourSend, colourAction, colourRemove}},
	{"Text", []colourKind{colourFound, colourNotFound, colourError, colourMatch}},
	{"Capture", []colourKind{colourCaptured, colourNotCaptured}},
	{"Timing", []colourKind{colourTiming}},
}

// swatchWidth is the width of every swatch, so that they line up.
const swatchWidth = 140

// The columns of the table of colours.
const (
	colColourName = iota
	colColourSwatch
)

// colourTable lists every colour in a table, which scrolls: a row for each,
// with its name and its swatch, under a heading row for each section, as
// the endpoints of a gRPC server are listed under their services.
func (d *settingsDialog) colourTable() *qt.QTableWidget {
	table := qt.NewQTableWidget3(0, 2)
	table.SetHorizontalHeaderLabels([]string{"Name", "Colour"})
	table.VerticalHeader().SetVisible(false)
	table.VerticalHeader().SetSectionResizeMode(qt.QHeaderView__ResizeToContents)
	table.SetSelectionMode(qt.QAbstractItemView__NoSelection)
	table.SetEditTriggers(qt.QAbstractItemView__NoEditTriggers)
	table.SetFocusPolicy(qt.NoFocus)
	table.SetVerticalScrollBarPolicy(qt.ScrollBarAsNeeded)
	table.SetHorizontalScrollBarPolicy(qt.ScrollBarAlwaysOff)
	header := table.HorizontalHeader()
	header.SetSectionResizeMode2(colColourName, qt.QHeaderView__Stretch)
	header.SetSectionResizeMode2(colColourSwatch, qt.QHeaderView__ResizeToContents)

	heading := func(title string) {
		// A heading is as wide as the table.
		item := qt.NewQTableWidgetItem2(title)
		item.SetFlags(qt.ItemIsEnabled)
		font := item.Font()
		font.SetBold(true)
		item.SetFont(font)

		row := table.RowCount()
		table.InsertRow(row)
		table.SetItem(row, colColourName, item)
		table.SetSpan(row, colColourName, 1, 2)
	}
	add := func(name string, s *swatch) {
		item := qt.NewQTableWidgetItem2(name)
		item.SetFlags(qt.ItemIsEnabled)

		// The swatch stands a little clear of the lines of its cell.
		cell := qt.NewQWidget2()
		layout := qt.NewQHBoxLayout(cell)
		layout.SetContentsMargins(6, 3, 6, 3)
		layout.AddWidget(s.button.QWidget)

		row := table.RowCount()
		table.InsertRow(row)
		table.SetItem(row, colColourName, item)
		table.SetCellWidget(row, colColourSwatch, cell)
	}

	for _, section := range colourSections {
		heading(section.title)
		for _, kind := range section.kinds {
			label := colourLabel(kind)
			s := d.newSwatch(label, customIconColour(&d.colours, kind), func() string { return defaultColour(kind) })
			d.swatches[kind] = s
			add(label, s)
		}
	}

	heading("JSON")
	for _, t := range tokenKinds {
		s := d.newSwatch("JSON "+strings.ToLower(t.label[:1])+t.label[1:], customColour(&d.jsonColours, t.kind), func() string { return defaultJSONColours()[t.kind] })
		d.json[t.kind] = s
		add(t.label, s)
	}

	return table
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
	s.button.OnClicked(func() {
		current := qt.NewQColor6(s.colour())
		defer current.Delete()
		picked := qt.QColorDialog_GetColor3(current, d.QWidget, "Colour of "+label)
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
	shown := qt.NewQColor6(colour)
	defer shown.Delete()
	if shown.Lightness() > 140 {
		text = "#000000"
	}

	s.button.SetText(colour)
	s.button.SetStyleSheet(fmt.Sprintf(
		"QPushButton { background-color: %s; color: %s; border: 1px solid #888888; border-radius: 4px; padding: 5px 0; }",
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
	base.DiffCommand = strings.TrimSpace(d.diffCommand.Text())
	base.NoHistory = !d.history.IsChecked()
	base.HistoryLimit = d.historyLimit.Value()
	if base.HistoryLimit == settings.DefaultHistoryLimit {
		// The default is not recorded, so it keeps following the default.
		base.HistoryLimit = 0
	}
	base.WorkspacesDir = location(d.workspaces, d.defaults.Workspaces)
	base.PublicKey = strings.TrimSpace(d.publicKey.Text())
	base.PrivateKey = strings.TrimSpace(d.privateKey.Text())
	base.JSONColours = d.jsonColours
	base.Colours = d.colours

	return base
}

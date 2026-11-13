package ui

import (
	"github.com/nomad-software/qute/qtcore"
	"github.com/nomad-software/qute/qtgui"
	"github.com/nomad-software/qute/qtwidgets"

	"fmt"
	"path/filepath"
	"strings"

	"github.com/nomad-software/triggerapi/internal/settings"
)

// settingsDialog edits the settings the user can choose, on a tab for each
// sort: Workspaces (settingsTabWorkspaces), Requests (settingsTabRequests),
// Network (settingsTabNetwork), History (settingsTabHistory) and User
// interface (settingsTabInterface).
type settingsDialog struct {
	*qtwidgets.QDialog

	tabs *qtwidgets.QTabWidget
	font *qtwidgets.QFontComboBox
	// theme chooses one of themes.
	theme *qtwidgets.QComboBox
	// timeout is how long a request can take, in seconds, which is zero
	// for no limit, and redirects whether a REST request follows them.
	timeout   *qtwidgets.QSpinBox
	redirects *qtwidgets.QCheckBox
	// network is the Network tab, in a file of its own.
	network *networkTab
	// history is whether the requests sent are kept in the history of the
	// workspace, and historyLimit how many are.
	history      *qtwidgets.QCheckBox
	historyLimit *qtwidgets.QSpinBox
	// diffCommand is the command the responses of the history are compared
	// with.
	diffCommand *qtwidgets.QLineEdit
	workspaces  *qtwidgets.QLineEdit
	// publicKey and privateKey are the paths of the files of the SSH keys
	// the saved files are encrypted with.
	publicKey  *qtwidgets.QLineEdit
	privateKey *qtwidgets.QLineEdit
	// exportButton and importButton write the workspaces to an archive
	// for someone else and add those of an archive, by calling onExport
	// and onImport, which are given the dialog to be the parent of theirs.
	exportButton *qtwidgets.QPushButton
	importButton *qtwidgets.QPushButton
	onExport     func(parent *qtwidgets.QWidget)
	onImport     func(parent *qtwidgets.QWidget)
	buttons      *qtwidgets.QDialogButtonBox

	// colours and jsonColours are the colours the user has chosen. One
	// that is empty is the default, which is what its swatch shows.
	colours     settings.Colours
	jsonColours settings.JSONColours
	swatches    map[colourKind]*swatch
	json        map[tokenKind]*swatch
	// colourList is the table the swatches are listed in.
	colourList *qtwidgets.QTableWidget

	// initialFont is the family the font selector started on. It can differ
	// from the setting if that font is not installed.
	initialFont string
	defaults    Files
}

// The tabs of the settings dialog.
const (
	settingsTabWorkspaces = iota
	settingsTabRequests
	settingsTabNetwork
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
func newSettingsDialog(parent *qtwidgets.QWidget, current settings.Settings, defaults Files) *settingsDialog {
	d := &settingsDialog{
		QDialog:      qtwidgets.NewQDialog(parent),
		tabs:         qtwidgets.NewQTabWidget(nil),
		font:         qtwidgets.NewQFontComboBox(nil),
		theme:        qtwidgets.NewQComboBox(nil),
		timeout:      qtwidgets.NewQSpinBox(nil),
		redirects:    qtwidgets.NewQCheckBoxWithText("Follow redirects", nil),
		history:      qtwidgets.NewQCheckBoxWithText("Enabled", nil),
		historyLimit: qtwidgets.NewQSpinBox(nil),
		diffCommand:  qtwidgets.NewQLineEdit(nil),
		workspaces:   qtwidgets.NewQLineEdit(nil),
		publicKey:    qtwidgets.NewQLineEdit(nil),
		privateKey:   qtwidgets.NewQLineEdit(nil),
		exportButton: qtwidgets.NewQPushButtonWithText("Export…", nil),
		importButton: qtwidgets.NewQPushButtonWithText("Import…", nil),
		buttons:      qtwidgets.NewQDialogButtonBoxWithStandardButton(qtwidgets.QDialogButtonBoxOk|qtwidgets.QDialogButtonBoxCancel, nil),
		defaults:     defaults,
		colours:      current.Colours,
		jsonColours:  current.JSONColours,
		swatches:     map[colourKind]*swatch{},
		json:         map[tokenKind]*swatch{},
	}
	d.SetWindowTitle("Settings")
	d.SetMinimumWidth(680)

	d.font.SetCurrentFont(qtgui.NewQFontWithFamily(current.ResponseFont))
	d.initialFont = d.font.CurrentFont().Family()

	for _, t := range themes {
		d.theme.AddItem(t.label)
	}
	d.theme.SetCurrentIndex(themeIndex(current.Theme))

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

	note := qtwidgets.NewQLabelWithText("Each workspace has a folder of its own in this one, holding its environments and requests. "+
		"Choosing a folder that already has workspaces shows those. "+
		"Choosing one that has none copies the current workspaces there. "+
		"A relative path is relative to the directory TriggerAPI is run from.", nil)
	note.SetWordWrap(true)
	note.SetEnabled(false)

	d.publicKey.SetText(current.PublicKey)
	d.publicKey.SetPlaceholderText("Such as ~/.ssh/id_ed25519.pub")
	d.privateKey.SetText(current.PrivateKey)
	d.privateKey.SetPlaceholderText("Such as ~/.ssh/id_ed25519")

	keysNote := qtwidgets.NewQLabelWithText("The environments and requests of every workspace are encrypted with this pair of SSH keys, "+
		"which can be Ed25519 or RSA. TriggerAPI needs them to work: without them nothing is loaded or saved. "+
		"Changing the keys encrypts every file again with the new ones. "+
		"The passphrase of a private key that has one is asked for when TriggerAPI starts.", nil)
	keysNote.SetWordWrap(true)
	keysNote.SetEnabled(false)

	encryption := sectionHeading("Encryption")
	encryption.SetContentsMargins(qtcore.QMargins{Left: 0, Top: 12, Right: 0, Bottom: 0})

	sharing := sectionHeading("Import and export")
	sharing.SetContentsMargins(qtcore.QMargins{Left: 0, Top: 12, Right: 0, Bottom: 0})

	d.exportButton.OnClicked(func(_ bool) {
		if d.onExport != nil {
			d.onExport(d.AsQWidget())
		}
	})
	d.importButton.OnClicked(func(_ bool) {
		if d.onImport != nil {
			d.onImport(d.AsQWidget())
		}
	})
	sharingRow := qtwidgets.NewQHBoxLayout(nil)
	sharingRow.AddWidget(d.exportButton.AsQWidget())
	sharingRow.AddWidget(d.importButton.AsQWidget())
	sharingRow.AddStretch()

	sharingNote := qtwidgets.NewQLabelWithText("Export asks for the public SSH key of whoever the workspaces are for, "+
		"and writes every workspace to a zip file encrypted so that only their private key can read it. "+
		"Your own files are left as they are. "+
		"Import adds the workspaces of such a file that was exported for your public key. "+
		"Both use the keys and the folder in effect, not changes made here that have yet to be accepted.", nil)
	sharingNote.SetWordWrap(true)
	sharingNote.SetEnabled(false)

	general := qtwidgets.NewQWidget(nil)
	form := qtwidgets.NewQFormLayout(general)
	form.SetContentsMargins(qtcore.QMargins{Left: tabMargin, Top: tabMargin, Right: tabMargin, Bottom: tabMargin})
	form.AddRowTextLayout("Workspaces folder", d.folderRow(d.workspaces, "Workspaces Folder"))
	// The note is about the folder above it.
	form.AddRowWidget(note.AsQWidget())
	form.AddRowWidget(encryption.AsQWidget())
	form.AddRowTextLayout("Public key", d.fileRow(d.publicKey, "Public Key"))
	form.AddRowTextLayout("Private key", d.fileRow(d.privateKey, "Private Key"))
	form.AddRowWidget(keysNote.AsQWidget())
	form.AddRowWidget(sharing.AsQWidget())
	form.AddRowSpanLayout(sharingRow)
	form.AddRowWidget(sharingNote.AsQWidget())

	requests := qtwidgets.NewQWidget(nil)
	requestsForm := qtwidgets.NewQFormLayout(requests)
	requestsForm.SetContentsMargins(qtcore.QMargins{Left: tabMargin, Top: tabMargin, Right: tabMargin, Bottom: tabMargin})
	requestsForm.AddRowText("Timeout", d.timeout.AsQWidget())
	requestsForm.AddRowText("", d.redirects.AsQWidget())

	historyNote := qtwidgets.NewQLabelWithText("Each workspace keeps the requests sent from it, newest first, with their responses, "+
		"encrypted like its requests, and written when the workspace is left or TriggerAPI closes. "+
		"The oldest are dropped beyond this number, and a response body larger than 1 MB is not kept.", nil)
	historyNote.SetWordWrap(true)
	historyNote.SetEnabled(false)
	diffHeading := sectionHeading("Comparing responses")
	diffHeading.SetContentsMargins(qtcore.QMargins{Left: 0, Top: 12, Right: 0, Bottom: 0})
	diffNote := qtwidgets.NewQLabelWithText("The responses of the entries ticked in the history are compared with this command. "+
		"Each is written to a file of its own, oldest first, and the paths of the files are added to the end of the command, "+
		"which is run as it is written, not by a shell; quote a word with spaces in it. "+
		"The files are deleted when TriggerAPI closes.", nil)
	diffNote.SetWordWrap(true)
	diffNote.SetEnabled(false)

	historyPage := qtwidgets.NewQWidget(nil)
	historyForm := qtwidgets.NewQFormLayout(historyPage)
	historyForm.SetContentsMargins(qtcore.QMargins{Left: tabMargin, Top: tabMargin, Right: tabMargin, Bottom: tabMargin})
	historyForm.AddRowText("", d.history.AsQWidget())
	historyForm.AddRowText("Keep", d.historyLimit.AsQWidget())
	historyForm.AddRowWidget(historyNote.AsQWidget())
	historyForm.AddRowWidget(diffHeading.AsQWidget())
	historyForm.AddRowText("Diff tool", d.diffCommand.AsQWidget())
	historyForm.AddRowWidget(diffNote.AsQWidget())

	colours := qtwidgets.NewQWidget(nil)
	coloursLayout := qtwidgets.NewQVBoxLayout(colours)
	coloursLayout.SetContentsMargins(qtcore.QMargins{Left: tabMargin, Top: tabMargin, Right: tabMargin, Bottom: tabMargin})
	themeRow := qtwidgets.NewQHBoxLayout(nil)
	themeRow.SetContentsMargins(qtcore.QMargins{Left: 12, Top: 0, Right: 0, Bottom: 0})
	themeRow.SetSpacing(16)
	themeRow.AddWidget(qtwidgets.NewQLabelWithText("Theme", nil).AsQWidget())
	themeRow.AddWidget(d.theme.AsQWidget())
	themeRow.AddStretch()
	themeNote := qtwidgets.NewQLabelWithText("Follow the system looks as the desktop's theme does, and changes with it.", nil)
	themeNote.SetWordWrap(true)
	themeNote.SetEnabled(false)
	coloursLayout.AddWidget(sectionHeading("Theme").AsQWidget())
	coloursLayout.AddLayout(themeRow.AsQLayout())
	coloursLayout.AddWidget(themeNote.AsQWidget())
	coloursLayout.AddSpacing(14)
	// The font has the whole width, as the names of fonts are long.
	fontRow := qtwidgets.NewQHBoxLayout(nil)
	fontRow.SetContentsMargins(qtcore.QMargins{Left: 12, Top: 0, Right: 0, Bottom: 0})
	fontRow.SetSpacing(16)
	fontRow.AddWidget(qtwidgets.NewQLabelWithText("Code", nil).AsQWidget())
	fontRow.AddWidgetFull(d.font.AsQWidget(), 1, 0)
	coloursLayout.AddWidget(sectionHeading("Font").AsQWidget())
	coloursLayout.AddLayout(fontRow.AsQLayout())
	coloursLayout.AddSpacing(14)
	coloursLayout.AddWidget(sectionHeading("Colours").AsQWidget())
	// The table has what height the tab has left, and scrolls.
	d.colourList = d.colourTable()
	d.colourList.SetMinimumHeight(colourTableHeight)
	coloursLayout.AddWidgetFull(d.colourList.AsQWidget(), 1, 0)

	reset := qtwidgets.NewQPushButtonWithText("Use Default Colours", nil)
	reset.OnClicked(func(_ bool) { d.resetColours() })
	resetRow := qtwidgets.NewQHBoxLayout(nil)
	resetRow.AddStretch()
	resetRow.AddWidget(reset.AsQWidget())
	coloursLayout.AddLayout(resetRow.AsQLayout())

	// The order matches the tab constants.
	d.tabs.AddTab(plainPage(general), "Workspaces")
	d.tabs.AddTab(plainPage(requests), "Requests")
	d.network = newNetworkTab(d.AsQWidget(), current.Network)
	d.tabs.AddTab(plainPage(d.network.root), "Network")
	d.tabs.AddTab(plainPage(historyPage), "History")
	d.tabs.AddTab(plainPage(colours), "User interface")

	d.buttons.OnAccepted(d.Accept)
	d.buttons.OnRejected(d.Reject)

	layout := qtwidgets.NewQVBoxLayout(d.AsQWidget())
	layout.AddWidgetFull(d.tabs.AsQWidget(), 1, 0)
	layout.AddWidget(d.buttons.AsQWidget())

	return d
}

// folderRow lays out a path field beside a button to browse for the folder.
func (d *settingsDialog) folderRow(edit *qtwidgets.QLineEdit, caption string) *qtwidgets.QLayout {
	return browseRow(edit, func() string {
		return askDirectory(d.AsQWidget(), caption, edit.Text())
	})
}

// fileRow lays out a path field beside a button to browse for the file,
// which starts in the folder of the path there is, or where SSH keys are
// kept.
func (d *settingsDialog) fileRow(edit *qtwidgets.QLineEdit, caption string) *qtwidgets.QLayout {
	return browseRow(edit, func() string {
		start := strings.TrimSpace(edit.Text())
		if start == "" {
			start = filepath.Join(qtcore.QDirHomePath(), ".ssh")
		}
		return askOpenFile(d.AsQWidget(), caption, start, "")
	})
}

// browseRow lays out a path field beside a button to browse for the path
// with, which puts what choose returns in the field unless it is empty.
func browseRow(edit *qtwidgets.QLineEdit, choose func() string) *qtwidgets.QLayout {
	return pathRow(edit, browseButton(edit, choose))
}

// browseButton returns a button to browse for a path with, which puts what
// choose returns in the field unless it is empty.
func browseButton(edit *qtwidgets.QLineEdit, choose func() string) *qtwidgets.QPushButton {
	browse := qtwidgets.NewQPushButtonWithText("Browse…", nil)
	browse.OnClicked(func(_ bool) {
		if path := choose(); path != "" {
			edit.SetText(path)
		}
	})
	return browse
}

// pathRow lays out a path field beside its button.
func pathRow(edit *qtwidgets.QLineEdit, button *qtwidgets.QPushButton) *qtwidgets.QLayout {
	row := qtwidgets.NewQHBoxLayout(nil)
	row.AddWidgetFull(edit.AsQWidget(), 1, 0)
	row.AddWidget(button.AsQWidget())
	return row.AsQLayout()
}

// sectionHeading returns the heading of a section of a tab.
func sectionHeading(title string) *qtwidgets.QLabel {
	label := qtwidgets.NewQLabelWithText(title, nil)
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
	{"Captures and assertions", []colourKind{colourPassed, colourFailed}},
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
func (d *settingsDialog) colourTable() *qtwidgets.QTableWidget {
	table := qtwidgets.NewQTableWidgetWithRows(0, 2, nil)
	table.SetHorizontalHeaderLabels([]string{"Name", "Colour"})
	table.VerticalHeader().SetVisible(false)
	table.VerticalHeader().SetSectionResizeModeAll(qtwidgets.QHeaderViewResizeToContents)
	table.SetSelectionMode(qtwidgets.QAbstractItemViewNoSelection)
	table.SetEditTriggers(qtwidgets.QAbstractItemViewNoEditTriggers)
	table.SetFocusPolicy(qtcore.NoFocus)
	table.SetVerticalScrollBarPolicy(qtcore.ScrollBarAsNeeded)
	table.SetHorizontalScrollBarPolicy(qtcore.ScrollBarAlwaysOff)
	header := table.HorizontalHeader()
	header.SetSectionResizeMode(colColourName, qtwidgets.QHeaderViewStretch)
	header.SetSectionResizeMode(colColourSwatch, qtwidgets.QHeaderViewResizeToContents)

	heading := func(title string) {
		// A heading is as wide as the table.
		item := qtwidgets.NewQTableWidgetItemWithText(title)
		item.SetFlags(qtcore.ItemIsEnabled)
		font := item.Font()
		font.SetBold(true)
		item.SetFont(font)

		row := table.RowCount()
		table.InsertRow(row)
		table.SetItem(row, colColourName, item)
		table.SetSpan(row, colColourName, 1, 2)
	}
	add := func(name string, s *swatch) {
		item := qtwidgets.NewQTableWidgetItemWithText(name)
		item.SetFlags(qtcore.ItemIsEnabled)

		// The swatch stands a little clear of the lines of its cell.
		cell := qtwidgets.NewQWidget(nil)
		layout := qtwidgets.NewQHBoxLayout(cell)
		layout.SetContentsMargins(qtcore.QMargins{Left: 6, Top: 3, Right: 6, Bottom: 3})
		layout.AddWidget(s.button.AsQWidget())

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

	last := ""
	for _, t := range tokenKinds {
		if t.heading != last {
			heading(t.heading)
			last = t.heading
		}
		s := d.newSwatch(t.heading+" "+strings.ToLower(t.label[:1])+t.label[1:], customColour(&d.jsonColours, t.kind), func() string { return defaultJSONColours()[t.kind] })
		d.json[t.kind] = s
		add(t.label, s)
	}

	return table
}

// swatch is a button which shows one of the colours and lets the user choose
// another for it.
type swatch struct {
	button *qtwidgets.QPushButton
	// chosen is where the colour the user has chosen is kept, which is
	// empty for the default.
	chosen *string
	// fallback returns the default colour.
	fallback func() string
}

// newSwatch returns a swatch for the colour kept at chosen, which is called
// label.
func (d *settingsDialog) newSwatch(label string, chosen *string, fallback func() string) *swatch {
	s := &swatch{button: qtwidgets.NewQPushButton(nil), chosen: chosen, fallback: fallback}
	s.button.SetFixedWidth(swatchWidth)
	s.button.OnClicked(func(_ bool) {
		current := qcolour(s.colour())
		picked := qtwidgets.QColorDialogGetColorFull(current, d.AsQWidget(), "Colour of "+label, 0)
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

	shown := qcolour(colour)
	text := readableOn(shown)

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

	location := func(edit *qtwidgets.QLineEdit, fallback string) string {
		if path := strings.TrimSpace(edit.Text()); path != fallback {
			return path
		}
		return ""
	}
	base.Theme = themes[max(d.theme.CurrentIndex(), 0)].name
	base.Timeout = d.timeout.Value()
	base.NoRedirects = !d.redirects.IsChecked()
	base.Network = d.network.config()
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

package ui

import (
	"fmt"
	"slices"

	qt "github.com/mappu/miqt/qt6"

	"github.com/nomad-software/triggerapi/internal/crypt"
	"github.com/nomad-software/triggerapi/internal/env"
	"github.com/nomad-software/triggerapi/internal/postman"
)

// envList lists the environments. The selected environment is the active one
// and every change is saved.
type envList struct {
	root *qt.QWidget
	list *qt.QListWidget

	store *env.Store
	// path is the file the store is saved to. Nothing is saved if it is empty.
	path     string
	updating bool
	// copied is a copy of the environment last copied with Ctrl+C, or nil.
	// It is kept in the list, which stays when the workspace changes, so
	// that it can be pasted into another; it never goes to the system
	// clipboard, as the values of variables are often secrets.
	copied *env.Environment

	// confirm asks the user a yes/no question.
	confirm func(question string) bool
	// chooseSource asks the user what kind of file to import. It reports
	// false if they decide against importing.
	chooseSource func() (envImportSource, bool)
	// chooseFile asks the user for a file of a kind to import. It reports
	// false if they decide against choosing one.
	chooseFile func(source envImportSource) (string, bool)
	// warn tells the user of a problem.
	warn func(message string)

	// onEdit is called when the user asks to edit the selected environment.
	onEdit func()
	// onSelect is called after a different environment, or none, is selected.
	onSelect func()
	// onChange is called after the environments or the selection change.
	onChange func()
	onError  func(error)
	// keys returns the keys the file is encrypted with, which are nil if
	// it is not to be.
	keys func() *crypt.Keys
}

func newEnvList(store *env.Store, path string) *envList {
	s := &envList{
		root: qt.NewQWidget2(),
		list: qt.NewQListWidget2(),
	}

	s.confirm = func(question string) bool {
		return qt.QMessageBox_Question(s.root, "TriggerAPI", question) == qt.QMessageBox__Yes
	}
	s.chooseSource = func() (envImportSource, bool) {
		return newEnvImportDialog(s.root).ask()
	}
	s.chooseFile = func(source envImportSource) (string, bool) {
		return chooseImportFile(s.root, envImportSources[source])
	}
	s.warn = func(message string) {
		qt.QMessageBox_Warning(s.root, "TriggerAPI", message)
	}

	// The buttons are icons along the bottom, each saying what it does in
	// its tooltip. They are the colour of the icon of the tab, but for the one
	// which removes, and the one which imports is apart at the right.
	add := newIconButton(colourEnvironments, drawPlus, "Add an environment")
	add.OnClicked(s.add)
	duplicate := newIconButton(colourEnvironments, drawCopy, "Duplicate the selected environment")
	duplicate.OnClicked(s.duplicate)
	importButton := newIconButton(colourEnvironments, drawImport, "Import an environment exported from Postman")
	importButton.OnClicked(s.importDocument)
	edit := newIconButton(colourEnvironments, drawPencil, "Edit the selected environment")
	edit.OnClicked(s.edit)
	remove := newIconButton(colourRemove, drawBin, "Remove the selected environment")
	remove.OnClicked(s.remove)

	hint := qt.NewQLabel3("Use {{variable}} in any request field to insert a value from the selected environment.")
	hint.SetWordWrap(true)
	hint.SetEnabled(false)

	layout := qt.NewQVBoxLayout(s.root)
	layout.AddWidget2(s.list.QWidget, 1)
	layout.AddWidget(hint.QWidget)
	buttons := newButtonRow(add, duplicate, edit, remove)
	buttons.AddWidget(importButton.QWidget)
	layout.AddLayout(buttons.QLayout)

	s.load(store, path)

	s.list.OnCurrentRowChanged(func(row int) {
		if s.updating {
			return
		}
		s.store.Active = row
		s.selected()
	})
	s.list.OnItemDoubleClicked(func(*qt.QListWidgetItem) { s.edit() })
	// F2 does what the edit button does, which is where an environment is
	// renamed.
	s.list.OnKeyPressEvent(func(super func(*qt.QKeyEvent), event *qt.QKeyEvent) {
		switch {
		case event.Key() == int(qt.Key_F2):
			s.edit()
		case event.Matches(qt.QKeySequence__Copy):
			s.copy()
		case event.Matches(qt.QKeySequence__Paste):
			s.paste()
		default:
			super(event)
		}
	})

	return s
}

// load replaces the environments with those of the store, which is saved to
// path, without calling onSelect or onChange.
func (s *envList) load(store *env.Store, path string) {
	defer func(prev bool) { s.updating = prev }(s.updating)
	s.updating = true

	s.store = store
	s.path = path

	s.list.Clear()
	for _, e := range store.Environments {
		item := qt.NewQListWidgetItem2(e.Name)
		item.SetIcon(environmentIcon())
		s.list.AddItemWithItem(item)
	}
	s.list.SetCurrentRow(store.Active)
}

// recolour gives the environments their icon again, after its colour has
// changed.
func (s *envList) recolour() {
	for row := range s.list.Count() {
		s.list.Item(row).SetIcon(environmentIcon())
	}
}

// selected reports a change of the active environment and saves it.
func (s *envList) selected() {
	if s.onSelect != nil {
		s.onSelect()
	}
	s.save()
}

// insert adds an environment and selects it, in its place in the order.
func (s *envList) insert(e env.Environment) {
	s.store.Environments = append(s.store.Environments, e)
	s.store.Active = len(s.store.Environments) - 1
	s.sort()
	s.selected()
}

// add creates an environment, selects it and opens it for editing.
func (s *envList) add() {
	s.insert(env.Environment{Name: "New Environment"})
	s.edit()
}

// duplicate makes a copy of the selected environment, selects it and opens
// it for editing.
func (s *envList) duplicate() {
	e, ok := s.store.Current()
	if !ok {
		return
	}
	copied := e.Clone()
	copied.Name += " copy"
	s.insert(copied)
	s.edit()
}

// copy keeps a copy of the selected environment, to be pasted.
func (s *envList) copy() {
	if e, ok := s.store.Current(); ok {
		copied := e.Clone()
		s.copied = &copied
	}
}

// paste adds a copy of the environment that was copied and selects it. If
// there is already one with its name, " copy" is added to it.
func (s *envList) paste() {
	if s.copied == nil {
		return
	}
	pasted := s.copied.Clone()
	pasted.Name = copyName(pasted.Name, func(name string) bool {
		return slices.ContainsFunc(s.store.Environments, func(e env.Environment) bool { return e.Name == name })
	})
	s.insert(pasted)
}

// envImportSource is a kind of file that an environment can be imported
// from.
type envImportSource string

const envImportPostman envImportSource = "postman"

// envImportOrder is the order the kinds of file are offered in.
var envImportOrder = []envImportSource{envImportPostman}

// envImportSources describes the kinds of file that an environment can be
// imported from.
var envImportSources = map[envImportSource]importKind[env.Environment]{
	envImportPostman: {"Postman", "Postman environments (*.json)", "#ef5b25", drawPlane, postman.ImportEnvironment},
}

// newEnvImportDialog returns a dialog which asks what kind of file to import
// an environment from.
func newEnvImportDialog(parent *qt.QWidget) *choiceDialog[envImportSource] {
	return newSourceDialog(parent, "Choose what to import an environment from.", envImportOrder, envImportSources)
}

// importDocument asks what kind of file to import, and for the file, and
// imports it.
func (s *envList) importDocument() {
	importDocument(s.chooseSource, s.chooseFile, s.importFile, s.warn)
}

// importFile adds the environment in the file at path, which is of the kind
// given, and selects it.
func (s *envList) importFile(source envImportSource, path string) error {
	environment, err := readImport(envImportSources[source], path)
	if err != nil {
		return err
	}
	s.insert(environment)
	return nil
}

// sort puts the environments in alphabetical order and lists them again,
// keeping the active one selected.
func (s *envList) sort() {
	s.store.Sort()
	s.load(s.store, s.path)
}

func (s *envList) edit() {
	if _, ok := s.store.Current(); ok && s.onEdit != nil {
		s.onEdit()
	}
}

// remove deletes the selected environment, once the user has agreed to it.
func (s *envList) remove() {
	row := s.list.CurrentRow()
	if row < 0 {
		return
	}
	if !s.confirm(fmt.Sprintf("Remove the environment %q?", s.store.Environments[row].Name)) {
		return
	}

	s.updating = true
	s.store.Environments = append(s.store.Environments[:row], s.store.Environments[row+1:]...)
	s.list.TakeItem(row).Delete()
	s.store.Active = s.list.CurrentRow()
	s.updating = false

	s.selected()
}

// update replaces the name and variables of the selected environment.
func (s *envList) update(name string, variables []env.Variable) {
	e, ok := s.store.Current()
	if !ok {
		return
	}
	renamed := e.Name != name
	e.Name = name
	e.Variables = variables
	if renamed {
		s.sort()
	}
	s.save()
}

// save reports the change, and writes the store to its file.
func (s *envList) save() {
	if s.onChange != nil {
		s.onChange()
	}
	saveStore(s.path, s.keys, s.store.SaveWith, s.onError)
}

// environmentIcon returns the icon shown before the name of an environment.
func environmentIcon() *qt.QIcon {
	return cachedIcon("environment", func() *qt.QIcon { return drawIcon(iconSize*2, themeColour(colourEnvironments), drawGlobe) })
}

package ui

import (
	"fmt"
	"os"
	"path/filepath"

	qt "github.com/mappu/miqt/qt6"

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
		kind := envImportSources[source]
		path := qt.QFileDialog_GetOpenFileName4(s.root, "Import "+kind.label, "", kind.filter+";;All files (*)")
		return path, path != ""
	}
	s.warn = func(message string) {
		qt.QMessageBox_Warning(s.root, "TriggerAPI", message)
	}

	// The buttons are icons along the bottom, each saying what it does in
	// its tooltip. They are the colour of the icon of the tab, but for the one
	// which removes, and the one which imports is apart at the right.
	add := newIconButton(environmentsColour, drawPlus, "Add an environment")
	add.OnClicked(s.add)
	importButton := newIconButton(environmentsColour, drawImport, "Import an environment exported from Postman")
	importButton.OnClicked(s.importDocument)
	edit := newIconButton(environmentsColour, drawPencil, "Edit the selected environment")
	edit.OnClicked(s.edit)
	remove := newIconButton(removeColour, drawBin, "Remove the selected environment")
	remove.OnClicked(s.remove)

	hint := qt.NewQLabel3("Use {{variable}} in any request field to insert a value from the selected environment.")
	hint.SetWordWrap(true)
	hint.SetEnabled(false)

	layout := qt.NewQVBoxLayout(s.root)
	layout.AddWidget2(s.list.QWidget, 1)
	layout.AddWidget(hint.QWidget)
	buttons := newButtonRow(add, edit, remove)
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
		s.list.AddItem(e.Name)
	}
	s.list.SetCurrentRow(store.Active)
}

// selected reports a change of the active environment and saves it.
func (s *envList) selected() {
	if s.onSelect != nil {
		s.onSelect()
	}
	s.save()
}

// add creates an environment, selects it and opens it for editing.
func (s *envList) add() {
	s.store.Environments = append(s.store.Environments, env.Environment{Name: "New Environment"})
	s.store.Active = len(s.store.Environments) - 1
	s.sort()
	s.selected()
	s.edit()
}

// envImportSource is a kind of file that an environment can be imported
// from.
type envImportSource string

const envImportPostman envImportSource = "postman"

// envImportSources describes the kinds of file that an environment can be
// imported from.
var envImportSources = map[envImportSource]struct {
	label  string
	filter string
	colour string
	glyph  func(*qt.QPainter)
	read   func([]byte) (env.Environment, error)
}{
	envImportPostman: {"Postman", "Postman environments (*.json)", "#ef5b25", drawPlane, postman.ImportEnvironment},
}

// newEnvImportDialog returns a dialog which asks what kind of file to import
// an environment from.
func newEnvImportDialog(parent *qt.QWidget) *choiceDialog[envImportSource] {
	var choices []choice[envImportSource]
	for _, source := range []envImportSource{envImportPostman} {
		kind := envImportSources[source]
		choices = append(choices, choice[envImportSource]{source, kind.label, drawIcon(iconSize*2, kind.colour, kind.glyph)})
	}
	return newChoiceDialog(parent, "Import", "Choose what to import an environment from.", choices)
}

// importDocument asks what kind of file to import, and for the file, and
// imports it.
func (s *envList) importDocument() {
	source, ok := s.chooseSource()
	if !ok {
		return
	}
	path, ok := s.chooseFile(source)
	if !ok {
		return
	}
	if err := s.importFile(source, path); err != nil {
		s.warn(fmt.Sprintf("%s could not be imported: %v.", filepath.Base(path), err))
	}
}

// importFile adds the environment in the file at path, which is of the kind
// given, and selects it.
func (s *envList) importFile(source envImportSource, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	environment, err := envImportSources[source].read(data)
	if err != nil {
		return err
	}

	s.store.Environments = append(s.store.Environments, environment)
	s.store.Active = len(s.store.Environments) - 1
	s.sort()
	s.selected()
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

func (s *envList) save() {
	if s.onChange != nil {
		s.onChange()
	}
	if s.path == "" {
		return
	}
	if err := s.store.Save(s.path); err != nil && s.onError != nil {
		s.onError(err)
	}
}

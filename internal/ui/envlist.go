package ui

import (
	qt "github.com/mappu/miqt/qt6"

	"github.com/nomad-software/postie/internal/env"
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

	add := qt.NewQPushButton3("Add")
	add.OnClicked(s.add)
	edit := qt.NewQPushButton3("Edit")
	edit.OnClicked(s.edit)
	remove := qt.NewQPushButton3("Remove")
	remove.OnClicked(s.remove)

	buttons := qt.NewQHBoxLayout2()
	buttons.AddWidget(add.QWidget)
	buttons.AddWidget(edit.QWidget)
	buttons.AddWidget(remove.QWidget)

	hint := qt.NewQLabel3("Use {{variable}} in any request field to insert a value from the selected environment.")
	hint.SetWordWrap(true)
	hint.SetEnabled(false)

	layout := qt.NewQVBoxLayout(s.root)
	layout.AddWidget2(s.list.QWidget, 1)
	layout.AddLayout(buttons.QLayout)
	layout.AddWidget(hint.QWidget)

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

// remove deletes the selected environment.
func (s *envList) remove() {
	row := s.list.CurrentRow()
	if row < 0 {
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

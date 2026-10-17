package ui

import (
	"fmt"
	"slices"
	"strings"

	qt "github.com/mappu/miqt/qt6"
	"github.com/mappu/miqt/qt6/mainthread"

	"github.com/nomad-software/triggerapi/internal/workspace"
)

// workspaceList lists the workspaces, each of which has its own environments
// and requests. The selected workspace is the one in use. A workspace is a
// folder on disk, so every change is made there at once.
type workspaceList struct {
	root *qt.QWidget
	list *qt.QListWidget

	// dir returns the folder the workspaces are kept in.
	dir func() string
	// names are the workspaces, in the order they are listed.
	names    []string
	updating bool

	// confirm asks the user a yes/no question.
	confirm func(question string) bool
	// warn tells the user of a problem.
	warn func(message string)

	// onOpen is called with the name of a workspace when it becomes the
	// one in use.
	onOpen func(name string)
	// onRename is called after the workspace in use is given another name.
	onRename func(name string)
	// onList is called after the workspaces have been listed again.
	onList func()
}

func newWorkspaceList(dir func() string) *workspaceList {
	s := &workspaceList{
		root: qt.NewQWidget2(),
		list: qt.NewQListWidget2(),
		dir:  dir,
	}

	s.confirm = func(question string) bool {
		return qt.QMessageBox_Question(s.root, "TriggerAPI", question) == qt.QMessageBox__Yes
	}
	s.warn = warnDialog(s.root)

	// Names are only edited through the rename button.
	s.list.SetEditTriggers(qt.QAbstractItemView__NoEditTriggers)

	// F2 does what the rename button does.
	s.list.OnKeyPressEvent(func(super func(*qt.QKeyEvent), event *qt.QKeyEvent) {
		if event.Key() == int(qt.Key_F2) {
			s.rename()
			return
		}
		super(event)
	})

	// The buttons are icons along the bottom, each saying what it does in
	// its tooltip. They are the colour of the icon of the tab, but for the
	// one which removes.
	add := newIconButton(colourWorkspaces, drawPlus, "Add a workspace")
	add.OnClicked(s.add)
	duplicate := newIconButton(colourWorkspaces, drawCopy, "Duplicate the selected workspace")
	duplicate.OnClicked(s.duplicate)
	rename := newIconButton(colourWorkspaces, drawPencil, "Rename the selected workspace")
	rename.OnClicked(s.rename)
	remove := newIconButton(colourRemove, drawBin, "Remove the selected workspace")
	remove.OnClicked(s.remove)

	hint := qt.NewQLabel3("Each workspace has its own environments and requests. The selected workspace is the one in use.")
	hint.SetWordWrap(true)
	hint.SetEnabled(false)

	layout := qt.NewQVBoxLayout(s.root)
	layout.AddWidget2(s.list.QWidget, 1)
	layout.AddWidget(hint.QWidget)
	layout.AddLayout(newButtonRow(add, duplicate, rename, remove).QLayout)

	s.list.OnCurrentRowChanged(func(row int) {
		if !s.updating && row >= 0 && row < len(s.names) && s.onOpen != nil {
			s.onOpen(s.names[row])
		}
	})
	s.list.OnItemChanged(func(item *qt.QListWidgetItem) {
		if !s.updating {
			s.renamed(s.list.Row(item), item.Text())
		}
	})

	return s
}

// load lists the workspaces that are in the folder, with the one of a name
// selected, without calling onOpen. It returns the name of the one that is
// selected, which is the first if there is none of that name, or an empty
// string if there are none at all.
func (s *workspaceList) load(selected string) (string, error) {
	defer func(prev bool) {
		s.updating = prev
		if s.onList != nil {
			s.onList()
		}
	}(s.updating)
	s.updating = true

	names, err := workspace.List(s.dir())
	s.names = names

	s.list.Clear()
	for _, name := range names {
		item := qt.NewQListWidgetItem2(name)
		item.SetIcon(workspaceIcon())
		item.SetFlags(item.Flags() | qt.ItemIsEditable)
		s.list.AddItemWithItem(item)
	}
	if len(names) == 0 {
		return "", err
	}

	row := max(0, slices.Index(names, selected))
	s.list.SetCurrentRow(row)
	return names[row], err
}

// recolour gives the workspaces their icon again, after its colour has
// changed.
func (s *workspaceList) recolour() {
	for row := range s.list.Count() {
		s.list.Item(row).SetIcon(workspaceIcon())
	}
}

// current returns the name of the workspace in use, or an empty string if
// there is none.
func (s *workspaceList) current() string {
	if row := s.list.CurrentRow(); row >= 0 && row < len(s.names) {
		return s.names[row]
	}
	return ""
}

// open lists the workspaces again and makes the one of a name the one in
// use.
func (s *workspaceList) open(name string) {
	name, err := s.load(name)
	if err != nil {
		s.warn(fmt.Sprintf("The workspaces could not be listed: %v.", err))
	}
	if name != "" && s.onOpen != nil {
		s.onOpen(name)
	}
}

// add creates a workspace, makes it the one in use and starts editing its
// name.
func (s *workspaceList) add() {
	name := workspace.Unique(s.dir(), "New Workspace")
	if err := workspace.Create(s.dir(), name); err != nil {
		s.warn(fmt.Sprintf("The workspace could not be added: %v.", err))
		return
	}

	s.open(name)
	s.rename()
}

// duplicate makes a copy of the selected workspace, with its environments
// and requests, makes it the one in use and starts editing its name.
func (s *workspaceList) duplicate() {
	current := s.current()
	if current == "" {
		return
	}
	name, err := workspace.Duplicate(s.dir(), current)
	if err != nil {
		s.warn(fmt.Sprintf("The workspace could not be duplicated: %v.", err))
		if name == "" {
			return
		}
	}

	s.open(name)
	s.rename()
}

// rename starts editing the name of the selected workspace.
func (s *workspaceList) rename() {
	if item := s.list.CurrentItem(); item != nil {
		s.list.EditItem(item)
	}
}

// renamed gives the workspace of a row the name that was typed for it, or
// puts the name back if it cannot have that one.
func (s *workspaceList) renamed(row int, name string) {
	if row < 0 || row >= len(s.names) {
		return
	}
	previous := s.names[row]
	name = strings.TrimSpace(name)

	err := workspace.Rename(s.dir(), previous, name)
	if err != nil {
		name = previous
	} else if name != previous {
		s.names[row] = name
		if s.onRename != nil {
			s.onRename(name)
		}
	}

	// The items must not be replaced while one of them is reporting its
	// change. The name may belong somewhere else in the order.
	mainthread.Start(func() {
		if _, listErr := s.load(name); listErr != nil {
			s.warn(fmt.Sprintf("The workspaces could not be listed: %v.", listErr))
		}
		if err != nil {
			s.warn(fmt.Sprintf("The workspace was not renamed: %v.", err))
		}
	})
}

// remove deletes the selected workspace, with its environments and
// requests, once the user has agreed to it. The one beside it becomes the
// one in use. The last workspace cannot be removed.
func (s *workspaceList) remove() {
	row := s.list.CurrentRow()
	if row < 0 || row >= len(s.names) {
		return
	}
	name := s.names[row]

	if len(s.names) == 1 {
		s.warn("The last workspace cannot be removed.")
		return
	}
	if !s.confirm(fmt.Sprintf("Remove the workspace %q with all of its environments and requests? This cannot be undone.", name)) {
		return
	}
	if err := workspace.Remove(s.dir(), name); err != nil {
		s.warn(fmt.Sprintf("The workspace was not removed: %v.", err))
		return
	}

	// The next one down takes its place, or the one above if it was last.
	next := s.names[max(0, row-1)]
	if row+1 < len(s.names) {
		next = s.names[row+1]
	}
	s.open(next)
}

// workspaceIcon returns the icon shown before the name of a workspace.
func workspaceIcon() *qt.QIcon {
	return cachedIcon("workspace", func() *qt.QIcon { return drawIcon(iconSize*2, themeColour(colourWorkspaces), drawMonitor) })
}

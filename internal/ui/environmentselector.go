package ui

import (
	qt "github.com/mappu/miqt/qt6"
)

// noEnvironment is the entry of the environment selector for using none.
const noEnvironment = "None"

// selectorMinWidth is the least width of a drop-down of the selector, which
// it is squeezed to when the window has no room for its longest name.
const selectorMinWidth = 120

// environmentSelector selects the workspace and the environment in use, and
// has a button to edit the environment with. It is on a line of its own above
// the tabs of the requests and not in any of them, as both are the same for
// them all.
type environmentSelector struct {
	root        *qt.QWidget
	workspace   *qt.QComboBox
	environment *qt.QComboBox
	// edit opens the environment in use in the editor. The window says what
	// it does when clicked.
	edit *qt.QToolButton

	updating bool

	// onWorkspace is called when the user selects a workspace, with its
	// index.
	onWorkspace func(index int)
	// onEnvironment is called when the user selects an environment, with
	// its index, or -1 for none.
	onEnvironment func(index int)
}

func newEnvironmentSelector() *environmentSelector {
	s := &environmentSelector{
		root:        qt.NewQWidget2(),
		workspace:   qt.NewQComboBox2(),
		environment: qt.NewQComboBox2(),
		edit:        newIconButton(colourEnvironments, drawGlobe, "Edit the selected environment"),
	}

	label := func(text string) *qt.QWidget {
		label := qt.NewQLabel3(text)
		label.SetStyleSheet("font-weight: bold")
		return label.QWidget
	}

	// Each is as wide as its longest name, which a combo box otherwise is
	// only for the names it has when first shown, where the window has the
	// room. It can be narrower than that, down to selectorMinWidth, so that
	// a long name never makes the window wider: the name is cut short.
	for _, combo := range []*qt.QComboBox{s.workspace, s.environment} {
		combo.SetSizeAdjustPolicy(qt.QComboBox__AdjustToContents)
		combo.SetMinimumWidth(selectorMinWidth)
	}
	s.workspace.OnCurrentIndexChanged(func(index int) {
		if !s.updating && index >= 0 && s.onWorkspace != nil {
			s.onWorkspace(index)
		}
	})
	s.environment.OnCurrentIndexChanged(func(index int) {
		if !s.updating && index >= 0 && s.onEnvironment != nil {
			// The first entry is for no environment.
			s.onEnvironment(index - 1)
		}
	})

	layout := qt.NewQHBoxLayout(s.root)
	layout.SetContentsMargins(9, 4, 9, 4)
	layout.AddStretch()
	layout.AddWidget(label("Workspace:"))
	layout.AddWidget(s.workspace.QWidget)
	layout.AddSpacing(12)
	layout.AddWidget(label("Environment:"))
	layout.AddWidget(s.environment.QWidget)
	layout.AddWidget(s.edit.QWidget)

	s.setEnvironments(nil, -1)

	return s
}

// setWorkspaces lists the workspaces to choose from, with the one at the
// active index selected. onWorkspace is not called.
func (s *environmentSelector) setWorkspaces(names []string, active int) {
	s.fill(s.workspace, names, active)
}

// setEnvironments lists the environments to choose from, with the one at the
// active index selected, or none if it is -1. onEnvironment is not called.
// There is nothing for the button to edit when none is selected.
func (s *environmentSelector) setEnvironments(names []string, active int) {
	s.fill(s.environment, append([]string{noEnvironment}, names...), active+1)
	s.edit.SetEnabled(active >= 0)
}

// fill gives a combo box its entries and selects one of them, without what
// it is listening with being called. The entries are left alone if they are
// already those wanted, as this is also called while one is being chosen.
func (s *environmentSelector) fill(combo *qt.QComboBox, names []string, current int) {
	defer func(prev bool) { s.updating = prev }(s.updating)
	s.updating = true

	same := combo.Count() == len(names)
	for i := 0; same && i < len(names); i++ {
		same = combo.ItemText(i) == names[i]
	}
	if !same {
		combo.Clear()
		combo.AddItems(names)
	}
	combo.SetCurrentIndex(current)
}

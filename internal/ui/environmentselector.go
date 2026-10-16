package ui

import (
	qt "github.com/mappu/miqt/qt6"
)

// noEnvironment is the entry of the environment selector for using none, and
// noSet that of the set selector for choosing none.
const (
	noEnvironment = "None"
	noSet         = "None"
)

// selectorMinWidth is the least width of a drop-down of the selector, which
// it is squeezed to when the window has no room for its longest name.
const selectorMinWidth = 80

// selectorIconSize is the size of the icons of the titles of the drop-downs.
const selectorIconSize = 20

// environmentSelector selects the workspace and the environment in use, and
// the set of requests whose tabs are open. It is on a line of its own above
// the tabs of the requests and not in any of them, as all are the same for
// them all. The title of each drop-down is an icon button, which goes to
// where what it selects is kept; the window says where.
type environmentSelector struct {
	root        *qt.QWidget
	workspace   *qt.QComboBox
	environment *qt.QComboBox
	// set chooses the set of requests whose tabs are open, or none.
	set *qt.QComboBox

	// The titles of the drop-downs.
	workspaceTitle   *qt.QToolButton
	environmentTitle *qt.QToolButton
	setTitle         *qt.QToolButton

	updating bool

	// onWorkspace is called when the user selects a workspace, with its
	// index.
	onWorkspace func(index int)
	// onEnvironment is called when the user selects an environment, with
	// its index, or -1 for none.
	onEnvironment func(index int)
	// onSet is called when the user selects a set, with its index, or -1
	// for none.
	onSet func(index int)
}

func newEnvironmentSelector() *environmentSelector {
	s := &environmentSelector{
		root:        qt.NewQWidget2(),
		workspace:   qt.NewQComboBox2(),
		environment: qt.NewQComboBox2(),
		set:         qt.NewQComboBox2(),
	}

	// The titles are icons, in the colours of the tabs of the sidebar they
	// go to, small enough to keep the line as tall as the drop-downs.
	title := func(colour colourKind, glyph func(*qt.QPainter), tip string) *qt.QToolButton {
		button := newIconButton(colour, glyph, tip)
		size := qt.NewQSize2(selectorIconSize, selectorIconSize)
		button.SetIconSize(size)
		size.Delete()
		button.SetFocusPolicy(qt.NoFocus)
		return button
	}
	s.workspaceTitle = title(colourWorkspaces, drawMonitor, "Workspaces")
	s.environmentTitle = title(colourEnvironments, drawGlobe, "Edit environment")
	s.setTitle = title(colourSets, drawSets, "Request sets")

	// Each is as wide as its longest name, which a combo box otherwise is
	// only for the names it has when first shown, where the window has the
	// room. It can be narrower than that, down to selectorMinWidth, so that
	// a long name never makes the window wider: the name is cut short.
	for _, combo := range []*qt.QComboBox{s.workspace, s.environment, s.set} {
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

	s.set.OnCurrentIndexChanged(func(index int) {
		if !s.updating && index >= 0 && s.onSet != nil {
			// The first entry is for no set.
			s.onSet(index - 1)
		}
	})

	// The drop-downs are in a frame of their own, kept apart from the tabs
	// beneath by the margin below it. The frame is the style's own panel,
	// in the theme's colours, with no style sheet: under one Qt draws the
	// titles in the colours of a light theme whatever the theme is.
	panel := qt.NewQFrame2()
	panel.SetFrameStyle(int(qt.QFrame__StyledPanel) | int(qt.QFrame__Plain))
	frame := panel.QWidget
	outer := qt.NewQHBoxLayout(s.root)
	outer.SetContentsMargins(9, 6, 9, 8)
	// The frame takes the whole width, with the drop-downs at its right.
	outer.AddWidget(frame)

	layout := qt.NewQHBoxLayout(frame)
	layout.SetContentsMargins(6, 4, 6, 4)
	layout.AddStretch()
	layout.AddWidget(s.workspaceTitle.QWidget)
	layout.AddWidget(s.workspace.QWidget)
	layout.AddSpacing(12)
	layout.AddWidget(s.environmentTitle.QWidget)
	layout.AddWidget(s.environment.QWidget)
	layout.AddSpacing(12)
	layout.AddWidget(s.setTitle.QWidget)
	layout.AddWidget(s.set.QWidget)

	s.setEnvironments(nil, -1)
	s.setSets(nil, -1)

	return s
}

// setWorkspaces lists the workspaces to choose from, with the one at the
// active index selected. onWorkspace is not called.
func (s *environmentSelector) setWorkspaces(names []string, active int) {
	s.fill(s.workspace, names, active)
}

// setEnvironments lists the environments to choose from, with the one at the
// active index selected, or none if it is -1. onEnvironment is not called.
func (s *environmentSelector) setEnvironments(names []string, active int) {
	s.fill(s.environment, append([]string{noEnvironment}, names...), active+1)
}

// setSets lists the sets of requests to choose from, with the one at the
// active index selected, or none if it is -1. onSet is not called.
func (s *environmentSelector) setSets(names []string, active int) {
	s.fill(s.set, append([]string{noSet}, names...), active+1)
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

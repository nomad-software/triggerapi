package ui

import (
	qt "github.com/mappu/miqt/qt6"
)

// noEnvironment is the entry of the environment selector for using none.
const noEnvironment = "None"

// environmentSelector selects the environment in use. It is beside the tabs
// of the requests and not in any of them, as the environment is the same for
// them all.
type environmentSelector struct {
	root        *qt.QWidget
	environment *qt.QComboBox

	updating bool

	// onEnvironment is called when the user selects an environment, with
	// its index, or -1 for none.
	onEnvironment func(index int)
}

func newEnvironmentSelector() *environmentSelector {
	s := &environmentSelector{
		root:        qt.NewQWidget2(),
		environment: qt.NewQComboBox2(),
	}

	label := qt.NewQLabel3("Environment:")
	label.SetStyleSheet("font-weight: bold")

	s.environment.SetMinimumContentsLength(16)
	s.environment.OnCurrentIndexChanged(func(index int) {
		if !s.updating && s.onEnvironment != nil {
			// The first entry is for no environment.
			s.onEnvironment(index - 1)
		}
	})

	layout := qt.NewQHBoxLayout(s.root)
	layout.SetContentsMargins(0, 0, 0, 0)
	layout.AddWidget(label.QWidget)
	layout.AddWidget(s.environment.QWidget)

	s.setEnvironments(nil, -1)

	return s
}

// setEnvironments lists the environments to choose from, with the one at the
// active index selected, or none if it is -1. onEnvironment is not called.
func (s *environmentSelector) setEnvironments(names []string, active int) {
	s.updating = true
	defer func() { s.updating = false }()

	s.environment.Clear()
	s.environment.AddItem(noEnvironment)
	s.environment.AddItems(names)
	s.environment.SetCurrentIndex(active + 1)
}

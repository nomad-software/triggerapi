package ui

import (
	qt "github.com/mappu/miqt/qt6"
)

// noEnvironment is the entry of the environment selector for using none.
const noEnvironment = "None"

// header is the line above a request which names the saved request being
// edited, at the left, and selects the environment in use, at the right.
type header struct {
	root *qt.QWidget

	requestLabel *qt.QLabel
	request      *qt.QLabel
	environment  *qt.QComboBox

	updating bool

	// onEnvironment is called when the user selects an environment, with
	// its index, or -1 for none.
	onEnvironment func(index int)
}

func newHeader() *header {
	h := &header{
		root:         qt.NewQWidget2(),
		requestLabel: qt.NewQLabel3("Request:"),
		request:      qt.NewQLabel2(),
		environment:  qt.NewQComboBox2(),
	}

	environmentLabel := qt.NewQLabel3("Environment:")
	for _, label := range []*qt.QLabel{h.requestLabel, environmentLabel} {
		label.SetStyleSheet("font-weight: bold")
	}
	h.request.SetTextFormat(qt.PlainText)

	h.environment.SetMinimumContentsLength(16)
	h.environment.OnCurrentIndexChanged(func(index int) {
		if !h.updating && h.onEnvironment != nil {
			// The first entry is for no environment.
			h.onEnvironment(index - 1)
		}
	})

	layout := qt.NewQHBoxLayout(h.root)
	layout.SetContentsMargins(0, 0, 0, 0)
	layout.AddWidget(h.requestLabel.QWidget)
	layout.AddWidget(h.request.QWidget)
	layout.AddStretch()
	layout.AddWidget(environmentLabel.QWidget)
	layout.AddWidget(h.environment.QWidget)

	h.setRequest("")
	h.setEnvironments(nil, -1)

	return h
}

// setRequest shows the name of the saved request being edited. The request
// is left out if name is empty, as the request is not a saved one.
func (h *header) setRequest(name string) {
	h.request.SetText(name)
	h.requestLabel.SetVisible(name != "")
	h.request.SetVisible(name != "")
}

// setEnvironments lists the environments to choose from, with the one at the
// active index selected, or none if it is -1. onEnvironment is not called.
func (h *header) setEnvironments(names []string, active int) {
	h.updating = true
	defer func() { h.updating = false }()

	h.environment.Clear()
	h.environment.AddItem(noEnvironment)
	h.environment.AddItems(names)
	h.environment.SetCurrentIndex(active + 1)
}

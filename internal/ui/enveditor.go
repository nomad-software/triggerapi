package ui

import (
	qt "github.com/mappu/miqt/qt6"

	"github.com/nomad-software/triggerapi/internal/env"
	"github.com/nomad-software/triggerapi/internal/rest"
)

// envEditor edits the name and variables of one environment. It takes the
// place of the request UI while it is open.
type envEditor struct {
	root *qt.QWidget

	name      *qt.QLineEdit
	variables *keyValueTable
	close     *qt.QPushButton

	updating bool

	// onChange is called with the edited values after every change.
	onChange func(name string, variables []env.Variable)
}

func newEnvEditor() *envEditor {
	e := &envEditor{
		root:      qt.NewQWidget2(),
		name:      qt.NewQLineEdit2(),
		variables: newKeyValueTable("Variable", "Value"),
		close:     qt.NewQPushButton3("Close"),
	}

	e.name.SetPlaceholderText("Environment name")
	e.variables.describe("Description")
	// Variables with one name are alternatives, of which one is in use.
	e.variables.exclusive = true

	bar := qt.NewQHBoxLayout2()
	bar.AddWidget(qt.NewQLabel3("Environment").QWidget)
	bar.AddWidget2(e.name.QWidget, 1)
	bar.AddWidget(e.close.QWidget)

	layout := qt.NewQVBoxLayout(e.root)
	layout.AddLayout(bar.QLayout)
	layout.AddWidget2(e.variables.widget(), 1)

	e.name.OnTextChanged(func(string) { e.changed() })
	e.variables.onChange = e.changed

	return e
}

// load shows the environment without calling onChange.
func (e *envEditor) load(environment *env.Environment) {
	items := make([]describedPair, 0, len(environment.Variables))
	for _, v := range environment.Variables {
		items = append(items, describedPair{rest.KeyValue{Key: v.Key, Value: v.Value, Enabled: v.Enabled}, v.Description})
	}

	e.updating = true
	e.name.SetText(environment.Name)
	e.variables.setDescribed(items)
	e.updating = false
}

func (e *envEditor) changed() {
	if e.updating || e.onChange == nil {
		return
	}

	var variables []env.Variable
	for _, item := range e.variables.allDescribed() {
		variables = append(variables, env.Variable{Key: item.Key, Value: item.Value, Enabled: item.Enabled, Description: item.Description})
	}
	e.onChange(e.name.Text(), variables)
}

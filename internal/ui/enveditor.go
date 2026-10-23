package ui

import (
	"strings"

	qt "github.com/mappu/miqt/qt6"

	"github.com/nomad-software/triggerapi/internal/env"
	"github.com/nomad-software/triggerapi/internal/rest"
)

// envEditor edits the name and variables of one environment. It takes the
// place of the request UI while it is open.
type envEditor struct {
	root *qt.QWidget

	name *qt.QLineEdit
	// named is the name the environment has. It is not the one in the
	// field while that is empty, as an environment cannot be without a
	// name.
	named     string
	variables *keyValueTable
	close     *qt.QPushButton

	updating bool

	// onChange is called with the edited values after every change.
	onChange func(name string, variables []env.Variable)
	// onSort is called with how the variables are shown sorted after the
	// user sorts them by the headings, which is nil for not at all.
	onSort func([]env.SortKey)
}

// sortNames are the columns variables can be sorted by, with the names they
// are kept by in the file of environments.
var sortNames = map[int]string{
	colKey:         env.SortVariable,
	colValue:       env.SortValue,
	colDescription: env.SortDescription,
}

// envSort returns the sort of the variables of an environment that keys of
// the table are.
func envSort(keys []sortKey) []env.SortKey {
	var sort []env.SortKey
	for _, k := range keys {
		sort = append(sort, env.SortKey{Column: sortNames[k.column], Descending: k.descending})
	}
	return sort
}

// tableSort returns the keys of the table a sort of the variables of an
// environment is, leaving out a column this version does not know.
func tableSort(sort []env.SortKey) []sortKey {
	var keys []sortKey
	for _, k := range sort {
		for column, name := range sortNames {
			if name == k.Column {
				keys = append(keys, sortKey{column, k.Descending})
			}
		}
	}
	return keys
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
	// The variables are kept in the order they were added, which decides
	// nothing but that, and shown sorted only as the user chooses.
	e.variables.sortByHeadings()
	e.variables.onSort = func(keys []sortKey) {
		if e.onSort != nil {
			e.onSort(envSort(keys))
		}
	}

	bar := qt.NewQHBoxLayout2()
	bar.AddWidget(qt.NewQLabel3("Environment").QWidget)
	bar.AddWidget2(e.name.QWidget, 1)
	bar.AddWidget(e.close.QWidget)

	layout := qt.NewQVBoxLayout(e.root)
	layout.AddLayout(bar.QLayout)
	layout.AddWidget2(e.variables.widget(), 1)

	e.name.OnTextChanged(func(string) { e.changed() })
	// A field left empty is given back the name the environment has.
	e.name.OnEditingFinished(func() {
		if strings.TrimSpace(e.name.Text()) == "" {
			e.name.SetText(e.named)
		}
	})
	e.variables.onChange = e.changed

	return e
}

// load shows the environment, with its variables sorted as it says, without
// calling onChange or onSort.
func (e *envEditor) load(environment *env.Environment) {
	items := make([]describedPair, 0, len(environment.Variables))
	for _, v := range environment.Variables {
		items = append(items, describedPair{rest.KeyValue{Key: v.Key, Value: v.Value, Enabled: v.Enabled}, v.Description})
	}

	e.updating = true
	e.named = environment.Name
	e.name.SetText(environment.Name)
	e.variables.setDescribed(items)
	e.variables.setSort(tableSort(environment.Sort))
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
	// The name is kept while the field is empty, which it has to be on the
	// way to another name being typed.
	if name := e.name.Text(); strings.TrimSpace(name) != "" {
		e.named = name
	}
	e.onChange(e.named, variables)
}

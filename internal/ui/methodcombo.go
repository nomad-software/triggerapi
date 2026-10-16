package ui

import (
	"slices"
	"strings"

	qt "github.com/mappu/miqt/qt6"
)

// methodCombo is a drop-down of what can be called on a server, grouped under
// headings: the methods of a gRPC server under their services, or the tools,
// resources and prompts of an MCP server. Each is given by its full name, as
// heading/name. The list shows the short name of each beneath its heading,
// and the closed drop-down what closed makes of the full name of the one
// selected, which is the full name itself unless it is set.
type methodCombo struct {
	*qt.QComboBox

	model *qt.QStandardItemModel
	// names holds the full name on each row, as heading/name, such as
	// package.Service/Method. It is empty for the heading rows.
	names []string
	// closed returns what the closed drop-down shows for a full name.
	closed func(full string) string
}

func newMethodCombo() *methodCombo {
	c := &methodCombo{QComboBox: qt.NewQComboBox2(), model: qt.NewQStandardItemModel()}

	c.SetModel(c.model.QAbstractItemModel)
	// Stop a long method name from forcing the window wider: the width is
	// not taken from the names, and the combo fills whatever room there is.
	c.SetSizeAdjustPolicy(qt.QComboBox__AdjustToMinimumContentsLengthWithIcon)
	c.SetSizePolicy2(qt.QSizePolicy__Expanding, qt.QSizePolicy__Fixed)
	c.SetMaxVisibleItems(25)

	// The text of a row is the short name, so the full name is substituted
	// when the closed drop-down is drawn.
	c.OnInitStyleOption(func(super func(*qt.QStyleOptionComboBox), option *qt.QStyleOptionComboBox) {
		super(option)
		if c.closed != nil {
			option.SetCurrentText(c.closed(c.current()))
		} else {
			option.SetCurrentText(c.current())
		}
	})

	return c
}

// setMethods replaces the list with the methods, which must be sorted so that
// those of a service are together, and selects one of them. If selected is
// not among them it is listed first, on its own. If it is empty the first
// method is selected.
func (c *methodCombo) setMethods(methods []string, selected string) {
	c.model.Clear()
	c.names = nil

	add := func(text, name string) *qt.QStandardItem {
		item := qt.NewQStandardItem2(text)
		c.model.AppendRowWithItem(item)
		c.names = append(c.names, name)
		return item
	}

	if selected != "" && !slices.Contains(methods, selected) {
		add(selected, selected)
	}

	service := ""
	for _, name := range methods {
		parent, method, _ := strings.Cut(name, "/")
		if parent != service {
			service = parent

			// A heading can be seen but not chosen.
			heading := add(service, "")
			heading.SetFlags(qt.ItemIsEnabled)
			font := heading.Font()
			font.SetBold(true)
			heading.SetFont(font)
		}
		add("    "+method, name)
	}

	if selected == "" {
		c.SetCurrentIndex(slices.IndexFunc(c.names, func(name string) bool { return name != "" }))
	} else {
		c.SetCurrentIndex(slices.Index(c.names, selected))
	}
	// The closed drop-down is not redrawn if the index did not change.
	c.Update()
}

// current returns the full name of the selected method, or an empty string
// if there is none.
func (c *methodCombo) current() string {
	if index := c.CurrentIndex(); index >= 0 && index < len(c.names) {
		return c.names[index]
	}
	return ""
}

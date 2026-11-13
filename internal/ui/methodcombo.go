package ui

import (
	"github.com/nomad-software/qute/qtcore"
	"github.com/nomad-software/qute/qtgui"
	"github.com/nomad-software/qute/qtwidgets"

	"slices"
	"strings"
)

// methodCombo is a drop-down of what can be called on a server, grouped under
// headings: the methods of a gRPC server under their services, or the tools,
// resources and prompts of an MCP server. Each is given by its full name, as
// heading/name. The list shows the short name of each beneath its heading,
// and the closed drop-down what closed makes of the full name of the one
// selected, which is the full name itself unless it is set.
type methodCombo struct {
	qtwidgets.QComboBoxBase

	model *qtgui.QStandardItemModel
	// names holds the full name on each row, as heading/name, such as
	// package.Service/Method. It is empty for the heading rows.
	names []string
	// closed returns what the closed drop-down shows for a full name.
	closed func(full string) string
}

func newMethodCombo() *methodCombo {
	c := &methodCombo{}
	qtwidgets.NewQComboBoxWith(c, nil)
	c.model = qtgui.NewQStandardItemModel(c)

	c.SetModel(c.model.AsQAbstractItemModel())
	// Stop a long method name from forcing the window wider: the width is
	// not taken from the names, and the combo fills whatever room there is.
	c.SetSizeAdjustPolicy(qtwidgets.QComboBoxAdjustToMinimumContentsLengthWithIcon)
	c.SetSizePolicy(qtwidgets.NewQSizePolicyWithPolicy(qtwidgets.QSizePolicyExpanding, qtwidgets.QSizePolicyFixed))
	c.SetMaxVisibleItems(25)

	return c
}

// InitStyleOption substitutes the full name for the text of the row, which
// is the short name, when the closed drop-down is drawn.
func (c *methodCombo) InitStyleOption(option *qtwidgets.QStyleOptionComboBox) {
	c.Super().InitStyleOption(option)
	if c.closed != nil {
		option.SetCurrentText(c.closed(c.current()))
	} else {
		option.SetCurrentText(c.current())
	}
}

// setMethods replaces the list with the methods, which must be sorted so that
// those of a service are together, and selects one of them. If selected is
// not among them it is listed first, on its own. If it is empty the first
// method is selected.
func (c *methodCombo) setMethods(methods []string, selected string) {
	c.model.Clear()
	c.names = nil

	add := func(text, name string) *qtgui.QStandardItem {
		item := qtgui.NewQStandardItemWithText(text)
		c.model.AppendRow(item)
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
			heading.SetFlags(qtcore.ItemIsEnabled)
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

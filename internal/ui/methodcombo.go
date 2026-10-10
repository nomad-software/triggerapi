package ui

import (
	"slices"
	"strings"

	qt "github.com/mappu/miqt/qt6"
)

// methodCombo is a drop-down of gRPC methods grouped under the name of their
// service. The list shows the short name of each method beneath a heading for
// its service, and the closed drop-down shows the full name of the selected
// method.
type methodCombo struct {
	*qt.QComboBox

	model *qt.QStandardItemModel
	// names holds the full name of the method on each row, as
	// package.Service/Method. It is empty for the heading rows.
	names []string
}

func newMethodCombo() *methodCombo {
	c := &methodCombo{QComboBox: qt.NewQComboBox2(), model: qt.NewQStandardItemModel()}

	c.SetModel(c.model.QAbstractItemModel)
	// Stop a long method name from forcing the window wider.
	c.SetSizeAdjustPolicy(qt.QComboBox__AdjustToMinimumContentsLengthWithIcon)
	c.SetMinimumContentsLength(20)
	c.SetMaxVisibleItems(25)

	// The text of a row is the short name, so the full name is substituted
	// when the closed drop-down is drawn.
	c.OnInitStyleOption(func(super func(*qt.QStyleOptionComboBox), option *qt.QStyleOptionComboBox) {
		super(option)
		option.SetCurrentText(c.current())
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

// selectMethod selects a method by its full name.
func (c *methodCombo) selectMethod(name string) {
	if index := slices.Index(c.names, name); index >= 0 && name != "" {
		c.SetCurrentIndex(index)
	}
}

// count returns the number of methods listed.
func (c *methodCombo) count() int {
	n := 0
	for _, name := range c.names {
		if name != "" {
			n++
		}
	}
	return n
}

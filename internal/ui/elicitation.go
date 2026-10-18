package ui

import (
	"fmt"
	"slices"

	qt "github.com/mappu/miqt/qt6"

	"github.com/nomad-software/triggerapi/internal/mcp"
)

// askElicitation shows what an MCP server asks the user for in a dialog, and
// calls done with how they answered once they have. It returns what closes
// the dialog, as cancelled, if it is still open, as it is when the call is
// given up. Tests replace it rather than let the dialog open.
var askElicitation = func(parent *qt.QWidget, server string, e *mcp.Elicitation, done func(*mcp.ElicitAnswer)) (close func()) {
	return newElicitDialog(parent, server, e).open(done)
}

// elicitDialog asks the user to fill in the form an MCP server sent, and to
// accept it, decline it or cancel.
type elicitDialog struct {
	*qt.QDialog
	inputs  []elicitInput
	problem statusLine
	// answer is how the user answered, which is nil until they have:
	// closing the dialog any other way cancels.
	answer *mcp.ElicitAnswer
}

// elicitInput is the widget of a field of the form, and what reads it: the
// value, and false if the field is left out of the answer. The widget is
// beside its description, if it has one, in what is put in the form.
type elicitInput struct {
	field  mcp.Field
	widget *qt.QWidget
	read   func() (any, bool, error)
}

// elicitMinWidth is the narrowest the dialog is.
const elicitMinWidth = 460

func newElicitDialog(parent *qt.QWidget, server string, e *mcp.Elicitation) *elicitDialog {
	d := &elicitDialog{QDialog: qt.NewQDialog(parent), problem: newStatusLine()}
	d.SetWindowTitle("The Server Asks")
	d.SetMinimumWidth(elicitMinWidth)

	layout := qt.NewQVBoxLayout(d.QWidget)
	layout.SetContentsMargins(18, 18, 18, 18)
	layout.SetSpacing(12)

	from := qt.NewQLabel3(server + " asks:")
	from.SetEnabled(false)
	from.SetWordWrap(true)
	layout.AddWidget(from.QWidget)
	message := qt.NewQLabel3(e.Message)
	message.SetWordWrap(true)
	message.SetTextInteractionFlags(qt.TextSelectableByMouse)
	layout.AddWidget(message.QWidget)

	if len(e.Fields) > 0 {
		form := qt.NewQFormLayout2()
		for _, field := range e.Fields {
			input, placed := newElicitInput(field)
			d.inputs = append(d.inputs, input)
			label := field.Label()
			if field.Required {
				label += " *"
			}
			form.AddRow3(label, placed)
		}
		layout.AddLayout(form.QLayout)
	}
	layout.AddWidget(d.problem.QWidget)

	accept := qt.NewQPushButton3("Accept")
	accept.SetDefault(true)
	accept.OnClicked(d.accept)
	decline := qt.NewQPushButton3("Decline")
	decline.SetAutoDefault(false)
	decline.OnClicked(func() { d.finish(&mcp.ElicitAnswer{Action: mcp.ActionDecline}) })
	cancel := qt.NewQPushButton3("Cancel")
	cancel.SetAutoDefault(false)
	cancel.OnClicked(d.Reject)

	buttons := qt.NewQHBoxLayout2()
	buttons.AddStretch()
	buttons.AddWidget(accept.QWidget)
	buttons.AddWidget(decline.QWidget)
	buttons.AddWidget(cancel.QWidget)
	layout.AddLayout(buttons.QLayout)
	return d
}

// newElicitInput returns the input of a field: a check box for a boolean, a
// drop-down for one of a few options, a check box for each of the options of
// which any can be chosen, and otherwise a line to type the value in. What
// is put in the form for it has its description beneath it.
func newElicitInput(field mcp.Field) (input elicitInput, placed *qt.QWidget) {
	input.field = field
	var widget *qt.QWidget
	switch {
	case field.Type == mcp.FieldBoolean:
		box := qt.NewQCheckBox2()
		box.SetChecked(field.Default == true)
		input.read = func() (any, bool, error) { return box.IsChecked(), true, nil }
		widget = box.QWidget

	case field.Type == mcp.FieldChoices:
		chosen, _ := field.Default.([]any)
		holder := qt.NewQWidget2()
		column := qt.NewQVBoxLayout(holder)
		column.SetContentsMargins(0, 0, 0, 0)
		var boxes []*qt.QCheckBox
		for _, option := range field.Options {
			box := qt.NewQCheckBox3(optionLabel(option))
			box.SetChecked(slices.Contains(chosen, any(option.Value)))
			column.AddWidget(box.QWidget)
			boxes = append(boxes, box)
		}
		input.read = func() (any, bool, error) {
			values := []string{}
			for i, box := range boxes {
				if box.IsChecked() {
					values = append(values, field.Options[i].Value)
				}
			}
			if len(values) == 0 && field.Required {
				return nil, false, fmt.Errorf("choose at least one for %s", field.Label())
			}
			return values, true, nil
		}
		widget = holder

	case len(field.Options) > 0:
		combo := qt.NewQComboBox2()
		values := []string{}
		// One that need not be given can be left out.
		if !field.Required {
			combo.AddItem("")
			values = append(values, "")
		}
		for _, option := range field.Options {
			combo.AddItem(optionLabel(option))
			values = append(values, option.Value)
		}
		if at := slices.Index(values, fmt.Sprint(field.Default)); field.Default != nil && at >= 0 {
			combo.SetCurrentIndex(at)
		}
		input.read = func() (any, bool, error) {
			value := values[max(combo.CurrentIndex(), 0)]
			return value, value != "", nil
		}
		widget = combo.QWidget

	default:
		line := qt.NewQLineEdit2()
		if field.Default != nil {
			line.SetText(fmt.Sprint(field.Default))
		}
		line.SetPlaceholderText(field.Format)
		input.read = func() (any, bool, error) { return field.Parse(line.Text()) }
		widget = line.QWidget
	}

	input.widget = widget
	if field.Description == "" {
		return input, widget
	}
	holder := qt.NewQWidget2()
	column := qt.NewQVBoxLayout(holder)
	column.SetContentsMargins(0, 0, 0, 0)
	column.SetSpacing(2)
	column.AddWidget(widget)
	description := qt.NewQLabel3(field.Description)
	description.SetWordWrap(true)
	description.SetEnabled(false)
	column.AddWidget(description.QWidget)
	return input, holder
}

// optionLabel returns what an option is called: its title, or its value.
func optionLabel(option mcp.Option) string {
	if option.Title != "" {
		return option.Title
	}
	return option.Value
}

// content returns the values of the form, or the first that is not valid.
func (d *elicitDialog) content() (map[string]any, error) {
	content := map[string]any{}
	for _, input := range d.inputs {
		value, set, err := input.read()
		if err != nil {
			return nil, err
		}
		if set {
			content[input.field.Name] = value
		}
	}
	return content, nil
}

// accept answers with the form, if all of it is valid, and otherwise says
// what is not.
func (d *elicitDialog) accept() {
	content, err := d.content()
	if err != nil {
		d.problem.set(err.Error() + ".")
		return
	}
	d.finish(&mcp.ElicitAnswer{Action: mcp.ActionAccept, Content: content})
}

// finish closes the dialog with the answer.
func (d *elicitDialog) finish(answer *mcp.ElicitAnswer) {
	d.answer = answer
	d.Accept()
}

// open shows the dialog, which keeps the window from being used until it is
// answered, and calls done with the answer once it is. It returns what
// closes it, as cancelled, if it is still open.
func (d *elicitDialog) open(done func(*mcp.ElicitAnswer)) func() {
	return openDialog(d.QDialog, func() {
		answer := d.answer
		if answer == nil {
			answer = &mcp.ElicitAnswer{Action: mcp.ActionCancel}
		}
		done(answer)
	})
}

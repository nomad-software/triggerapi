package ui

import (
	qt "github.com/mappu/miqt/qt6"

	"github.com/nomad-software/postie/internal/collection"
)

// requestTypes are the types of request that can be created, in the order
// they are offered.
var requestTypes = []struct {
	typ    collection.Type
	colour string
	// glyph draws the symbol of the type. See drawIcon.
	glyph func(*qt.QPainter)
}{
	{collection.TypeREST, "#2e9e5b", drawBraces},
	{collection.TypeGRPC, "#2f7fd1", drawArrows},
}

// requestTypeIcons holds the icons once they have been drawn.
var requestTypeIcons = map[collection.Type]*qt.QIcon{}

// requestTypeIcon returns the icon for a type of request, or nil if the type
// is not one that can be created.
func requestTypeIcon(typ collection.Type) *qt.QIcon {
	if icon, ok := requestTypeIcons[typ]; ok {
		return icon
	}

	for _, t := range requestTypes {
		if t.typ == typ {
			icon := drawIcon(iconSize*2, t.colour, t.glyph)
			requestTypeIcons[typ] = icon
			return icon
		}
	}
	return nil
}

// requestTypeDialog asks what type of request to create, offering each type
// as an icon with its name beneath.
type requestTypeDialog struct {
	*qt.QDialog

	buttons map[collection.Type]*qt.QToolButton
	// chosen is the type that was picked, if the dialog was accepted.
	chosen collection.Type
}

func newRequestTypeDialog(parent *qt.QWidget) *requestTypeDialog {
	d := &requestTypeDialog{
		QDialog: qt.NewQDialog(parent),
		buttons: map[collection.Type]*qt.QToolButton{},
	}
	d.SetWindowTitle("New Request")

	types := qt.NewQHBoxLayout2()
	for _, t := range requestTypes {
		button := qt.NewQToolButton2()
		button.SetToolButtonStyle(qt.ToolButtonTextUnderIcon)
		button.SetAutoRaise(true)
		button.SetIcon(requestTypeIcon(t.typ))
		button.SetIconSize(qt.NewQSize2(iconSize, iconSize))
		button.SetText(typeLabels[t.typ])
		button.SetMinimumSize2(iconSize+48, iconSize+56)
		button.OnClicked(func() {
			d.chosen = t.typ
			d.Accept()
		})

		d.buttons[t.typ] = button
		types.AddWidget(button.QWidget)
	}

	cancel := qt.NewQPushButton3("Cancel")
	cancel.OnClicked(d.Reject)

	bottom := qt.NewQHBoxLayout2()
	bottom.AddStretch()
	bottom.AddWidget(cancel.QWidget)

	layout := qt.NewQVBoxLayout(d.QWidget)
	layout.AddWidget(qt.NewQLabel3("Choose the type of request to create.").QWidget)
	layout.AddLayout(types.QLayout)
	layout.AddLayout(bottom.QLayout)

	return d
}

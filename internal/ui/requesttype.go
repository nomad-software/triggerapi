package ui

import (
	qt "github.com/mappu/miqt/qt6"

	"github.com/nomad-software/triggerapi/internal/collection"
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

// newRequestTypeDialog returns a dialog which asks what type of request to
// create.
func newRequestTypeDialog(parent *qt.QWidget) *choiceDialog[collection.Type] {
	choices := make([]choice[collection.Type], len(requestTypes))
	for i, t := range requestTypes {
		choices[i] = choice[collection.Type]{t.typ, typeLabels[t.typ], requestTypeIcon(t.typ)}
	}
	return newChoiceDialog(parent, "New Request", "Choose the type of request to create.", choices)
}

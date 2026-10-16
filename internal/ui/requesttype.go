package ui

import (
	qt "github.com/mappu/miqt/qt6"

	"github.com/nomad-software/triggerapi/internal/collection"
)

// requestTypes are the types of request that can be created, in the order
// they are offered.
var requestTypes = []struct {
	typ collection.Type
	// label is the name the type is shown by.
	label  string
	colour colourKind
	// glyph draws the symbol of the type. See drawIcon.
	glyph func(*qt.QPainter)
}{
	{collection.TypeREST, "REST", colourREST, drawBraces},
	{collection.TypeGRPC, "gRPC", colourGRPC, drawArrows},
	{collection.TypeWebSocket, "WebSocket", colourWebSocket, drawPulse},
	{collection.TypeGraphQL, "GraphQL", colourGraphQL, drawGraph},
	{collection.TypeMCP, "MCP", colourMCP, drawPlug},
}

// typeLabels are the names the request types are shown by.
var typeLabels = func() map[collection.Type]string {
	labels := map[collection.Type]string{}
	for _, t := range requestTypes {
		labels[t.typ] = t.label
	}
	return labels
}()

// requestTypeIcon returns the icon for a type of request, or nil if the type
// is not one that can be created. It is drawn again after the colours
// change.
func requestTypeIcon(typ collection.Type) *qt.QIcon {
	for _, t := range requestTypes {
		if t.typ == typ {
			return cachedIcon("type "+string(typ), func() *qt.QIcon {
				return drawIcon(iconSize*2, themeColour(t.colour), t.glyph)
			})
		}
	}
	return nil
}

// newRequestTypeDialog returns a dialog which asks what type of request to
// create.
func newRequestTypeDialog(parent *qt.QWidget) *choiceDialog[collection.Type] {
	choices := make([]choice[collection.Type], len(requestTypes))
	for i, t := range requestTypes {
		choices[i] = choice[collection.Type]{t.typ, t.label, requestTypeIcon(t.typ)}
	}
	return newChoiceDialog(parent, "New Request", "Choose the type of request to create.", choices)
}

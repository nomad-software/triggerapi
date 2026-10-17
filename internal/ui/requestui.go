package ui

import (
	qt "github.com/mappu/miqt/qt6"

	"github.com/nomad-software/triggerapi/internal/collection"
)

// requestEditor is the panel a type of request is composed in. The window
// shows each on a page of its own, above a response panel (requestUI), and
// goes through this for everything that is the same for every type.
type requestEditor interface {
	// widget returns the panel.
	widget() *qt.QWidget
	// show fills the panel with a request of its type, without reporting
	// a change.
	show(r *collection.Request)
	// compose returns the request in the panel, with a name. It is not in
	// the store.
	compose(name string) *collection.Request
	// saveButton returns the button which saves a request that is not
	// saved yet.
	saveButton() *qt.QPushButton
	// highlighter returns what colours the placeholders of the panel.
	highlighter() *placeholderHighlighter
	// setOnChange sets what is called after the user edits the request.
	setOnChange(onChange func())
	// setCodeFont sets the font of the code the panel shows.
	setCodeFont(family string)
	// refreshPlaceholders recolours the placeholders after the environment
	// or the colours have changed, without reporting a change.
	refreshPlaceholders()
	// recolour shows what is in the colours of the settings in them again.
	recolour()
	// escape closes what the panel shows in place of its fields, and
	// reports if there was anything to close.
	escape() bool
}

// requestUI is the UI of one type of request: the panel it is composed in,
// and the panel its response is shown in beneath it, on a page of the stack
// of the window.
type requestUI struct {
	typ      collection.Type
	page     int
	editor   requestEditor
	response *responsePanel
	// send sends the request, as Ctrl+Return does.
	send func()
	// shown is called after a request of the type has been shown, if it is
	// not nil.
	shown func()
	// endpoints is the table of the endpoints of the server, from which
	// requests of the type are made, or nil if the type has none.
	endpoints *endpointSource
	// timing records that requests of the type go through rest.Send, and so
	// their responses have a Timing tab.
	timing bool
}

// panelState is what every requestEditor keeps: what colours its
// placeholders, whether it is being filled, which is not reported as a change,
// and what to call when the user changes the request.
type panelState struct {
	// highlight colours the placeholders in the fields of the request.
	highlight *placeholderHighlighter
	updating  bool
	// onChange is called after the user edits any part of the request.
	onChange func()
}

func newPanelState() panelState {
	return panelState{highlight: newPlaceholderHighlighter()}
}

func (s *panelState) highlighter() *placeholderHighlighter { return s.highlight }

func (s *panelState) setOnChange(onChange func()) { s.onChange = onChange }

// changed reports an edit, unless the panel is being filled.
func (s *panelState) changed() {
	if !s.updating && s.onChange != nil {
		s.onChange()
	}
}

// quietly makes changes to the panel without reporting them as edits.
func (s *panelState) quietly(change func()) {
	defer func(prev bool) { s.updating = prev }(s.updating)
	s.updating = true
	change()
}

// refreshPlaceholders recolours the placeholders after the environment has
// changed. Recolouring a document reports its text as changed, which must not
// be taken for an edit.
func (s *panelState) refreshPlaceholders() {
	s.quietly(s.highlight.refresh)
}

// escape reports that nothing was closed: a panel has nothing to close
// unless it says otherwise.
func (s *panelState) escape() bool { return false }

// newRequestPage returns the page of a request UI: the request above the
// response, divided by a handle that is dragged to share the height.
func newRequestPage(ui *requestUI) *qt.QSplitter {
	split := qt.NewQSplitter3(qt.Vertical)
	split.AddWidget(ui.editor.widget())
	split.AddWidget(ui.response.root)
	split.SetChildrenCollapsible(false)
	split.SetStretchFactor(0, 1)
	split.SetStretchFactor(1, 1)
	return split
}

// requestUI returns the UI of a type of request. A type that is not known is
// REST, as it is when it is loaded.
func (w *Window) requestUI(typ collection.Type) *requestUI {
	for _, ui := range w.uis {
		if ui.typ == typ {
			return ui
		}
	}
	return w.uis[0]
}

// pageUI returns the request UI on a page of the stack, or nil if the page is
// not one (the environment editor).
func (w *Window) pageUI(page int) *requestUI {
	for _, ui := range w.uis {
		if ui.page == page {
			return ui
		}
	}
	return nil
}

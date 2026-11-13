package ui

import (
	"github.com/nomad-software/qute/qtcore"
	"github.com/nomad-software/qute/qtwidgets"
)

// statusLine is a line of a request panel which says what went wrong with
// something loaded for it, such as the service definition of a gRPC server
// or the schema of a GraphQL one, in the colour of errors. It is hidden
// while there is nothing to say.
type statusLine struct {
	*qtwidgets.QLabel
}

func newStatusLine() statusLine {
	s := statusLine{qtwidgets.NewQLabel(nil)}
	s.SetTextFormat(qtcore.PlainText)
	s.SetWordWrap(true)
	s.SetTextInteractionFlags(qtcore.TextSelectableByMouse)
	s.SetVisible(false)
	s.recolour()
	return s
}

// set shows the text, or hides the line if it is empty.
func (s statusLine) set(text string) {
	s.SetText(text)
	s.SetVisible(text != "")
}

// recolour gives the line the colour of errors in the settings.
func (s statusLine) recolour() {
	s.SetStyleSheet("color: " + themeColour(colourError))
}

// always reports true. It is what a highlighter or folder is given for an
// editor that always holds the language it colours or folds.
func always() bool { return true }

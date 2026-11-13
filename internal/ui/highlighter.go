package ui

import (
	"github.com/nomad-software/qute/qtgui"
)

// highlighter is a syntax highlighter of a text document which colours each
// line with a function of its own, given after it is made. The function sets
// the formats and states of the line through the highlighter.
type highlighter struct {
	qtgui.QSyntaxHighlighterBase
	block func(text string)
}

// newHighlighter returns a highlighter of a document, which colours nothing
// until it is given its function.
func newHighlighter(doc *qtgui.QTextDocument) *highlighter {
	h := &highlighter{}
	qtgui.NewQSyntaxHighlighterWith(h, doc)
	h.SetDocument(doc)
	return h
}

// HighlightBlock colours a line of the document.
func (h *highlighter) HighlightBlock(text string) {
	if h.block != nil {
		h.block(text)
	}
}

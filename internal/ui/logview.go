package ui

import (
	"github.com/nomad-software/qute/qtwidgets"
)

// What is shown as a log in an editor: entries, oldest first, with a blank
// line between them, as the messages of a WebSocket connection, the events of
// a stream and those of an MCP call are.

// appendEntry adds an entry to the end of a log.
func appendEntry(log *qtwidgets.QPlainTextEdit, text string) {
	if log.Document().IsEmpty() {
		log.SetPlainText(text)
		return
	}
	log.AppendPlainText("\n" + text)
}

// atEnd reports if the end of a log, where the newest entry is, is in view.
func atEnd(log *qtwidgets.QPlainTextEdit) bool {
	bar := log.VerticalScrollBar()
	return bar.Value() == bar.Maximum()
}

// scrollToEnd brings the end of a log, where the newest entry is, into view.
func scrollToEnd(log *qtwidgets.QPlainTextEdit) {
	bar := log.VerticalScrollBar()
	bar.SetValue(bar.Maximum())
}

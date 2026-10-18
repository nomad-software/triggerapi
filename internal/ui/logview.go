package ui

import (
	qt "github.com/mappu/miqt/qt6"
)

// What is shown as a log in an editor: entries, oldest first, with a blank
// line between them, as the messages of a WebSocket connection, the events of
// a stream and those of an MCP call are.

// appendEntry adds an entry to the end of a log.
func appendEntry(log *qt.QPlainTextEdit, text string) {
	if log.Document().IsEmpty() {
		log.SetPlainText(text)
		return
	}
	log.AppendPlainText("\n" + text)
}

// atEnd reports if the end of a log, where the newest entry is, is in view.
func atEnd(log *qt.QPlainTextEdit) bool {
	bar := log.VerticalScrollBar()
	return bar.Value() == bar.Maximum()
}

// scrollToEnd brings the end of a log, where the newest entry is, into view.
func scrollToEnd(log *qt.QPlainTextEdit) {
	bar := log.VerticalScrollBar()
	bar.SetValue(bar.Maximum())
}

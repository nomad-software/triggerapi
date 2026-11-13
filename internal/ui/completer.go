package ui

import (
	"github.com/nomad-software/qute/qtcore"
	"github.com/nomad-software/qute/qtgui"
	"github.com/nomad-software/qute/qtwidgets"

	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/nomad-software/triggerapi/internal/completion"
)

// completer offers what can be typed at the cursor of a code editor, as the
// GraphQL query and the gRPC message are completed, in a list beneath it: as
// a name or a quote is typed, and with Ctrl+Space wherever the cursor is.
// Up and Down choose in the list, Return or Tab types what is chosen, and
// Escape or typing something else closes it.
type completer struct {
	edit  *qtwidgets.QPlainTextEdit
	popup *qtwidgets.QCompleter
	// list is the popup of the completer, which shows the model.
	list  *qtwidgets.QTreeView
	model *qtgui.QStandardItemModel
	// complete returns what can be typed at an offset of the text, in bytes.
	complete func(text string, offset int) completion.Completion

	// offered is what the list holds, for the text that was in the editor
	// when it was made.
	offered completion.Completion
	text    string
}

// completionTriggers are the characters which, typed, have what can be
// typed after them offered, besides those of a name: the quote of a key of
// JSON, and the @ of a directive of GraphQL.
const completionTriggers = `"@`

// newCompleter has what complete says can be typed offered in an editor.
func newCompleter(edit *qtwidgets.QPlainTextEdit, complete func(text string, offset int) completion.Completion) *completer {
	c := &completer{
		edit:     edit,
		model:    qtgui.NewQStandardItemModel(edit.AsQObject()),
		complete: complete,
	}
	c.popup = qtwidgets.NewQCompleterWithModel(c.model.AsQAbstractItemModel(), edit.AsQObject())
	c.popup.SetWidget(edit.AsQWidget())
	// The items are those for what has been typed already, so the list
	// shows them as they are.
	c.popup.SetCompletionMode(qtwidgets.QCompleterUnfilteredPopupCompletion)
	c.popup.SetMaxVisibleItems(10)

	// The name of each item, with what it is beside it, faded.
	c.list = qtwidgets.NewQTreeView(nil)
	c.list.SetRootIsDecorated(false)
	c.list.SetUniformRowHeights(true)
	c.list.Header().Hide()
	c.list.SetSelectionBehavior(qtwidgets.QAbstractItemViewSelectRows)
	c.popup.SetPopup(c.list.AsQAbstractItemView())

	c.popup.OnActivatedIndex(func(index *qtcore.QModelIndex) { c.insert(index.Row()) })

	edit.OverrideKeyPressEvent(func(super func(*qtgui.QKeyEvent), event *qtgui.QKeyEvent) {
		open := c.list.IsVisible()
		// The list has these keys while it is open.
		if open {
			switch qtcore.Key(event.Key()) {
			case qtcore.Key_Return, qtcore.Key_Enter, qtcore.Key_Tab, qtcore.Key_Backtab, qtcore.Key_Escape:
				event.Ignore()
				return
			}
		}
		// Ctrl+Space offers what there is, whatever has been typed.
		if qtcore.Key(event.Key()) == qtcore.Key_Space && event.Modifiers()&qtcore.ControlModifier != 0 {
			c.offer(true)
			return
		}
		super(event)

		typed := event.Text()
		last, _ := utf8.DecodeLastRuneInString(typed)
		trigger := typed != "" && (last < utf8.RuneSelf && completion.Name(byte(last)) || strings.ContainsRune(completionTriggers, last))
		switch {
		case trigger:
			c.offer(strings.ContainsRune(completionTriggers, last))
		case open:
			// Deleting or moving keeps the list up to date, or closes it.
			c.offer(false)
		}
	})
	return c
}

// offer shows what can be typed at the cursor, or closes the list if there
// is nothing. Unless always is set, nothing is offered until something of a
// name has been typed.
func (c *completer) offer(always bool) {
	text := c.edit.ToPlainText()
	cursor := c.edit.TextCursor()
	position := cursor.Position()
	cursor.Delete()

	offered := c.complete(text, byteOffset(text, position))
	if len(offered.Items) == 0 || !always && offered.Start == offered.End {
		c.list.Hide()
		return
	}
	c.offered, c.text = offered, text

	c.model.Clear()
	faded := qcolour(fadedTextColour())
	fadedBrush := qtgui.NewQBrushWithColor(faded)
	defer fadedBrush.Delete()
	for _, item := range offered.Items {
		// The model owns the items.
		label, detail := qtgui.NewQStandardItemWithText(item.Label), qtgui.NewQStandardItemWithText(item.Detail)
		label.SetEditable(false)
		detail.SetEditable(false)
		detail.SetForeground(fadedBrush)
		c.model.AppendRowItems([]*qtgui.QStandardItem{label, detail})
	}

	// The names are kept apart from what they are by a gap of a few
	// characters.
	c.list.SetFont(c.edit.Font())
	gap := c.list.FontMetrics().HorizontalAdvance("   ")
	names, details := c.list.SizeHintForColumn(0)+gap, c.list.SizeHintForColumn(1)
	c.list.SetColumnWidth(0, names)
	c.list.SetColumnWidth(1, details)

	rect := c.edit.CursorRect()
	rect.Width = names + details + c.list.VerticalScrollBar().SizeHint().Width + 4*c.list.FrameWidth()
	c.popup.CompleteFull(rect)
	c.popup.SetCurrentRow(0)
	c.list.SetCurrentIndex(c.popup.CurrentIndex())
}

// insert types the item at a row of the list in place of what has been
// typed of it.
func (c *completer) insert(row int) {
	if row < 0 || row >= len(c.offered.Items) || c.edit.ToPlainText() != c.text {
		return
	}
	cursor := c.edit.TextCursor()
	defer cursor.Delete()
	cursor.SetPosition(utf16Offset(c.text, c.offered.Start))
	cursor.SetPositionFull(utf16Offset(c.text, c.offered.End), qtgui.QTextCursorKeepAnchor)
	cursor.InsertText(c.offered.Items[row].Insert)
	c.edit.SetTextCursor(cursor)
}

// byteOffset returns the offset in bytes of a position in text, counted in
// the UTF-16 code units Qt counts in.
func byteOffset(text string, position int) int {
	offset := 0
	for _, r := range text {
		if position <= 0 {
			break
		}
		position -= utf16.RuneLen(r)
		offset += utf8.RuneLen(r)
	}
	return offset
}

// utf16Offset returns the position of an offset of text in bytes, counted in
// UTF-16 code units, as Qt counts.
func utf16Offset(text string, offset int) int {
	return utf16Len(text[:min(offset, len(text))])
}

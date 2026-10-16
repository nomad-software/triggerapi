package ui

import (
	"runtime/debug"
	"testing"

	"github.com/mappu/miqt/qt6/mainthread"
)

// Text cursors are freed on the GUI thread, not by the garbage collector on
// a thread of its own, which crashed when it came while a document was being
// changed. Here it is made to come all the time.
func TestTextCursorsWhileCollecting(t *testing.T) {
	defer debug.SetGCPercent(debug.SetGCPercent(1))

	const body = "{\n  \"name\": \"a\",\n       \"data\": {\n    \"year\": 2019,\n          \"tags\": [1, 2]\n      }\n   }"

	var w *Window
	mainthread.Wait(func() {
		w = NewWindow(testFiles(t).Files)
		w.Show()
		w.request.bodyType.SetCurrentIndex(1)
		w.response.find.open()
		w.response.find.field.SetText("a")
	})

	var garbage []byte
	for range 1500 {
		mainthread.Wait(func() {
			p := w.request
			p.bodyRaw.SetPlainText(body)
			p.bodyFold.reset()
			p.bodyFold.toggle(2)
			p.bodyFold.lineAt(4)
			garbage = make([]byte, 1<<16)
			p.format.button.Click()

			w.response.setBody(body, true)
			w.response.find.step(1)
		})
	}
	_ = garbage
}

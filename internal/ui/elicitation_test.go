package ui

import (
	"reflect"
	"testing"

	qt "github.com/mappu/miqt/qt6"
	"github.com/mappu/miqt/qt6/mainthread"

	"github.com/nomad-software/triggerapi/internal/mcp"
)

func TestElicitDialog(t *testing.T) {
	one := 1.0
	e := &mcp.Elicitation{Message: "Book a seat", Fields: []mcp.Field{
		{Name: "class", Type: mcp.FieldString, Options: []mcp.Option{{Value: "economy", Title: "Economy"}, {Value: "business"}}, Default: "business"},
		{Name: "extras", Type: mcp.FieldChoices, Options: []mcp.Option{{Value: "bag"}, {Value: "lounge"}}, Default: []any{"lounge"}},
		{Name: "name", Type: mcp.FieldString, Required: true, Description: "As on the passport"},
		{Name: "seats", Type: mcp.FieldInteger, Minimum: &one},
		{Name: "window", Type: mcp.FieldBoolean, Default: true},
	}}

	mainthread.Wait(func() {
		var answer *mcp.ElicitAnswer
		d := newElicitDialog(nil, "localhost", e)
		d.open(func(a *mcp.ElicitAnswer) { answer = a })

		// What is required has to be given.
		d.accept()
		if answer != nil || d.problem.Text() != "name has to be given." {
			t.Errorf("answer %+v, problem %q", answer, d.problem.Text())
		}
		lineOf := func(i int) *qt.QLineEdit { return qt.UnsafeNewQLineEdit(d.inputs[i].widget.UnsafePointer()) }
		lineOf(2).SetText("Ada")
		lineOf(3).SetText("0")
		d.accept()
		if answer != nil || d.problem.Text() != "seats has to be at least 1." {
			t.Errorf("answer %+v, problem %q", answer, d.problem.Text())
		}
		lineOf(3).SetText("2")
		d.accept()
		want := map[string]any{"class": "business", "extras": []string{"lounge"}, "name": "Ada", "seats": int64(2), "window": true}
		if answer == nil || answer.Action != mcp.ActionAccept || !reflect.DeepEqual(answer.Content, want) {
			t.Errorf("answer %+v, want %v", answer, want)
		}

		// Closing it any other way cancels, and declining declines.
		d = newElicitDialog(nil, "localhost", e)
		close := d.open(func(a *mcp.ElicitAnswer) { answer = a })
		close()
		if answer.Action != mcp.ActionCancel {
			t.Errorf("closed: %+v", answer)
		}
		d = newElicitDialog(nil, "localhost", e)
		d.open(func(a *mcp.ElicitAnswer) { answer = a })
		d.finish(&mcp.ElicitAnswer{Action: mcp.ActionDecline})
		if answer.Action != mcp.ActionDecline {
			t.Errorf("declined: %+v", answer)
		}
	})
}

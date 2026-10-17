package ui

import (
	"testing"

	"github.com/mappu/miqt/qt6/mainthread"

	"github.com/nomad-software/triggerapi/internal/collection"
	"github.com/nomad-software/triggerapi/internal/rest"
)

func TestHeadingsShaded(t *testing.T) {
	files := testFiles(t)
	store := &collection.Store{}
	store.AddFolder("Pages").Add(collection.NewREST("Get", rest.Request{Method: "GET"}))
	store.AddFolder("Empty")
	if err := store.Save(files.Requests); err != nil {
		t.Fatal(err)
	}

	mainthread.Wait(func() {
		w := NewWindow(files.Files)
		w.Show()
		tree := w.requests.tree
		heading, folder := tree.TopLevelItem(0), tree.TopLevelItem(1)
		if group, ok := headingGroup(heading); !ok || group != collection.TypeREST || folder.Text(colName) != "Empty" {
			t.Fatalf("top level %q, %q", heading.Text(colName), folder.Text(colName))
		}

		// A heading is shaded; a folder, even one at the top level, is not.
		if rowColour(tree, heading) == rowColour(tree, folder) {
			t.Error("the heading is not shaded")
		}
		if rowColour(tree, heading.Child(0)) != rowColour(tree, folder) {
			t.Error("a folder under a heading is shaded")
		}
		w.Close()
	})
}

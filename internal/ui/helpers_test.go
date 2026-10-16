package ui

// Helpers shared by the tests of several features: finding things in the
// request tree, reading the bar beneath a response, waiting for what happens
// in the background, and the like.

import (
	"slices"
	"strings"
	"testing"
	"time"

	qt "github.com/mappu/miqt/qt6"
	"github.com/mappu/miqt/qt6/mainthread"

	"github.com/nomad-software/triggerapi/internal/collection"
)

// selectMethod selects a method of the drop-down by its full name.
func (c *methodCombo) selectMethod(name string) {
	if index := slices.Index(c.names, name); index >= 0 && name != "" {
		c.SetCurrentIndex(index)
	}
}

// count returns the number of methods the drop-down lists.
func (c *methodCombo) count() int {
	return len(slices.DeleteFunc(slices.Clone(c.names), func(name string) bool { return name == "" }))
}

// count returns the number of methods the table lists.
func (e *endpointTable) count() int {
	return len(slices.DeleteFunc(slices.Clone(e.names), func(name string) bool { return name == "" }))
}

// fieldValue returns the value shown for a label in the bar beneath a
// response, or an empty string if the bar does not have it.
func fieldValue(p *responsePanel, label string) string {
	for _, f := range p.fields {
		if f.label == label {
			return f.value
		}
	}
	return ""
}

// isFailure reports if the bar beneath a response shows a failure, which is
// in red.
func isFailure(p *responsePanel) bool {
	return strings.Contains(p.result.Text(), themeColour(colourError))
}

// itemType returns the type whose icon an item of the request tree has, or
// an empty type if it has the icon of a folder.
func itemType(item *qt.QTreeWidgetItem) collection.Type {
	icon := item.Icon(colName)
	if icon.IsNull() || icon.CacheKey() == folderIcon().CacheKey() {
		return ""
	}
	for _, known := range requestTypes {
		if requestTypeIcon(known.typ).CacheKey() == icon.CacheKey() {
			return known.typ
		}
	}
	return "unknown"
}

// topFolders returns the items of the top level folders in the order they
// are shown: those under each heading, then those on their own.
func topFolders(tree *qt.QTreeWidget) []*qt.QTreeWidgetItem {
	var items []*qt.QTreeWidgetItem
	for i := range tree.TopLevelItemCount() {
		item := tree.TopLevelItem(i)
		if _, ok := headingGroup(item); !ok {
			items = append(items, item)
			continue
		}
		for j := range item.ChildCount() {
			items = append(items, item.Child(j))
		}
	}
	return items
}

// topCount returns the number of top level folders in the tree.
func topCount(tree *qt.QTreeWidget) int {
	return len(topFolders(tree))
}

// topFolder returns the item of a top level folder, by its position among
// them as they are shown, or nil if there is none there.
func topFolder(tree *qt.QTreeWidget, index int) *qt.QTreeWidgetItem {
	if items := topFolders(tree); index < len(items) {
		return items[index]
	}
	return nil
}

// waitFor polls a condition on the main thread until it holds.
func waitFor(t *testing.T, what string, condition func() bool) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for !mainthread.Wait2(condition) {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// addGRPCRequest adds a gRPC request to the window. It must be called on the
// main thread.
func addGRPCRequest(w *Window) *collection.Request {
	testRequestType = collection.TypeGRPC
	defer func() { testRequestType = collection.TypeREST }()

	w.requests.addRequest()
	return w.open
}

// formatColour returns the colour of a text format, as #rrggbb.
func formatColour(format *qt.QTextCharFormat) string {
	return format.Foreground().Color().Name()
}

// iconColour returns the colour an icon is drawn on, as #rrggbb.
func iconColour(icon *qt.QIcon) string {
	// The edge of the icon is clear of its symbol. An icon is not made
	// larger than it was drawn, so the picture can be smaller than asked.
	image := icon.Pixmap2(64, 64).ToImage()
	return image.PixelColor(image.Width()/16, image.Height()/2).Name()
}

// variables returns the variables of the active environment by name.
func variables(w *Window) map[string]string {
	values := map[string]string{}
	if e, ok := w.environments.store.Current(); ok {
		for _, v := range e.Variables {
			if v.Enabled {
				values[v.Key] = v.Value
			}
		}
	}
	return values
}

// treeItem finds an item of the request tree by the names on the way to it
// from the top. It returns nil if there is none.
func treeItem(tree *qt.QTreeWidget, names ...string) *qt.QTreeWidgetItem {
	var item *qt.QTreeWidgetItem
	for depth, name := range names {
		var found *qt.QTreeWidgetItem
		if depth == 0 {
			for i := range topCount(tree) {
				if topFolder(tree, i).Text(colName) == name {
					found = topFolder(tree, i)
				}
			}
		} else {
			for i := range item.ChildCount() {
				if item.Child(i).Text(colName) == name {
					found = item.Child(i)
				}
			}
		}
		if found == nil {
			return nil
		}
		item = found
	}
	return item
}

// treeNames lists the names of the children of an item of the request tree,
// with a slash after those of folders.
func treeNames(item *qt.QTreeWidgetItem) []string {
	var names []string
	for i := range item.ChildCount() {
		child := item.Child(i)
		name := child.Text(colName)
		if itemType(child) == "" {
			name += "/"
		}
		names = append(names, name)
	}
	return names
}

// tabTitle returns the name on the tab that is shown above the request UI.
func tabTitle(w *Window) string {
	return w.tabs.bar.TabText(w.tabs.bar.CurrentIndex())
}

// tabNames lists the tabs above the request UI, with a star after the name
// of each that is pinned.
func tabNames(w *Window) []string {
	var names []string
	for i, tab := range w.tabs.tabs {
		name := w.tabs.bar.TabText(i)
		if tab.pinned {
			name += "*"
		}
		names = append(names, name)
	}
	return names
}

// doubleClick double clicks a button as the pointer does: a press and a
// release, then a double click and a release.
func doubleClick(button *qt.QToolButton) {
	for _, kind := range []qt.QEvent__Type{qt.QEvent__MouseButtonPress, qt.QEvent__MouseButtonRelease, qt.QEvent__MouseButtonDblClick, qt.QEvent__MouseButtonRelease} {
		buttons := qt.LeftButton
		if kind == qt.QEvent__MouseButtonRelease {
			buttons = qt.NoButton
		}
		event := qt.NewQMouseEvent(kind, qt.NewQPointF3(2, 2), qt.LeftButton, buttons, qt.NoModifier)
		qt.QCoreApplication_SendEvent(button.QObject, event.QEvent)
		event.Delete()
	}
}

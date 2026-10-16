package ui

import (
	"slices"

	"github.com/nomad-software/triggerapi/internal/collection"
)

// Moving folders and requests between folders by dragging them in the
// request tree.

// dropPosition is where something dragged was let go, relative to an item.
type dropPosition int

const (
	// dropNowhere is the empty space beneath the items.
	dropNowhere dropPosition = iota
	dropOn
	dropAbove
	dropBelow
)

// drop moves the folder or request that was dragged to where it was dropped:
// on, above or below the target folder or request, or nowhere. It reports if
// anything was moved.
//
// Dropping on a folder puts it inside, at the end. Dropping above or below
// something puts it beside that, in the same folder, with the one limit that
// folders always come before requests. Dropping in the empty space, or by a
// heading, puts a folder at the end of the top level; a request cannot go
// there, as requests are always in a folder.
func (t *requestTree) drop(folder *collection.Folder, request *collection.Request, targetFolder *collection.Folder, targetRequest *collection.Request, where dropPosition) bool {
	after := 0
	if where == dropBelow {
		after = 1
	}

	// to is the folder to move into, which is nil for the top level, and
	// index the position there among the things of the same kind.
	var to *collection.Folder
	index := 0

	switch {
	case where == dropNowhere || targetFolder == nil:
		// Nowhere, or by a heading.
		index = len(t.store.Folders)

	case targetRequest != nil:
		// Beside a request, in the folder that holds it.
		to = targetFolder
		index = len(to.Folders)
		if request != nil {
			index = slices.Index(to.Requests, targetRequest) + after
		}

	case where == dropOn:
		to = targetFolder
		index = len(to.Folders)
		if request != nil {
			index = len(to.Requests)
		}

	default:
		// Beside a folder, in the folder that holds it. A request goes
		// before the requests there, which is as near as it can be.
		parent, ok := t.store.ParentOf(targetFolder)
		if !ok {
			return false
		}
		to = parent
		if request == nil {
			siblings := t.store.Folders
			if to != nil {
				siblings = to.Folders
			}
			index = slices.Index(siblings, targetFolder) + after
		}
	}

	var moved any
	switch {
	case request != nil:
		if !t.store.MoveRequest(request, to, index) {
			return false
		}
		moved = request
	default:
		if !t.store.MoveFolder(folder, to, index) {
			return false
		}
		moved = folder
	}

	// What something is dropped into is opened to show it.
	if to != nil {
		to.Open = true
	}
	// Everything is kept in alphabetical order, so where exactly in a
	// folder something was dropped makes no difference.
	t.store.Sort()
	t.draw(moved)
	t.save()
	return true
}

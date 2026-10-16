package ui

import (
	"maps"

	"github.com/nomad-software/triggerapi/internal/collection"
)

// mixedGroup is the heading of the folders that hold requests of more than
// one type.
const mixedGroup collection.Type = "mixed"

// groupLabels are the names the headings of the request tree are shown by:
// those of the types, and Mixed.
var groupLabels = func() map[collection.Type]string {
	labels := maps.Clone(typeLabels)
	labels[mixedGroup] = "Mixed"
	return labels
}()

// groupOrder is the order the headings are listed in.
var groupOrder = []collection.Type{collection.TypeGraphQL, collection.TypeGRPC, collection.TypeMCP, collection.TypeREST, collection.TypeWebSocket, mixedGroup}

// groupOf returns the heading a top level folder is listed under: the type
// of its requests, at any depth, if they are all of one type, mixedGroup if
// they are not, and "" if it holds none.
func groupOf(f *collection.Folder) collection.Type {
	var group collection.Type
	for _, r := range f.All() {
		switch {
		case group == "":
			group = r.Type
		case group != r.Type:
			return mixedGroup
		}
	}
	return group
}

// topEntry is what an item at the top level of the request tree stands for:
// a heading, with the folders listed under it in the order of the store, or
// a folder of its own when group is "".
type topEntry struct {
	group   collection.Type
	folders []*collection.Folder
}

// groupFolders sorts the top level folders under their headings, in the
// order of groupOrder, leaving out the headings with none. The folders that
// hold no requests come after the headings, each on its own. Nothing of this
// is kept in the store; it is worked out again whenever the tree is drawn.
func groupFolders(folders []*collection.Folder) []topEntry {
	grouped := map[collection.Type][]*collection.Folder{}
	var empty []*collection.Folder
	for _, f := range folders {
		if group := groupOf(f); group != "" {
			grouped[group] = append(grouped[group], f)
		} else {
			empty = append(empty, f)
		}
	}

	var entries []topEntry
	for _, group := range groupOrder {
		if fs := grouped[group]; len(fs) > 0 {
			entries = append(entries, topEntry{group, fs})
		}
	}
	for _, f := range empty {
		entries = append(entries, topEntry{"", []*collection.Folder{f}})
	}
	return entries
}

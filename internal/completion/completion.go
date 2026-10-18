// Package completion has what the GraphQL and gRPC engines say can be typed
// at a place in a query or message, which the UI offers to complete what is
// being typed with. It has no dependency on the UI.
package completion

import (
	"slices"
	"strings"
)

// Item is something that can be typed.
type Item struct {
	// Label is what it is called, and Detail what kind of thing it is, such
	// as the type of a field, for the list it is offered in.
	Label  string
	Detail string
	// Insert is the text it is typed as, which can be more than its label,
	// such as the quotes and colon of a key of JSON.
	Insert string
}

// Completion is what can be typed at a place in some text: the items, and
// the part of the text they replace, from Start to End, in bytes, which is
// what has been typed of the item already.
type Completion struct {
	Start, End int
	Items      []Item
}

// Name reports if a byte is part of a name, as a field or a key is.
func Name(c byte) bool {
	return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

// NameStart returns where the name that ends at an offset of text starts,
// which is the offset itself if there is none.
func NameStart(text string, offset int) int {
	start := offset
	for start > 0 && Name(text[start-1]) {
		start--
	}
	return start
}

// Matching returns the items whose labels start with a prefix, ignoring
// case, in alphabetical order of their labels, leaving out an item that is
// all of what has been typed already, as there is nothing to complete.
func Matching(items []Item, prefix string) []Item {
	lower := strings.ToLower(prefix)
	var matched []Item
	for _, item := range items {
		label := strings.ToLower(item.Label)
		if strings.HasPrefix(label, lower) && item.Label != prefix {
			matched = append(matched, item)
		}
	}
	slices.SortStableFunc(matched, func(a, b Item) int {
		return strings.Compare(strings.ToLower(a.Label), strings.ToLower(b.Label))
	})
	return matched
}

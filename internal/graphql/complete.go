package graphql

import (
	"strings"

	"github.com/nomad-software/triggerapi/internal/completion"
)

// Complete returns what can be typed at an offset of a query, in bytes:
//
//   - in a selection, the fields of its type, and __typename;
//   - in the arguments of a field, their names, and where the value of one
//     goes, the values of its enum, or true and false;
//   - after on, the types a fragment can be on;
//   - after @, the built-in directives;
//   - outside any selection, the kinds of operation the schema has, and
//     fragment.
//
// The query is read as far as the offset, whether or not it is valid, as it
// is being typed. What has been typed of the name at the offset is what the
// items replace. There is nothing to complete inside a string, a comment or
// a variable.
func (s *Schema) Complete(query string, offset int) completion.Completion {
	offset = min(max(offset, 0), len(query))
	start := completion.NameStart(query, offset)
	c := completion.Completion{Start: start, End: offset}
	if start < len(query) && start < offset && query[start] >= '0' && query[start] <= '9' {
		// A number, not a name.
		return c
	}
	if start > 0 && query[start-1] == '$' {
		return c
	}

	r := &reader{schema: s}
	if !r.read(query[:start]) {
		return c
	}
	c.Items = completion.Matching(r.items(), query[start:offset])
	return c
}

// expectation is what comes next in the arguments of a field.
type expectation int

const (
	expectName expectation = iota
	expectColon
	expectValue
)

// argumentList is the arguments of a field, or those of a directive or the
// definitions of the variables of an operation, for which field is nil, as
// they are read.
type argumentList struct {
	field  *field
	expect expectation
	// argument is the one whose value is expected.
	argument *inputValue
	// depth counts the braces and brackets of an input object or a list
	// that a value is in.
	depth int
}

// selection is a set of fields of a type, which is nil if it is not known.
type selection struct {
	typ *fullType
	// field is the field last selected, whose arguments or selection may
	// follow.
	field *field
}

// reader reads a query as far as it has been typed, keeping where in it the
// end is.
type reader struct {
	schema *Schema

	selections []*selection
	arguments  []*argumentList
	// next is the type of the selection the next brace opens, or nil.
	next *fullType
	// on is set after on, where the name of a type is wanted, spread after
	// ..., directive after @, named after the name of a directive, whose
	// arguments are not those of a field, and fragment while the name of a
	// fragment and its on are read.
	on, spread, directive, named bool
	fragment                     int
	// last is the last token read.
	last string
}

// read reads the query, and reports false if it ends in a string or a
// comment.
func (r *reader) read(query string) bool {
	for i := 0; i < len(query); {
		c := query[i]
		switch {
		case c == '#':
			end := strings.IndexByte(query[i:], '\n')
			if end < 0 {
				return false
			}
			i += end + 1
			continue
		case c == '"':
			end, ok := stringEnd(query, i)
			if !ok {
				return false
			}
			r.value()
			i = end
			continue
		case c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == ',' && len(r.arguments) == 0:
			i++
			continue
		case strings.HasPrefix(query[i:], "..."):
			r.token("...")
			i += 3
			continue
		case completion.Name(c) || c == '-':
			end := i + 1
			for end < len(query) && (completion.Name(query[end]) || query[end] == '.') {
				end++
			}
			r.token(query[i:end])
			i = end
			continue
		}
		r.token(string(c))
		i++
	}
	return true
}

// stringEnd returns where the string that starts at an offset ends, after
// its closing quotes, and reports false if it does not end.
func stringEnd(query string, start int) (int, bool) {
	if strings.HasPrefix(query[start:], `"""`) {
		end := strings.Index(query[start+3:], `"""`)
		if end < 0 {
			return 0, false
		}
		return start + 3 + end + 3, true
	}
	for i := start + 1; i < len(query); i++ {
		switch query[i] {
		case '\\':
			i++
		case '"':
			return i + 1, true
		case '\n':
			return 0, false
		}
	}
	return 0, false
}

// token takes in a token that is not a string.
func (r *reader) token(t string) {
	defer func() { r.last = t }()
	directive := r.named
	r.named = false

	if len(r.arguments) > 0 {
		r.argumentToken(t)
		return
	}

	name := completion.Name(t[0]) || t[0] == '-'
	switch {
	case name && r.on:
		r.on = false
		r.next = r.schema.types[t]
	case name && r.spread:
		r.spread = false
		r.on = t == "on"
	case name && r.directive:
		r.directive = false
		r.named = true
	case name && r.fragment == 1:
		r.fragment = 2
	case name && r.fragment == 2:
		r.fragment = 0
		r.on = t == "on"
	case name && len(r.selections) == 0:
		switch t {
		case "query", "mutation", "subscription":
			r.next = r.schema.root(t)
		case "fragment":
			r.fragment = 1
		}
	case name:
		top := r.selections[len(r.selections)-1]
		top.field = top.typ.field(t)
		r.next = nil
		if top.field != nil {
			r.next = r.schema.types[top.field.Type.named()]
		}
	case t == "...":
		r.spread = true
	case t == "@":
		r.directive = true
	case t == "(":
		a := &argumentList{}
		if len(r.selections) > 0 && !directive && r.last != "" && completion.Name(r.last[0]) {
			a.field = r.selections[len(r.selections)-1].field
		}
		r.arguments = append(r.arguments, a)
	case t == "{":
		typ := r.next
		if typ == nil && len(r.selections) == 0 {
			// A query written without the word query.
			typ = r.schema.root("query")
		}
		r.selections = append(r.selections, &selection{typ: typ})
		r.next = nil
	case t == "}":
		if len(r.selections) > 0 {
			r.selections = r.selections[:len(r.selections)-1]
		}
		r.next = nil
	}
}

// argumentToken takes in a token in the arguments of a field.
func (r *reader) argumentToken(t string) {
	a := r.arguments[len(r.arguments)-1]
	switch {
	case t == ")" && a.depth == 0:
		r.arguments = r.arguments[:len(r.arguments)-1]
	case t == "{" || t == "[":
		a.depth++
	case t == "}" || t == "]":
		a.depth--
		if a.depth <= 0 {
			a.depth = 0
			a.expect = expectName
		}
	case a.depth > 0:
	case t == ",":
		a.expect = expectName
	case t == ":" && a.expect == expectColon:
		a.expect = expectValue
	case t == "$" && a.expect == expectValue:
	case completion.Name(t[0]) || t[0] == '-':
		if a.expect == expectName {
			a.argument = a.field.argument(t)
			a.expect = expectColon
		} else {
			a.expect = expectName
		}
	}
}

// value takes in a string, which is a value.
func (r *reader) value() {
	if len(r.arguments) > 0 {
		if a := r.arguments[len(r.arguments)-1]; a.depth == 0 && a.expect == expectValue {
			a.expect = expectName
		}
	}
	r.last = `""`
}

// items returns what can be typed where the query ends.
func (r *reader) items() []completion.Item {
	if len(r.arguments) > 0 {
		return r.argumentItems(r.arguments[len(r.arguments)-1])
	}
	switch {
	case r.on:
		return r.typeItems()
	case r.spread:
		return []completion.Item{{Label: "on", Insert: "on "}}
	case r.directive:
		return []completion.Item{{Label: "include", Detail: "directive", Insert: "include(if: "}, {Label: "skip", Detail: "directive", Insert: "skip(if: "}}
	case r.fragment > 0:
		return nil
	case len(r.selections) == 0:
		var items []completion.Item
		for _, operation := range []string{"query", "mutation", "subscription"} {
			if r.schema.root(operation) != nil {
				items = append(items, completion.Item{Label: operation, Detail: "operation", Insert: operation + " "})
			}
		}
		return append(items, completion.Item{Label: "fragment", Detail: "fragment", Insert: "fragment "})
	}

	typ := r.selections[len(r.selections)-1].typ
	if typ == nil {
		return nil
	}
	items := []completion.Item{{Label: "__typename", Detail: "String!", Insert: "__typename"}}
	for _, f := range typ.Fields {
		items = append(items, completion.Item{Label: f.Name, Detail: f.Type.String(), Insert: f.Name})
	}
	return items
}

// argumentItems returns what can be typed in arguments: their names, or
// the values of the one whose value is wanted, if it is an enum or a
// boolean.
func (r *reader) argumentItems(a *argumentList) []completion.Item {
	if a.field == nil || a.depth > 0 {
		return nil
	}
	switch a.expect {
	case expectName:
		var items []completion.Item
		for _, arg := range a.field.Args {
			items = append(items, completion.Item{Label: arg.Name, Detail: arg.Type.String(), Insert: arg.Name + ": "})
		}
		return items
	case expectValue:
		if a.argument == nil {
			return nil
		}
		name := a.argument.Type.named()
		if name == "Boolean" {
			return []completion.Item{{Label: "true", Detail: name, Insert: "true"}, {Label: "false", Detail: name, Insert: "false"}}
		}
		var items []completion.Item
		if t := r.schema.types[name]; t != nil {
			for _, v := range t.EnumValues {
				items = append(items, completion.Item{Label: v.Name, Detail: name, Insert: v.Name})
			}
		}
		return items
	}
	return nil
}

// typeItems returns the types a fragment can be on: those the selection it
// is in can be, or any type with fields, or of a union, if it is in none.
func (r *reader) typeItems() []completion.Item {
	var items []completion.Item
	add := func(t *fullType) {
		if t != nil && (t.Kind == "OBJECT" || t.Kind == "INTERFACE" || t.Kind == "UNION") && !strings.HasPrefix(t.Name, "__") {
			items = append(items, completion.Item{Label: t.Name, Detail: strings.ToLower(t.Kind), Insert: t.Name + " "})
		}
	}
	if len(r.selections) > 0 {
		if typ := r.selections[len(r.selections)-1].typ; typ != nil && len(typ.PossibleTypes) > 0 {
			add(typ)
			for _, p := range typ.PossibleTypes {
				add(r.schema.types[p.named()])
			}
			return items
		}
	}
	for i := range r.schema.schema.Types {
		add(&r.schema.schema.Types[i])
	}
	return items
}

// root returns the root type of a kind of operation, or nil if the schema
// has none.
func (s *Schema) root(operation string) *fullType {
	var root *named
	switch operation {
	case "query":
		root = s.schema.QueryType
	case "mutation":
		root = s.schema.MutationType
	case "subscription":
		root = s.schema.SubscriptionType
	}
	if root == nil {
		return nil
	}
	return s.types[root.Name]
}

// field returns the field of a type by its name, or nil if the type is nil
// or has no such field.
func (t *fullType) field(name string) *field {
	if t == nil {
		return nil
	}
	for i := range t.Fields {
		if t.Fields[i].Name == name {
			return &t.Fields[i]
		}
	}
	return nil
}

// argument returns the argument of a field by its name, or nil if the field
// is nil or has no such argument.
func (f *field) argument(name string) *inputValue {
	if f == nil {
		return nil
	}
	for i := range f.Args {
		if f.Args[i].Name == name {
			return &f.Args[i]
		}
	}
	return nil
}

// named returns the name of the type inside any lists and non-nulls.
func (t typeRef) named() string {
	for t.Name == nil && t.OfType != nil {
		t = *t.OfType
	}
	if t.Name == nil {
		return ""
	}
	return *t.Name
}

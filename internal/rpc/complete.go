package rpc

import (
	"strings"

	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/nomad-software/triggerapi/internal/completion"
)

// Complete returns what can be typed at an offset of the JSON message of a
// method, in bytes:
//
//   - where a key goes, the fields of the message it is in that are not
//     there yet, by their JSON names;
//   - where a value goes, the values of an enum, or true and false.
//
// The message is read as far as the offset, whether or not it is valid, as
// it is being typed, following the keys into the messages, lists and maps
// they are of. What has been typed of the key or value at the offset is
// what the items replace, with its opening quote if it has one, which an
// item then has too. Nothing is offered for a method the schema does not
// have, or inside a value the schema says nothing of, such as one of
// protobuf's own types.
func (s *Schema) Complete(method, message string, offset int) completion.Completion {
	offset = min(max(offset, 0), len(message))
	c := completion.Completion{Start: offset, End: offset}
	m, ok := s.Method(method)
	if !ok {
		return c
	}
	return completeMessage(m.desc.Input(), message, offset)
}

// expectation is what comes next in an object of JSON.
type expectation int

const (
	expectKey expectation = iota
	expectColon
	expectValue
	expectComma
)

// container is an object or an array of JSON that the message is in, as it
// is read.
type container struct {
	array bool
	// message is the message whose fields are the keys of an object, and
	// field is the field the values of the object or array are of, which is
	// of a map or a list. Both are nil where the schema says nothing of what
	// is inside.
	message protoreflect.MessageDescriptor
	field   protoreflect.FieldDescriptor
	// expect is what comes next, which in an array is a value while it is
	// expectKey, and a comma once it is expectComma.
	expect expectation
	// key is the key of the value expected, and seen the keys there have
	// been in the object.
	key  string
	seen map[string]bool
}

// value returns the field a value in the container is of: that of its key
// in a message, or the value of a map, or the element of a list, which is
// the field itself, or nil if it is not known.
func (c *container) value() protoreflect.FieldDescriptor {
	switch {
	case c.message != nil:
		fields := c.message.Fields()
		if field := fields.ByJSONName(c.key); field != nil {
			return field
		}
		return fields.ByName(protoreflect.Name(c.key))
	case c.field != nil && c.field.IsMap():
		return c.field.MapValue()
	}
	return c.field
}

// open returns the container a value of a field opens, which is an object
// unless array is set.
func open(field protoreflect.FieldDescriptor, array bool) *container {
	c := &container{array: array, seen: map[string]bool{}}
	switch {
	case field == nil:
	case array && field.IsList():
		c.field = field
	case array:
	case field.IsMap():
		c.field = field
	case field.Message() != nil && !isWellKnown(field.Message()):
		c.message = field.Message()
	}
	return c
}

// completeMessage returns what can be typed at an offset of the JSON of a
// message (see Schema.Complete).
func completeMessage(desc protoreflect.MessageDescriptor, text string, offset int) completion.Completion {
	c := completion.Completion{Start: offset, End: offset}
	var stack []*container
	// quote is where a string that does not end by the offset starts.
	quote := -1

	top := func() *container {
		if len(stack) == 0 {
			return nil
		}
		return stack[len(stack)-1]
	}
	// valued records that a value has been read in the container.
	valued := func() {
		if t := top(); t != nil {
			t.expect = expectComma
		}
	}

	start := completion.NameStart(text, offset)
	for i := 0; i < start; {
		ch := text[i]
		t := top()
		switch {
		case ch == '"':
			end := stringEnd(text[:start], i)
			if end < 0 {
				quote = i
				i = start
				continue
			}
			s := text[i+1 : end]
			i = end + 1
			switch {
			case t != nil && !t.array && t.expect == expectKey:
				t.key, t.seen[s] = s, true
				t.expect = expectColon
			default:
				valued()
			}
			continue
		case ch == '{' || ch == '[':
			var field protoreflect.FieldDescriptor
			switch {
			case t == nil:
			case t.array && t.field != nil:
				field = t.field
			case !t.array:
				field = t.value()
			}
			var next *container
			switch {
			case t == nil && ch == '{':
				next = &container{message: desc, seen: map[string]bool{}}
			case t != nil && t.array && ch == '{' && field != nil && field.Message() != nil && !isWellKnown(field.Message()):
				// An element of a list of messages.
				next = &container{message: field.Message(), seen: map[string]bool{}}
			case t != nil && t.array:
				next = open(nil, ch == '[')
			default:
				next = open(field, ch == '[')
			}
			stack = append(stack, next)
		case ch == '}' || ch == ']':
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
			valued()
		case ch == ':':
			if t != nil && t.expect == expectColon {
				t.expect = expectValue
			}
		case ch == ',':
			// A key is expected next, or in an array a value.
			if t != nil {
				t.expect = expectKey
			}
		case completion.Name(ch) || ch == '-':
			for i < start && (completion.Name(text[i]) || text[i] == '-' || text[i] == '.') {
				i++
			}
			valued()
			continue
		}
		i++
	}

	t := top()
	if t == nil {
		return c
	}
	if quote >= 0 {
		c.Start = quote
	} else {
		c.Start = start
	}
	typed := text[c.Start:offset]
	if quote >= 0 {
		typed = typed[1:]
	}
	closed := strings.HasPrefix(text[offset:], `"`)

	var items []completion.Item
	switch {
	case !t.array && t.expect == expectKey && t.message != nil:
		fields := t.message.Fields()
		for i := range fields.Len() {
			field := fields.Get(i)
			name := field.JSONName()
			if t.seen[name] || t.seen[string(field.Name())] {
				continue
			}
			insert := `"` + name + `": `
			if closed {
				insert = `"` + name
			}
			items = append(items, completion.Item{Label: name, Detail: kind(field), Insert: insert})
		}
	case !t.array && t.expect == expectValue:
		items = values(t.value(), quote >= 0, closed)
	case t.array && t.expect != expectComma:
		items = values(t.field, quote >= 0, closed)
	}
	c.Items = completion.Matching(items, typed)
	return c
}

// stringEnd returns the offset of the quote that ends the string of JSON
// which starts at an offset of text, or -1 if it does not end.
func stringEnd(text string, start int) int {
	for i := start + 1; i < len(text); i++ {
		switch text[i] {
		case '\\':
			i++
		case '"':
			return i
		}
	}
	return -1
}

// values returns the values that can be typed of a field, which is in a
// list if it is a list: the names of an enum, as strings, or true and false.
// A value being typed in quotes is only offered as a string.
func values(field protoreflect.FieldDescriptor, quoted, closed bool) []completion.Item {
	if field == nil || field.IsMap() {
		return nil
	}
	switch field.Kind() {
	case protoreflect.EnumKind:
		enum := field.Enum()
		var items []completion.Item
		for i := range enum.Values().Len() {
			name := string(enum.Values().Get(i).Name())
			insert := `"` + name + `"`
			if closed {
				insert = `"` + name
			}
			items = append(items, completion.Item{Label: name, Detail: string(enum.Name()), Insert: insert})
		}
		return items
	case protoreflect.BoolKind:
		if quoted {
			return nil
		}
		return []completion.Item{{Label: "true", Detail: "bool", Insert: "true"}, {Label: "false", Detail: "bool", Insert: "false"}}
	}
	return nil
}

// kind says what a field holds, as it is written in a .proto file.
func kind(field protoreflect.FieldDescriptor) string {
	of := func(f protoreflect.FieldDescriptor) string {
		switch {
		case f.Message() != nil:
			return string(f.Message().Name())
		case f.Enum() != nil:
			return string(f.Enum().Name())
		}
		return f.Kind().String()
	}
	switch {
	case field.IsMap():
		return "map<" + of(field.MapKey()) + ", " + of(field.MapValue()) + ">"
	case field.IsList():
		return "repeated " + of(field)
	}
	return of(field)
}

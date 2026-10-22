package rpc

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"

	"google.golang.org/protobuf/reflect/protoreflect"
)

// mapEntries gives the JSON of a message of desc with each map that is
// written as an array of entries, {"key": ..., "value": ...} objects as
// Postman takes them, written as the object protojson reads, at any depth.
// Text that is not JSON, or has no map written so, is returned as it is, so
// that reading it says what is wrong with it where it is.
func mapEntries(text string, desc protoreflect.MessageDescriptor) (string, error) {
	decoder := json.NewDecoder(strings.NewReader(text))
	decoder.UseNumber()
	var value any
	if decoder.Decode(&value) != nil {
		return text, nil
	}
	if _, err := decoder.Token(); err != io.EOF {
		return text, nil
	}
	changed, err := rewriteMaps(value, desc)
	if err != nil || !changed {
		return text, err
	}
	data, err := json.Marshal(value)
	if err != nil {
		return text, nil
	}
	return string(data), nil
}

// rewriteMaps rewrites the maps written as arrays of entries in value, the
// decoded JSON of a message of desc, reporting whether there were any.
func rewriteMaps(value any, desc protoreflect.MessageDescriptor) (bool, error) {
	object, ok := value.(map[string]any)
	if !ok || isWellKnown(desc) {
		return false, nil
	}
	changed := false
	fields := desc.Fields()
	for name, member := range object {
		field := fields.ByJSONName(name)
		if field == nil {
			field = fields.ByName(protoreflect.Name(name))
		}
		if field == nil {
			continue
		}

		var inner []any
		var message protoreflect.MessageDescriptor
		switch {
		case field.IsMap():
			if list, ok := member.([]any); ok {
				entries, ok, err := mapOf(list, name)
				if err != nil {
					return false, err
				}
				if ok {
					object[name], member, changed = entries, entries, true
				}
			}
			if m, ok := member.(map[string]any); ok {
				for _, v := range m {
					inner = append(inner, v)
				}
			}
			message = field.MapValue().Message()
		case field.IsList():
			inner, _ = member.([]any)
			message = field.Message()
		default:
			inner = []any{member}
			message = field.Message()
		}
		if message == nil {
			continue
		}
		for _, v := range inner {
			c, err := rewriteMaps(v, message)
			if err != nil {
				return false, err
			}
			changed = changed || c
		}
	}
	return changed, nil
}

// mapOf gives the object of the entries of the map named name, reporting
// whether list is entries: objects with a key, which is a string, a number or
// a boolean, and a value, and nothing else.
func mapOf(list []any, name string) (map[string]any, bool, error) {
	entries := make(map[string]any, len(list))
	for _, element := range list {
		entry, ok := element.(map[string]any)
		if !ok || len(entry) != 2 {
			return nil, false, nil
		}
		value, ok := entry["value"]
		if !ok {
			return nil, false, nil
		}
		var key string
		switch k := entry["key"].(type) {
		case string:
			key = k
		case json.Number:
			key = k.String()
		case bool:
			key = strconv.FormatBool(k)
		default:
			return nil, false, nil
		}
		if _, taken := entries[key]; taken {
			return nil, false, fmt.Errorf("%s has the key %q more than once", name, key)
		}
		entries[key] = value
	}
	return entries, true, nil
}

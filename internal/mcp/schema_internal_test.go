package mcp

import (
	"encoding/json"
	"strings"
	"testing"
)

// example returns the example arguments of a tool whose input has a schema.
func example(t *testing.T, schema string) string {
	t.Helper()
	var input any
	if err := json.Unmarshal([]byte(schema), &input); err != nil {
		t.Fatal(err)
	}
	text, err := Item{Kind: KindTool, input: input}.Example()
	if err != nil {
		t.Fatalf("%s: %v", schema, err)
	}
	// Compared without the spaces and line breaks it is set out with.
	return strings.Join(strings.Fields(text), "")
}

func TestExample(t *testing.T) {
	for _, c := range []struct{ schema, want string }{
		// A value of each type, and a property with no type that has
		// properties is an object.
		{`{"type": "object", "properties": {"s": {"type": "string"}, "i": {"type": "integer"}, "n": {"type": "number"}, "b": {"type": "boolean"}}}`,
			`{"b":false,"i":0,"n":0,"s":""}`},
		{`{"properties": {"inner": {"properties": {"x": {"type": "string"}}}}}`, `{"inner":{"x":""}}`},
		// A value the schema gives comes first: a constant, then a
		// default, then an example, then the first of an enum.
		{`{"properties": {"c": {"const": 1, "default": 2}, "d": {"type": "string", "default": "x", "examples": ["y"]}, "e": {"type": "string", "examples": ["y"], "enum": ["z"]}, "f": {"enum": ["z", "w"]}}}`,
			`{"c":1,"d":"x","e":"y","f":"z"}`},
		// The first of a choice, and a type other than null.
		{`{"properties": {"a": {"anyOf": [{"type": "integer"}, {"type": "string"}]}, "o": {"oneOf": [{"type": "boolean"}]}, "t": {"type": ["null", "string"]}}}`,
			`{"a":0,"o":false,"t":""}`},
		// An array has one example item, or none if its items are not said.
		{`{"properties": {"list": {"type": "array", "items": {"type": "string"}}, "any": {"type": "array"}}}`, `{"any":[],"list":[""]}`},
		// References to definitions are followed.
		{`{"properties": {"p": {"$ref": "#/$defs/point"}, "q": {"$ref": "#/definitions/name"}}, "$defs": {"point": {"properties": {"x": {"type": "number"}}}}, "definitions": {"name": {"type": "string"}}}`,
			`{"p":{"x":0},"q":""}`},
		// Something with no type is null.
		{`{"properties": {"x": {}}}`, `{"x":null}`},
		// Arguments are always an object.
		{`{"type": "string"}`, `{}`},
		{`{}`, `{}`},
	} {
		if got := example(t, c.schema); got != c.want {
			t.Errorf("%s:\n got %s\nwant %s", c.schema, got, c.want)
		}
	}

	// A schema that refers to itself ends.
	deep := example(t, `{"properties": {"node": {"$ref": "#/$defs/node"}}, "$defs": {"node": {"properties": {"next": {"$ref": "#/$defs/node"}}}}}`)
	if strings.Count(deep, "next") == 0 || strings.Count(deep, "next") > maxDepth {
		t.Errorf("self reference: %s", deep)
	}

	// A tool with no schema takes an empty object, and a schema that cannot
	// be read is an error.
	if text, err := (Item{Kind: KindTool}).Example(); err != nil || text != "{}" {
		t.Errorf("no schema: %q, %v", text, err)
	}
	if _, err := (Item{Kind: KindTool, input: map[string]any{"type": 5}}).Example(); err == nil {
		t.Error("a schema that cannot be read was used")
	}
}

func TestCodeName(t *testing.T) {
	for code, want := range map[int64]string{
		-32700: "PARSE_ERROR",
		-32600: "INVALID_REQUEST",
		-32601: "METHOD_NOT_FOUND",
		-32602: "INVALID_PARAMS",
		-32603: "INTERNAL_ERROR",
		-32002: "RESOURCE_NOT_FOUND",
		-32000: "ERROR",
		1:      "ERROR",
	} {
		if got := CodeName(code); got != want {
			t.Errorf("CodeName(%d) = %s, want %s", code, got, want)
		}
	}
}

package rest

import "testing"

func TestFormatJSON(t *testing.T) {
	for text, want := range map[string]string{
		"":                                   "",
		" \n":                                " \n",
		`{"a":1,"b":[true,null,"x"],"c":{}}`: "{\n  \"a\": 1,\n  \"b\": [\n    true,\n    null,\n    \"x\"\n  ],\n  \"c\": {}\n}",
		// It is left alone if it is set out already, but for the space
		// around it.
		"\n{\n  \"a\": 1\n}\n": "{\n  \"a\": 1\n}",
		" [ 1,2 ] ":            "[\n  1,\n  2\n]",
		// Numbers and strings are kept as they are written.
		`{"n":1.50,"s":"é <&> \"q\""}`: "{\n  \"n\": 1.50,\n  \"s\": \"é <&> \\\"q\\\"\"\n}",
		// Placeholders are kept, in a string or in place of a value.
		`{"id":{{id}},"auth":"Bearer {{token}}","{{key}}":[{{a}},{{ b }}]}`: "{\n  \"id\": {{id}},\n  \"auth\": \"Bearer {{token}}\",\n  \"{{key}}\": [\n    {{a}},\n    {{ b }}\n  ]\n}",
		`{{body}}`:                   `{{body}}`,
		`{"n":7309185264,"v":{{v}}}`: "{\n  \"n\": 7309185264,\n  \"v\": {{v}}\n}",
	} {
		if got, err := FormatJSON(text); err != nil || got != want {
			t.Errorf("FormatJSON(%q) = %q, %v, want %q", text, got, err, want)
		}
	}

	for _, text := range []string{`{"a":`, `{a: 1}`, `{"a": 1} x`, `{ {{key}}: 1 }`, `hello`} {
		if got, err := FormatJSON(text); err == nil {
			t.Errorf("FormatJSON(%q) = %q, want an error", text, got)
		}
	}
}

func TestFormatJSONValues(t *testing.T) {
	got, err := FormatJSONValues(`{"a":1} {"b":{{b}}}` + "\n\n" + `[1]`)
	if want := "{\n  \"a\": 1\n}\n{\n  \"b\": {{b}}\n}\n[\n  1\n]"; err != nil || got != want {
		t.Errorf("FormatJSONValues = %q, %v, want %q", got, err, want)
	}
	if got, err := FormatJSONValues(`{"a":1} {`); err == nil {
		t.Errorf("FormatJSONValues of JSON that is not valid = %q", got)
	}
}

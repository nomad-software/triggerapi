package ui

import (
	"reflect"
	"testing"
)

func TestJSONTokens(t *testing.T) {
	tests := []struct {
		line string
		want []jsonToken
	}{
		{``, nil},
		{`{`, nil},
		{`  "name": "triggerapi",`, []jsonToken{{2, 6, tokenKey}, {10, 12, tokenString}}},
		{`"n" : -1.5e+3, "ok":true,"x":null`, []jsonToken{
			{0, 3, tokenKey}, {6, 7, tokenNumber},
			{15, 4, tokenKey}, {20, 4, tokenLiteral},
			{25, 3, tokenKey}, {29, 4, tokenLiteral},
		}},
		{`["a\"b", 12, false]`, []jsonToken{{1, 6, tokenString}, {9, 2, tokenNumber}, {13, 5, tokenLiteral}}},
		// Positions are counted in UTF-16 code units: 😀 is two.
		{`"😀": "é😀", 1`, []jsonToken{{0, 4, tokenKey}, {6, 5, tokenString}, {13, 1, tokenNumber}}},
		// Text that is not valid JSON is coloured as far as possible.
		{`"unterminated`, []jsonToken{{0, 13, tokenString}}},
		{`"ends with escape\`, []jsonToken{{0, 18, tokenString}}},
		{`nope - {{host}} truthy`, nil},
	}

	for _, tt := range tests {
		if got := jsonTokens(tt.line); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("jsonTokens(%q) = %v, want %v", tt.line, got, tt.want)
		}
	}
}

func TestFoldRegions(t *testing.T) {
	text := `{
  "a": {
    "b": [1, 2],
    "c": [
      "}] not a bracket",
      3
    ]
  },
  "d": {}, "e": [
  ]
}`

	want := map[int]int{0: 10, 1: 7, 3: 6, 8: 9}
	if got := foldRegions(text); !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}

	// Text that is not balanced, or not JSON, gives what can be found.
	for text, want := range map[string]map[int]int{
		"":               {},
		"plain text":     {},
		"}\n]\n{\n":      {},
		"[\n{\n}\n":      {1: 2},
		"{ \"a\": \"\n}": {0: 1},
	} {
		if got := foldRegions(text); !reflect.DeepEqual(got, want) {
			t.Errorf("foldRegions(%q) = %v, want %v", text, got, want)
		}
	}
}

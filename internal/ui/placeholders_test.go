package ui

import (
	"reflect"
	"strings"
	"testing"
)

func TestPlaceholderRanges(t *testing.T) {
	expand := func(s string) string { return strings.ReplaceAll(s, "{{host}}", "example.com") }

	tests := []struct {
		text string
		want []placeholderRange
	}{
		{"no placeholders", nil},
		{"{{host}}/users/{{id}}", []placeholderRange{{0, 8, true}, {15, 6, false}}},
		// Positions are counted in UTF-16 code units: é is one, 😀 is two.
		{"é😀{{host}}😀{{id}}", []placeholderRange{{3, 8, true}, {13, 6, false}}},
	}

	for _, tt := range tests {
		if got := placeholderRanges(tt.text, expand); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("placeholderRanges(%q) = %v, want %v", tt.text, got, tt.want)
		}
	}
}

package ui

import (
	"reflect"
	"testing"
)

func TestFindAll(t *testing.T) {
	tests := []struct {
		name      string
		text      string
		query     string
		matchCase bool
		want      []span
	}{
		{"nothing to find", "abc", "", false, nil},
		{"not there", "abc", "d", false, nil},
		{"longer than the text", "ab", "abc", false, nil},
		{"each place", "alpha, Alphabet", "alpha", false, []span{{0, 5}, {7, 5}}},
		{"capitals told apart", "alpha, Alphabet", "Alpha", true, []span{{7, 5}}},
		{"capitals in what is typed", "alpha", "ALPHA", false, []span{{0, 5}}},
		{"no overlapping", "aaaa", "aa", false, []span{{0, 2}, {2, 2}}},
		{"across lines", "a\nb\na", "a", false, []span{{0, 1}, {4, 1}}},
		// Positions are in UTF-16 code units: é is one, and 😀 two.
		{"after other letters", "é😀 id, 😀", "id", false, []span{{4, 2}}},
		{"other letters", "é😀 id, 😀", "😀", false, []span{{1, 2}, {8, 2}}},
	}

	for _, test := range tests {
		if got := findAll(test.text, test.query, test.matchCase); !reflect.DeepEqual(got, test.want) {
			t.Errorf("%s: findAll(%q, %q, %v) = %v, want %v", test.name, test.text, test.query, test.matchCase, got, test.want)
		}
	}
}

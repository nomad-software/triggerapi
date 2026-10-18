package completion

import (
	"slices"
	"testing"
)

func TestNameStart(t *testing.T) {
	for _, test := range []struct {
		text   string
		offset int
		want   int
	}{
		{"{ user", 6, 2},
		{"{ user", 4, 2},
		{"{ ", 2, 2},
		{"", 0, 0},
		{"a_b1", 4, 0},
	} {
		if got := NameStart(test.text, test.offset); got != test.want {
			t.Errorf("NameStart(%q, %d) = %d, want %d", test.text, test.offset, got, test.want)
		}
	}
}

func TestMatching(t *testing.T) {
	items := []Item{{Label: "name"}, {Label: "Nick"}, {Label: "id"}, {Label: "names"}}
	var got []string
	for _, item := range Matching(items, "n") {
		got = append(got, item.Label)
	}
	if want := []string{"name", "names", "Nick"}; !slices.Equal(got, want) {
		t.Errorf("n: %q", got)
	}
	// What has been typed in full is left out.
	got = nil
	for _, item := range Matching(items, "name") {
		got = append(got, item.Label)
	}
	if want := []string{"names"}; !slices.Equal(got, want) {
		t.Errorf("name: %q", got)
	}
	if len(Matching(items, "")) != 4 {
		t.Error("an empty prefix does not match everything")
	}
}

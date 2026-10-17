package shell

import (
	"slices"
	"testing"
)

func TestSplit(t *testing.T) {
	for command, want := range map[string][]string{
		"meld":                           {"meld"},
		"  code --diff   --wait ":        {"code", "--diff", "--wait"},
		`"/opt/My Tools/diff" -a`:        {"/opt/My Tools/diff", "-a"},
		`'it''s' x`:                      {"its", "x"},
		`a\ b "c \"d\"" 'e\f'`:           {"a b", `c "d"`, `e\f`},
		`tool ""`:                        {"tool", ""},
		"":                               nil,
		"one\ttwo\nthree":                {"one", "two", "three"},
		`kdiff3 --L1 "old one" --L2 new`: {"kdiff3", "--L1", "old one", "--L2", "new"},
		// A backslash at the end of a line joins it to the next.
		"curl 'a' \\\n  -H 'b'":   {"curl", "a", "-H", "b"},
		"curl 'a' \\\r\n  -H 'b'": {"curl", "a", "-H", "b"},
		"a\\\nb":                  {"ab"},
		"\"a\\\nb\"":              {"ab"},
		// In double quotes a backslash only escapes what is special there.
		`"a\nb\\c\$d"`: {`a\nb\c$d`},
		// $'...' quotes have escapes.
		"$'it\\'s\\n\\t\\x41\\101\\u00e9\\xc3\\xa9\\q'": {"it's\n\tAA\u00e9\u00e9\\q"},
		`x$'a'"b"'c'`: {"xabc"},
		`$`:           {"$"},
	} {
		got, err := Split(command)
		if err != nil || !slices.Equal(got, want) {
			t.Errorf("Split(%q) = %q, %v, want %q", command, got, err, want)
		}
	}
	for _, command := range []string{`meld "a`, `meld 'a`, `meld a\`, `$'a`, `$'a\'`} {
		if _, err := Split(command); err == nil {
			t.Errorf("Split(%q) has no error", command)
		}
	}
}

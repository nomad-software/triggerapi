package rpc

import (
	"slices"
	"testing"
)

func TestSplitMessages(t *testing.T) {
	for _, test := range []struct {
		text   string
		stream bool
		want   []string
	}{
		{"  ", false, []string{"{}"}},
		{`{"a": 1}`, false, []string{`{"a": 1}`}},
		{"{\"a\": 1}\n{\"a\": 2}", false, []string{`{"a": 1}`, `{"a": 2}`}},
		{`[{"a": 1}, {"a": 2}]`, true, []string{`{"a": 1}`, `{"a": 2}`}},
		{`[]`, true, []string{}},
		// An array is a message, which is not valid, unless it is of a
		// stream.
		{`[{"a": 1}]`, false, []string{`[{"a": 1}]`}},
		// Text that is not JSON is one message.
		{`{"a": 1} {oops`, true, []string{`{"a": 1} {oops`}},
	} {
		if got := splitMessages(test.text, test.stream); !slices.Equal(got, test.want) {
			t.Errorf("%q (stream %v): %q, want %q", test.text, test.stream, got, test.want)
		}
	}
}

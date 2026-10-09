package rest

import (
	"reflect"
	"testing"
)

func on(key, value string) KeyValue  { return KeyValue{Key: key, Value: value, Enabled: true} }
func off(key, value string) KeyValue { return KeyValue{Key: key, Value: value} }

func TestJoinURL(t *testing.T) {
	tests := []struct {
		base   string
		params []KeyValue
		want   string
	}{
		{"{{host}}/users", nil, "{{host}}/users"},
		{"{{host}}/users", []KeyValue{on("page", "2"), off("x", "1"), on("", "y"), on("q", "{{term}}")}, "{{host}}/users?page=2&q={{term}}"},
		{"a", []KeyValue{on("k=", "a b&c#d%e=f"), on("empty", "")}, "a?k%3D=a b%26c%23d%25e=f&empty="},
		{"a", []KeyValue{off("x", "1")}, "a"},
	}
	for _, tt := range tests {
		if got := JoinURL(tt.base, tt.params); got != tt.want {
			t.Errorf("JoinURL(%q, %v) = %q, want %q", tt.base, tt.params, got, tt.want)
		}
	}
}

func TestSplitURL(t *testing.T) {
	tests := []struct {
		raw    string
		base   string
		params []KeyValue
	}{
		{"{{host}}/users", "{{host}}/users", nil},
		{"a?", "a", nil},
		{"a?page=2&q={{term}}", "a", []KeyValue{on("page", "2"), on("q", "{{term}}")}},
		{"a?flag&x=1=2&&=nokey&", "a", []KeyValue{on("flag", ""), on("x", "1=2")}},
		{"a?q=a%20b+c&bad=100%", "a", []KeyValue{on("q", "a b+c"), on("bad", "100%")}},
	}
	for _, tt := range tests {
		base, params := SplitURL(tt.raw)
		if base != tt.base || !reflect.DeepEqual(params, tt.params) {
			t.Errorf("SplitURL(%q) = %q, %v, want %q, %v", tt.raw, base, params, tt.base, tt.params)
		}
	}
}

func TestJoinSplitRoundTrip(t *testing.T) {
	params := []KeyValue{on("k=&#%", "a b&c#d%e=f+g"), on("{{name}}", "{{value}}"), on("empty", "")}

	base, got := SplitURL(JoinURL("http://x/y", params))
	if base != "http://x/y" || !reflect.DeepEqual(got, params) {
		t.Errorf("got %q, %v", base, got)
	}
}

func TestMergeParams(t *testing.T) {
	existing := []KeyValue{on("a", "1"), off("x", "9"), on("b", "2")}

	tests := []struct {
		name  string
		typed []KeyValue
		want  []KeyValue
	}{
		{"edited", []KeyValue{on("a", "5"), on("b", "2")}, []KeyValue{on("a", "5"), off("x", "9"), on("b", "2")}},
		{"added", []KeyValue{on("a", "1"), on("b", "2"), on("c", "3")}, []KeyValue{on("a", "1"), off("x", "9"), on("b", "2"), on("c", "3")}},
		{"removed", []KeyValue{on("a", "1")}, []KeyValue{on("a", "1"), off("x", "9")}},
		{"cleared", nil, []KeyValue{off("x", "9")}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MergeParams(existing, tt.typed); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

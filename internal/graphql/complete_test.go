package graphql

import (
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/nomad-software/triggerapi/internal/completion"
)

func testCompletionSchema(t *testing.T) *Schema {
	t.Helper()
	s, err := parseSchema(http.StatusOK, "200 OK", []byte(testSchema))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// labels returns the labels of what can be typed where the query has a |,
// which is taken out.
func labels(s *Schema, query string) ([]string, completion.Completion) {
	offset := strings.Index(query, "|")
	query = strings.Replace(query, "|", "", 1)
	c := s.Complete(query, offset)
	var got []string
	for _, item := range c.Items {
		got = append(got, item.Label)
	}
	return got, c
}

func TestComplete(t *testing.T) {
	s := testCompletionSchema(t)
	for _, test := range []struct {
		query string
		want  []string
	}{
		// Fields of the root of a query, written with the word or without.
		{"{ |", []string{"__typename", "user"}},
		{"query Get($id: ID!) { u|", []string{"user"}},
		{"query { user(id: 1) { |", []string{"__typename", "friends", "id", "nick"}},
		{"query { user(id: 1) { fr|", []string{"friends"}},
		{"{ user { friends(first: 2) { n|", []string{"nick"}},
		// An alias is not a field.
		{"{ me: user { i|", []string{"id"}},
		// Fields after a selection is closed.
		{"{ user { friends { id } n|", []string{"nick"}},
		// The root of a mutation.
		{"mutation { |", []string{"__typename", "rename"}},
		// Arguments, and their values.
		{"{ user(|", []string{"id"}},
		{"{ user { friends(first: 1, |", []string{"first"}},
		{"{ user { friends(f|", []string{"first"}},
		{"{ user(id: |", nil},
		// Fragments.
		{"fragment F on |", []string{"Change", "Node", "Query", "Result", "User"}},
		{"fragment F on User { n|", []string{"nick"}},
		{"{ user { ... on |", []string{"Change", "Node", "Query", "Result", "User"}},
		{"{ user { ... on User { i|", []string{"id"}},
		{"{ user { ...|", []string{"on"}},
		// Directives, whose arguments are not those of a field.
		{"{ user { id @|", []string{"include", "skip"}},
		{"{ user { id @include(|", nil},
		// Outside any selection: the operations the schema has.
		{"|", []string{"fragment", "mutation", "query"}},
		{"qu|", []string{"query"}},
		// Nothing in strings, comments, variables, or a field that is not
		// known.
		{`{ user(id: "a|`, nil},
		{"{ # user|", nil},
		{"{ user(id: $|", nil},
		{"{ nothing { |", nil},
		// A name typed in full has nothing left to complete.
		{"{ user|", nil},
	} {
		if got, _ := labels(s, test.query); !slices.Equal(got, test.want) {
			t.Errorf("%q: %q, want %q", test.query, got, test.want)
		}
	}
}

func TestCompleteValues(t *testing.T) {
	s, err := parseSchema(http.StatusOK, "200 OK", []byte(`{"data": {"__schema": {
		"queryType": {"name": "Query"},
		"types": [
			{"kind": "OBJECT", "name": "Query", "fields": [
				{"name": "users", "args": [
					{"name": "role", "type": {"kind": "ENUM", "name": "Role"}},
					{"name": "active", "type": {"kind": "SCALAR", "name": "Boolean"}},
					{"name": "where", "type": {"kind": "INPUT_OBJECT", "name": "Where"}}
				], "type": {"kind": "SCALAR", "name": "Int"}}
			]},
			{"kind": "ENUM", "name": "Role", "enumValues": [{"name": "ADMIN"}, {"name": "GUEST"}]}
		]}}}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		query string
		want  []string
	}{
		{"{ users(role: |", []string{"ADMIN", "GUEST"}},
		{"{ users(role: G|", []string{"GUEST"}},
		{"{ users(active: |", []string{"false", "true"}},
		// After a value, the other arguments, with or without a comma.
		{"{ users(role: ADMIN |", []string{"active", "role", "where"}},
		{`{ users(where: {a: "x"}, |`, []string{"active", "role", "where"}},
		// Nothing inside an input object.
		{"{ users(where: {|", nil},
	} {
		if got, _ := labels(s, test.query); !slices.Equal(got, test.want) {
			t.Errorf("%q: %q, want %q", test.query, got, test.want)
		}
	}
}

func TestCompleteItems(t *testing.T) {
	s := testCompletionSchema(t)
	_, c := labels(s, "{ user(i|")
	if c.Start != 7 || c.End != 8 || len(c.Items) != 1 || c.Items[0] != (completion.Item{Label: "id", Detail: "ID!", Insert: "id: "}) {
		t.Errorf("argument: %+v", c)
	}
	_, c = labels(s, "{ user { friends(first: 1) { f|")
	if len(c.Items) != 1 || c.Items[0] != (completion.Item{Label: "friends", Detail: "[User!]", Insert: "friends"}) {
		t.Errorf("field: %+v", c)
	}
}

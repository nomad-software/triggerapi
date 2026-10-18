package assert

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

// facts are those of a response that the tests check.
var facts = Facts{
	Status:  []string{"200", "200 OK"},
	Time:    250 * time.Millisecond,
	Headers: http.Header{"Content-Type": {"application/json"}, "X-Many": {"a", "b"}},
	Body:    `{"name": "Ann", "count": 3, "price": 1.50, "ok": true, "none": null, "tags": ["a", "b"], "empty": [], "blank": "", "item": {"id": 7, "on": false}}`,
	Size:    120,
}

// noPlaceholders leaves a value as it is.
func noPlaceholders(s string) string { return s }

func TestRun(t *testing.T) {
	cases := []struct {
		name   string
		check  Check
		facts  *Facts
		passed bool
		reason string
	}{
		{name: "status equals", check: Check{Subject: Status, Comparison: Equals, Value: "200"}, passed: true},
		{name: "status equals its line", check: Check{Subject: Status, Comparison: Equals, Value: "200 ok"}, passed: true},
		{name: "status not equal", check: Check{Subject: Status, Comparison: Equals, Value: "201"}, reason: "the status is 200, not 201"},
		{name: "status not equals", check: Check{Subject: Status, Comparison: NotEquals, Value: "404"}, passed: true},
		{name: "status less", check: Check{Subject: Status, Comparison: Less, Value: "300"}, passed: true},
		{name: "status greater fails", check: Check{Subject: Status, Comparison: Greater, Value: "399"}, reason: "the status is 200, which is not greater than 399"},
		{name: "grpc status by code", check: Check{Subject: Status, Comparison: Equals, Value: "5"}, facts: &Facts{Status: []string{"NOT_FOUND", "5"}}, passed: true},
		{name: "grpc status by name, any case", check: Check{Subject: Status, Comparison: Equals, Value: "not_found"}, facts: &Facts{Status: []string{"NOT_FOUND", "5"}}, passed: true},
		{name: "grpc status compared by code", check: Check{Subject: Status, Comparison: Greater, Value: "0"}, facts: &Facts{Status: []string{"NOT_FOUND", "5"}}, passed: true},
		{name: "status contains", check: Check{Subject: Status, Comparison: Contains, Value: "OK"}, passed: true},
		{name: "status matches", check: Check{Subject: Status, Comparison: Matches, Value: `^2\d\d$`}, passed: true},

		{name: "time less", check: Check{Subject: Time, Comparison: Less, Value: "500"}, passed: true},
		{name: "time too long", check: Check{Subject: Time, Comparison: Less, Value: "100"}, reason: "the time in ms is 250, which is not less than 100"},
		{name: "size greater", check: Check{Subject: Size, Comparison: Greater, Value: "100"}, passed: true},
		{name: "size equals", check: Check{Subject: Size, Comparison: Equals, Value: "120"}, passed: true},

		{name: "header equals, any case", check: Check{Subject: Header, Path: "content-type", Comparison: Equals, Value: "application/json"}, passed: true},
		{name: "header contains", check: Check{Subject: Header, Path: "Content-Type", Comparison: Contains, Value: "json"}, passed: true},
		{name: "header of several values", check: Check{Subject: Header, Path: "X-Many", Comparison: Equals, Value: "a, b"}, passed: true},
		{name: "header exists", check: Check{Subject: Header, Path: "X-Many", Comparison: Exists}, passed: true},
		{name: "header missing", check: Check{Subject: Header, Path: "X-Gone", Comparison: Exists}, reason: "there is no header X-Gone"},
		{name: "header not there", check: Check{Subject: Header, Path: "X-Gone", Comparison: NotExists}, passed: true},
		{name: "header there", check: Check{Subject: Header, Path: "X-Many", Comparison: NotExists}, reason: "the header X-Many is there"},
		{name: "header unnamed", check: Check{Subject: Header, Comparison: Exists}, reason: "the path is to be the name of the header"},

		{name: "string equals", check: Check{Subject: Body, Path: "name", Comparison: Equals, Value: "Ann"}, passed: true},
		{name: "string not equal", check: Check{Subject: Body, Path: "name", Comparison: Equals, Value: "Bob"}, reason: `name is "Ann", not Bob`},
		{name: "number equals", check: Check{Subject: Body, Path: "count", Comparison: Equals, Value: "3.0"}, passed: true},
		{name: "number written otherwise", check: Check{Subject: Body, Path: "price", Comparison: Equals, Value: "1.5"}, passed: true},
		{name: "boolean equals", check: Check{Subject: Body, Path: "ok", Comparison: Equals, Value: "true"}, passed: true},
		{name: "null equals", check: Check{Subject: Body, Path: "none", Comparison: Equals, Value: "null"}, passed: true},
		{name: "null is not empty text", check: Check{Subject: Body, Path: "none", Comparison: Equals, Value: ""}, reason: "none is null, not "},
		{name: "object equals", check: Check{Subject: Body, Path: "item", Comparison: Equals, Value: `{"on":false,"id":7.0}`}, passed: true},
		{name: "array equals", check: Check{Subject: Body, Path: "tags", Comparison: Equals, Value: `[ "a", "b" ]`}, passed: true},
		{name: "array not equal", check: Check{Subject: Body, Path: "tags", Comparison: NotEquals, Value: `["b", "a"]`}, passed: true},
		{name: "not equals fails", check: Check{Subject: Body, Path: "count", Comparison: NotEquals, Value: "3"}, reason: "count is 3"},
		{name: "array contains", check: Check{Subject: Body, Path: "tags", Comparison: Contains, Value: "b"}, passed: true},
		{name: "array does not contain", check: Check{Subject: Body, Path: "tags", Comparison: Contains, Value: "c"}, reason: "tags does not contain c"},
		{name: "string contains", check: Check{Subject: Body, Path: "name", Comparison: Contains, Value: "nn"}, passed: true},
		{name: "not contains", check: Check{Subject: Body, Path: "name", Comparison: NotContains, Value: "x"}, passed: true},
		{name: "not contains fails", check: Check{Subject: Body, Path: "tags", Comparison: NotContains, Value: "a"}, reason: "tags contains a"},
		{name: "null contains nothing", check: Check{Subject: Body, Path: "none", Comparison: Contains, Value: ""}, reason: "none does not contain "},
		{name: "matches", check: Check{Subject: Body, Path: "item.id", Comparison: Matches, Value: `^\d+$`}, passed: true},
		{name: "does not match", check: Check{Subject: Body, Path: "name", Comparison: Matches, Value: `^B`}, reason: `name is "Ann", which does not match the regex`},
		{name: "regex not valid", check: Check{Subject: Body, Path: "name", Comparison: Matches, Value: `(`}, reason: "the regex is not valid: missing closing ): `(`"},
		{name: "exists", check: Check{Subject: Body, Path: "none", Comparison: Exists}, passed: true},
		{name: "does not exist", check: Check{Subject: Body, Path: "gone", Comparison: Exists}, reason: `there is no "gone" in the response`},
		{name: "not exists", check: Check{Subject: Body, Path: "tags[5]", Comparison: NotExists}, passed: true},
		{name: "not exists, inside a value", check: Check{Subject: Body, Path: "name.first", Comparison: NotExists}, passed: true},
		{name: "not exists fails", check: Check{Subject: Body, Path: "item.id", Comparison: NotExists}, reason: "item.id is there"},
		{name: "missing for a comparison", check: Check{Subject: Body, Path: "gone", Comparison: Equals, Value: "1"}, reason: `there is no "gone" in the response`},
		{name: "less needs a number", check: Check{Subject: Body, Path: "name", Comparison: Less, Value: "3"}, reason: `less than needs a number, and name is "Ann"`},
		{name: "less than what is not a number", check: Check{Subject: Body, Path: "count", Comparison: Less, Value: "abc"}, reason: `less than needs a number, and "abc" is not one`},
		{name: "greater", check: Check{Subject: Body, Path: "price", Comparison: Greater, Value: "1"}, passed: true},
		{name: "length of an array", check: Check{Subject: Body, Path: "tags", Comparison: Length, Value: "2"}, passed: true},
		{name: "length of an object", check: Check{Subject: Body, Path: "item", Comparison: Length, Value: "2"}, passed: true},
		{name: "length of a string", check: Check{Subject: Body, Path: "name", Comparison: Length, Value: "3"}, passed: true},
		{name: "wrong length", check: Check{Subject: Body, Path: "tags", Comparison: Length, Value: "3"}, reason: "the length of tags is 2, not 3"},
		{name: "no length", check: Check{Subject: Body, Path: "count", Comparison: Length, Value: "1"}, reason: "count is 3, which has no length"},
		{name: "length not a number", check: Check{Subject: Body, Path: "tags", Comparison: Length, Value: "two"}, reason: `the length to compare with, "two", is not a whole number`},
		{name: "empty array", check: Check{Subject: Body, Path: "empty", Comparison: Empty}, passed: true},
		{name: "empty string", check: Check{Subject: Body, Path: "blank", Comparison: Empty}, passed: true},
		{name: "null is empty", check: Check{Subject: Body, Path: "none", Comparison: Empty}, passed: true},
		{name: "not empty", check: Check{Subject: Body, Path: "tags", Comparison: Empty}, reason: `tags is ["a","b"], which is not empty`},
		{name: "a number is not empty", check: Check{Subject: Body, Path: "count", Comparison: Empty}, reason: "count is 3, which is not empty"},

		{name: "whole body", check: Check{Subject: Body, Comparison: Contains, Value: `"Ann"`}, passed: true},
		{name: "whole body of text", check: Check{Subject: Body, Comparison: Contains, Value: "hello"}, facts: &Facts{Body: "<p>hello</p>"}, passed: true},
		{name: "no body is empty", check: Check{Subject: Body, Comparison: Empty}, facts: &Facts{}, passed: true},
		{name: "a path in no body", check: Check{Subject: Body, Path: "id", Comparison: Exists}, facts: &Facts{}, reason: "the response has no body"},
		{name: "a path in no body is not there", check: Check{Subject: Body, Path: "id", Comparison: NotExists}, facts: &Facts{}, passed: true},
		{name: "a path in text", check: Check{Subject: Body, Path: "id", Comparison: Exists}, facts: &Facts{Body: "text"}, reason: "the response is not JSON"},
		{name: "a path in text is not checked", check: Check{Subject: Body, Path: "id", Comparison: NotExists}, facts: &Facts{Body: "text"}, reason: "the response is not JSON"},
		{name: "a path that cannot be read", check: Check{Subject: Body, Path: "tags[x]", Comparison: Exists}, reason: "[x] in the path is not a number or a name in quotes"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := facts
			if c.facts != nil {
				f = *c.facts
			}
			c.check.Enabled = true
			results := Run([]Check{c.check}, f, noPlaceholders)
			if len(results) != 1 {
				t.Fatalf("got %d results", len(results))
			}
			r := results[0]
			if r.Passed != c.passed || r.Reason != c.reason {
				t.Errorf("got passed %v, %q; want %v, %q", r.Passed, r.Reason, c.passed, c.reason)
			}
		})
	}
}

func TestRunInUse(t *testing.T) {
	checks := []Check{
		{Subject: Status, Comparison: Equals, Value: "200", Enabled: true},
		{Subject: Status, Comparison: Equals, Value: "500"},
		{Subject: "colour", Comparison: Equals, Value: "red", Enabled: true},
		{Subject: Status, Comparison: "about", Value: "200", Enabled: true},
		{Subject: Body, Path: "count", Comparison: Equals, Value: "{{count}}", Enabled: true},
	}
	expand := func(s string) string { return strings.ReplaceAll(s, "{{count}}", "4") }
	results := Run(checks, facts, expand)
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2: %+v", len(results), results)
	}
	if !results[0].Passed || results[0].Check != checks[0] {
		t.Errorf("first result is %+v", results[0])
	}
	// The value is expanded, but the check is kept as it was.
	if results[1].Passed || results[1].Reason != "count is 3, not 4" || results[1].Check != checks[4] {
		t.Errorf("second result is %+v", results[1])
	}
	if Failed(results) != 1 {
		t.Errorf("Failed is %d", Failed(results))
	}
}

func TestShowCutsShort(t *testing.T) {
	long := strings.Repeat("x", 200)
	got := show(long)
	if !strings.HasSuffix(got, "…") || len([]rune(got)) != maxShown+1 {
		t.Errorf("show gave %q", got)
	}
}

// TestJSON checks that a check is written with short names, leaving out what
// it does not have.
func TestJSON(t *testing.T) {
	data, err := json.Marshal(Check{Subject: Status, Comparison: Equals, Value: "200", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"subject":"status","comparison":"equals","value":"200","enabled":true}`; string(data) != want {
		t.Errorf("got %s, want %s", data, want)
	}
}

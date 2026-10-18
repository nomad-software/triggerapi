package capture

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

const document = `{
	"token": "abc.def",
	"count": 12345678901234567890,
	"price": 417.50,
	"ok": true,
	"none": null,
	"a.b": "dotted",
	"data": {"items": [{"id": 1, "tags": ["x", "y"]}, {"id": 2}], "user": {"name": "Ada"}}
}`

func TestExtract(t *testing.T) {
	for path, want := range map[string]string{
		"token":                 "abc.def",
		" token ":               "abc.def",
		"$.token":               "abc.def",
		"count":                 "12345678901234567890",
		"price":                 "417.50",
		"ok":                    "true",
		"none":                  "",
		`["a.b"]`:               "dotted",
		"['a.b']":               "dotted",
		"data.user.name":        "Ada",
		"data.items[0].id":      "1",
		"data.items[1].id":      "2",
		"data.items[-1].id":     "2",
		"data.items[0].tags":    `["x","y"]`,
		"data.items[0].tags[1]": "y",
		`data["user"].name`:     "Ada",
		"data.user":             `{"name":"Ada"}`,
	} {
		if got, err := Extract(document, path); err != nil || got != want {
			t.Errorf("%q: got %q, %v, want %q", path, got, err, want)
		}
	}

	// An empty path is the whole document.
	if got, err := Extract(`"plain"`, ""); err != nil || got != "plain" {
		t.Errorf("got %q, %v", got, err)
	}
	if got, err := Extract(`[10, 20]`, "[1]"); err != nil || got != "20" {
		t.Errorf("got %q, %v", got, err)
	}
}

func TestExtractErrors(t *testing.T) {
	for path, want := range map[string]string{
		"missing":          `no "missing"`,
		"data.items[2]":    "no [2] in an array of 2",
		"data.items[-3]":   "no [-3]",
		"data.items.id":    "there is an array there",
		"data[0]":          "there is an object there",
		"token.length":     "nothing to look for",
		"data..user":       "name missing",
		"data.":            "name missing",
		"data.items[0":     "no ]",
		"data.items[zero]": "not a number",
	} {
		if got, err := Extract(document, path); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: got %q, %v, want an error with %q", path, got, err, want)
		}
	}

	for _, body := range []string{"", "<html>", `{"a": 1} trailing`} {
		if _, err := Extract(body, "a"); err == nil || !strings.Contains(err.Error(), "not JSON") {
			t.Errorf("%q: %v", body, err)
		}
	}
}

// TestFind checks that a value is given as it is decoded, and that a path
// which is not in the document is told apart from one that cannot be looked
// for.
func TestFind(t *testing.T) {
	value, err := Find(document, "price")
	if err != nil || value != json.Number("417.50") {
		t.Errorf("Find gave %#v, %v", value, err)
	}

	var missing *MissingError
	for _, path := range []string{"gone", "data.items[99]", "token.part", "data.items.name", "data.user[0]"} {
		if _, err := Find(document, path); !errors.As(err, &missing) {
			t.Errorf("Find(%q) gave %v, not a MissingError", path, err)
		}
	}
	for _, path := range []string{"data.items[x]", "a..b"} {
		if _, err := Find(document, path); err == nil || errors.As(err, &missing) {
			t.Errorf("Find(%q) gave %v, which should be another error", path, err)
		}
	}
	if _, err := Find("not json", ""); err == nil || errors.As(err, &missing) {
		t.Errorf("Find of text gave %v, which should be another error", err)
	}
}

func TestMatch(t *testing.T) {
	tests := []struct {
		value, pattern, want string
	}{
		{"abc123", "", "abc123"},
		{"abc123", `\d+`, "123"},
		{"session=xyz; Path=/", `session=([^;]+)`, "xyz"},
		// The first group is the one that is kept, and the first match.
		{"a1 b2", `([a-z])(\d)`, "a"},
		{"Bearer TOKEN", `(?i)bearer (.+)`, "TOKEN"},
		// A group that matched nothing gives nothing.
		{"abc", `abc(\d)?`, ""},
	}
	for _, test := range tests {
		if got, err := Match(test.value, test.pattern); err != nil || got != test.want {
			t.Errorf("Match(%q, %q) = %q, %v, want %q", test.value, test.pattern, got, err, test.want)
		}
	}

	for pattern, want := range map[string]string{
		`\d+`:  "the regex does not match the value",
		`(abc`: "the regex is not valid: missing closing ): `(abc`",
	} {
		if _, err := Match("abc", pattern); err == nil || err.Error() != want {
			t.Errorf("Match(abc, %q): %v, want %q", pattern, err, want)
		}
	}
}

func TestRuleExtract(t *testing.T) {
	const document = `{"link": "https://example.com/users/42?page=2"}`

	rule := Rule{Variable: "id", Path: "link", Regex: `/users/(\d+)`}
	if got, err := rule.Extract(document); err != nil || got != "42" {
		t.Errorf("Extract = %q, %v", got, err)
	}

	// Without a regex it is the whole value, and the path comes first.
	rule.Regex = ""
	if got, err := rule.Extract(document); err != nil || got != "https://example.com/users/42?page=2" {
		t.Errorf("Extract = %q, %v", got, err)
	}
	rule.Path, rule.Regex = "nothing", `(`
	if _, err := rule.Extract(document); err == nil || err.Error() != `there is no "nothing" in the response` {
		t.Errorf("Extract: %v", err)
	}
}

func TestPathAt(t *testing.T) {
	const doc = `{
  "token": "abc",
  "a.b": 1,
  "$id": true,
  "data": {
    "items": [{"id": 1, "tags": ["x", "é😀"]}, {"id": 2}],
    "user": {"name": "Ada", "": null}
  },
  "list": [[1, 2], []]
}`

	// at gives the offset of the nth place some text is in the document.
	at := func(text string, nth int) int {
		offset := -1
		for range nth {
			offset += 1 + strings.Index(doc[offset+1:], text)
		}
		return offset
	}

	tests := []struct {
		what   string
		offset int
		path   string
		name   string
	}{
		{"a value", at(`"abc"`, 1) + 2, "token", "token"},
		{"the end of a value", at(`"abc"`, 1) + 5, "token", "token"},
		{"its name", at(`"token"`, 1) + 3, "token", "token"},
		{"the colon after its name", at(`"token"`, 1) + 7, "token", "token"},
		{"a name with a dot", at(`"a.b"`, 1), `["a.b"]`, "a.b"},
		{"a name with a $ first", at(`"$id"`, 1), `["$id"]`, "$id"},
		{"an object", at(`"data"`, 1), "data", "data"},
		{"the space in an object", at(`"items"`, 1) - 2, "data", "data"},
		{"an array", at(`"items"`, 1), "data.items", "items"},
		{"an element", at(`{"id": 1`, 1), "data.items[0]", "items"},
		{"in an element", at(`"id": 1`, 1) + 6, "data.items[0].id", "id"},
		{"in an array in an element", at(`"x"`, 1), "data.items[0].tags[0]", "tags"},
		{"after other letters", at(`"é😀"`, 1) + 3, "data.items[0].tags[1]", "tags"},
		{"in a later element", at(`"id": 2`, 1) + 6, "data.items[1].id", "id"},
		{"after an object", at(`"name"`, 1), "data.user.name", "name"},
		{"a name that is empty", at(`"": null`, 1) + 5, `data.user[""]`, "user"},
		{"in an array in an array", at(`2]`, 1), "list[0][1]", "list"},
		{"an empty array", at(`[]`, 1) + 1, "list[1]", "list"},
		{"the whole document", 0, "", "value"},
		{"the end of the document", len(doc), "", "value"},
	}
	for _, test := range tests {
		path, name, ok := PathAt(doc, test.offset)
		if !ok || path != test.path || name != test.name {
			t.Errorf("%s: PathAt(%d) = %q, %q, %v, want %q, %q", test.what, test.offset, path, name, ok, test.path, test.name)
			continue
		}
		// The path leads back to a value.
		if _, err := Extract(doc, path); err != nil {
			t.Errorf("%s: Extract(%q): %v", test.what, path, err)
		}
	}

	for what, test := range map[string]struct {
		doc    string
		offset int
	}{
		"before the document":       {"  1", 0},
		"after the document":        {"1  ", 3},
		"outside the document":      {"1", 5},
		"below the document":        {"1", -1},
		"not JSON":                  {"hello", 1},
		"not JSON before the value": {`{"a" 1, "b": 2}`, 12},
		"a name with a . and a ]":   {`{"a.]": 1}`, 8},
	} {
		if path, _, ok := PathAt(test.doc, test.offset); ok {
			t.Errorf("%s: PathAt(%q, %d) = %q", what, test.doc, test.offset, path)
		}
	}

	// A value that is all there is has no path.
	if path, name, ok := PathAt(`"abc"`, 2); !ok || path != "" || name != "value" {
		t.Errorf("PathAt of a string = %q, %q, %v", path, name, ok)
	}
}

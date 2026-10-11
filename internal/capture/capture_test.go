package capture

import (
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

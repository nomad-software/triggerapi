package env

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestExpand(t *testing.T) {
	e := Environment{Variables: []Variable{
		{Key: "host", Value: "old.test", Enabled: true},
		{Key: "host", Value: "api.test", Enabled: true},
		{Key: "token", Value: "abc", Enabled: true},
		{Key: "off", Value: "x"},
	}}

	tests := map[string]string{
		"https://{{host}}/v1":       "https://api.test/v1",
		"{{ host }}{{token}}":       "api.testabc",
		"{{off}} {{missing}}":       "{{off}} {{missing}}",
		"no placeholders":           "no placeholders",
		`{"a": {"b": "{{token}}"}}`: `{"a": {"b": "abc"}}`,
	}

	for in, want := range tests {
		if got := e.Expand(in); got != want {
			t.Errorf("Expand(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestStoreExpandWithoutActive(t *testing.T) {
	s := &Store{Active: -1, Environments: []Environment{{Variables: []Variable{{Key: "a", Value: "1", Enabled: true}}}}}
	if got := s.Expand("{{a}}"); got != "{{a}}" {
		t.Errorf("got %q", got)
	}
	s.Active = 0
	if got := s.Expand("{{a}}"); got != "1" {
		t.Errorf("got %q", got)
	}
}

func TestSaveLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "environments.json")

	want := &Store{Active: 1, Environments: []Environment{
		{Name: "Local", Variables: []Variable{{Key: "host", Value: "localhost:8080", Enabled: true}}},
		{Name: "Live", Variables: []Variable{{Key: "host", Value: "example.com"}}},
	}}
	// A store is put in alphabetical order when it is loaded.
	want.Sort()
	if err := want.Save(path); err != nil {
		t.Fatal(err)
	}

	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}

	if entries, _ := os.ReadDir(filepath.Dir(path)); len(entries) != 1 {
		t.Errorf("temporary files left behind: %v", entries)
	}
}

func TestLoadMissing(t *testing.T) {
	s, err := Load(filepath.Join(t.TempDir(), "none.json"))
	if err != nil {
		t.Fatal(err)
	}
	if s.Active != -1 || len(s.Environments) != 0 {
		t.Errorf("got %+v", s)
	}
}

func TestLoadInvalid(t *testing.T) {
	path := filepath.Join(t.TempDir(), "environments.json")
	os.WriteFile(path, []byte("{oops"), 0o644)

	s, err := Load(path)
	if err == nil {
		t.Fatal("expected an error")
	}
	if s == nil || s.Active != -1 {
		t.Errorf("got %+v", s)
	}
}

func TestLoadActiveOutOfRange(t *testing.T) {
	path := filepath.Join(t.TempDir(), "environments.json")
	os.WriteFile(path, []byte(`{"active": 5, "environments": [{"name": "a"}]}`), 0o644)

	s, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if s.Active != -1 {
		t.Errorf("Active = %d", s.Active)
	}
}

func TestSort(t *testing.T) {
	names := func(s *Store) []string {
		var out []string
		for _, e := range s.Environments {
			out = append(out, e.Name)
		}
		return out
	}

	s := &Store{Active: 0, Environments: []Environment{{Name: "staging"}, {Name: "Local"}, {Name: "live"}, {Name: "Dev"}}}
	s.Sort()
	if got, want := names(s), []string{"Dev", "live", "Local", "staging"}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	// The active environment is still the one named staging.
	if s.Active != 3 {
		t.Errorf("Active = %d", s.Active)
	}

	s.Active = -1
	s.Sort()
	if s.Active != -1 {
		t.Errorf("Active = %d", s.Active)
	}

	// A saved file is put in order when it is loaded.
	path := filepath.Join(t.TempDir(), "environments.json")
	os.WriteFile(path, []byte(`{"active": 0, "environments": [{"name": "b"}, {"name": "A"}]}`), 0o644)
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := names(loaded); !reflect.DeepEqual(got, []string{"A", "b"}) || loaded.Active != 1 {
		t.Errorf("loaded %v, active %d", got, loaded.Active)
	}
}

func TestSet(t *testing.T) {
	e := Environment{Variables: []Variable{{Key: "host", Value: "a", Enabled: true}, {Key: "token", Value: "old"}}}
	e.Set("token", "new")
	e.Set("id", "7")

	want := []Variable{{Key: "host", Value: "a", Enabled: true}, {Key: "token", Value: "new", Enabled: true}, {Key: "id", Value: "7", Enabled: true}}
	if !slices.Equal(e.Variables, want) {
		t.Errorf("got %+v, want %+v", e.Variables, want)
	}
}

func TestSetAmongDuplicates(t *testing.T) {
	// The one in use is set: the last that is enabled.
	e := Environment{Variables: []Variable{{Key: "host", Value: "a"}, {Key: "host", Value: "b", Enabled: true}, {Key: "host", Value: "c"}}}
	e.Set("host", "new")
	if e.Variables[0].Value != "a" || e.Variables[1].Value != "new" || e.Variables[2].Value != "c" || e.Expand("{{host}}") != "new" {
		t.Errorf("got %+v", e.Variables)
	}

	// With none enabled it is the first.
	e = Environment{Variables: []Variable{{Key: "host", Value: "a"}, {Key: "host", Value: "b"}}}
	e.Set("host", "new")
	if e.Variables[0] != (Variable{Key: "host", Value: "new", Enabled: true}) || e.Variables[1].Enabled {
		t.Errorf("got %+v", e.Variables)
	}
}

// noExpand leaves text as it is.
func noExpand(s string) string { return s }

func TestEvaluate(t *testing.T) {
	// Late in the day in a zone behind UTC, so the date in UTC is the next.
	zone := time.FixedZone("behind", -5*60*60)
	at := time.Date(2026, time.February, 27, 22, 30, 15, 0, zone)

	for value, want := range map[string]string{
		"iso_date()":         "2026-02-27",
		"iso_date(0)":        "2026-02-27",
		"iso_date(1)":        "2026-02-28",
		"iso_date(2)":        "2026-03-01",
		"iso_date(-27)":      "2026-01-31",
		" iso_date( 365 ) ":  "2027-02-27",
		"iso_timestamp()":    "2026-02-28T03:30:15Z",
		"iso_timestamp(2)":   "2026-03-02T03:30:15Z",
		"iso_timestamp(-1)":  "2026-02-27T03:30:15Z",
		"unix_timestamp()":   "1772249415",
		"unix_timestamp(2)":  "1772422215",
		"unix_timestamp(-1)": "1772163015",
	} {
		if got, ok := Evaluate(value, at, noExpand); !ok || got != want {
			t.Errorf("Evaluate(%q) = %q, %v, want %q", value, got, ok, want)
		}
	}

	uuid := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	first, ok := Evaluate("random_uuid()", at, noExpand)
	second, _ := Evaluate(" random_uuid( ) ", at, noExpand)
	if !ok || !uuid.MatchString(first) || !uuid.MatchString(second) || first == second {
		t.Errorf("random_uuid() = %q, then %q, %v", first, second, ok)
	}

	// Anything else is a value as it stands.
	for _, value := range []string{"", "abc", "iso_date", "iso_date(x)", "iso_date(1.5)", "iso_date(1, 2)", "iso_date(99999999999999999999)", "random_uuid(1)", "random_uuid(1, 2)", "iso_date(1, 2, 3)", "random_int(5, 1)", "random_int(x)", "random_int(1, 2, 3)", "Base64(a)", "base64(a) b", "other()", "ISO_DATE()", "id-random_uuid()", "iso_date() and more"} {
		if got, ok := Evaluate(value, at, noExpand); ok {
			t.Errorf("Evaluate(%q) = %q, want it left alone", value, got)
		}
	}
}

func TestEvaluateText(t *testing.T) {
	at := time.Now()
	shout := func(s string) string { return strings.ToUpper(s) }
	for value, want := range map[string]string{
		"base64(user:pass)":  "dXNlcjpwYXNz",
		"base64()":           "",
		"base64( a b )":      "IGEgYiA=",
		"base64(f(x))":       "Zih4KQ==",
		"base64(two\nlines)": "dHdvCmxpbmVz",
		"sha256(abc)":        "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad",
		"sha256()":           "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		" sha256(abc) ":      "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad",
	} {
		if got, ok := Evaluate(value, at, noExpand); !ok || got != want {
			t.Errorf("Evaluate(%q) = %q, %v, want %q", value, got, ok, want)
		}
	}
	// What a function of text is given has its placeholders replaced.
	if got, _ := Evaluate("base64(abc)", at, shout); got != "QUJD" {
		t.Errorf("base64 of expanded text = %q", got)
	}
}

func TestRandomInt(t *testing.T) {
	at := time.Now()
	for value, bounds := range map[string][2]int64{
		"random_int()":                       {0, defaultRandomMax},
		"random_int(5)":                      {0, 5},
		"random_int(-3, 3)":                  {-3, 3},
		"random_int( 7 , 7 )":                {7, 7},
		"random_int(-10, -9)":                {-10, -9},
		"random_int(0, 9223372036854775807)": {0, 9223372036854775807},
	} {
		seen := map[int64]bool{}
		for range 200 {
			got, ok := Evaluate(value, at, noExpand)
			n, err := strconv.ParseInt(got, 10, 64)
			if !ok || err != nil || n < bounds[0] || n > bounds[1] {
				t.Fatalf("Evaluate(%q) = %q, %v", value, got, ok)
			}
			seen[n] = true
		}
		// Both ends of a small range come up.
		if bounds[1]-bounds[0] <= 6 && (!seen[bounds[0]] || !seen[bounds[1]]) {
			t.Errorf("%s gave only %v", value, seen)
		}
	}
}

// TestExpandTextFunctions checks that the text given to a function is
// expanded with the other variables, which may be made by functions too, and
// that a variable which needs itself is left alone.
func TestExpandTextFunctions(t *testing.T) {
	e := Environment{Variables: []Variable{
		{Key: "user", Value: "ann", Enabled: true},
		{Key: "password", Value: "secret", Enabled: true},
		{Key: "basic", Value: "base64({{user}}:{{password}})", Enabled: true},
		{Key: "hashed", Value: "sha256({{basic}})", Enabled: true},
		{Key: "id", Value: "random_uuid()", Enabled: true},
		{Key: "signed", Value: "sha256({{id}})", Enabled: true},
		{Key: "loop", Value: "base64({{loop}})", Enabled: true},
		{Key: "plain", Value: "{{user}}", Enabled: true},
	}}
	expand := e.Expander()
	if got := expand("{{basic}}"); got != "YW5uOnNlY3JldA==" {
		t.Errorf("basic = %q", got)
	}
	sum := sha256.Sum256([]byte("YW5uOnNlY3JldA=="))
	if got := expand("{{hashed}}"); got != hex.EncodeToString(sum[:]) {
		t.Errorf("hashed = %q", got)
	}
	// A value made by a function is the same where another uses it.
	id := expand("{{id}}")
	sum = sha256.Sum256([]byte(id))
	if got := expand("{{signed}}"); got != hex.EncodeToString(sum[:]) {
		t.Errorf("signed = %q for id %q", got, id)
	}
	if got := expand("{{loop}}"); got != base64.StdEncoding.EncodeToString([]byte("{{loop}}")) {
		t.Errorf("loop = %q", got)
	}
	// A value which is not a function is not expanded, as before.
	if got := expand("{{plain}}"); got != "{{user}}" {
		t.Errorf("plain = %q", got)
	}
}

func TestExpandFunctions(t *testing.T) {
	defer func(prev func() time.Time) { now = prev }(now)
	now = func() time.Time { return time.Date(2026, time.October, 11, 9, 0, 0, 0, time.UTC) }

	e := Environment{Variables: []Variable{
		{Key: "id", Value: "random_uuid()", Enabled: true},
		{Key: "tomorrow", Value: "iso_date(1)", Enabled: true},
		{Key: "at", Value: "iso_timestamp()", Enabled: true},
		{Key: "off", Value: "iso_date()", Enabled: false},
		{Key: "plain", Value: "the iso_date() function", Enabled: true},
	}}

	if got, want := e.Expand("{{tomorrow}} {{at}} {{off}} {{plain}}"), "2026-10-12 2026-10-11T09:00:00Z {{off}} the iso_date() function"; got != want {
		t.Errorf("Expand = %q, want %q", got, want)
	}

	// A value is made again each time the variables are expanded, but is
	// the same wherever one expander puts it.
	if first, second := e.Expand("{{id}}"), e.Expand("{{id}}"); first == second || len(first) != 36 {
		t.Errorf("expanded twice: %q and %q", first, second)
	}
	expand := e.Expander()
	if first, second := expand("{{id}}"), expand("a {{id}}"); "a "+first != second || len(first) != 36 {
		t.Errorf("one expander: %q and %q", first, second)
	}

	store := Store{Active: 0, Environments: []Environment{e}}
	if got := store.Expander()("{{tomorrow}}"); got != "2026-10-12" {
		t.Errorf("the store's expander: %q", got)
	}
	store.Active = -1
	if got := store.Expander()("{{tomorrow}}"); got != "{{tomorrow}}" {
		t.Errorf("with no environment: %q", got)
	}
}

// Load reads a store that is not encrypted.
func Load(path string) (*Store, error) {
	return LoadWith(path, nil)
}

func TestEnvironmentClone(t *testing.T) {
	e := Environment{Name: "Dev", Variables: []Variable{{Key: "a", Value: "1", Enabled: true, Description: "d"}}}
	clone := e.Clone()
	if !reflect.DeepEqual(clone, e) {
		t.Fatalf("Clone() = %+v, want %+v", clone, e)
	}
	clone.Variables[0].Value = "2"
	if e.Variables[0].Value != "1" {
		t.Error("the clone shares its variables with the original")
	}
}

func TestFindPlaceholders(t *testing.T) {
	// The offsets are of bytes. A placeholder holds anything but braces,
	// with spaces around it allowed, even nothing.
	for text, want := range map[string][][]int{
		"":                   nil,
		"no placeholders":    nil,
		"{{host}}/users":     {{0, 8}},
		"a{{x}}b{{y_2}}":     {{1, 6}, {7, 14}},
		"{{ spaced }}":       {{0, 12}},
		"{{unclosed} {{ok}}": {{12, 18}},
		"{ {x} }":            nil,
		"{{}}":               {{0, 4}},
		"{{café}}":           {{0, 9}},
		"{{{triple}}}":       {{1, 11}},
	} {
		if got := FindPlaceholders(text); !reflect.DeepEqual(got, want) {
			t.Errorf("FindPlaceholders(%q) = %v, want %v", text, got, want)
		}
	}
}

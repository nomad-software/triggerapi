package env

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
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

func TestPlaceholders(t *testing.T) {
	got := Placeholders("{{a}}/x/{{ b }}/{c}")
	if want := []string{"{{a}}", "{{ b }}"}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
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
		if got, ok := Evaluate(value, at); !ok || got != want {
			t.Errorf("Evaluate(%q) = %q, %v, want %q", value, got, ok, want)
		}
	}

	uuid := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	first, ok := Evaluate("random_uuid()", at)
	second, _ := Evaluate(" random_uuid( ) ", at)
	if !ok || !uuid.MatchString(first) || !uuid.MatchString(second) || first == second {
		t.Errorf("random_uuid() = %q, then %q, %v", first, second, ok)
	}

	// Anything else is a value as it stands.
	for _, value := range []string{"", "abc", "iso_date", "iso_date(x)", "iso_date(1.5)", "iso_date(1, 2)", "iso_date(99999999999999999999)", "random_uuid(1)", "other()", "ISO_DATE()", "id-random_uuid()", "iso_date() and more"} {
		if got, ok := Evaluate(value, at); ok {
			t.Errorf("Evaluate(%q) = %q, want it left alone", value, got)
		}
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

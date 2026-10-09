package env

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
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

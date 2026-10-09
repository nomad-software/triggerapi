package collection

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/nomad-software/postie/internal/rest"
)

func TestSaveLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "requests.json")

	want := &Store{}
	users := want.AddFolder("Users")
	users.Add("List", rest.Request{Method: "GET", URL: "{{host}}/users"})
	users.Add("Create", rest.Request{
		Method:   "POST",
		URL:      "{{host}}/users",
		Params:   []rest.KeyValue{{Key: "dry", Value: "1"}},
		Headers:  []rest.KeyValue{{Key: "X-A", Value: "b", Enabled: true}},
		Auth:     rest.Auth{Type: rest.AuthBearer, Token: "{{token}}"},
		BodyType: rest.BodyJSON,
		Body:     `{"name": "a"}`,
	})
	want.AddFolder("Empty")

	if err := want.Save(path); err != nil {
		t.Fatal(err)
	}

	// The request fields are stored alongside the name, not nested.
	data, _ := os.ReadFile(path)
	for _, s := range []string{`"name": "Create"`, `"method": "POST"`, `"type": "bearer"`, `"bodyType": "json"`} {
		if !strings.Contains(string(data), s) {
			t.Errorf("file does not contain %s:\n%s", s, data)
		}
	}

	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %s", data)
	}
}

func TestRemove(t *testing.T) {
	s := &Store{}
	a := s.AddFolder("a")
	s.AddFolder("b")
	first := a.Add("1", rest.Request{})
	second := a.Add("2", rest.Request{})

	a.Remove(0)
	if len(a.Requests) != 1 || a.Requests[0] != second || first.Name != "1" {
		t.Errorf("requests = %+v", a.Requests)
	}

	s.RemoveFolder(0)
	if len(s.Folders) != 1 || s.Folders[0].Name != "b" {
		t.Errorf("folders = %+v", s.Folders)
	}
}

func TestLoadMissingAndInvalid(t *testing.T) {
	dir := t.TempDir()

	s, err := Load(filepath.Join(dir, "none.json"))
	if err != nil || s == nil || len(s.Folders) != 0 {
		t.Errorf("missing: %+v, %v", s, err)
	}

	path := filepath.Join(dir, "requests.json")
	os.WriteFile(path, []byte("{oops"), 0o644)
	s, err = Load(path)
	if err == nil || s == nil {
		t.Errorf("invalid: %+v, %v", s, err)
	}

	os.WriteFile(path, []byte(`{"folders": [null, {"name": "a", "requests": [null]}]}`), 0o644)
	s, err = Load(path)
	if err != nil || len(s.Folders) != 1 || len(s.Folders[0].Requests) != 0 {
		t.Errorf("nulls: %+v, %v", s, err)
	}
}

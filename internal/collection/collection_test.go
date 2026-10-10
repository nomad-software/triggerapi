package collection

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/nomad-software/postie/internal/rest"
	"github.com/nomad-software/postie/internal/rpc"
)

func TestSaveLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "requests.json")

	want := &Store{}
	users := want.AddFolder("Users")
	users.Add(NewREST("List", rest.Request{Method: "GET", URL: "{{host}}/users"}))
	users.Add(NewREST("Create", rest.Request{
		Method:   "POST",
		URL:      "{{host}}/users",
		Params:   []rest.KeyValue{{Key: "dry", Value: "1"}},
		Headers:  []rest.KeyValue{{Key: "X-A", Value: "b", Enabled: true}},
		Auth:     rest.Auth{Type: rest.AuthBearer, Token: "{{token}}"},
		BodyType: rest.BodyJSON,
		Body:     `{"name": "a"}`,
	}))
	users.Add(NewGRPC("Say", rpc.Request{
		URL:      "{{host}}:50051",
		TLS:      true,
		Method:   "test.v1.Echo/Say",
		Message:  `{"text": "hi"}`,
		Metadata: []rest.KeyValue{{Key: "x-trace", Value: "1", Enabled: true}},
		Auth:     rest.Auth{Type: rest.AuthBasic, Username: "u", Password: "p"},
	}))
	want.AddFolder("Empty")

	if err := want.Save(path); err != nil {
		t.Fatal(err)
	}

	// The request fields are stored alongside the name, not nested.
	data, _ := os.ReadFile(path)
	for _, s := range []string{`"name": "Create"`, `"method": "POST"`, `"type": "bearer"`, `"bodyType": "json"`, `"type": "rest"`, `"type": "grpc"`, `"method": "test.v1.Echo/Say"`} {
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
	first := a.Add(NewREST("1", rest.Request{}))
	second := a.Add(NewREST("2", rest.Request{}))

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

func TestLoadNormalises(t *testing.T) {
	path := filepath.Join(t.TempDir(), "requests.json")
	os.WriteFile(path, []byte(`{"folders": [{"name": "a", "requests": [
		{"name": "old", "method": "POST", "url": "example.com", "auth": {}},
		{"name": "empty"},
		{"name": "rpc", "type": "grpc", "grpc": {"url": "localhost:1", "method": "a.B/C", "auth": {}}},
		{"name": "bare rpc", "type": "grpc", "url": "ignored"}
	]}]}`), 0o644)

	s, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	requests := s.Folders[0].Requests

	// A request saved before there were types is a REST request.
	if r := requests[0]; r.Type != TypeREST || r.GRPC != nil || r.Method != "POST" || r.URL != "example.com" {
		t.Errorf("old = %+v", r)
	}
	if r := requests[1]; r.Type != TypeREST || r.Request == nil {
		t.Errorf("empty = %+v", r)
	}
	if r := requests[2]; r.Type != TypeGRPC || r.Request != nil || r.GRPC.Method != "a.B/C" {
		t.Errorf("rpc = %+v", r)
	}
	if r := requests[3]; r.Type != TypeGRPC || r.Request != nil || r.GRPC == nil {
		t.Errorf("bare rpc = %+v", r)
	}
}

func TestNestedFolders(t *testing.T) {
	path := filepath.Join(t.TempDir(), "requests.json")

	want := &Store{}
	service := want.AddFolder("em-ohip-hotel")
	rpc1 := service.AddFolder("ohip_hotel")
	first := rpc1.Add(NewGRPC("GetHotel", rpc.Request{URL: "localhost:1", Method: "ohip_hotel.Hotels/Get"}))
	deep := rpc1.AddFolder("admin").AddFolder("reports")
	second := deep.Add(NewREST("Daily", rest.Request{Method: "GET", URL: "example.com/daily"}))
	service.AddFolder("empty")
	third := service.Add(NewREST("Health", rest.Request{Method: "GET", URL: "example.com/health"}))

	if got, wantAll := service.All(), []*Request{third, first, second}; !reflect.DeepEqual(got, wantAll) {
		t.Errorf("All = %v", got)
	}
	if got := service.Descendants(); got != 4 {
		t.Errorf("Descendants = %d, want 4", got)
	}

	if err := want.Save(path); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		data, _ := os.ReadFile(path)
		t.Errorf("got %s", data)
	}

	// Removing a folder takes everything in it.
	rpc1.RemoveFolder(0)
	if len(rpc1.Folders) != 0 || len(service.All()) != 2 {
		t.Errorf("after remove: %d folders, %d requests", len(rpc1.Folders), len(service.All()))
	}
}

func TestLoadNestedNulls(t *testing.T) {
	path := filepath.Join(t.TempDir(), "requests.json")
	os.WriteFile(path, []byte(`{"folders": [{"name": "a", "folders": [null, {"name": "b", "folders": [null], "requests": [null, {"name": "r"}]}]}]}`), 0o644)

	s, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	b := s.Folders[0].Folders
	if len(b) != 1 || len(b[0].Folders) != 0 || len(b[0].Requests) != 1 || b[0].Requests[0].Type != TypeREST {
		t.Errorf("got %+v", b)
	}
}

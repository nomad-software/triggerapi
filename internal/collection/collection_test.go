package collection

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/nomad-software/triggerapi/internal/rest"
	"github.com/nomad-software/triggerapi/internal/rpc"
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

	// A store is put in alphabetical order when it is loaded.
	want.Sort()
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
	// The requests are loaded in alphabetical order, so are found by name.
	requests := map[int]*Request{}
	for _, r := range s.Folders[0].Requests {
		requests[slices.Index([]string{"old", "empty", "rpc", "bare rpc"}, r.Name)] = r
	}

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

// names lists the names of the folders and then the requests of a folder.
func names(f *Folder) []string {
	var out []string
	for _, sub := range f.Folders {
		out = append(out, sub.Name+"/")
	}
	for _, r := range f.Requests {
		out = append(out, r.Name)
	}
	return out
}

func TestMoveRequest(t *testing.T) {
	s := &Store{}
	a := s.AddFolder("a")
	b := a.AddFolder("b")
	r1 := a.Add(NewREST("1", rest.Request{}))
	r2 := a.Add(NewREST("2", rest.Request{}))
	r3 := a.Add(NewREST("3", rest.Request{}))
	r4 := b.Add(NewREST("4", rest.Request{}))

	if s.FolderOf(r4) != b || s.FolderOf(r1) != a || s.FolderOf(NewREST("x", rest.Request{})) != nil {
		t.Error("FolderOf is wrong")
	}

	steps := []struct {
		request *Request
		to      *Folder
		index   int
		wantA   []string
		wantB   []string
	}{
		// Reordering within a folder, later and then earlier.
		{r1, a, 2, []string{"b/", "2", "1", "3"}, []string{"4"}},
		{r3, a, 0, []string{"b/", "3", "2", "1"}, []string{"4"}},
		// To the end, and to an index past the end.
		{r3, a, 3, []string{"b/", "2", "1", "3"}, []string{"4"}},
		{r2, a, 99, []string{"b/", "1", "3", "2"}, []string{"4"}},
		// Into another folder.
		{r1, b, 0, []string{"b/", "3", "2"}, []string{"1", "4"}},
		{r2, b, 2, []string{"b/", "3"}, []string{"1", "4", "2"}},
		// Where it already is.
		{r4, b, 1, []string{"b/", "3"}, []string{"1", "4", "2"}},
	}
	for i, step := range steps {
		if !s.MoveRequest(step.request, step.to, step.index) {
			t.Fatalf("step %d: not moved", i)
		}
		if got := names(a); !reflect.DeepEqual(got, step.wantA) {
			t.Errorf("step %d: a = %v, want %v", i, got, step.wantA)
		}
		if got := names(b); !reflect.DeepEqual(got, step.wantB) {
			t.Errorf("step %d: b = %v, want %v", i, got, step.wantB)
		}
	}

	if s.MoveRequest(NewREST("x", rest.Request{}), a, 0) || s.MoveRequest(r3, nil, 0) {
		t.Error("moved a request that is not in the store, or to no folder")
	}
}

func TestMoveFolder(t *testing.T) {
	s := &Store{}
	a := s.AddFolder("a")
	b := s.AddFolder("b")
	c := a.AddFolder("c")
	d := c.AddFolder("d")
	c.Add(NewREST("r", rest.Request{}))

	top := func() []string {
		var out []string
		for _, f := range s.Folders {
			out = append(out, f.Name)
		}
		return out
	}

	if parent, ok := s.ParentOf(d); !ok || parent != c {
		t.Error("ParentOf(d) is wrong")
	}
	if parent, ok := s.ParentOf(a); !ok || parent != nil {
		t.Error("ParentOf(a) is wrong")
	}
	if _, ok := s.ParentOf(&Folder{}); ok {
		t.Error("found the parent of a folder that is not in the store")
	}

	// A folder cannot go inside itself or anything inside it.
	if s.MoveFolder(a, a, 0) || s.MoveFolder(a, d, 0) || s.MoveFolder(c, d, 0) || s.MoveFolder(&Folder{}, a, 0) {
		t.Error("moved a folder inside itself")
	}
	if got := names(a); !reflect.DeepEqual(got, []string{"c/"}) {
		t.Errorf("a = %v", got)
	}

	// Into another folder, taking its contents with it.
	if !s.MoveFolder(c, b, 0) || len(a.Folders) != 0 || !reflect.DeepEqual(names(b), []string{"c/"}) || !reflect.DeepEqual(names(c), []string{"d/", "r"}) {
		t.Errorf("a = %v, b = %v, c = %v", names(a), names(b), names(c))
	}

	// Out to the top level, between the two folders there.
	if !s.MoveFolder(d, nil, 1) || !reflect.DeepEqual(top(), []string{"a", "d", "b"}) || len(c.Folders) != 0 {
		t.Errorf("top = %v", top())
	}

	// Reordering the top level.
	if !s.MoveFolder(a, nil, 3) || !reflect.DeepEqual(top(), []string{"d", "b", "a"}) {
		t.Errorf("top = %v", top())
	}
	if !s.MoveFolder(a, nil, 0) || !reflect.DeepEqual(top(), []string{"a", "d", "b"}) {
		t.Errorf("top = %v", top())
	}
}

func TestSort(t *testing.T) {
	s := &Store{}
	b := s.AddFolder("beta")
	s.AddFolder("Alpha")
	b.AddFolder("zeta")
	inner := b.AddFolder("Eta")
	b.Add(NewREST("list", rest.Request{}))
	b.Add(NewREST("Create", rest.Request{}))
	inner.Add(NewREST("b", rest.Request{}))
	inner.Add(NewREST("A", rest.Request{}))

	s.Sort()

	if s.Folders[0].Name != "Alpha" || s.Folders[1] != b {
		t.Errorf("top = %q, %q", s.Folders[0].Name, s.Folders[1].Name)
	}
	if got, want := names(b), []string{"Eta/", "zeta/", "Create", "list"}; !reflect.DeepEqual(got, want) {
		t.Errorf("beta = %v, want %v", got, want)
	}
	if got, want := names(inner), []string{"A", "b"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Eta = %v, want %v", got, want)
	}
}

func TestPathAndFind(t *testing.T) {
	store := &Store{}
	hotel := store.AddFolder("Hotel")
	ping := hotel.Add(NewREST("Ping", rest.Request{Method: "GET"}))
	rooms := hotel.AddFolder("Rooms")
	book := rooms.Add(NewREST("Book", rest.Request{Method: "POST"}))
	// Another folder of the same name, further on, with a request of its own.
	other := store.AddFolder("Hotel")
	late := other.Add(NewREST("Late", rest.Request{Method: "GET"}))

	if got, want := store.Path(book), []string{"Hotel", "Rooms", "Book"}; !reflect.DeepEqual(got, want) {
		t.Errorf("path = %q, want %q", got, want)
	}
	if got, want := store.Path(ping), []string{"Hotel", "Ping"}; !reflect.DeepEqual(got, want) {
		t.Errorf("path = %q, want %q", got, want)
	}
	if got := store.Path(NewREST("Stray", rest.Request{})); got != nil {
		t.Errorf("path of a request that is not in the store = %q", got)
	}

	for _, r := range []*Request{ping, book, late} {
		if got := store.Find(store.Path(r)); got != r {
			t.Errorf("found %+v for %q", got, store.Path(r))
		}
	}
	for _, path := range [][]string{nil, {"Hotel"}, {"Hotel", "Missing"}, {"Hotel", "Rooms"}, {"Nowhere", "Ping"}, {"Hotel", "Rooms", "Book", "More"}} {
		if got := store.Find(path); got != nil {
			t.Errorf("found %+v for %q", got, path)
		}
	}
}

func TestClone(t *testing.T) {
	folder := &Folder{Name: "Hotel"}
	original := folder.Add(NewREST("Book", rest.Request{
		Method:  "POST",
		URL:     "api.test",
		Headers: []rest.KeyValue{{Key: "X-Key", Value: "abc", Enabled: true}},
		Auth:    rest.Auth{Type: rest.AuthBearer, Token: "t"},
		Body:    "{}",
	}))
	call := folder.AddFolder("Inner").Add(NewGRPC("Call", rpc.Request{URL: "c.test:1", Method: "a.B/C", Message: "{}"}))

	// A copy of a request is the same, and changing it leaves the first
	// alone.
	clone := original.Clone()
	if clone == original || clone.Request == original.Request || !reflect.DeepEqual(clone, original) {
		t.Fatalf("clone = %+v, want a copy of %+v", clone, original)
	}
	clone.Headers[0].Value = "changed"
	clone.URL = "other.test"
	if original.Headers[0].Value != "abc" || original.URL != "api.test" {
		t.Errorf("the request copied from was changed: %+v", original.Request)
	}

	if grpc := call.Clone(); grpc.GRPC == call.GRPC || !reflect.DeepEqual(grpc, call) || grpc.Request != nil {
		t.Errorf("clone = %+v, want a copy of %+v", grpc, call)
	}

	// So is a copy of a folder, with everything in it.
	copied := folder.Clone()
	if copied == folder || !reflect.DeepEqual(copied, folder) || copied.Requests[0] == original || copied.Folders[0] == folder.Folders[0] {
		t.Fatalf("clone = %+v, want a copy of %+v", copied, folder)
	}
	copied.Folders[0].Requests[0].GRPC.URL = "changed"
	if call.GRPC.URL != "c.test:1" {
		t.Error("the folder copied from was changed")
	}
}

// Load reads a store that is not encrypted.
func Load(path string) (*Store, error) {
	return LoadWith(path, nil)
}

func TestFavouriteDropped(t *testing.T) {
	// A file from when folders could be favourites loads with nothing else
	// lost, and the mark is not written again.
	path := filepath.Join(t.TempDir(), "requests.json")
	old := `{"folders":[{"name":"a","collapsed":true,"favourite":true,"requests":[{"name":"r","method":"GET","url":"example.com"}]}]}`
	if err := os.WriteFile(path, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := LoadWith(path, nil)
	if err != nil || len(s.Folders) != 1 || len(s.Folders[0].Requests) != 1 || s.Folders[0].Requests[0].Request.URL != "example.com" {
		t.Fatalf("loaded %+v, %v", s.Folders, err)
	}
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if strings.Contains(string(data), "favourite") {
		t.Errorf("favourite written: %s", data)
	}
}

func TestSets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "requests.json")

	// A file from before there were sets has none, and is written without
	// them.
	old := `{"folders":[{"name":"A","requests":[{"name":"get","method":"GET","url":"a.example"}]}]}`
	if err := os.WriteFile(path, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Load(path)
	if err != nil || len(s.Sets) != 0 || len(s.Folders) != 1 {
		t.Fatalf("loaded %+v, %v", s, err)
	}
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(path); strings.Contains(string(data), "sets") {
		t.Errorf("sets written: %s", data)
	}

	// A set holds requests from any folder, as their paths in the file.
	get := s.Folders[0].Requests[0]
	b := s.AddFolder("B").AddFolder("Inner")
	ping := b.Add(NewGRPC("ping", rpc.Request{}))
	s.AddSet("work", []*Request{ping, get})
	s.AddSet("empty", nil)
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	s, err = Load(path)
	if err != nil || len(s.Sets) != 2 || s.Sets[0].Name != "empty" || s.Sets[1].Name != "work" {
		t.Fatalf("read back %+v, %v", s.Sets, err)
	}
	work := s.Sets[1]
	if len(work.Requests) != 2 || work.Requests[0].Name != "ping" || work.Requests[1].Name != "get" {
		t.Fatalf("requests of the set: %+v", work.Requests)
	}

	// Renaming or moving a request keeps it in the set.
	work.Requests[0].Name = "pong"
	s.MoveRequest(work.Requests[1], s.Folders[1], 0)
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	if s, err = Load(path); err != nil || len(s.Sets[1].Requests) != 2 || s.Sets[1].Requests[0].Name != "pong" {
		t.Fatalf("after rename and move: %+v, %v", s.Sets[1].Requests, err)
	}

	// A request removed is taken out of the set.
	s.FolderOf(s.Sets[1].Requests[0]).Remove(0)
	s.PruneSets()
	if got := s.Sets[1].Requests; len(got) != 1 || got[0].Name != "get" {
		t.Errorf("after removal: %+v", got)
	}

	// A path that cannot be found is left out.
	broken := `{"folders":[{"name":"A","requests":[{"name":"get"}]}],"sets":[{"name":"t","requests":[["A","gone"],["A","get"],["A","get"]]}]}`
	if err := os.WriteFile(path, []byte(broken), 0o600); err != nil {
		t.Fatal(err)
	}
	if s, err = Load(path); err != nil || len(s.Sets) != 1 || len(s.Sets[0].Requests) != 1 {
		t.Errorf("broken paths: %+v, %v", s.Sets, err)
	}
}

package collection

import (
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/nomad-software/triggerapi/internal/assert"
	"github.com/nomad-software/triggerapi/internal/capture"
	"github.com/nomad-software/triggerapi/internal/graphql"
	"github.com/nomad-software/triggerapi/internal/mcp"
	"github.com/nomad-software/triggerapi/internal/rest"
	"github.com/nomad-software/triggerapi/internal/rpc"
	"github.com/nomad-software/triggerapi/internal/ws"
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
	old := `{"folders":[{"name":"a","collapsed":true,"favourite":true,"requests":[{"name":"1 rest","method":"GET","url":"example.com"}]}]}`
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

func TestWebSocketRequest(t *testing.T) {
	path := filepath.Join(t.TempDir(), "requests.json")
	s := &Store{}
	f := s.AddFolder("Sockets")
	f.Add(NewWebSocket("Chat", ws.Request{URL: "ws://chat.test/{{room}}", Message: `{"say":"hi"}`, Headers: []rest.KeyValue{{Key: "X-A", Value: "b", Enabled: true}}}))
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), `"type": "websocket"`) || !strings.Contains(string(data), `"websocket": {`) {
		t.Errorf("written as %s", data)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	r := loaded.Folders[0].Requests[0]
	if r.Type != TypeWebSocket || r.WebSocket == nil || r.Request != nil || r.GRPC != nil || r.WebSocket.URL != "ws://chat.test/{{room}}" || len(r.WebSocket.Headers) != 1 {
		t.Errorf("loaded %+v %+v", r, r.WebSocket)
	}
	if c := r.Clone(); c.WebSocket == r.WebSocket || c.WebSocket.Message != r.WebSocket.Message {
		t.Errorf("clone %+v", c.WebSocket)
	}

	// One with nothing of its own is given an empty request.
	if err := os.WriteFile(path, []byte(`{"folders":[{"name":"a","requests":[{"name":"w","type":"websocket"}]}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if loaded, err = Load(path); err != nil || loaded.Folders[0].Requests[0].WebSocket == nil {
		t.Errorf("empty websocket request: %+v, %v", loaded.Folders[0].Requests[0], err)
	}
}

func TestNew(t *testing.T) {
	for _, tc := range []struct {
		typ  Type
		want *Request
	}{
		{TypeREST, NewREST("A", rest.Request{Method: "GET"})},
		{TypeGRPC, NewGRPC("A", rpc.Request{})},
		{TypeWebSocket, NewWebSocket("A", ws.Request{})},
		// A type that is not known is a REST request, as one loaded is.
		{"other", NewREST("A", rest.Request{Method: "GET"})},
	} {
		if got := New(tc.typ, "A"); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("New(%q) = %+v, want %+v", tc.typ, got, tc.want)
		}
	}
}

func TestFolderPlacement(t *testing.T) {
	s := &Store{}
	top := s.AddFolder("Top")
	inner := top.AddFolder("Inner")

	if got := s.FoldersIn(nil); !slices.Equal(got, []*Folder{top}) {
		t.Errorf("FoldersIn(nil) = %v", got)
	}
	if got := s.FoldersIn(top); !slices.Equal(got, []*Folder{inner}) {
		t.Errorf("FoldersIn(top) = %v", got)
	}

	other := &Folder{Name: "Other"}
	s.Insert(nil, other)
	deeper := &Folder{Name: "Deeper"}
	s.Insert(inner, deeper)
	if !slices.Equal(s.Folders, []*Folder{top, other}) || !slices.Equal(inner.Folders, []*Folder{deeper}) {
		t.Fatalf("Insert put the folders in the wrong places")
	}

	if !s.Delete(deeper) || len(inner.Folders) != 0 {
		t.Error("an inner folder was not deleted")
	}
	if !s.Delete(top) || !slices.Equal(s.Folders, []*Folder{other}) {
		t.Error("a top level folder was not deleted")
	}
	if s.Delete(top) {
		t.Error("a folder not in the store was reported deleted")
	}
}

func TestSetContent(t *testing.T) {
	r := NewREST("Kept", rest.Request{Method: "GET", URL: "a"})
	r.SetContent(NewREST("Other", rest.Request{Method: "POST", URL: "b"}))
	if r.Name != "Kept" || r.Type != TypeREST || r.Method != "POST" || r.URL != "b" {
		t.Errorf("after a REST request: %+v", r)
	}
	r.SetContent(NewGRPC("Other", rpc.Request{URL: "c"}))
	if r.Name != "Kept" || r.Type != TypeGRPC || r.Request != nil || r.GRPC.URL != "c" {
		t.Errorf("after a gRPC request: %+v", r)
	}
	r.SetContent(NewWebSocket("Other", ws.Request{URL: "d"}))
	if r.Name != "Kept" || r.Type != TypeWebSocket || r.GRPC != nil || r.WebSocket.URL != "d" {
		t.Errorf("after a WebSocket request: %+v", r)
	}
}

func TestGraphQLRequest(t *testing.T) {
	path := filepath.Join(t.TempDir(), "requests.json")
	s := &Store{}
	f := s.AddFolder("Graphs")
	f.Add(NewGraphQL("Me", graphql.Request{URL: "{{host}}/graphql", Query: "{ me { id } }", Variables: `{"a": 1}`, Captures: []capture.Rule{{Variable: "id", Path: "data.me.id"}}}))
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), `"type": "graphql"`) || !strings.Contains(string(data), `"graphql": {`) {
		t.Errorf("written as %s", data)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	r := loaded.Folders[0].Requests[0]
	if r.Type != TypeGraphQL || r.GraphQL == nil || r.Request != nil || r.GRPC != nil || r.WebSocket != nil || r.GraphQL.Query != "{ me { id } }" || r.GraphQL.Variables != `{"a": 1}` || len(r.GraphQL.Captures) != 1 {
		t.Errorf("loaded %+v %+v", r, r.GraphQL)
	}
	if c := r.Clone(); c.GraphQL == r.GraphQL || c.GraphQL.Query != r.GraphQL.Query {
		t.Errorf("clone %+v", c.GraphQL)
	}
	if n := New(TypeGraphQL, "n"); n.Type != TypeGraphQL || n.GraphQL == nil {
		t.Errorf("new %+v", n)
	}
	other := New(TypeREST, "other")
	other.SetContent(r)
	if other.Name != "other" || other.Type != TypeGraphQL || other.GraphQL != r.GraphQL || other.Request != nil {
		t.Errorf("content %+v", other)
	}

	// One with nothing of its own is given an empty request.
	if err := os.WriteFile(path, []byte(`{"folders":[{"name":"a","requests":[{"name":"g","type":"graphql"}]}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if loaded, err = Load(path); err != nil || loaded.Folders[0].Requests[0].GraphQL == nil {
		t.Errorf("empty graphql request: %+v, %v", loaded.Folders[0].Requests[0], err)
	}
}

func TestMCPRequest(t *testing.T) {
	path := filepath.Join(t.TempDir(), "requests.json")
	s := &Store{}
	s.AddFolder("Tools").Add(NewMCP("Echo", mcp.Request{URL: "{{host}}/mcp", Kind: mcp.KindTool, Name: "echo", Arguments: `{"text": "a"}`, Captures: []capture.Rule{{Variable: "said", Path: "structuredContent.said"}}}))
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	// No transport is written for Streamable HTTP, which is all there is.
	if !strings.Contains(string(data), `"type": "mcp"`) || !strings.Contains(string(data), `"mcp": {`) || strings.Contains(string(data), "transport") {
		t.Errorf("written as %s", data)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	r := loaded.Folders[0].Requests[0]
	if r.Type != TypeMCP || r.MCP == nil || r.Request != nil || r.GraphQL != nil || r.MCP.Name != "echo" || r.MCP.Kind != mcp.KindTool || len(r.MCP.Captures) != 1 {
		t.Errorf("loaded %+v %+v", r, r.MCP)
	}
	if c := r.Clone(); c.MCP == r.MCP || c.MCP.Arguments != r.MCP.Arguments {
		t.Errorf("clone %+v", c.MCP)
	}
	if n := New(TypeMCP, "n"); n.Type != TypeMCP || n.MCP == nil || n.Request != nil {
		t.Errorf("new %+v", n)
	}
	if n := New("unknown", "n"); n.Type != TypeREST || n.Request == nil || n.Method != http.MethodGet {
		t.Errorf("new of an unknown type %+v", n)
	}
	other := New(TypeREST, "other")
	other.SetContent(r)
	if other.Name != "other" || other.Type != TypeMCP || other.MCP != r.MCP || other.Request != nil {
		t.Errorf("content %+v", other)
	}

	// One with nothing of its own is given an empty request.
	if err := os.WriteFile(path, []byte(`{"folders":[{"name":"a","requests":[{"name":"4 mcp","type":"mcp"}]}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if loaded, err = Load(path); err != nil || loaded.Folders[0].Requests[0].MCP == nil {
		t.Errorf("empty mcp request: %+v, %v", loaded.Folders[0].Requests[0], err)
	}
}

func TestFindAndRemoveSets(t *testing.T) {
	s := &Store{}
	r := s.AddFolder("A").Add(NewREST("get", rest.Request{Method: http.MethodGet}))
	first := s.AddSet("Work", []*Request{r})
	second := s.AddSet("Work", nil)
	other := s.AddSet("Play", nil)

	// The first of a name is found.
	if s.FindSet("Work") != first || s.FindSet("Play") != other || s.FindSet("work") != nil || s.FindSet("None") != nil {
		t.Error("the wrong sets were found")
	}

	// Removing one leaves the others, and the requests it held.
	s.RemoveSet(first)
	if !slices.Equal(s.Sets, []*Set{second, other}) || s.FindSet("Work") != second || len(s.Folders[0].Requests) != 1 {
		t.Errorf("after removing: %+v", s.Sets)
	}
	// Removing one that is not there changes nothing.
	s.RemoveSet(first)
	if len(s.Sets) != 2 {
		t.Errorf("after removing again: %+v", s.Sets)
	}
}

// A file from before there was OAuth 2.0 loads and saves as it did, and the
// settings of OAuth are kept by every type of request that has an
// authorization.
func TestOAuthSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "requests.json")
	old := `{"folders": [{"name": "a", "requests": [
		{"name": "bearer", "method": "GET", "url": "example.com", "auth": {"type": "bearer", "token": "t"}}
	]}]}`
	os.WriteFile(path, []byte(old), 0o644)
	s, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if r := s.Folders[0].Requests[0]; r.Auth != (rest.Auth{Type: rest.AuthBearer, Token: "t"}) {
		t.Errorf("bearer = %+v", r.Auth)
	}
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(path); strings.Contains(string(data), "oauth") {
		t.Errorf("saved %s", data)
	}

	o := &rest.OAuth{Grant: rest.GrantAuthorizationCode, TokenURL: "{{idp}}/token", AuthURL: "{{idp}}/auth", ClientID: "app", Scopes: "read write", Params: "audience=api", ClientAuth: rest.ClientAuthBody}
	auth := rest.Auth{Type: rest.AuthOAuth2, OAuth: o, Access: "never saved"}
	want := &Store{}
	f := want.AddFolder("b")
	f.Add(NewREST("rest", rest.Request{Method: "GET", URL: "example.com", Auth: auth}))
	f.Add(NewGRPC("grpc", rpc.Request{URL: "localhost:1", Auth: auth}))
	f.Add(NewGraphQL("graphql", graphql.Request{URL: "example.com", Auth: auth}))
	f.Add(NewMCP("mcp", mcp.Request{URL: "example.com", Auth: auth}))
	f.Add(NewWebSocket("ws", ws.Request{URL: "example.com", Auth: auth}))
	if err := want.Save(path); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(path); strings.Contains(string(data), "never saved") {
		t.Errorf("the access token was saved: %s", data)
	}
	saved := auth
	saved.Access = ""
	for _, r := range got.Folders[0].Requests {
		var a rest.Auth
		switch r.Type {
		case TypeREST:
			a = r.Auth
		case TypeGRPC:
			a = r.GRPC.Auth
		case TypeGraphQL:
			a = r.GraphQL.Auth
		case TypeMCP:
			a = r.MCP.Auth
		case TypeWebSocket:
			a = r.WebSocket.Auth
		}
		if !reflect.DeepEqual(a, saved) {
			t.Errorf("%s: %+v %+v", r.Name, a, a.OAuth)
		}
	}
}

// TestAssertions checks that a file from before requests had assertions
// reads and writes as it did, and that the assertions of every type that has
// them are kept.
func TestAssertions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "requests.json")
	old := `{"folders":[{"name":"a","requests":[` +
		`{"name":"1 rest","method":"GET","url":"{{host}}/a","auth":{"type":"none"},"captures":[{"key":"id","value":"id","enabled":true}]},` +
		`{"name":"2 grpc","type":"grpc","grpc":{"url":"localhost:1","method":"a.B/C","auth":{"type":"none"}}},` +
		`{"name":"3 graphql","type":"graphql","graphql":{"url":"{{host}}/graphql","auth":{"type":"none"},"query":"{ a }"}},` +
		`{"name":"4 mcp","type":"mcp","mcp":{"url":"{{host}}/mcp","auth":{"type":"none"},"name":"echo"}}]}]}`
	if err := os.WriteFile(path, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(path); strings.Contains(string(data), "assertions") {
		t.Errorf("a file without assertions was written with them: %s", data)
	}
	requests := s.Folders[0].Requests
	if len(requests) != 4 || len(requests[0].Request.Captures) != 1 {
		t.Fatalf("loaded %+v", requests)
	}

	check := []assert.Check{{Subject: assert.Body, Path: "id", Comparison: assert.Equals, Value: "{{id}}", Enabled: true}}
	requests[0].Request.Assertions = check
	requests[1].GRPC.Assertions = check
	requests[2].GraphQL.Assertions = check
	requests[3].MCP.Assertions = check
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	got := loaded.Folders[0].Requests
	for i, assertions := range [][]assert.Check{got[0].Request.Assertions, got[1].GRPC.Assertions, got[2].GraphQL.Assertions, got[3].MCP.Assertions} {
		if !reflect.DeepEqual(assertions, check) {
			t.Errorf("request %d has assertions %+v", i, assertions)
		}
	}
	if c := got[0].Clone(); !reflect.DeepEqual(c.Request.Assertions, check) {
		t.Errorf("clone has assertions %+v", c.Request.Assertions)
	}
}

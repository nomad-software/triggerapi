// Package collection stores saved requests organised into folders. It has no
// dependency on the UI.
package collection

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"

	"github.com/nomad-software/triggerapi/internal/crypt"
	"github.com/nomad-software/triggerapi/internal/graphql"
	"github.com/nomad-software/triggerapi/internal/jsonfile"
	"github.com/nomad-software/triggerapi/internal/rest"
	"github.com/nomad-software/triggerapi/internal/rpc"
	"github.com/nomad-software/triggerapi/internal/ws"
)

// Type is the kind of a saved request.
type Type string

const (
	TypeREST      Type = "rest"
	TypeGRPC      Type = "grpc"
	TypeWebSocket Type = "websocket"
	TypeGraphQL   Type = "graphql"
)

// Request is a saved request. Only the field for its type is set: Request for
// TypeREST, whose fields are stored alongside the name, GRPC for TypeGRPC,
// WebSocket for TypeWebSocket and GraphQL for TypeGraphQL.
type Request struct {
	Name string `json:"name"`
	Type Type   `json:"type"`
	*rest.Request
	GRPC      *rpc.Request     `json:"grpc,omitempty"`
	WebSocket *ws.Request      `json:"websocket,omitempty"`
	GraphQL   *graphql.Request `json:"graphql,omitempty"`
}

// NewREST returns a REST request.
func NewREST(name string, request rest.Request) *Request {
	return &Request{Name: name, Type: TypeREST, Request: &request}
}

// NewGRPC returns a gRPC request.
func NewGRPC(name string, request rpc.Request) *Request {
	return &Request{Name: name, Type: TypeGRPC, GRPC: &request}
}

// NewWebSocket returns a WebSocket request.
func NewWebSocket(name string, request ws.Request) *Request {
	return &Request{Name: name, Type: TypeWebSocket, WebSocket: &request}
}

// NewGraphQL returns a GraphQL request.
func NewGraphQL(name string, request graphql.Request) *Request {
	return &Request{Name: name, Type: TypeGraphQL, GraphQL: &request}
}

// New returns an empty request of a type, with a name. A REST request is a
// GET, and a type that is not known is taken for REST, as it is on loading.
func New(typ Type, name string) *Request {
	switch typ {
	case TypeGRPC:
		return NewGRPC(name, rpc.Request{})
	case TypeWebSocket:
		return NewWebSocket(name, ws.Request{})
	case TypeGraphQL:
		return NewGraphQL(name, graphql.Request{})
	}
	return NewREST(name, rest.Request{Method: http.MethodGet})
}

// SetContent makes the request the same as another, but for its name: of the
// type of the other, with what the other holds. The two then share it.
func (r *Request) SetContent(from *Request) {
	r.Type, r.Request, r.GRPC, r.WebSocket, r.GraphQL = from.Type, from.Request, from.GRPC, from.WebSocket, from.GraphQL
}

// normalise makes the request hold exactly the field for its type. Requests
// saved before there were types are REST requests.
func (r *Request) normalise() {
	switch r.Type {
	case TypeGRPC:
		r.Request, r.WebSocket, r.GraphQL = nil, nil, nil
		if r.GRPC == nil {
			r.GRPC = &rpc.Request{}
		}
		return
	case TypeWebSocket:
		r.Request, r.GRPC, r.GraphQL = nil, nil, nil
		if r.WebSocket == nil {
			r.WebSocket = &ws.Request{}
		}
		return
	case TypeGraphQL:
		r.Request, r.GRPC, r.WebSocket = nil, nil, nil
		if r.GraphQL == nil {
			r.GraphQL = &graphql.Request{}
		}
		return
	}

	r.Type = TypeREST
	r.GRPC, r.WebSocket, r.GraphQL = nil, nil, nil
	if r.Request == nil {
		r.Request = &rest.Request{}
	}
}

// Clone returns a copy of the request which shares nothing with it.
func (r *Request) Clone() *Request {
	clone := &Request{}
	deepCopy(r, clone)
	clone.normalise()
	return clone
}

// deepCopy copies from into to by way of JSON, which every request and
// folder can always be written as and read back from.
func deepCopy(from, to any) {
	if data, err := json.Marshal(from); err == nil {
		_ = json.Unmarshal(data, to)
	}
}

// Folder is a named group of requests. It can hold other folders, to any
// depth. Its folders come before its requests wherever its contents are
// listed.
type Folder struct {
	Name string `json:"name"`
	// Open records that the folder is shown open, for as long as the
	// program runs. It is never saved, so every folder starts closed (and
	// a "collapsed" mark in a file from before is ignored).
	Open     bool       `json:"-"`
	Folders  []*Folder  `json:"folders,omitempty"`
	Requests []*Request `json:"requests,omitempty"`
}

// Clone returns a copy of the folder, and of everything in it, which shares
// nothing with it.
func (f *Folder) Clone() *Folder {
	clone := &Folder{}
	deepCopy(f, clone)
	clone.normalise()
	return clone
}

// Store is the collection of folders that is saved to disk. Folders and
// requests are held by pointer so they keep their identity when others are
// added or removed.
type Store struct {
	Folders []*Folder
	// Sets are sets of requests to be kept open in tabs together. They
	// are not in folders.
	Sets []*Set

	// paths are the requests of each set as they were read, until they
	// are found in the folders by LoadWith.
	paths [][][]string
}

// Set is a named set of requests, from any folders, which are opened in
// tabs together. It holds the requests themselves, so renaming or moving
// one leaves it in the set; in the file each is written as its path.
type Set struct {
	Name     string
	Requests []*Request
}

// storeFile is how a store is written. Sets are left out of the file when
// there are none, so a file from before there were sets reads and writes
// as it did.
type storeFile struct {
	Folders []*Folder `json:"folders"`
	Sets    []setFile `json:"sets,omitempty"`
}

// setFile is how a set is written: each request as the path Store.Path
// gives it.
type setFile struct {
	Name     string     `json:"name"`
	Requests [][]string `json:"requests,omitempty"`
}

// MarshalJSON writes the store, with each request of a set as its path.
// A request of a set that is not in the store is left out.
func (s *Store) MarshalJSON() ([]byte, error) {
	file := storeFile{Folders: s.Folders}
	for _, set := range s.Sets {
		written := setFile{Name: set.Name}
		for _, r := range set.Requests {
			if path := s.Path(r); path != nil {
				written.Requests = append(written.Requests, path)
			}
		}
		file.Sets = append(file.Sets, written)
	}
	return json.Marshal(file)
}

// UnmarshalJSON reads the store. The requests of the sets are found by
// LoadWith once the folders have been normalised.
func (s *Store) UnmarshalJSON(data []byte) error {
	var file storeFile
	if err := json.Unmarshal(data, &file); err != nil {
		return err
	}
	s.Folders, s.Sets, s.paths = file.Folders, nil, nil
	for _, set := range file.Sets {
		s.Sets = append(s.Sets, &Set{Name: set.Name})
		s.paths = append(s.paths, set.Requests)
	}
	return nil
}

// AddSet appends a set holding the requests given.
func (s *Store) AddSet(name string, requests []*Request) *Set {
	set := &Set{Name: name, Requests: slices.Clone(requests)}
	s.Sets = append(s.Sets, set)
	return set
}

// RemoveSet deletes a set. The requests it held are left as they are.
func (s *Store) RemoveSet(set *Set) {
	s.Sets = slices.DeleteFunc(s.Sets, func(t *Set) bool { return t == set })
}

// SortSets puts the sets in alphabetical order, ignoring case.
func (s *Store) SortSets() {
	slices.SortStableFunc(s.Sets, func(a, b *Set) int { return compareNames(a.Name, b.Name) })
}

// FindSet returns the first set with a name, or nil if there is none.
func (s *Store) FindSet(name string) *Set {
	for _, set := range s.Sets {
		if set.Name == name {
			return set
		}
	}
	return nil
}

// PruneSets takes out of every set the requests that are no longer in the
// store, as after they have been removed.
func (s *Store) PruneSets() {
	held := map[*Request]bool{}
	for _, f := range s.Folders {
		for _, r := range f.All() {
			held[r] = true
		}
	}
	for _, set := range s.Sets {
		set.Requests = slices.DeleteFunc(set.Requests, func(r *Request) bool { return !held[r] })
	}
}

// AddFolder appends a new folder at the top level.
func (s *Store) AddFolder(name string) *Folder {
	f := &Folder{Name: name}
	s.Folders = append(s.Folders, f)
	return f
}

// RemoveFolder deletes the top level folder at index along with everything
// in it.
func (s *Store) RemoveFolder(index int) {
	s.Folders = slices.Delete(s.Folders, index, index+1)
}

// FoldersIn returns the folders inside parent, or those at the top level if
// parent is nil.
func (s *Store) FoldersIn(parent *Folder) []*Folder {
	return *s.foldersOf(parent)
}

// foldersOf returns the list that holds the folders inside parent, or those
// at the top level if parent is nil.
func (s *Store) foldersOf(parent *Folder) *[]*Folder {
	if parent == nil {
		return &s.Folders
	}
	return &parent.Folders
}

// Insert appends a folder, with everything in it, inside parent, or at the
// top level if parent is nil.
func (s *Store) Insert(parent, f *Folder) {
	folders := s.foldersOf(parent)
	*folders = append(*folders, f)
}

// Delete removes a folder, with everything in it, from wherever it is in the
// store. It reports false if the folder is not in the store.
func (s *Store) Delete(f *Folder) bool {
	parent, ok := s.ParentOf(f)
	if !ok {
		return false
	}
	folders := s.foldersOf(parent)
	*folders = slices.DeleteFunc(*folders, func(other *Folder) bool { return other == f })
	return true
}

// AddFolder appends a new folder inside the folder.
func (f *Folder) AddFolder(name string) *Folder {
	sub := &Folder{Name: name}
	f.Folders = append(f.Folders, sub)
	return sub
}

// RemoveFolder deletes the folder at index inside the folder, along with
// everything in it.
func (f *Folder) RemoveFolder(index int) {
	f.Folders = slices.Delete(f.Folders, index, index+1)
}

// Add appends a request to the folder.
func (f *Folder) Add(r *Request) *Request {
	f.Requests = append(f.Requests, r)
	return r
}

// Remove deletes the request at index.
func (f *Folder) Remove(index int) {
	f.Requests = slices.Delete(f.Requests, index, index+1)
}

// All returns the requests in the folder and in every folder inside it.
func (f *Folder) All() []*Request {
	requests := slices.Clone(f.Requests)
	for _, sub := range f.Folders {
		requests = append(requests, sub.All()...)
	}
	return requests
}

// Descendants returns the number of folders inside the folder, at any depth.
func (f *Folder) Descendants() int {
	n := len(f.Folders)
	for _, sub := range f.Folders {
		n += sub.Descendants()
	}
	return n
}

// compareNames orders names alphabetically, ignoring case.
func compareNames(a, b string) int {
	return strings.Compare(strings.ToLower(a), strings.ToLower(b))
}

// Sort puts the folders of the store, and the folders and requests inside
// each of them, in alphabetical order, ignoring case.
func (s *Store) Sort() {
	slices.SortStableFunc(s.Folders, func(a, b *Folder) int { return compareNames(a.Name, b.Name) })
	for _, f := range s.Folders {
		f.sort()
	}
}

func (f *Folder) sort() {
	slices.SortStableFunc(f.Folders, func(a, b *Folder) int { return compareNames(a.Name, b.Name) })
	slices.SortStableFunc(f.Requests, func(a, b *Request) int { return compareNames(a.Name, b.Name) })
	for _, sub := range f.Folders {
		sub.sort()
	}
}

// contains reports if other is the folder itself or is inside it.
func (f *Folder) contains(other *Folder) bool {
	if f == other {
		return true
	}
	return slices.ContainsFunc(f.Folders, func(sub *Folder) bool { return sub.contains(other) })
}

// walk visits every folder of the store, depth first with a folder before
// those inside it, giving each with the folder that holds it (nil at the top
// level) and the names of the folders it is in, outermost first, followed by
// its own. It stops at the first folder for which visit returns true.
func (s *Store) walk(visit func(f, parent *Folder, path []string) bool) {
	var walk func(f, parent *Folder, path []string) bool
	walk = func(f, parent *Folder, path []string) bool {
		path = append(slices.Clip(path), f.Name)
		if visit(f, parent, path) {
			return true
		}
		for _, sub := range f.Folders {
			if walk(sub, f, path) {
				return true
			}
		}
		return false
	}
	for _, top := range s.Folders {
		if walk(top, nil, nil) {
			return
		}
	}
}

// ParentOf returns the folder that holds a folder, or nil if it is at the top
// level. It reports false if the folder is not in the store.
func (s *Store) ParentOf(f *Folder) (*Folder, bool) {
	var parent *Folder
	found := false
	s.walk(func(in, holder *Folder, _ []string) bool {
		parent, found = holder, in == f
		return found
	})
	if !found {
		return nil, false
	}
	return parent, true
}

// FolderOf returns the folder that holds a request, or nil if the request is
// not in the store.
func (s *Store) FolderOf(r *Request) *Folder {
	var holder *Folder
	s.walk(func(in, _ *Folder, _ []string) bool {
		if slices.Contains(in.Requests, r) {
			holder = in
		}
		return holder != nil
	})
	return holder
}

// Path returns the names of the folders a request is in, outermost first,
// followed by its own name. It is nil if the request is not in the store.
func (s *Store) Path(r *Request) []string {
	var found []string
	s.walk(func(in, _ *Folder, path []string) bool {
		if slices.Contains(in.Requests, r) {
			found = append(slices.Clone(path), r.Name)
		}
		return found != nil
	})
	return found
}

// Find returns the request at a path, as Path gives it, or nil if there is
// none. Where names are shared it is the first that is found.
func (s *Store) Find(path []string) *Request {
	var find func(folders []*Folder, path []string) *Request
	find = func(folders []*Folder, path []string) *Request {
		if len(path) < 2 {
			return nil
		}
		for _, folder := range folders {
			if folder.Name != path[0] {
				continue
			}
			if len(path) == 2 {
				for _, r := range folder.Requests {
					if r.Name == path[1] {
						return r
					}
				}
			} else if found := find(folder.Folders, path[1:]); found != nil {
				return found
			}
		}
		return nil
	}
	return find(s.Folders, path)
}

// move takes item out of from and puts it in to, before the item that was at
// index there, or at the end if the index is past it. The two can be the same
// list, which is how an item is reordered.
func move[T comparable](from, to *[]T, item T, index int) {
	old := slices.Index(*from, item)
	*from = slices.Delete(*from, old, old+1)

	// Taking the item out of the list it is going back into moves what came
	// after it down by one.
	if from == to && old < index {
		index--
	}
	index = min(max(index, 0), len(*to))
	*to = slices.Insert(*to, index, item)
}

// MoveRequest moves a request into a folder, before the request at index
// there. It reports false, having done nothing, if the request is not in the
// store.
func (s *Store) MoveRequest(r *Request, to *Folder, index int) bool {
	from := s.FolderOf(r)
	if from == nil || to == nil {
		return false
	}
	move(&from.Requests, &to.Requests, r, index)
	return true
}

// MoveFolder moves a folder into another, or to the top level if that is
// nil, before the folder at index there. It reports false,
// having done nothing, if the folder is not in the store or would be moved
// inside itself.
func (s *Store) MoveFolder(f *Folder, to *Folder, index int) bool {
	parent, ok := s.ParentOf(f)
	if !ok || (to != nil && f.contains(to)) {
		return false
	}

	from, into := &s.Folders, &s.Folders
	if parent != nil {
		from = &parent.Folders
	}
	if to != nil {
		into = &to.Folders
	}
	move(from, into, f, index)
	return true
}

// normalise drops nulls from the folder and everything inside it, so the
// rest of the program need not check for them, and normalises its requests.
func (f *Folder) normalise() {
	f.Folders = slices.DeleteFunc(f.Folders, func(sub *Folder) bool { return sub == nil })
	for _, sub := range f.Folders {
		sub.normalise()
	}

	f.Requests = slices.DeleteFunc(f.Requests, func(r *Request) bool { return r == nil })
	for _, r := range f.Requests {
		r.normalise()
	}
}

// LoadWith reads a store from the JSON file at path, decrypting the file
// with keys if it is encrypted. A missing file is not an error and yields an
// empty store.
func LoadWith(path string, keys *crypt.Keys) (*Store, error) {
	store := &Store{}
	if err := jsonfile.LoadWith(path, store, keys); err != nil {
		return &Store{}, err
	}

	store.Folders = slices.DeleteFunc(store.Folders, func(f *Folder) bool { return f == nil })
	for _, f := range store.Folders {
		f.normalise()
	}
	// A request of a set that cannot be found, or is in it twice, is left
	// out.
	for i, set := range store.Sets {
		for _, path := range store.paths[i] {
			if r := store.Find(path); r != nil && !slices.Contains(set.Requests, r) {
				set.Requests = append(set.Requests, r)
			}
		}
	}
	store.paths = nil
	store.Sort()
	store.SortSets()

	return store, nil
}

// Save writes the store to the JSON file at path.
func (s *Store) Save(path string) error {
	return s.SaveWith(path, nil)
}

// SaveWith writes the store to the JSON file at path, encrypted with keys
// unless they are nil.
func (s *Store) SaveWith(path string, keys *crypt.Keys) error {
	return jsonfile.SaveWith(path, s, keys)
}

// Package collection stores saved requests organised into folders. It has no
// dependency on the UI.
package collection

import (
	"encoding/json"
	"slices"
	"strings"

	"github.com/nomad-software/triggerapi/internal/crypt"
	"github.com/nomad-software/triggerapi/internal/jsonfile"
	"github.com/nomad-software/triggerapi/internal/rest"
	"github.com/nomad-software/triggerapi/internal/rpc"
)

// Type is the kind of a saved request.
type Type string

const (
	TypeREST Type = "rest"
	TypeGRPC Type = "grpc"
)

// Request is a saved request. Only the field for its type is set: Request for
// TypeREST, whose fields are stored alongside the name, and GRPC for
// TypeGRPC.
type Request struct {
	Name string `json:"name"`
	Type Type   `json:"type"`
	*rest.Request
	GRPC *rpc.Request `json:"grpc,omitempty"`
}

// NewREST returns a REST request.
func NewREST(name string, request rest.Request) *Request {
	return &Request{Name: name, Type: TypeREST, Request: &request}
}

// NewGRPC returns a gRPC request.
func NewGRPC(name string, request rpc.Request) *Request {
	return &Request{Name: name, Type: TypeGRPC, GRPC: &request}
}

// normalise makes the request hold exactly the field for its type. Requests
// saved before there were types are REST requests.
func (r *Request) normalise() {
	if r.Type == TypeGRPC {
		r.Request = nil
		if r.GRPC == nil {
			r.GRPC = &rpc.Request{}
		}
		return
	}

	r.Type = TypeREST
	r.GRPC = nil
	if r.Request == nil {
		r.Request = &rest.Request{}
	}
}

// Clone returns a copy of the request which shares nothing with it.
func (r *Request) Clone() *Request {
	clone := &Request{}
	// A request can always be written as JSON and read back.
	if data, err := json.Marshal(r); err == nil {
		_ = json.Unmarshal(data, clone)
	}
	clone.normalise()
	return clone
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
	if data, err := json.Marshal(f); err == nil {
		_ = json.Unmarshal(data, clone)
	}
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

// ParentOf returns the folder that holds a folder, or nil if it is at the top
// level. It reports false if the folder is not in the store.
func (s *Store) ParentOf(f *Folder) (*Folder, bool) {
	if slices.Contains(s.Folders, f) {
		return nil, true
	}

	var find func(in *Folder) *Folder
	find = func(in *Folder) *Folder {
		if slices.Contains(in.Folders, f) {
			return in
		}
		for _, sub := range in.Folders {
			if parent := find(sub); parent != nil {
				return parent
			}
		}
		return nil
	}

	for _, top := range s.Folders {
		if parent := find(top); parent != nil {
			return parent, true
		}
	}
	return nil, false
}

// FolderOf returns the folder that holds a request, or nil if the request is
// not in the store.
func (s *Store) FolderOf(r *Request) *Folder {
	var find func(in *Folder) *Folder
	find = func(in *Folder) *Folder {
		if slices.Contains(in.Requests, r) {
			return in
		}
		for _, sub := range in.Folders {
			if holder := find(sub); holder != nil {
				return holder
			}
		}
		return nil
	}

	for _, top := range s.Folders {
		if holder := find(top); holder != nil {
			return holder
		}
	}
	return nil
}

// Path returns the names of the folders a request is in, outermost first,
// followed by its own name. It is nil if the request is not in the store.
func (s *Store) Path(r *Request) []string {
	var find func(in *Folder, path []string) []string
	find = func(in *Folder, path []string) []string {
		path = append(path, in.Name)
		if slices.Contains(in.Requests, r) {
			return append(path, r.Name)
		}
		for _, sub := range in.Folders {
			if found := find(sub, slices.Clone(path)); found != nil {
				return found
			}
		}
		return nil
	}

	for _, top := range s.Folders {
		if found := find(top, nil); found != nil {
			return found
		}
	}
	return nil
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

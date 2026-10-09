// Package collection stores saved requests organised into folders. It has no
// dependency on the UI.
package collection

import (
	"slices"

	"github.com/nomad-software/postie/internal/jsonfile"
	"github.com/nomad-software/postie/internal/rest"
)

// Request is a saved request.
type Request struct {
	Name string `json:"name"`
	rest.Request
}

// Folder is a named group of requests.
type Folder struct {
	Name     string     `json:"name"`
	Requests []*Request `json:"requests"`
}

// Store is the collection of folders that is saved to disk. Folders and
// requests are held by pointer so they keep their identity when others are
// added or removed.
type Store struct {
	Folders []*Folder `json:"folders"`
}

// AddFolder appends a new folder.
func (s *Store) AddFolder(name string) *Folder {
	f := &Folder{Name: name}
	s.Folders = append(s.Folders, f)
	return f
}

// RemoveFolder deletes the folder at index along with its requests.
func (s *Store) RemoveFolder(index int) {
	s.Folders = slices.Delete(s.Folders, index, index+1)
}

// Add appends a new request to the folder.
func (f *Folder) Add(name string, request rest.Request) *Request {
	r := &Request{Name: name, Request: request}
	f.Requests = append(f.Requests, r)
	return r
}

// Remove deletes the request at index.
func (f *Folder) Remove(index int) {
	f.Requests = slices.Delete(f.Requests, index, index+1)
}

// Load reads a store from the JSON file at path. A missing file is not an
// error and yields an empty store.
func Load(path string) (*Store, error) {
	store := &Store{}
	if err := jsonfile.Load(path, store); err != nil {
		return &Store{}, err
	}

	// Drop nulls so the rest of the program need not check for them.
	store.Folders = slices.DeleteFunc(store.Folders, func(f *Folder) bool { return f == nil })
	for _, f := range store.Folders {
		f.Requests = slices.DeleteFunc(f.Requests, func(r *Request) bool { return r == nil })
	}

	return store, nil
}

// Save writes the store to the JSON file at path.
func (s *Store) Save(path string) error {
	return jsonfile.Save(path, s)
}

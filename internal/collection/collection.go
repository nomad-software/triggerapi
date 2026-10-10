// Package collection stores saved requests organised into folders. It has no
// dependency on the UI.
package collection

import (
	"slices"

	"github.com/nomad-software/postie/internal/jsonfile"
	"github.com/nomad-software/postie/internal/rest"
	"github.com/nomad-software/postie/internal/rpc"
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

// Add appends a request to the folder.
func (f *Folder) Add(r *Request) *Request {
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
		for _, r := range f.Requests {
			r.normalise()
		}
	}

	return store, nil
}

// Save writes the store to the JSON file at path.
func (s *Store) Save(path string) error {
	return jsonfile.Save(path, s)
}

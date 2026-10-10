// Package env stores named sets of variables which can be substituted into
// requests using {{name}} placeholders. It has no dependency on the UI.
package env

import (
	"regexp"
	"slices"
	"strings"

	"github.com/nomad-software/triggerapi/internal/jsonfile"
)

// Variable is a single named value in an environment.
type Variable struct {
	Key     string `json:"key"`
	Value   string `json:"value"`
	Enabled bool   `json:"enabled"`
}

// Environment is a named set of variables.
type Environment struct {
	Name      string     `json:"name"`
	Variables []Variable `json:"variables"`
}

// Store is the collection of environments that is saved to disk.
type Store struct {
	// Active is the index of the environment in use, or -1 for none.
	Active       int           `json:"active"`
	Environments []Environment `json:"environments"`
}

var placeholder = regexp.MustCompile(`\{\{\s*([^{}]*?)\s*\}\}`)

// Expand replaces each {{key}} placeholder in s with the value of the enabled
// variable of that name. If a key is defined more than once the last one wins.
// Unknown placeholders are left untouched.
func (e Environment) Expand(s string) string {
	if !strings.Contains(s, "{{") {
		return s
	}

	values := map[string]string{}
	for _, v := range e.Variables {
		if v.Enabled && v.Key != "" {
			values[v.Key] = v.Value
		}
	}

	return placeholder.ReplaceAllStringFunc(s, func(match string) string {
		key := placeholder.FindStringSubmatch(match)[1]
		if value, ok := values[key]; ok {
			return value
		}
		return match
	})
}

// Placeholders returns the {{key}} placeholders found in s.
func Placeholders(s string) []string {
	return placeholder.FindAllString(s, -1)
}

// FindPlaceholders returns the start and end byte offsets of each {{key}}
// placeholder in s.
func FindPlaceholders(s string) [][]int {
	return placeholder.FindAllStringIndex(s, -1)
}

// Current returns the active environment, if there is one.
func (s *Store) Current() (*Environment, bool) {
	if s.Active < 0 || s.Active >= len(s.Environments) {
		return nil, false
	}
	return &s.Environments[s.Active], true
}

// Expand substitutes the variables of the active environment into text.
func (s *Store) Expand(text string) string {
	if e, ok := s.Current(); ok {
		return e.Expand(text)
	}
	return text
}

// Sort puts the environments in alphabetical order, ignoring case. The
// active environment stays the active one.
func (s *Store) Sort() {
	order := make([]int, len(s.Environments))
	for i := range order {
		order[i] = i
	}
	slices.SortStableFunc(order, func(a, b int) int {
		return compareNames(s.Environments[a].Name, s.Environments[b].Name)
	})

	sorted := make([]Environment, len(order))
	active := -1
	for to, from := range order {
		sorted[to] = s.Environments[from]
		if from == s.Active {
			active = to
		}
	}
	s.Environments = sorted
	s.Active = active
}

// compareNames orders names alphabetically, ignoring case.
func compareNames(a, b string) int {
	return strings.Compare(strings.ToLower(a), strings.ToLower(b))
}

// Load reads a store from the JSON file at path. A missing file is not an
// error and yields an empty store.
func Load(path string) (*Store, error) {
	store := &Store{Active: -1}
	if err := jsonfile.Load(path, store); err != nil {
		return &Store{Active: -1}, err
	}
	if _, ok := store.Current(); !ok {
		store.Active = -1
	}
	store.Sort()
	return store, nil
}

// Save writes the store to the JSON file at path.
func (s *Store) Save(path string) error {
	return jsonfile.Save(path, s)
}

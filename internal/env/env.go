// Package env stores named sets of variables which can be substituted into
// requests using {{name}} placeholders. It has no dependency on the UI.
package env

import (
	"regexp"
	"slices"
	"strings"

	"github.com/nomad-software/triggerapi/internal/crypt"
	"github.com/nomad-software/triggerapi/internal/jsonfile"
)

// Variable is a single named value in an environment.
type Variable struct {
	Key     string `json:"key"`
	Value   string `json:"value"`
	Enabled bool   `json:"enabled"`
	// Description says what the variable is for. It is only for the
	// reader: several variables can have one name, to keep the values it
	// is switched between, and the description tells them apart.
	Description string `json:"description,omitempty"`
}

// Environment is a named set of variables.
type Environment struct {
	Name      string     `json:"name"`
	Variables []Variable `json:"variables"`
}

// Clone returns a copy of the environment which shares nothing with it.
func (e Environment) Clone() Environment {
	return Environment{Name: e.Name, Variables: slices.Clone(e.Variables)}
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
// Unknown placeholders are left untouched. A value that is a call of a
// built-in function is worked out each time (see Evaluate); use Expander to
// have it the same in several texts.
func (e Environment) Expand(s string) string {
	if !strings.Contains(s, "{{") {
		return s
	}
	return e.Expander()(s)
}

// Expander returns a function which does as Expand does to each text it is
// given, with the values as they are now. A value that is a call of a
// built-in function is worked out once, so it is the same wherever its
// variable is used: in every part of one request, say. The placeholders in
// the text given to a function that takes text, such as base64, are
// replaced with the other variables; one that would need the value being
// made, directly or through others, is left as it is.
func (e Environment) Expander() func(string) string {
	at := now()
	written := map[string]string{}
	for _, v := range e.Variables {
		if v.Enabled && v.Key != "" {
			written[v.Key] = v.Value
		}
	}

	// Every value is made before the expander is returned, so that it can
	// be used from any goroutine.
	values := map[string]string{}
	making := map[string]bool{}
	var resolve func(key string) (string, bool)
	expand := func(s string) string {
		return placeholder.ReplaceAllStringFunc(s, func(match string) string {
			if value, ok := resolve(placeholder.FindStringSubmatch(match)[1]); ok {
				return value
			}
			return match
		})
	}
	resolve = func(key string) (string, bool) {
		if value, ok := values[key]; ok {
			return value, true
		}
		value, ok := written[key]
		if !ok || making[key] {
			return "", false
		}
		making[key] = true
		if made, ok := Evaluate(value, at, expand); ok {
			value = made
		}
		making[key] = false
		values[key] = value
		return value, true
	}
	for key := range written {
		resolve(key)
	}

	return func(s string) string {
		return placeholder.ReplaceAllStringFunc(s, func(match string) string {
			if value, ok := values[placeholder.FindStringSubmatch(match)[1]]; ok {
				return value
			}
			return match
		})
	}
}

// Set gives the variable with a key a value, and enables it. The variable is
// added if there is none. If several have the key, it is the one in use that
// is set: the last that is enabled, which is the one Expand takes the value
// of, or the first of them if none is.
func (e *Environment) Set(key, value string) {
	found := -1
	for i, v := range e.Variables {
		if v.Key == key && (found < 0 || v.Enabled) {
			found = i
		}
	}
	if found < 0 {
		e.Variables = append(e.Variables, Variable{Key: key, Value: value, Enabled: true})
		return
	}
	e.Variables[found].Value = value
	e.Variables[found].Enabled = true
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

// Expander returns a function which substitutes the variables of the active
// environment into each text it is given. See Environment.Expander.
func (s *Store) Expander() func(string) string {
	if e, ok := s.Current(); ok {
		return e.Expander()
	}
	return func(text string) string { return text }
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

// LoadWith reads a store from the JSON file at path, decrypting the file
// with keys if it is encrypted. A missing file is not an error and yields an
// empty store.
func LoadWith(path string, keys *crypt.Keys) (*Store, error) {
	store := &Store{Active: -1}
	if err := jsonfile.LoadWith(path, store, keys); err != nil {
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
	return s.SaveWith(path, nil)
}

// SaveWith writes the store to the JSON file at path, encrypted with keys
// unless they are nil.
func (s *Store) SaveWith(path string, keys *crypt.Keys) error {
	return jsonfile.SaveWith(path, s, keys)
}

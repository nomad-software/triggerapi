// Package capture takes values out of the JSON of a response, so that they
// can be kept in the variables of an environment. It has no dependency on
// the UI.
package capture

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// step is one step of a path: into the member of an object with a name, or
// into the element of an array at an index.
type step struct {
	name    string
	index   int
	isIndex bool
}

func (s step) String() string {
	if s.isIndex {
		return fmt.Sprintf("[%d]", s.index)
	}
	return strconv.Quote(s.name)
}

// parse reads a path. Names are separated by dots, and an index into an
// array is in square brackets: data.items[0].id. An index below zero counts
// from the end, so [-1] is the last element. A name that has a dot or a
// bracket in it is written in brackets and quotes: data["a.b"]. A $ before
// the path, as JSONPath has, is allowed.
func parse(path string) ([]step, error) {
	path = strings.TrimPrefix(strings.TrimSpace(path), "$")

	var steps []step
	for i := 0; i < len(path); {
		switch path[i] {
		case '.':
			i++
			if i == len(path) || path[i] == '.' || path[i] == '[' {
				return nil, errors.New("there is a name missing after a dot in the path")
			}
		case '[':
			end := strings.IndexByte(path[i:], ']')
			if end < 0 {
				return nil, errors.New("there is a [ with no ] in the path")
			}
			inside := strings.TrimSpace(path[i+1 : i+end])
			i += end + 1

			if len(inside) >= 2 && (inside[0] == '"' || inside[0] == '\'') && inside[len(inside)-1] == inside[0] {
				steps = append(steps, step{name: inside[1 : len(inside)-1]})
				continue
			}
			index, err := strconv.Atoi(inside)
			if err != nil {
				return nil, fmt.Errorf("[%s] in the path is not a number or a name in quotes", inside)
			}
			steps = append(steps, step{index: index, isIndex: true})
		default:
			end := strings.IndexAny(path[i:], ".[")
			if end < 0 {
				end = len(path) - i
			}
			steps = append(steps, step{name: strings.TrimSpace(path[i : i+end])})
			i += end
		}
	}
	return steps, nil
}

// Extract returns the value at a path in a JSON document, as text: a string
// as it is, without its quotes; a number, true or false as written; nothing
// for null; and an object or an array as JSON. An empty path is the whole
// document. See parse for how a path is written.
func Extract(document, path string) (string, error) {
	steps, err := parse(path)
	if err != nil {
		return "", err
	}

	// Numbers are kept as they are written, not turned into floats.
	decoder := json.NewDecoder(strings.NewReader(document))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return "", errors.New("the response is not JSON")
	}
	if _, err := decoder.Token(); err != io.EOF {
		return "", errors.New("the response is not JSON")
	}

	for _, s := range steps {
		switch in := value.(type) {
		case map[string]any:
			if s.isIndex {
				return "", fmt.Errorf("%s is for an array, but there is an object there", s)
			}
			member, ok := in[s.name]
			if !ok {
				return "", fmt.Errorf("there is no %s in the response", s)
			}
			value = member
		case []any:
			if !s.isIndex {
				return "", fmt.Errorf("%s is for an object, but there is an array there", s)
			}
			index := s.index
			if index < 0 {
				index += len(in)
			}
			if index < 0 || index >= len(in) {
				return "", fmt.Errorf("there is no %s in an array of %d", s, len(in))
			}
			value = in[index]
		default:
			return "", fmt.Errorf("there is nothing to look for %s in", s)
		}
	}

	switch v := value.(type) {
	case nil:
		return "", nil
	case string:
		return v, nil
	case json.Number:
		return v.String(), nil
	case bool:
		return strconv.FormatBool(v), nil
	default:
		data, err := json.Marshal(v)
		return string(data), err
	}
}

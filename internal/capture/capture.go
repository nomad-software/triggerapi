// Package capture takes values out of the JSON of a response, so that they
// can be kept in the variables of an environment. It has no dependency on
// the UI.
package capture

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
)

// Rule says what to capture from a response: the variable to keep it in,
// the path of the value in the JSON of the response, and a regular
// expression which picks out the part of the value that is wanted, if not
// all of it is. One that is not enabled is not followed.
type Rule struct {
	Variable string `json:"key"`
	Path     string `json:"value"`
	Regex    string `json:"regex,omitempty"`
	Enabled  bool   `json:"enabled"`
}

// Extract returns the value the rule captures from a JSON document: the
// value at its path, or the part of that which its regular expression
// picks out.
func (r Rule) Extract(document string) (string, error) {
	value, err := Extract(document, r.Path)
	if err != nil {
		return "", err
	}
	return Match(value, r.Regex)
}

// Match returns the part of a value that a regular expression picks out:
// what its first group matched, if it has groups, and otherwise all that it
// matched, at the first place it matches. With no expression that is the
// whole value. The syntax is that of the regexp package.
func Match(value, pattern string) (string, error) {
	if pattern == "" {
		return value, nil
	}

	expression, err := regexp.Compile(pattern)
	if err != nil {
		return "", fmt.Errorf("the regex is not valid: %s", strings.TrimPrefix(err.Error(), "error parsing regexp: "))
	}

	found := expression.FindStringSubmatch(value)
	if found == nil {
		return "", errors.New("the regex does not match the value")
	}
	if len(found) > 1 {
		return found[1], nil
	}
	return found[0], nil
}

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

// write returns steps as the path that parse reads them from, and the last
// name among them, which is "value" if there is none. A name with a ] in it
// cannot be written.
func write(steps []step) (path, name string, ok bool) {
	var b strings.Builder
	name = "value"
	for _, s := range steps {
		// A name is written as it is unless parse would take part of it
		// for something else.
		plain := s.name != "" && !strings.ContainsAny(s.name, ".[") && strings.TrimSpace(s.name) == s.name &&
			(b.Len() > 0 || !strings.HasPrefix(s.name, "$"))
		switch {
		case s.isIndex:
			fmt.Fprintf(&b, "[%d]", s.index)
		case plain:
			if b.Len() > 0 {
				b.WriteByte('.')
			}
			b.WriteString(s.name)
		case strings.Contains(s.name, "]"):
			return "", "", false
		default:
			b.WriteString(`["` + s.name + `"]`)
		}
		if !s.isIndex && s.name != "" {
			name = s.name
		}
	}
	return b.String(), name, true
}

// PathAt returns the path of the value at an offset, in bytes, into a JSON
// document: the innermost value that the offset is in or at the end of. The
// name of a member of an object counts as part of its value. It also
// returns the last name in the path, to call the value by. Nothing is found
// if the offset is outside the document, the document is not JSON as far
// as the value, or the path cannot be written.
func PathAt(document string, offset int) (path, name string, found bool) {
	// frame is an object or array that the walk is inside.
	type frame struct {
		array bool
		// index is that of the next element of an array.
		index int
		// key is the name of the next value of an object, once it has
		// been read (keyed), and keyStart where it starts.
		key      string
		keyed    bool
		keyStart int
		// start is where the object or array starts, with its name.
		start int
		// depth is the length of the path of what it is in.
		depth int
	}

	decoder := json.NewDecoder(strings.NewReader(document))
	decoder.UseNumber()

	// steps is the path of the innermost frame.
	var steps []step
	var frames []frame
	// next moves on from a value of the innermost frame, and reports if
	// there is no frame, as the document has then been read.
	next := func() bool {
		if len(frames) == 0 {
			return false
		}
		if in := &frames[len(frames)-1]; in.array {
			in.index++
		} else {
			in.keyed = false
		}
		return true
	}

	end := 0
	for {
		// The token starts after what separates it from the last.
		start := end
		for start < len(document) && strings.IndexByte(" \t\r\n,:", document[start]) >= 0 {
			start++
		}
		token, err := decoder.Token()
		if err != nil {
			return "", "", false
		}
		end = int(decoder.InputOffset())

		delim, isDelim := token.(json.Delim)
		if isDelim && (delim == '}' || delim == ']') {
			closed := frames[len(frames)-1]
			frames = frames[:len(frames)-1]
			// Nothing inside it was at the offset, or it would have
			// been returned.
			if closed.start <= offset && offset <= end {
				return write(steps)
			}
			steps = steps[:closed.depth]
			if !next() {
				return "", "", false
			}
			continue
		}

		// The name of a member comes before its value.
		if len(frames) > 0 {
			if in := &frames[len(frames)-1]; !in.array && !in.keyed {
				in.key, in.keyed, in.keyStart = token.(string), true, start
				continue
			}
		}

		// Anything else is a value, which has a step from what it is in.
		valueStart, depth := start, len(steps)
		value := steps
		if len(frames) > 0 {
			if in := frames[len(frames)-1]; in.array {
				value = append(value, step{index: in.index, isIndex: true})
			} else {
				value = append(value, step{name: in.key})
				valueStart = in.keyStart
			}
		}

		if isDelim {
			frames = append(frames, frame{array: delim == '[', start: valueStart, depth: depth})
			steps = value
			continue
		}
		if valueStart <= offset && offset <= end {
			return write(value)
		}
		if !next() {
			return "", "", false
		}
	}
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

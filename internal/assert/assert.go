// Package assert checks the responses of requests: that their status, the
// time they took, a header, a value in their body or their size is what it
// should be. It has no dependency on the UI.
package assert

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/nomad-software/triggerapi/internal/capture"
)

// Subject is what of a response a check looks at.
type Subject string

const (
	// Status is the status of the response (see Facts.Status).
	Status Subject = "status"
	// Time is how long the response took, in milliseconds.
	Time Subject = "time"
	// Header is the header the path of the check names.
	Header Subject = "header"
	// Body is the value at the path of the check in the body, or the whole
	// body if the path is empty.
	Body Subject = "body"
	// Size is the size of the body in bytes.
	Size Subject = "size"
)

// Subjects are those there are, in the order they are offered.
var Subjects = []Subject{Status, Time, Header, Body, Size}

// HasPath reports whether a check of the subject has a path: the name of a
// header, or where a value is in the body.
func (s Subject) HasPath() bool {
	return s == Header || s == Body
}

// Comparison is how a check compares what it looks at with its value.
type Comparison string

const (
	Equals      Comparison = "equals"
	NotEquals   Comparison = "notEquals"
	Contains    Comparison = "contains"
	NotContains Comparison = "notContains"
	Matches     Comparison = "matches"
	Exists      Comparison = "exists"
	NotExists   Comparison = "notExists"
	Less        Comparison = "less"
	Greater     Comparison = "greater"
	Length      Comparison = "length"
	Empty       Comparison = "empty"
)

// Comparisons are those there are, in the order they are offered.
var Comparisons = []Comparison{Equals, NotEquals, Contains, NotContains, Matches, Exists, NotExists, Less, Greater, Length, Empty}

// HasValue reports whether a check with the comparison has a value to
// compare with.
func (c Comparison) HasValue() bool {
	switch c {
	case Exists, NotExists, Empty:
		return false
	}
	return true
}

// Check is one thing a response should be: what of it is looked at, the
// path of that for a header or the body, how it is compared, and what with.
// Placeholders in the value are replaced when it is checked, and those in
// the path are not, as with the paths of captures. One that is not enabled
// is not checked.
type Check struct {
	Subject    Subject    `json:"subject"`
	Path       string     `json:"path,omitempty"`
	Comparison Comparison `json:"comparison"`
	Value      string     `json:"value,omitempty"`
	Enabled    bool       `json:"enabled"`
}

// known reports whether the check is of a subject and comparison there are,
// which one from a newer version of the program might not be.
func (c Check) known() bool {
	for _, s := range Subjects {
		if s == c.Subject {
			for _, comparison := range Comparisons {
				if comparison == c.Comparison {
					return true
				}
			}
		}
	}
	return false
}

// Facts are what of a response is checked.
type Facts struct {
	// Status is the status as it is compared, then the other names it
	// goes by, any of which equals takes: for HTTP the code, then the
	// status line (200, 200 OK); for gRPC the name of the code, then its
	// number (NOT_FOUND, 5); for MCP the outcome (OK, Tool error, or the
	// code and name of a JSON-RPC error, then each of those alone).
	Status []string
	// Time is how long the response took.
	Time time.Duration
	// Headers are those of the response.
	Headers http.Header
	// Body is the document values are looked for in, which is empty if
	// there is none, and Size the size of the body in bytes.
	Body string
	Size int
}

// Result is what came of a check: whether it passed, and why not if it did
// not.
type Result struct {
	Check  Check  `json:"check"`
	Passed bool   `json:"passed,omitempty"`
	Reason string `json:"reason,omitempty"`
}

// InUse returns the checks that are enabled and that this version knows.
func InUse(checks []Check) []Check {
	var used []Check
	for _, c := range checks {
		if c.Enabled && c.known() {
			used = append(used, c)
		}
	}
	return used
}

// Run checks the facts of a response with each of the checks that is in
// use, in order, replacing the placeholders of their values with expand.
func Run(checks []Check, facts Facts, expand func(string) string) []Result {
	var results []Result
	for _, c := range InUse(checks) {
		err := c.run(facts, expand(c.Value))
		result := Result{Check: c, Passed: err == nil}
		if err != nil {
			result.Reason = err.Error()
		}
		results = append(results, result)
	}
	return results
}

// Failed returns how many of the results are of checks that did not pass.
func Failed(results []Result) int {
	failed := 0
	for _, r := range results {
		if !r.Passed {
			failed++
		}
	}
	return failed
}

// actual is what a check looks at: its value as Find decodes JSON, the name
// it goes by in the reason a check fails, and for the status every name it
// goes by. A plain value is text which is shown without quotes.
type actual struct {
	value any
	name  string
	names []string
	plain bool
}

// shown returns the value as it is written in the reason a check failed.
func (a actual) shown() string {
	if s, ok := a.value.(string); ok && a.plain {
		return cut(s)
	}
	return show(a.value)
}

// look returns what the check looks at in the facts, or why it is not
// there, as a capture.MissingError, or cannot be looked at.
func (c Check) look(f Facts) (actual, error) {
	switch c.Subject {
	case Status:
		if len(f.Status) == 0 {
			return actual{}, capture.Missing("there is no status")
		}
		return actual{value: f.Status[0], name: "the status", names: f.Status, plain: true}, nil
	case Time:
		return actual{value: json.Number(strconv.FormatInt(f.Time.Milliseconds(), 10)), name: "the time in ms"}, nil
	case Size:
		return actual{value: json.Number(strconv.Itoa(f.Size)), name: "the size in bytes"}, nil
	case Header:
		name := strings.TrimSpace(c.Path)
		if name == "" {
			return actual{}, errors.New("the path is to be the name of the header")
		}
		var values []string
		for key, v := range f.Headers {
			if strings.EqualFold(key, name) {
				values = append(values, v...)
			}
		}
		if values == nil {
			return actual{}, capture.Missing("there is no header %s", name)
		}
		return actual{value: strings.Join(values, ", "), name: "the header " + name, plain: true}, nil
	}

	// The whole body is looked at as JSON if it is, and as text if it is
	// not, so that what is in a page or a message of text can be checked
	// too. Only JSON has paths.
	path := strings.TrimSpace(c.Path)
	if path == "" {
		if !json.Valid([]byte(f.Body)) {
			return actual{value: f.Body, name: "the body"}, nil
		}
		value, err := capture.Find(f.Body, "")
		return actual{value: value, name: "the body"}, err
	}
	if strings.TrimSpace(f.Body) == "" {
		return actual{}, capture.Missing("the response has no body")
	}
	value, err := capture.Find(f.Body, path)
	return actual{value: value, name: path}, err
}

// isMissing reports whether an error is that what was looked for is not
// there.
func isMissing(err error) bool {
	var missing *capture.MissingError
	return errors.As(err, &missing)
}

// run checks the facts, with the value the check compares with, and returns
// why the check fails, or nil if it passes.
func (c Check) run(f Facts, expected string) error {
	a, err := c.look(f)
	switch {
	case c.Comparison == Exists:
		return err
	case c.Comparison == NotExists:
		if err == nil {
			return fmt.Errorf("%s is there", a.name)
		}
		if isMissing(err) {
			return nil
		}
		return err
	case err != nil:
		return err
	}

	switch c.Comparison {
	case Equals, NotEquals:
		equal := a.any(func(v any) bool { return equals(v, expected, a.names != nil) })
		switch {
		case c.Comparison == Equals && !equal:
			return fmt.Errorf("%s is %s, not %s", a.name, a.shown(), expected)
		case c.Comparison == NotEquals && equal:
			return fmt.Errorf("%s is %s", a.name, a.shown())
		}
	case Contains, NotContains:
		found := a.any(func(v any) bool { return contains(v, expected) })
		switch {
		case c.Comparison == Contains && !found:
			return fmt.Errorf("%s does not contain %s", a.name, expected)
		case c.Comparison == NotContains && found:
			return fmt.Errorf("%s contains %s", a.name, expected)
		}
	case Matches:
		expression, err := regexp.Compile(expected)
		if err != nil {
			return fmt.Errorf("the regex is not valid: %s", strings.TrimPrefix(err.Error(), "error parsing regexp: "))
		}
		if !a.any(func(v any) bool { return expression.MatchString(text(v)) }) {
			return fmt.Errorf("%s is %s, which does not match the regex", a.name, a.shown())
		}
	case Less, Greater:
		return a.compare(c.Comparison, expected)
	case Length:
		want, err := strconv.Atoi(strings.TrimSpace(expected))
		if err != nil {
			return fmt.Errorf("the length to compare with, %q, is not a whole number", expected)
		}
		have, ok := length(a.value)
		switch {
		case !ok:
			return fmt.Errorf("%s is %s, which has no length", a.name, a.shown())
		case have != want:
			return fmt.Errorf("the length of %s is %d, not %d", a.name, have, want)
		}
	case Empty:
		if have, ok := length(a.value); a.value != nil && (!ok || have > 0) {
			return fmt.Errorf("%s is %s, which is not empty", a.name, a.shown())
		}
	}
	return nil
}

// any reports whether what is looked at, by any of the names it goes by,
// is as test says.
func (a actual) any(test func(any) bool) bool {
	if a.names == nil {
		return test(a.value)
	}
	for _, name := range a.names {
		if test(name) {
			return true
		}
	}
	return false
}

// compare checks that what is looked at, which has to be a number, is less
// or greater than the number expected.
func (a actual) compare(comparison Comparison, expected string) error {
	word := map[Comparison]string{Less: "less than", Greater: "greater than"}[comparison]
	want, ok := number(expected)
	if !ok {
		return fmt.Errorf("%s needs a number, and %q is not one", word, expected)
	}

	// The status is compared by the first of its names that is a number,
	// which for gRPC is its code.
	have, ok := 0.0, false
	if a.names != nil {
		for _, name := range a.names {
			if have, ok = number(name); ok {
				break
			}
		}
	} else if n, isNumber := a.value.(json.Number); isNumber {
		have, ok = number(n.String())
	}
	switch {
	case !ok:
		return fmt.Errorf("%s needs a number, and %s is %s", word, a.name, a.shown())
	case comparison == Less && !(have < want), comparison == Greater && !(have > want):
		return fmt.Errorf("%s is %s, which is not %s %s", a.name, a.shown(), word, expected)
	}
	return nil
}

// number reads text as a number.
func number(text string) (float64, bool) {
	n, err := strconv.ParseFloat(strings.TrimSpace(text), 64)
	return n, err == nil
}

// equals reports whether a value is the same as what is expected: as a
// number for a number, so 1 equals 1.0, as JSON for an object or an array,
// so the spacing makes no difference, and otherwise as text, which for null
// is null. A status is compared ignoring case.
func equals(value any, expected string, ignoreCase bool) bool {
	switch v := value.(type) {
	case nil:
		return strings.TrimSpace(expected) == "null"
	case json.Number:
		have, ok := number(v.String())
		want, isNumber := number(expected)
		if ok && isNumber {
			return have == want
		}
		return v.String() == expected
	case map[string]any, []any:
		decoder := json.NewDecoder(strings.NewReader(expected))
		decoder.UseNumber()
		var want any
		if err := decoder.Decode(&want); err != nil {
			return false
		}
		return reflect.DeepEqual(numbers(v), numbers(want))
	case string:
		if ignoreCase {
			return strings.EqualFold(v, expected)
		}
	}
	return text(value) == expected
}

// numbers returns a value that Find returned with its numbers as float64s,
// so that the same number written differently compares as the same.
func numbers(value any) any {
	switch v := value.(type) {
	case json.Number:
		if n, ok := number(v.String()); ok {
			return n
		}
	case map[string]any:
		m := make(map[string]any, len(v))
		for key, member := range v {
			m[key] = numbers(member)
		}
		return m
	case []any:
		s := make([]any, len(v))
		for i, element := range v {
			s[i] = numbers(element)
		}
		return s
	}
	return value
}

// contains reports whether a value contains what is expected: an array an
// element that equals it, and anything else its text.
func contains(value any, expected string) bool {
	if elements, ok := value.([]any); ok {
		for _, element := range elements {
			if equals(element, expected, false) {
				return true
			}
		}
		return false
	}
	return value != nil && strings.Contains(text(value), expected)
}

// length returns how many elements an array has, how many members an
// object has or how many characters a string has, and reports false for
// anything else.
func length(value any) (int, bool) {
	switch v := value.(type) {
	case []any:
		return len(v), true
	case map[string]any:
		return len(v), true
	case string:
		return utf8.RuneCountInString(v), true
	}
	return 0, false
}

// text returns a value as capture.Text does.
func text(value any) string {
	s, _ := capture.Text(value)
	return s
}

// maxShown is the most characters of a value that the reason a check
// failed shows.
const maxShown = 80

// show returns a value as it is written in the reason a check failed: as
// JSON, with a string in quotes, null as null, and anything long cut short.
func show(value any) string {
	var s string
	switch v := value.(type) {
	case nil:
		s = "null"
	case string:
		s = strconv.Quote(v)
	default:
		s = text(v)
	}
	return cut(s)
}

// cut returns text cut short if it is long.
func cut(s string) string {
	if utf8.RuneCountInString(s) > maxShown {
		return string([]rune(s)[:maxShown]) + "…"
	}
	return s
}

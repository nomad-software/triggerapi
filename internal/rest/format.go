package rest

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/nomad-software/triggerapi/internal/env"
)

// FormatJSON returns JSON set out with each member and element on a line of
// its own, indented by two spaces, as a response is shown. {{name}}
// placeholders are left as they are, and may stand for a value as well as
// be inside a string: {"id": {{id}}} can be set out though it is not JSON
// until the placeholder has been replaced. Text that is nothing but space
// is returned as it is.
func FormatJSON(text string) (string, error) {
	return formatJSON(text, false)
}

// FormatJSONValues is FormatJSON for text which can hold several values of
// JSON one after another, as the messages of a stream of gRPC can be, which
// are set out one beneath another.
func FormatJSONValues(text string) (string, error) {
	return formatJSON(text, true)
}

// formatJSON sets out JSON, which is one value unless several is set.
func formatJSON(text string, several bool) (string, error) {
	if strings.TrimSpace(text) == "" {
		return text, nil
	}

	// Each placeholder is swapped for a number while the text is set out,
	// as a number is allowed both inside a string and as a value. The
	// numbers start with digits that are nowhere in the text, so that
	// putting the placeholders back changes nothing else.
	prefix := "7309185264"
	for strings.Contains(text, prefix) {
		prefix += "1"
	}

	var swapped strings.Builder
	var back []string
	last := 0
	for i, loc := range env.FindPlaceholders(text) {
		number := fmt.Sprintf("%s%06d", prefix, i)
		swapped.WriteString(text[last:loc[0]])
		swapped.WriteString(number)
		back = append(back, number, text[loc[0]:loc[1]])
		last = loc[1]
	}
	swapped.WriteString(text[last:])

	values := []json.RawMessage{json.RawMessage(strings.TrimSpace(swapped.String()))}
	if several {
		var err error
		if values, err = SplitJSON(swapped.String()); err != nil {
			return "", err
		}
	}
	var out bytes.Buffer
	for i, value := range values {
		if i > 0 {
			out.WriteString("\n")
		}
		if err := json.Indent(&out, value, "", "  "); err != nil {
			return "", err
		}
	}
	return strings.NewReplacer(back...).Replace(out.String()), nil
}

// SplitJSON returns the values of JSON one after another in text, which is
// an error if any is not valid.
func SplitJSON(text string) ([]json.RawMessage, error) {
	decoder := json.NewDecoder(strings.NewReader(text))
	var values []json.RawMessage
	for {
		var value json.RawMessage
		err := decoder.Decode(&value)
		if errors.Is(err, io.EOF) {
			return values, nil
		}
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
}

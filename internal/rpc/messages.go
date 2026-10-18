package rpc

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/nomad-software/triggerapi/internal/rest"
)

// splitMessages returns the JSON of each message of the text of a request:
// one object, or objects one after another, or for a method that takes a
// stream of them, which stream says, an array of them. Empty text is one
// empty message. Text that is not JSON is returned whole, as one message, so
// that reading it as one says what is wrong with it.
func splitMessages(text string, stream bool) []string {
	if strings.TrimSpace(text) == "" {
		return []string{"{}"}
	}
	values, err := rest.SplitJSON(text)
	if err != nil {
		return []string{text}
	}

	// An array is never a message, which is an object, so it is one of
	// messages.
	if stream && len(values) == 1 && bytes.HasPrefix(values[0], []byte("[")) {
		var elements []json.RawMessage
		if json.Unmarshal(values[0], &elements) == nil {
			values = elements
		}
	}
	messages := make([]string, len(values))
	for i, value := range values {
		messages[i] = string(value)
	}
	return messages
}

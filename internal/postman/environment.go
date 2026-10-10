package postman

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/nomad-software/triggerapi/internal/env"
)

// exportedEnvironment is an environment, or the globals, exported from
// Postman.
type exportedEnvironment struct {
	Name   string     `json:"name"`
	Values []variable `json:"values"`
}

// variable is a variable of an exported environment.
type variable struct {
	Key   string `json:"key"`
	Value text   `json:"value"`
	// Enabled is missing from a variable that is enabled.
	Enabled *bool `json:"enabled"`
}

// text is a value which is usually a string, but is kept as it was written
// when it is a number or anything else.
type text string

func (t *text) UnmarshalJSON(data []byte) error {
	var s string
	switch {
	case json.Unmarshal(data, &s) == nil:
		*t = text(s)
	case string(data) == "null":
		*t = ""
	default:
		*t = text(data)
	}
	return nil
}

// ImportEnvironment reads an environment exported from Postman. Variables
// without a name are left out.
func ImportEnvironment(data []byte) (env.Environment, error) {
	// The values are looked for first, so that a file of some other kind is
	// described as that and not as badly formed.
	var shape struct {
		Values json.RawMessage `json:"values"`
	}
	if err := json.Unmarshal(data, &shape); err != nil {
		return env.Environment{}, fmt.Errorf("not valid JSON: %w", err)
	}
	if !strings.HasPrefix(strings.TrimSpace(string(shape.Values)), "[") {
		return env.Environment{}, errors.New("not a Postman environment")
	}

	var doc exportedEnvironment
	if err := json.Unmarshal(data, &doc); err != nil {
		return env.Environment{}, fmt.Errorf("not a Postman environment: %w", err)
	}

	environment := env.Environment{Name: strings.TrimSpace(doc.Name)}
	if environment.Name == "" {
		environment.Name = "Imported Environment"
	}
	for _, v := range doc.Values {
		if v.Key == "" {
			continue
		}
		environment.Variables = append(environment.Variables, env.Variable{
			Key:     v.Key,
			Value:   string(v.Value),
			Enabled: v.Enabled == nil || *v.Enabled,
		})
	}
	return environment, nil
}

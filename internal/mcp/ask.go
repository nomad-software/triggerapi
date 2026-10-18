//lint:file-ignore SA1019 Sampling is deprecated since protocol version 2026-07-28, but servers still ask for it while it is.

package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// What a server can ask of the user while a call is under way: to fill in a
// form (elicitation), or to give the message a language model would have
// (sampling).

// The actions an elicitation is answered with.
const (
	ActionAccept  = "accept"
	ActionDecline = "decline"
	ActionCancel  = "cancel"
)

// The outcomes of sampling.
const (
	SamplingReplied  = "replied"
	SamplingDeclined = "declined"
)

// ErrDeclined is what a Listener's Sample returns for a request that the
// user declines.
var ErrDeclined = errors.New("the user declined the request")

// Elicitation is what a server asks the user for: a message, and the fields
// of a form to fill in, which can be none.
type Elicitation struct {
	Message string
	Fields  []Field
}

// The types of a Field.
const (
	FieldString  = "string"
	FieldNumber  = "number"
	FieldInteger = "integer"
	FieldBoolean = "boolean"
	// FieldChoices is a list of options of which any number are chosen.
	FieldChoices = "array"
)

// Field is a field of the form of an elicitation, which is a value of one of
// the types of field.
type Field struct {
	Name        string
	Title       string
	Description string
	Type        string
	// Format is what kind of string a string is, such as email, uri or
	// date, if it is given.
	Format   string
	Required bool
	// Options are the values to choose from, if there are any, for a
	// string or for FieldChoices.
	Options []Option
	// Default is the value it starts with, or nil.
	Default any
	// Minimum and Maximum are the least and greatest a number can be, or
	// for a string its length, where they are given.
	Minimum, Maximum *float64
}

// Option is a value a field can be given, with what to call it.
type Option struct {
	Value string
	Title string
}

// Label returns what to call the field: its title, or its name.
func (f Field) Label() string {
	if f.Title != "" {
		return f.Title
	}
	return f.Name
}

// Parse returns the value of a string, number or integer field typed as
// text, or an error if it is not one. It reports false for a field with no
// value, which is left out of the answer.
func (f Field) Parse(text string) (any, bool, error) {
	if f.Type != FieldString {
		text = strings.TrimSpace(text)
	}
	if text == "" {
		if f.Required {
			return nil, false, fmt.Errorf("%s has to be given", f.Label())
		}
		return nil, false, nil
	}
	switch f.Type {
	case FieldInteger:
		n, err := strconv.ParseInt(text, 10, 64)
		if err != nil {
			return nil, false, fmt.Errorf("%s has to be a whole number", f.Label())
		}
		return n, true, f.inRange(float64(n), "")
	case FieldNumber:
		n, err := strconv.ParseFloat(text, 64)
		if err != nil {
			return nil, false, fmt.Errorf("%s has to be a number", f.Label())
		}
		return n, true, f.inRange(n, "")
	}
	return text, true, f.inRange(float64(len([]rune(text))), " characters")
}

// inRange returns an error if a number, or the length of a string, is
// outside the least and greatest the field allows.
func (f Field) inRange(n float64, unit string) error {
	if f.Minimum != nil && n < *f.Minimum {
		return fmt.Errorf("%s has to be at least %s%s", f.Label(), formatNumber(*f.Minimum), unit)
	}
	if f.Maximum != nil && n > *f.Maximum {
		return fmt.Errorf("%s has to be at most %s%s", f.Label(), formatNumber(*f.Maximum), unit)
	}
	return nil
}

// ElicitAnswer is how the user answered an elicitation: one of the actions,
// with the values of the fields if they accepted.
type ElicitAnswer struct {
	Action  string
	Content map[string]any
}

// elicitation makes an Elicitation of what the server asked. The fields are
// in alphabetical order of their names, as the order the server gave them
// in is lost when the request is read.
func elicitation(params *sdk.ElicitParams) (*Elicitation, error) {
	e := &Elicitation{Message: params.Message}
	if params.RequestedSchema == nil {
		return e, nil
	}
	data, err := json.Marshal(params.RequestedSchema)
	if err != nil {
		return nil, err
	}
	var schema jsonschema.Schema
	if err := json.Unmarshal(data, &schema); err != nil {
		return nil, fmt.Errorf("the form the server asked to be filled in cannot be read: %w", err)
	}
	for _, name := range slices.Sorted(maps.Keys(schema.Properties)) {
		property := schema.Properties[name]
		if property == nil {
			continue
		}
		field := Field{
			Name:        name,
			Title:       property.Title,
			Description: property.Description,
			Type:        property.Type,
			Format:      property.Format,
			Required:    slices.Contains(schema.Required, name),
			Minimum:     property.Minimum,
			Maximum:     property.Maximum,
		}
		if field.Type == "" {
			field.Type = FieldString
		}
		if field.Type == FieldString {
			field.Minimum, field.Maximum = length(property.MinLength), length(property.MaxLength)
		}
		if len(property.Default) > 0 {
			_ = json.Unmarshal(property.Default, &field.Default)
		}
		field.Options = options(property)
		if field.Type == FieldChoices && property.Items != nil {
			field.Options = options(property.Items)
		}
		e.Fields = append(e.Fields, field)
	}
	return e, nil
}

// length returns a least or greatest length as a number.
func length(n *int) *float64 {
	if n == nil {
		return nil
	}
	f := float64(*n)
	return &f
}

// options returns the values a property can be, which are given as an enum,
// with their names in enumNames, or as constants with titles, in oneOf or
// anyOf.
func options(property *jsonschema.Schema) []Option {
	var found []Option
	names, _ := property.Extra["enumNames"].([]any)
	for i, value := range property.Enum {
		option := Option{Value: fmt.Sprint(value)}
		if i < len(names) {
			option.Title, _ = names[i].(string)
		}
		found = append(found, option)
	}
	for _, choice := range append(slices.Clone(property.OneOf), property.AnyOf...) {
		if choice != nil && choice.Const != nil {
			found = append(found, Option{Value: fmt.Sprint(*choice.Const), Title: choice.Title})
		}
	}
	return found
}

// Sampling is what a server asks a language model for: the messages so far,
// with a system prompt, and what it would like of the model and its answer.
type Sampling struct {
	SystemPrompt string
	Messages     []SamplingMessage
	MaxTokens    int64
	Temperature  float64
	// Models are names of models the server would like, best first.
	Models        []string
	StopSequences []string
}

// SamplingMessage is a message of those a server gives to be answered: who
// it is from, user or assistant, and what it says, which for anything but
// text says what it is.
type SamplingMessage struct {
	Role string
	Text string
}

// SampleAnswer is the message given to the server in place of the answer of
// a language model, and the name of the model it is said to be from.
type SampleAnswer struct {
	Text       string
	Model      string
	StopReason string
}

// sampling makes a Sampling of what the server asked.
func sampling(params *sdk.CreateMessageParams) *Sampling {
	s := &Sampling{
		SystemPrompt:  params.SystemPrompt,
		MaxTokens:     params.MaxTokens,
		Temperature:   params.Temperature,
		StopSequences: params.StopSequences,
	}
	if params.ModelPreferences != nil {
		for _, hint := range params.ModelPreferences.Hints {
			if hint != nil && hint.Name != "" {
				s.Models = append(s.Models, hint.Name)
			}
		}
	}
	for _, m := range params.Messages {
		if m != nil {
			s.Messages = append(s.Messages, SamplingMessage{Role: string(m.Role), Text: contentText(m.Content)})
		}
	}
	return s
}

// contentText returns the text of content, or for anything else what kind of
// content it is.
func contentText(content sdk.Content) string {
	switch c := content.(type) {
	case *sdk.TextContent:
		return c.Text
	case *sdk.ImageContent:
		return "[an image, " + c.MIMEType + "]"
	case *sdk.AudioContent:
		return "[audio, " + c.MIMEType + "]"
	case nil:
		return ""
	}
	return fmt.Sprintf("[%T]", content)
}

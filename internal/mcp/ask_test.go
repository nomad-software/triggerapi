package mcp

import (
	"reflect"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestElicitation(t *testing.T) {
	// What the server asked, as the client reads it.
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"seats":  map[string]any{"type": "integer", "minimum": 1, "maximum": 9, "default": 2},
			"class":  map[string]any{"type": "string", "enum": []any{"economy", "business"}, "enumNames": []any{"Economy", "Business"}},
			"meal":   map[string]any{"type": "string", "oneOf": []any{map[string]any{"const": "veg", "title": "Vegetarian"}}},
			"email":  map[string]any{"type": "string", "format": "email", "title": "Email", "maxLength": 20},
			"window": map[string]any{"type": "boolean", "description": "A seat by the window"},
			"extras": map[string]any{"type": "array", "items": map[string]any{"enum": []any{"bag", "lounge"}}},
		},
		"required": []any{"email"},
	}
	e, err := elicitation(&sdk.ElicitParams{Message: "Book", RequestedSchema: schema})
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, f := range e.Fields {
		names = append(names, f.Name)
	}
	if want := []string{"class", "email", "extras", "meal", "seats", "window"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("fields %v, want %v", names, want)
	}
	class, email, extras, meal, seats, window := e.Fields[0], e.Fields[1], e.Fields[2], e.Fields[3], e.Fields[4], e.Fields[5]
	if want := []Option{{"economy", "Economy"}, {"business", "Business"}}; !reflect.DeepEqual(class.Options, want) {
		t.Errorf("class options %v", class.Options)
	}
	if want := []Option{{"veg", "Vegetarian"}}; !reflect.DeepEqual(meal.Options, want) {
		t.Errorf("meal options %v", meal.Options)
	}
	if want := []Option{{Value: "bag"}, {Value: "lounge"}}; extras.Type != FieldChoices || !reflect.DeepEqual(extras.Options, want) {
		t.Errorf("extras %+v", extras)
	}
	if !email.Required || email.Format != "email" || email.Label() != "Email" || email.Maximum == nil || *email.Maximum != 20 {
		t.Errorf("email %+v", email)
	}
	if seats.Default != float64(2) || *seats.Minimum != 1 || seats.Label() != "seats" {
		t.Errorf("seats %+v", seats)
	}
	if window.Type != FieldBoolean || window.Description != "A seat by the window" {
		t.Errorf("window %+v", window)
	}

	for _, test := range []struct {
		field Field
		text  string
		want  any
		set   bool
		fails bool
	}{
		{seats, " 3 ", int64(3), true, false},
		{seats, "0", int64(0), true, true},
		{seats, "2.5", nil, false, true},
		{seats, "", nil, false, false},
		{email, "", nil, false, true},
		{email, "a@b.c", "a@b.c", true, false},
		{email, "a-very-long-address@example.com", "a-very-long-address@example.com", true, true},
		{Field{Name: "n", Type: FieldNumber}, "1.5", 1.5, true, false},
	} {
		got, set, err := test.field.Parse(test.text)
		if (err != nil) != test.fails || set != test.set || (!test.fails && got != test.want) {
			t.Errorf("%s %q: %v, %v, %v, want %v, %v, failing %v", test.field.Name, test.text, got, set, err, test.want, test.set, test.fails)
		}
	}
}

func TestEventText(t *testing.T) {
	for _, test := range []struct {
		event Event
		want  string
	}{
		{Event{At: 1500e6, Kind: EventProgress, Progress: 1, Total: 4, Message: "Indexing"}, "+1.500 s  Progress 1/4 (25%)\nIndexing"},
		{Event{Kind: EventProgress, Progress: 7}, "+0.000 s  Progress 7"},
		{Event{Kind: EventLogged, Level: "warning", Logger: "db", Message: "slow"}, "+0.000 s  Log warning db\nslow"},
		{Event{Kind: EventElicitation, Message: "Who?", Outcome: ActionAccept, Answer: []byte(`{"a":1}`)}, "+0.000 s  Elicitation: accept\nWho?\nAnswer: {\n  \"a\": 1\n}"},
		{Event{Kind: EventSampling, Message: "Hi", Outcome: SamplingDeclined}, "+0.000 s  Sampling: declined\nHi"},
	} {
		if got := test.event.Text(); got != test.want {
			t.Errorf("Text = %q, want %q", got, test.want)
		}
	}
}

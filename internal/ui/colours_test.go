package ui

import (
	"reflect"
	"testing"

	"github.com/nomad-software/triggerapi/internal/settings"
)

// fieldSet returns the name of the one field of a struct of strings that is
// not empty, or "" if there is not exactly one.
func fieldSet(value any) string {
	v := reflect.ValueOf(value)
	name := ""
	for i := range v.NumField() {
		if v.Field(i).String() != "" {
			if name != "" {
				return ""
			}
			name = v.Type().Field(i).Name
		}
	}
	return name
}

// TestColourSettingFields checks that each colour is kept in its own field of the
// settings.
func TestColourSettingFields(t *testing.T) {
	want := map[colourKind]string{
		colourWorkspaces:   "Workspaces",
		colourEnvironments: "Environments",
		colourRequests:     "Requests",
		colourREST:         "REST",
		colourGRPC:         "GRPC",
		colourWebSocket:    "WebSocket",
		colourGraphQL:      "GraphQL",
		colourMCP:          "MCP",
		colourRemove:       "Remove",
		colourAction:       "Action",
		colourSend:         "Send",
		colourFound:        "Found",
		colourNotFound:     "NotFound",
		colourError:        "Error",
		colourMatch:        "Match",
		colourSets:         "Sets",
		colourHistory:      "History",
		colourTiming:       "Timing",
		colourPassed:       "Passed",
		colourFailed:       "Failed",
	}
	if len(colourKinds) != len(want) || reflect.TypeFor[settings.Colours]().NumField() != len(want) {
		t.Fatalf("there are %d colours and %d settings, want %d of each", len(colourKinds), reflect.TypeFor[settings.Colours]().NumField(), len(want))
	}
	for _, k := range colourKinds {
		var c settings.Colours
		*customIconColour(&c, k.kind) = "#123456"
		if got := fieldSet(c); got != want[k.kind] {
			t.Errorf("%s is kept in %q, want %q", k.label, got, want[k.kind])
		}
	}
}

// TestJSONColourSettingFields checks that each kind of token is kept in its own
// field of the settings.
func TestJSONColourSettingFields(t *testing.T) {
	want := map[tokenKind]string{tokenKey: "Key", tokenString: "String", tokenNumber: "Number", tokenLiteral: "Literal"}
	for _, k := range tokenKinds {
		var c settings.JSONColours
		*customColour(&c, k.kind) = "#123456"
		if got := fieldSet(c); got != want[k.kind] {
			t.Errorf("%s is kept in %q, want %q", k.label, got, want[k.kind])
		}
	}
}

package graphql

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nomad-software/triggerapi/internal/rest"
)

func TestBody(t *testing.T) {
	body, err := Request{Query: "{ me { id } }"}.Body()
	if err != nil || body != "{\n  \"query\": \"{ me { id } }\"\n}" {
		t.Errorf("no variables: %q, %v", body, err)
	}

	body, err = Request{Query: "query($id: ID!) { user(id: $id) { name } }", Variables: ` {"id": "7"} `}.Body()
	var sent map[string]any
	if err != nil || json.Unmarshal([]byte(body), &sent) != nil || sent["variables"].(map[string]any)["id"] != "7" {
		t.Errorf("variables: %q, %v", body, err)
	}

	// Variables of null are none.
	if body, err = (Request{Query: "{ a }", Variables: "null"}).Body(); err != nil || strings.Contains(body, "variables") {
		t.Errorf("null variables: %q, %v", body, err)
	}

	for _, variables := range []string{"[1]", `"a"`, "{", "{{id}}"} {
		if _, err := (Request{Query: "{ a }", Variables: variables}).Body(); err == nil || !strings.Contains(err.Error(), "JSON object") {
			t.Errorf("variables %s: %v", variables, err)
		}
	}
}

func TestHTTP(t *testing.T) {
	r := Request{
		URL:     "api.test/graphql",
		Headers: []rest.KeyValue{{Key: "X-A", Value: "b", Enabled: true}},
		Auth:    rest.Auth{Type: rest.AuthBearer, Token: "t"},
		Query:   "{ me }",
	}
	req, err := r.HTTP()
	if err != nil || req.Method != http.MethodPost || req.BodyType != rest.BodyJSON || req.URL != r.URL || len(req.Headers) != 1 || req.Auth != r.Auth || !strings.Contains(req.Body, `"{ me }"`) {
		t.Errorf("%+v, %v", req, err)
	}

	if _, err := (Request{URL: "a", Query: "  "}).HTTP(); err == nil {
		t.Error("an empty query was sent")
	}
}

func TestExpand(t *testing.T) {
	vars := map[string]string{"{{host}}": "api.test", "{{id}}": "7", "{{who}}": "me", "{{token}}": "secret"}
	f := func(s string) string {
		for k, v := range vars {
			s = strings.ReplaceAll(s, k, v)
		}
		return s
	}
	r := Request{
		URL:       "{{host}}/graphql",
		Headers:   []rest.KeyValue{{Key: "X-Who", Value: "{{who}}", Enabled: true}},
		Auth:      rest.Auth{Type: rest.AuthBearer, Token: "{{token}}"},
		Query:     "{ user(id: {{id}}) { name } }",
		Variables: `{"id": {{id}}}`,
	}.Expand(f)
	if r.URL != "api.test/graphql" || r.Headers[0].Value != "me" || r.Auth.Token != "secret" || r.Query != "{ user(id: 7) { name } }" || r.Variables != `{"id": 7}` {
		t.Errorf("%+v", r)
	}
}

func TestErrors(t *testing.T) {
	for body, want := range map[string]string{
		`{"data": {"a": 1}}`: "",
		`{"errors": [{"message": "one"}, {"message": " "}, {"message": "two"}]}`: "one; two",
		`not json`:          "",
		`{"errors": "odd"}`: "",
	} {
		if got := Errors([]byte(body)); got != want {
			t.Errorf("%s: %q", body, got)
		}
	}
}

// testSchema is the answer of a server to the introspection query.
const testSchema = `{"data": {"__schema": {
	"queryType": {"name": "Query"},
	"mutationType": {"name": "Change"},
	"subscriptionType": null,
	"types": [
		{"kind": "OBJECT", "name": "User", "description": "Someone who uses it.", "interfaces": [{"kind": "INTERFACE", "name": "Node"}], "fields": [
			{"name": "id", "args": [], "type": {"kind": "NON_NULL", "ofType": {"kind": "SCALAR", "name": "ID"}}},
			{"name": "friends", "args": [{"name": "first", "type": {"kind": "SCALAR", "name": "Int"}, "defaultValue": "10"}], "type": {"kind": "LIST", "ofType": {"kind": "NON_NULL", "ofType": {"kind": "OBJECT", "name": "User"}}}},
			{"name": "nick", "args": [], "type": {"kind": "SCALAR", "name": "String"}, "isDeprecated": true, "deprecationReason": "Use name."}
		]},
		{"kind": "OBJECT", "name": "Query", "interfaces": [], "fields": [
			{"name": "user", "args": [{"name": "id", "description": "Whose.", "type": {"kind": "NON_NULL", "ofType": {"kind": "SCALAR", "name": "ID"}}}], "type": {"kind": "OBJECT", "name": "User"}}
		]},
		{"kind": "OBJECT", "name": "Change", "interfaces": [], "fields": [
			{"name": "rename", "args": [{"name": "input", "type": {"kind": "NON_NULL", "ofType": {"kind": "INPUT_OBJECT", "name": "Rename"}}}], "type": {"kind": "SCALAR", "name": "Boolean"}}
		]},
		{"kind": "INTERFACE", "name": "Node", "fields": [{"name": "id", "args": [], "type": {"kind": "NON_NULL", "ofType": {"kind": "SCALAR", "name": "ID"}}}]},
		{"kind": "INPUT_OBJECT", "name": "Rename", "inputFields": [{"name": "name", "type": {"kind": "SCALAR", "name": "String"}, "defaultValue": "\"x\""}]},
		{"kind": "ENUM", "name": "Role", "enumValues": [{"name": "ADMIN"}, {"name": "GUEST", "isDeprecated": true, "deprecationReason": "No longer supported"}]},
		{"kind": "UNION", "name": "Result", "possibleTypes": [{"kind": "OBJECT", "name": "User"}, {"kind": "OBJECT", "name": "Query"}]},
		{"kind": "SCALAR", "name": "Date", "description": "A day.\nAs 2026-10-16."},
		{"kind": "SCALAR", "name": "String"},
		{"kind": "OBJECT", "name": "__Type", "fields": []}
	]
}}}`

const testSDL = `schema {
  mutation: Change
}

type Query {
  user(
    """Whose."""
    id: ID!
  ): User
}

type Change {
  rename(input: Rename!): Boolean
}

"""
A day.
As 2026-10-16.
"""
scalar Date

interface Node {
  id: ID!
}

input Rename {
  name: String = "x"
}

union Result = User | Query

enum Role {
  ADMIN
  GUEST @deprecated
}

"""Someone who uses it."""
type User implements Node {
  id: ID!
  friends(first: Int = 10): [User!]
  nick: String @deprecated(reason: "Use name.")
}
`

func TestIntrospect(t *testing.T) {
	var asked map[string]any
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		json.Unmarshal(data, &asked)
		auth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, testSchema)
	}))
	defer srv.Close()

	sdl, err := Introspect(context.Background(), srv.Client(), Request{URL: srv.URL, Auth: rest.Auth{Type: rest.AuthBearer, Token: "t"}, Query: "{ ignored }", Variables: "not json"})
	if err != nil {
		t.Fatal(err)
	}
	if sdl != testSDL {
		t.Errorf("schema:\n%s", sdl)
	}
	if !strings.Contains(asked["query"].(string), "__schema") || auth != "Bearer t" {
		t.Errorf("asked %v with %q", asked, auth)
	}
}

func TestParseSchemaFailures(t *testing.T) {
	for _, c := range []struct {
		code int
		body string
		want string
	}{
		{200, `{"errors": [{"message": "introspection is disabled"}]}`, "introspection is disabled"},
		{404, `Not Found`, "404 Not Found"},
		{200, `<html>`, "not JSON"},
		{200, `{"data": null}`, "no schema"},
	} {
		if _, err := parseSchema(c.code, fmt.Sprint(c.code, " ", http.StatusText(c.code)), []byte(c.body)); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%d %s: %v", c.code, c.body, err)
		}
	}
}

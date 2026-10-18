package graphql

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/nomad-software/triggerapi/internal/rest"
)

// introspectionQuery asks a server to describe its schema. Lists and
// non-null types are nested to a depth that covers any type in practice.
const introspectionQuery = `query IntrospectionQuery {
  __schema {
    queryType { name }
    mutationType { name }
    subscriptionType { name }
    types {
      kind
      name
      description
      fields(includeDeprecated: true) {
        name
        description
        args { ...InputValue }
        type { ...TypeRef }
        isDeprecated
        deprecationReason
      }
      inputFields { ...InputValue }
      interfaces { ...TypeRef }
      enumValues(includeDeprecated: true) {
        name
        description
        isDeprecated
        deprecationReason
      }
      possibleTypes { ...TypeRef }
    }
  }
}

fragment InputValue on __InputValue {
  name
  description
  type { ...TypeRef }
  defaultValue
}

fragment TypeRef on __Type {
  kind
  name
  ofType { kind name ofType { kind name ofType { kind name ofType { kind name ofType { kind name ofType { kind name ofType { kind name } } } } } } }
}`

// Schema is the schema of a server, as it described itself.
type Schema struct {
	schema *schema
	// types are the types of the schema, by name.
	types map[string]*fullType
}

func newSchema(s *schema) *Schema {
	types := map[string]*fullType{}
	for i := range s.Types {
		types[s.Types[i].Name] = &s.Types[i]
	}
	return &Schema{schema: s, types: types}
}

// SDL returns the schema in the schema definition language: the roots
// first, as they are where a query starts, then the other types in
// alphabetical order. The types of introspection itself and the built-in
// scalars are left out.
func (s *Schema) SDL() string {
	return s.schema.sdl()
}

// Introspect asks the server of the request for its schema, with the
// headers and authorization of the request. The query and variables of the
// request are not used.
func Introspect(ctx context.Context, client *http.Client, r Request) (*Schema, error) {
	r.Query, r.Variables = introspectionQuery, ""
	req, err := r.HTTP()
	if err != nil {
		return nil, err
	}
	res, err := rest.Send(ctx, client, req)
	if err != nil {
		return nil, err
	}
	return parseSchema(res.StatusCode, res.Status, res.Body)
}

// parseSchema reads the answer to the introspection query and returns the
// schema it describes.
func parseSchema(code int, status string, body []byte) (*Schema, error) {
	var response struct {
		Data *struct {
			Schema *schema `json:"__schema"`
		} `json:"data"`
	}
	err := json.Unmarshal(body, &response)
	switch {
	case err == nil && response.Data != nil && response.Data.Schema != nil:
		return newSchema(response.Data.Schema), nil
	case Errors(body) != "":
		return nil, errors.New(Errors(body))
	case code >= 400:
		return nil, fmt.Errorf("the server answered %s", status)
	case err != nil:
		return nil, fmt.Errorf("the answer is not JSON: %w", err)
	}
	return nil, errors.New("the answer has no schema")
}

// schema is the answer to the introspection query.
type schema struct {
	QueryType        *named     `json:"queryType"`
	MutationType     *named     `json:"mutationType"`
	SubscriptionType *named     `json:"subscriptionType"`
	Types            []fullType `json:"types"`
}

type named struct {
	Name string `json:"name"`
}

type fullType struct {
	Kind          string       `json:"kind"`
	Name          string       `json:"name"`
	Description   string       `json:"description"`
	Fields        []field      `json:"fields"`
	InputFields   []inputValue `json:"inputFields"`
	Interfaces    []typeRef    `json:"interfaces"`
	EnumValues    []enumValue  `json:"enumValues"`
	PossibleTypes []typeRef    `json:"possibleTypes"`
}

type field struct {
	Name              string       `json:"name"`
	Description       string       `json:"description"`
	Args              []inputValue `json:"args"`
	Type              typeRef      `json:"type"`
	IsDeprecated      bool         `json:"isDeprecated"`
	DeprecationReason *string      `json:"deprecationReason"`
}

type inputValue struct {
	Name         string  `json:"name"`
	Description  string  `json:"description"`
	Type         typeRef `json:"type"`
	DefaultValue *string `json:"defaultValue"`
}

type enumValue struct {
	Name              string  `json:"name"`
	Description       string  `json:"description"`
	IsDeprecated      bool    `json:"isDeprecated"`
	DeprecationReason *string `json:"deprecationReason"`
}

type typeRef struct {
	Kind   string   `json:"kind"`
	Name   *string  `json:"name"`
	OfType *typeRef `json:"ofType"`
}

// String returns the type as it is written in the schema definition
// language, as [Name!]!.
func (t typeRef) String() string {
	inner := ""
	if t.OfType != nil {
		inner = t.OfType.String()
	}
	switch t.Kind {
	case "NON_NULL":
		return inner + "!"
	case "LIST":
		return "[" + inner + "]"
	}
	if t.Name != nil {
		return *t.Name
	}
	return inner
}

// builtInScalars are the scalars every schema has, which are not written.
var builtInScalars = []string{"String", "Int", "Float", "Boolean", "ID"}

// sdl returns the schema in the schema definition language (see
// Schema.SDL).
func (s *schema) sdl() string {
	roots := map[string]int{}
	var b strings.Builder

	// The schema itself is written only where its roots are not named as
	// usual.
	var custom []string
	for i, root := range []struct {
		operation string
		typ       *named
	}{{"query", s.QueryType}, {"mutation", s.MutationType}, {"subscription", s.SubscriptionType}} {
		if root.typ == nil {
			continue
		}
		roots[root.typ.Name] = i + 1
		if !strings.EqualFold(root.operation, root.typ.Name) {
			custom = append(custom, "  "+root.operation+": "+root.typ.Name)
		}
	}
	if len(custom) > 0 {
		b.WriteString("schema {\n" + strings.Join(custom, "\n") + "\n}\n")
	}

	types := slices.Clone(s.Types)
	types = slices.DeleteFunc(types, func(t fullType) bool {
		return strings.HasPrefix(t.Name, "__") || t.Kind == "SCALAR" && slices.Contains(builtInScalars, t.Name)
	})
	slices.SortStableFunc(types, func(a, b fullType) int {
		ra, rb := roots[a.Name], roots[b.Name]
		if ra == 0 {
			ra = len(roots) + 1
		}
		if rb == 0 {
			rb = len(roots) + 1
		}
		return cmp.Or(cmp.Compare(ra, rb), strings.Compare(a.Name, b.Name))
	})

	for _, t := range types {
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		writeType(&b, t)
	}
	return b.String()
}

// writeType writes one type in the schema definition language.
func writeType(b *strings.Builder, t fullType) {
	writeDescription(b, t.Description, "")
	switch t.Kind {
	case "SCALAR":
		b.WriteString("scalar " + t.Name + "\n")
	case "OBJECT", "INTERFACE":
		keyword := "type"
		if t.Kind == "INTERFACE" {
			keyword = "interface"
		}
		b.WriteString(keyword + " " + t.Name + implements(t.Interfaces) + " {\n")
		for _, f := range t.Fields {
			writeDescription(b, f.Description, "  ")
			b.WriteString("  " + f.Name + arguments(f.Args) + ": " + f.Type.String() + deprecated(f.IsDeprecated, f.DeprecationReason) + "\n")
		}
		b.WriteString("}\n")
	case "UNION":
		names := make([]string, len(t.PossibleTypes))
		for i, p := range t.PossibleTypes {
			names[i] = p.String()
		}
		b.WriteString("union " + t.Name + " = " + strings.Join(names, " | ") + "\n")
	case "ENUM":
		b.WriteString("enum " + t.Name + " {\n")
		for _, v := range t.EnumValues {
			writeDescription(b, v.Description, "  ")
			b.WriteString("  " + v.Name + deprecated(v.IsDeprecated, v.DeprecationReason) + "\n")
		}
		b.WriteString("}\n")
	case "INPUT_OBJECT":
		b.WriteString("input " + t.Name + " {\n")
		for _, f := range t.InputFields {
			writeDescription(b, f.Description, "  ")
			b.WriteString("  " + inputDefinition(f) + "\n")
		}
		b.WriteString("}\n")
	}
}

// implements returns what a type implements, if anything, as it follows its
// name.
func implements(interfaces []typeRef) string {
	if len(interfaces) == 0 {
		return ""
	}
	names := make([]string, len(interfaces))
	for i, t := range interfaces {
		names[i] = t.String()
	}
	return " implements " + strings.Join(names, " & ")
}

// arguments returns the arguments of a field, if it has any, on one line;
// those which are described are set out a line each, with their
// descriptions.
func arguments(args []inputValue) string {
	if len(args) == 0 {
		return ""
	}
	described := slices.ContainsFunc(args, func(a inputValue) bool { return a.Description != "" })
	if !described {
		parts := make([]string, len(args))
		for i, a := range args {
			parts[i] = inputDefinition(a)
		}
		return "(" + strings.Join(parts, ", ") + ")"
	}

	var b strings.Builder
	b.WriteString("(\n")
	for _, a := range args {
		writeDescription(&b, a.Description, "    ")
		b.WriteString("    " + inputDefinition(a) + "\n")
	}
	b.WriteString("  )")
	return b.String()
}

// inputDefinition returns an argument or a field of an input, with its
// default value if it has one.
func inputDefinition(v inputValue) string {
	text := v.Name + ": " + v.Type.String()
	if v.DefaultValue != nil {
		text += " = " + *v.DefaultValue
	}
	return text
}

// defaultDeprecation is the reason a deprecation is given when it is given
// none, which is not written.
const defaultDeprecation = "No longer supported"

// deprecated returns the mark of something deprecated, or nothing if it is
// not.
func deprecated(is bool, reason *string) string {
	switch {
	case !is:
		return ""
	case reason == nil || *reason == "" || *reason == defaultDeprecation:
		return " @deprecated"
	}
	return " @deprecated(reason: " + strconv.Quote(*reason) + ")"
}

// writeDescription writes a description, if there is one, as a block string
// on the lines before what it describes.
func writeDescription(b *strings.Builder, description, indent string) {
	if description == "" {
		return
	}
	text := strings.ReplaceAll(description, `"""`, `\"""`)
	if !strings.Contains(text, "\n") && !strings.HasSuffix(text, `"`) {
		b.WriteString(indent + `"""` + text + `"""` + "\n")
		return
	}
	b.WriteString(indent + `"""` + "\n")
	for _, line := range strings.Split(text, "\n") {
		if line == "" {
			b.WriteString("\n")
		} else {
			b.WriteString(indent + line + "\n")
		}
	}
	b.WriteString(indent + `"""` + "\n")
}

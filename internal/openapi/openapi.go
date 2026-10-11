// Package openapi turns an OpenAPI description of an API into saved requests.
// It reads OpenAPI 3 and Swagger 2 documents, written as JSON or YAML. It has
// no dependency on the UI.
package openapi

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"regexp"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/nomad-software/triggerapi/internal/collection"
	"github.com/nomad-software/triggerapi/internal/rest"
)

// document is the part of an OpenAPI document that requests are made from.
// The fields of both versions of the format are read into the one type.
type document struct {
	OpenAPI string `yaml:"openapi"`
	Swagger string `yaml:"swagger"`

	Info struct {
		Title string `yaml:"title"`
	} `yaml:"info"`
	Tags []struct {
		Name string `yaml:"name"`
	} `yaml:"tags"`
	Paths    map[string]*pathItem  `yaml:"paths"`
	Security []map[string][]string `yaml:"security"`

	// OpenAPI 3.
	Servers []struct {
		URL string `yaml:"url"`
	} `yaml:"servers"`
	Components struct {
		Schemas         map[string]*schema         `yaml:"schemas"`
		Parameters      map[string]*parameter      `yaml:"parameters"`
		RequestBodies   map[string]*requestBody    `yaml:"requestBodies"`
		SecuritySchemes map[string]*securityScheme `yaml:"securitySchemes"`
	} `yaml:"components"`

	// Swagger 2.
	Host                string                     `yaml:"host"`
	BasePath            string                     `yaml:"basePath"`
	Schemes             []string                   `yaml:"schemes"`
	Consumes            []string                   `yaml:"consumes"`
	Definitions         map[string]*schema         `yaml:"definitions"`
	Parameters          map[string]*parameter      `yaml:"parameters"`
	SecurityDefinitions map[string]*securityScheme `yaml:"securityDefinitions"`
}

type pathItem struct {
	Parameters []*parameter `yaml:"parameters"`

	Get     *operation `yaml:"get"`
	Post    *operation `yaml:"post"`
	Put     *operation `yaml:"put"`
	Patch   *operation `yaml:"patch"`
	Delete  *operation `yaml:"delete"`
	Head    *operation `yaml:"head"`
	Options *operation `yaml:"options"`
}

// operations returns the operations of the path with their methods, in the
// order requests are made for them.
func (p *pathItem) operations() []methodOperation {
	all := []methodOperation{
		{http.MethodGet, p.Get},
		{http.MethodPost, p.Post},
		{http.MethodPut, p.Put},
		{http.MethodPatch, p.Patch},
		{http.MethodDelete, p.Delete},
		{http.MethodHead, p.Head},
		{http.MethodOptions, p.Options},
	}
	return slices.DeleteFunc(all, func(m methodOperation) bool { return m.operation == nil })
}

type methodOperation struct {
	method    string
	operation *operation
}

type operation struct {
	Tags        []string     `yaml:"tags"`
	Summary     string       `yaml:"summary"`
	OperationID string       `yaml:"operationId"`
	Parameters  []*parameter `yaml:"parameters"`
	RequestBody *requestBody `yaml:"requestBody"`
	Consumes    []string     `yaml:"consumes"`
	// Security is a pointer as an empty list, which means no authorization,
	// is not the same as there being none, which means the default.
	Security *[]map[string][]string `yaml:"security"`
}

type parameter struct {
	Ref      string  `yaml:"$ref"`
	Name     string  `yaml:"name"`
	In       string  `yaml:"in"`
	Required bool    `yaml:"required"`
	Example  any     `yaml:"example"`
	Schema   *schema `yaml:"schema"`

	// Swagger 2 describes the value in the parameter itself.
	Type    any   `yaml:"type"`
	Default any   `yaml:"default"`
	Enum    []any `yaml:"enum"`
}

type requestBody struct {
	Ref     string                `yaml:"$ref"`
	Content map[string]*mediaType `yaml:"content"`
}

type mediaType struct {
	Schema   *schema `yaml:"schema"`
	Example  any     `yaml:"example"`
	Examples map[string]struct {
		Value any `yaml:"value"`
	} `yaml:"examples"`
}

type schema struct {
	Ref string `yaml:"$ref"`
	// Type is a string, or a list of them in OpenAPI 3.1.
	Type       any                `yaml:"type"`
	Format     string             `yaml:"format"`
	Properties map[string]*schema `yaml:"properties"`
	Items      *schema            `yaml:"items"`
	Enum       []any              `yaml:"enum"`
	Example    any                `yaml:"example"`
	Default    any                `yaml:"default"`
	AllOf      []*schema          `yaml:"allOf"`
	OneOf      []*schema          `yaml:"oneOf"`
	AnyOf      []*schema          `yaml:"anyOf"`
}

type securityScheme struct {
	Type   string `yaml:"type"`
	Scheme string `yaml:"scheme"`
	Name   string `yaml:"name"`
	In     string `yaml:"in"`
}

// Import reads an OpenAPI document and returns a folder of requests for it:
// one for each operation, named after its summary. The folder is named after
// the API. Operations with a tag are in a folder inside it for their first
// tag.
//
// A request is given what the document says of it: its query parameters and
// headers, with those that are not required switched off, a body made from
// the example or schema, and its authorization. Values that are not known are
// left as placeholders, such as {{token}}, to be defined in an environment,
// as are the parameters in a path.
func Import(data []byte) (*collection.Folder, error) {
	// YAML is read as it is, and JSON too, as JSON is also valid YAML.
	doc := &document{}
	if err := yaml.Unmarshal(data, doc); err != nil {
		return nil, fmt.Errorf("the file could not be read as JSON or YAML: %w", err)
	}
	if doc.OpenAPI == "" && doc.Swagger == "" {
		return nil, errors.New("the file is not an OpenAPI document")
	}

	name := strings.TrimSpace(doc.Info.Title)
	if name == "" {
		name = "Imported API"
	}
	root := &collection.Folder{Name: name}

	// The folders for the tags are in the order the document lists them,
	// followed by any it does not list, in the order they are come across.
	tagged := map[string]*collection.Folder{}
	folderFor := func(tag string) *collection.Folder {
		if tag == "" {
			return root
		}
		if tagged[tag] == nil {
			tagged[tag] = root.AddFolder(tag)
		}
		return tagged[tag]
	}
	for _, tag := range doc.Tags {
		folderFor(tag.Name)
	}

	base := doc.baseURL()
	count := 0

	for _, path := range slices.Sorted(maps.Keys(doc.Paths)) {
		item := doc.Paths[path]
		if item == nil {
			continue
		}

		for _, m := range item.operations() {
			op := m.operation

			request := rest.Request{Method: m.method, URL: base + placeholders(path)}
			doc.addParameters(&request, append(slices.Clone(item.Parameters), op.Parameters...))
			doc.addBody(&request, op.RequestBody)
			doc.addSecurity(&request, op)

			name := strings.TrimSpace(op.Summary)
			if name == "" {
				name = op.OperationID
			}
			if name == "" {
				name = m.method + " " + path
			}

			tag := ""
			if len(op.Tags) > 0 {
				tag = op.Tags[0]
			}
			folderFor(tag).Add(collection.NewREST(name, request))
			count++
		}
	}

	if count == 0 {
		return nil, errors.New("the document does not describe any operations")
	}

	// A tag the document lists but does not use has an empty folder.
	root.Folders = slices.DeleteFunc(root.Folders, func(f *collection.Folder) bool { return len(f.Requests) == 0 })

	return root, nil
}

var pathParameter = regexp.MustCompile(`\{([^{}]+)\}`)

// placeholders rewrites the {name} parameters of a path or server address as
// {{name}} placeholders.
func placeholders(s string) string {
	return pathParameter.ReplaceAllString(s, "{{$1}}")
}

// baseURL returns the address of the server the requests are sent to. It is
// the {{baseUrl}} placeholder if the document does not give one, and starts
// with it if the document gives only a path.
func (d *document) baseURL() string {
	base := ""
	switch {
	case len(d.Servers) > 0:
		base = d.Servers[0].URL
	case d.Host != "":
		scheme := "https"
		if len(d.Schemes) > 0 {
			scheme = d.Schemes[0]
		}
		base = scheme + "://" + d.Host + d.BasePath
	default:
		base = d.BasePath
	}

	base = strings.TrimRight(placeholders(base), "/")
	if !strings.Contains(base, "://") {
		base = "{{baseUrl}}" + base
	}
	return base
}

// resolve follows a reference to something defined elsewhere in the document,
// such as #/components/schemas/User. It reports false if there is nothing
// defined by that name.
func resolve[T any](ref string, sources ...map[string]*T) (*T, bool) {
	name := ref[strings.LastIndex(ref, "/")+1:]
	for _, source := range sources {
		if found := source[name]; found != nil {
			return found, true
		}
	}
	return nil, false
}

// addParameters gives the request the query parameters and headers of an
// operation. Those of the path apply to every operation on it, and are
// replaced by any of the operation with the same name. A parameter that is
// not required is switched off.
func (d *document) addParameters(request *rest.Request, parameters []*parameter) {
	type key struct{ in, name string }
	seen := map[key]int{}
	var unique []*parameter

	for _, p := range parameters {
		if p == nil {
			continue
		}
		if p.Ref != "" {
			found, ok := resolve(p.Ref, d.Components.Parameters, d.Parameters)
			if !ok {
				continue
			}
			p = found
		}

		k := key{p.In, p.Name}
		if at, ok := seen[k]; ok {
			unique[at] = p
			continue
		}
		seen[k] = len(unique)
		unique = append(unique, p)
	}

	var form []rest.KeyValue
	for _, p := range unique {
		pair := rest.KeyValue{Key: p.Name, Value: d.parameterValue(p), Enabled: p.Required}

		switch p.In {
		case "query":
			request.Params = append(request.Params, pair)
		case "header":
			request.Headers = append(request.Headers, pair)
		case "formData":
			// Swagger 2 lists the fields of a form as parameters.
			form = append(form, pair)
		case "body":
			// Swagger 2 gives the body as a parameter with a schema.
			d.setJSONBody(request, d.example(p.Schema, 0))
		}
	}

	if len(form) > 0 {
		request.BodyType = rest.BodyForm
		request.Form = form
	}
}

// parameterValue returns a value to start a parameter off with, which is
// empty if the document does not suggest one.
func (d *document) parameterValue(p *parameter) string {
	for _, value := range []any{p.Example, p.Default} {
		if value != nil {
			return text(value)
		}
	}
	if len(p.Enum) > 0 {
		return text(p.Enum[0])
	}

	if s := d.schemaOf(p.Schema); s != nil {
		for _, value := range []any{s.Example, s.Default} {
			if value != nil {
				return text(value)
			}
		}
		if len(s.Enum) > 0 {
			return text(s.Enum[0])
		}
	}
	return ""
}

// text returns a value as it is written in a parameter.
func text(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case map[string]any, []any:
		if data, err := json.Marshal(v); err == nil {
			return string(data)
		}
	}
	return fmt.Sprint(value)
}

// addBody gives the request the body of an operation, from the first kind of
// content it can send: JSON, then a form, then text or XML.
func (d *document) addBody(request *rest.Request, body *requestBody) {
	if body == nil {
		return
	}
	if body.Ref != "" {
		found, ok := resolve(body.Ref, d.Components.RequestBodies)
		if !ok {
			return
		}
		body = found
	}

	types := slices.Sorted(maps.Keys(body.Content))
	find := func(match func(string) bool) *mediaType {
		for _, t := range types {
			if match(strings.ToLower(t)) && body.Content[t] != nil {
				return body.Content[t]
			}
		}
		return nil
	}

	if media := find(func(t string) bool { return strings.Contains(t, "json") }); media != nil {
		d.setJSONBody(request, d.mediaExample(media))
		return
	}

	if media := find(func(t string) bool { return t == "application/x-www-form-urlencoded" }); media != nil {
		request.BodyType = rest.BodyForm
		if s := d.schemaOf(media.Schema); s != nil {
			for _, name := range slices.Sorted(maps.Keys(s.Properties)) {
				value := d.example(s.Properties[name], 1)
				if value == nil {
					value = ""
				}
				request.Form = append(request.Form, rest.KeyValue{Key: name, Value: text(value), Enabled: true})
			}
		}
		return
	}

	for _, other := range []struct {
		kind     string
		bodyType rest.BodyType
	}{{"xml", rest.BodyXML}, {"text/", rest.BodyText}} {
		if media := find(func(t string) bool { return strings.Contains(t, other.kind) }); media != nil {
			request.BodyType = other.bodyType
			if example, ok := media.Example.(string); ok {
				request.Body = example
			}
			return
		}
	}
}

// setJSONBody makes the body of the request the value, as indented JSON.
func (d *document) setJSONBody(request *rest.Request, value any) {
	request.BodyType = rest.BodyJSON
	if value == nil {
		return
	}
	if data, err := json.MarshalIndent(value, "", "  "); err == nil {
		request.Body = string(data)
	}
}

// mediaExample returns an example of the content: the one the document
// gives, or one made from its schema.
func (d *document) mediaExample(media *mediaType) any {
	if media.Example != nil {
		return media.Example
	}
	for _, name := range slices.Sorted(maps.Keys(media.Examples)) {
		if value := media.Examples[name].Value; value != nil {
			return value
		}
	}
	return d.example(media.Schema, 0)
}

// schemaOf follows the reference of a schema, if it is one.
func (d *document) schemaOf(s *schema) *schema {
	for range maxDepth {
		if s == nil || s.Ref == "" {
			return s
		}
		found, ok := resolve(s.Ref, d.Components.Schemas, d.Definitions)
		if !ok {
			return nil
		}
		s = found
	}
	return nil
}

// maxDepth limits how deep an example goes.
const maxDepth = 8

// maxValues limits how many values are made for one example, counting those
// there is nothing to go on for. Schemas which refer to one another, as those
// of a large API do, would otherwise make an example many times the size of
// the document. The properties of an object there is nothing left for are
// left out of it.
const maxValues = 2000

// exampleMaker makes one example from the schemas of a document.
type exampleMaker struct {
	*document
	// within holds the schemas the value being made is inside, as a schema
	// is not made inside itself, which would never end.
	within map[*schema]bool
	// left is the number of values that can still be made.
	left int
}

// example makes a value that fits a schema: the example or default it gives,
// or a placeholder value of its type. It is nil if there is nothing to go on.
func (d *document) example(s *schema, depth int) any {
	maker := &exampleMaker{document: d, within: map[*schema]bool{}, left: maxValues}
	return maker.example(s, depth)
}

func (d *exampleMaker) example(s *schema, depth int) any {
	if d.left <= 0 {
		return nil
	}
	d.left--

	s = d.schemaOf(s)
	if s == nil || depth > maxDepth || d.within[s] {
		return nil
	}
	d.within[s] = true
	defer delete(d.within, s)

	if s.Example != nil {
		return s.Example
	}
	if s.Default != nil {
		return s.Default
	}
	if len(s.Enum) > 0 {
		return s.Enum[0]
	}

	// A schema made of others has the properties of them all, or is the
	// first of its alternatives.
	if len(s.AllOf) > 0 {
		merged := map[string]any{}
		for _, part := range append(slices.Clone(s.AllOf), &schema{Properties: s.Properties}) {
			if object, ok := d.example(part, depth+1).(map[string]any); ok {
				maps.Copy(merged, object)
			}
		}
		return merged
	}
	for _, alternatives := range [][]*schema{s.OneOf, s.AnyOf} {
		if len(alternatives) > 0 {
			return d.example(alternatives[0], depth+1)
		}
	}

	switch kind := s.kind(); {
	case kind == "object" || len(s.Properties) > 0:
		// The properties that are plain values come first, so that they
		// are not the ones left out if there are too many values.
		names := slices.Sorted(maps.Keys(s.Properties))
		slices.SortStableFunc(names, func(a, b string) int {
			return cmp.Compare(d.nests(s.Properties[a]), d.nests(s.Properties[b]))
		})

		object := map[string]any{}
		for _, name := range names {
			if d.left <= 0 {
				break
			}
			object[name] = d.example(s.Properties[name], depth+1)
		}
		return object
	case kind == "array":
		if item := d.example(s.Items, depth+1); item != nil {
			return []any{item}
		}
		return []any{}
	case kind == "string":
		switch s.Format {
		case "date":
			return "2024-01-01"
		case "date-time":
			return "2024-01-01T00:00:00Z"
		case "uuid":
			return "00000000-0000-0000-0000-000000000000"
		case "email":
			return "user@example.com"
		}
		return "string"
	case kind == "integer" || kind == "number":
		return 0
	case kind == "boolean":
		return false
	}
	return nil
}

// nests reports, as 1 or 0, if a schema is of a value that has others inside
// it.
func (d *exampleMaker) nests(s *schema) int {
	s = d.schemaOf(s)
	if s != nil && (len(s.Properties) > 0 || s.Items != nil || len(s.AllOf)+len(s.OneOf)+len(s.AnyOf) > 0) {
		return 1
	}
	return 0
}

// kind returns the type of the schema. OpenAPI 3.1 allows a list of types,
// of which the first that is not null is taken.
func (s *schema) kind() string {
	switch t := s.Type.(type) {
	case string:
		return t
	case []any:
		for _, each := range t {
			if name, ok := each.(string); ok && name != "null" {
				return name
			}
		}
	}
	return ""
}

// addSecurity gives the request the authorization an operation needs, which
// is the default of the document unless the operation says otherwise. Only
// the first of the schemes it could use is taken.
func (d *document) addSecurity(request *rest.Request, op *operation) {
	requirements := d.Security
	if op.Security != nil {
		requirements = *op.Security
	}

	for _, requirement := range requirements {
		for _, name := range slices.Sorted(maps.Keys(requirement)) {
			scheme, ok := resolve("/"+name, d.Components.SecuritySchemes, d.SecurityDefinitions)
			if !ok {
				continue
			}

			switch kind := strings.ToLower(scheme.Type); {
			case kind == "basic" || (kind == "http" && strings.EqualFold(scheme.Scheme, "basic")):
				request.Auth = rest.Auth{Type: rest.AuthBasic, Username: "{{username}}", Password: "{{password}}"}
			case kind == "http" || kind == "oauth2" || kind == "openidconnect":
				request.Auth = rest.Auth{Type: rest.AuthBearer, Token: "{{token}}"}
			case kind == "apikey" && scheme.In == "header":
				request.Headers = append(request.Headers, rest.KeyValue{Key: scheme.Name, Value: "{{apiKey}}", Enabled: true})
			case kind == "apikey" && scheme.In == "query":
				request.Params = append(request.Params, rest.KeyValue{Key: scheme.Name, Value: "{{apiKey}}", Enabled: true})
			default:
				continue
			}
			return
		}
	}
}

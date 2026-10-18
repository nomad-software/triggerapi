package openapi

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/nomad-software/triggerapi/internal/collection"
	"github.com/nomad-software/triggerapi/internal/rest"
)

const petStore = `
openapi: 3.0.3
info:
  title: Pet Store
tags:
  - name: pets
  - name: unused
servers:
  - url: https://api.example.com/v1/
security:
  - bearer: []
paths:
  /pets:
    parameters:
      - $ref: '#/components/parameters/Trace'
    get:
      tags: [pets]
      summary: List pets
      parameters:
        - name: limit
          in: query
          schema:
            type: integer
            default: 20
        - name: status
          in: query
          required: true
          schema:
            type: string
            enum: [available, sold]
    post:
      tags: [pets, admin]
      operationId: createPet
      requestBody:
        content:
          application/json:
            schema:
              $ref: '#/components/schemas/Pet'
  /pets/{petId}:
    get:
      tags: [pets]
      summary: "  Get a pet  "
      security: []
      parameters:
        - name: petId
          in: path
          required: true
          schema:
            type: string
    delete:
      tags: [admin]
      security:
        - key: []
  /login:
    post:
      security:
        - basic: []
      requestBody:
        content:
          application/x-www-form-urlencoded:
            schema:
              type: object
              properties:
                username:
                  type: string
                remember:
                  type: boolean
components:
  parameters:
    Trace:
      name: X-Trace
      in: header
      example: abc
  schemas:
    Pet:
      type: object
      properties:
        id:
          type: integer
        name:
          type: string
          example: Rex
        born:
          type: string
          format: date
        tags:
          type: array
          items:
            type: string
        owner:
          $ref: '#/components/schemas/Owner'
    Owner:
      allOf:
        - type: object
          properties:
            email:
              type: string
              format: email
        - type: object
          properties:
            pets:
              type: array
              items:
                $ref: '#/components/schemas/Pet'
  securitySchemes:
    bearer:
      type: http
      scheme: bearer
    basic:
      type: http
      scheme: basic
    key:
      type: apiKey
      in: header
      name: X-API-Key
`

// find returns the request with a name in a folder.
func find(t *testing.T, folder *collection.Folder, name string) rest.Request {
	t.Helper()
	for _, r := range folder.Requests {
		if r.Name == name {
			return *r.Request
		}
	}
	t.Fatalf("no request named %q in %q", name, folder.Name)
	return rest.Request{}
}

func TestImport(t *testing.T) {
	root, err := Import([]byte(petStore))
	if err != nil {
		t.Fatal(err)
	}

	// The folder is named after the API, with a folder for each tag that
	// is used, in the order the document lists them, then any others.
	if root.Name != "Pet Store" || len(root.Folders) != 2 || root.Folders[0].Name != "pets" || root.Folders[1].Name != "admin" {
		t.Fatalf("root = %q with folders %+v", root.Name, root.Folders)
	}
	pets, admin := root.Folders[0], root.Folders[1]

	// Operations are in the order of their paths, then their methods, and
	// are named by their summary, then their id, then what they are.
	names := func(f *collection.Folder) []string {
		var out []string
		for _, r := range f.Requests {
			out = append(out, r.Name)
		}
		return out
	}
	if got, want := names(pets), []string{"List pets", "createPet", "Get a pet"}; !reflect.DeepEqual(got, want) {
		t.Errorf("pets = %q, want %q", got, want)
	}
	if got, want := names(admin), []string{"DELETE /pets/{petId}"}; !reflect.DeepEqual(got, want) {
		t.Errorf("admin = %q, want %q", got, want)
	}
	if got, want := names(root), []string{"POST /login"}; !reflect.DeepEqual(got, want) {
		t.Errorf("untagged = %q, want %q", got, want)
	}

	list := find(t, pets, "List pets")
	want := rest.Request{
		Method: "GET",
		URL:    "https://api.example.com/v1/pets",
		Params: []rest.KeyValue{
			{Key: "limit", Value: "20"},
			{Key: "status", Value: "available", Enabled: true},
		},
		Headers: []rest.KeyValue{{Key: "X-Trace", Value: "abc"}},
		Auth:    rest.Auth{Type: rest.AuthBearer, Token: "{{token}}"},
	}
	if !reflect.DeepEqual(list, want) {
		t.Errorf("list = %+v, want %+v", list, want)
	}

	// A body is made from the schema, following references, and stopping
	// where a schema contains itself.
	create := find(t, pets, "createPet")
	if create.Method != "POST" || create.BodyType != rest.BodyJSON {
		t.Errorf("create = %+v", create)
	}
	for _, part := range []string{`"id": 0`, `"name": "Rex"`, `"born": "2024-01-01"`, `"email": "user@example.com"`, "\"tags\": [\n    \"string\"\n  ]"} {
		if !strings.Contains(create.Body, part) {
			t.Errorf("body does not contain %s:\n%s", part, create.Body)
		}
	}

	// Path parameters become placeholders, and an operation can turn the
	// default authorization off.
	get := find(t, pets, "Get a pet")
	if get.URL != "https://api.example.com/v1/pets/{{petId}}" || get.Auth != (rest.Auth{}) || len(get.Params) != 0 {
		t.Errorf("get = %+v", get)
	}

	// An API key is sent in the header it names.
	remove := find(t, admin, "DELETE /pets/{petId}")
	wantHeaders := []rest.KeyValue{{Key: "X-API-Key", Value: "{{apiKey}}", Enabled: true}}
	if remove.Method != "DELETE" || !reflect.DeepEqual(remove.Headers, wantHeaders) || remove.Auth != (rest.Auth{}) {
		t.Errorf("remove = %+v", remove)
	}

	login := find(t, root, "POST /login")
	wantForm := []rest.KeyValue{{Key: "remember", Value: "false", Enabled: true}, {Key: "username", Value: "string", Enabled: true}}
	if login.BodyType != rest.BodyForm || !reflect.DeepEqual(login.Form, wantForm) || login.Auth.Type != rest.AuthBasic || login.Auth.Username != "{{username}}" {
		t.Errorf("login = %+v", login)
	}
}

func TestImportSwagger2JSON(t *testing.T) {
	root, err := Import([]byte(`{
		"swagger": "2.0",
		"info": {"title": "Old API"},
		"host": "old.example.com",
		"basePath": "/api",
		"schemes": ["http", "https"],
		"securityDefinitions": {"auth": {"type": "basic"}},
		"security": [{"auth": []}],
		"parameters": {"Page": {"name": "page", "in": "query", "type": "integer", "default": 1}},
		"definitions": {"User": {"type": "object", "properties": {"name": {"type": "string"}, "active": {"type": "boolean"}}}},
		"paths": {
			"/users": {
				"get": {"summary": "List users", "parameters": [{"$ref": "#/parameters/Page"}]},
				"post": {
					"summary": "Create user",
					"parameters": [{"name": "body", "in": "body", "schema": {"$ref": "#/definitions/User"}}]
				}
			},
			"/upload": {
				"post": {
					"summary": "Upload",
					"parameters": [{"name": "title", "in": "formData", "type": "string", "required": true}]
				}
			}
		}
	}`))
	if err != nil {
		t.Fatal(err)
	}

	if root.Name != "Old API" || len(root.Folders) != 0 || len(root.Requests) != 3 {
		t.Fatalf("root = %q, %d folders, %d requests", root.Name, len(root.Folders), len(root.Requests))
	}

	list := find(t, root, "List users")
	if list.URL != "http://old.example.com/api/users" || !reflect.DeepEqual(list.Params, []rest.KeyValue{{Key: "page", Value: "1"}}) || list.Auth.Type != rest.AuthBasic {
		t.Errorf("list = %+v", list)
	}

	create := find(t, root, "Create user")
	if create.BodyType != rest.BodyJSON || create.Body != "{\n  \"active\": false,\n  \"name\": \"string\"\n}" {
		t.Errorf("create body = %q", create.Body)
	}

	upload := find(t, root, "Upload")
	if upload.BodyType != rest.BodyForm || !reflect.DeepEqual(upload.Form, []rest.KeyValue{{Key: "title", Enabled: true}}) {
		t.Errorf("upload = %+v", upload)
	}
}

func TestImportBaseURL(t *testing.T) {
	const operation = `paths: {/a: {get: {}}}`

	tests := map[string]string{
		"openapi: 3.1.0\n" + operation:                                              "{{baseUrl}}/a",
		"openapi: 3.1.0\nservers: [{url: /api/}]\n" + operation:                     "{{baseUrl}}/api/a",
		"openapi: 3.1.0\nservers: [{url: 'https://{region}.x.test'}]\n" + operation: "https://{{region}}.x.test/a",
		"swagger: '2.0'\nhost: x.test\n" + operation:                                "https://x.test/a",
		"swagger: '2.0'\nbasePath: /v2\n" + operation:                               "{{baseUrl}}/v2/a",
	}
	for doc, want := range tests {
		root, err := Import([]byte(doc))
		if err != nil {
			t.Errorf("%q: %v", doc, err)
			continue
		}
		if root.Name != "Imported API" || root.Requests[0].URL != want || root.Requests[0].Name != "GET /a" {
			t.Errorf("%q: %q %q, want %q", doc, root.Requests[0].Name, root.Requests[0].URL, want)
		}
	}
}

func TestImportErrors(t *testing.T) {
	tests := map[string]string{
		"not YAML":              "{oops: [",
		"not OpenAPI":           "name: something else\npaths: {}",
		"no operations":         "openapi: 3.0.0\ninfo: {title: Empty}\npaths: {/a: {}}",
		"no paths":              "openapi: 3.0.0",
		"an empty file":         "",
		"a list, not an object": "- a\n- b",
	}
	for name, doc := range tests {
		if root, err := Import([]byte(doc)); err == nil {
			t.Errorf("%s: expected an error, got %+v", name, root)
		}
	}
}

func TestImportTolerant(t *testing.T) {
	// References to things that are not defined, and a schema that is its
	// own alias, are skipped rather than stopping the import.
	root, err := Import([]byte(`
openapi: 3.1.0
paths:
  /a:
    post:
      parameters:
        - $ref: '#/components/parameters/Missing'
        - null
      security:
        - missing: []
      requestBody:
        content:
          application/json:
            schema:
              type: [object, "null"]
              properties:
                loop:
                  $ref: '#/components/schemas/Loop'
                gone:
                  $ref: '#/components/schemas/Gone'
                either:
                  oneOf:
                    - type: number
                    - type: string
components:
  schemas:
    Loop:
      $ref: '#/components/schemas/Loop'
`))
	if err != nil {
		t.Fatal(err)
	}

	got := *root.Requests[0].Request
	if got.BodyType != rest.BodyJSON || got.Body != "{\n  \"either\": 0,\n  \"gone\": null,\n  \"loop\": null\n}" || len(got.Params) != 0 || got.Auth != (rest.Auth{}) {
		t.Errorf("request = %+v", got)
	}
}

// Schemas which refer to one another do not make an example that grows
// without limit: a schema is not made inside itself, and there is a limit to
// the size of one example.
func TestExampleOfSchemasThatReferToEachOther(t *testing.T) {
	const count = 30

	var b strings.Builder
	b.WriteString("openapi: 3.0.0\ninfo:\n  title: T\npaths:\n  /a:\n    post:\n      requestBody:\n        content:\n          application/json:\n            schema:\n              $ref: '#/components/schemas/N0'\ncomponents:\n  schemas:\n")
	for i := range count {
		fmt.Fprintf(&b, "    N%d:\n      type: object\n      properties:\n        name:\n          type: string\n", i)
		for j := range count {
			fmt.Fprintf(&b, "        p%d:\n          $ref: '#/components/schemas/N%d'\n", j, j)
		}
	}

	folder, err := Import([]byte(b.String()))
	if err != nil {
		t.Fatal(err)
	}
	body := folder.Requests[0].Request.Body
	if len(body) > 200_000 || !json.Valid([]byte(body)) {
		t.Errorf("the body is %d bytes, valid %v", len(body), json.Valid([]byte(body)))
	}

	// The schema itself is not inside the example of it, but another is.
	var example map[string]any
	if err := json.Unmarshal([]byte(body), &example); err != nil {
		t.Fatal(err)
	}
	if example["name"] != "string" || example["p0"] != nil {
		t.Errorf("name = %v, p0 = %v", example["name"], example["p0"])
	}
	if inner, ok := example["p1"].(map[string]any); !ok || inner["name"] != "string" {
		t.Errorf("p1 = %v", example["p1"])
	}
}

// An OAuth 2.0 scheme with a flow the program has gets its token by that
// flow, with the scopes the operation needs, and one with none is a bearer
// token.
func TestImportOAuth(t *testing.T) {
	root, err := Import([]byte(`
openapi: 3.2.0
paths:
  /client:
    get:
      summary: client
      security: [{client: [read, write]}]
  /code:
    get:
      summary: code
      security: [{code: [read]}]
  /device:
    get:
      summary: device
      security: [{device: []}]
  /implicit:
    get:
      summary: implicit
      security: [{implicit: []}]
components:
  securitySchemes:
    client:
      type: oauth2
      flows:
        clientCredentials: {tokenUrl: 'https://idp/token', scopes: {read: r, write: w}}
        authorizationCode: {authorizationUrl: 'https://idp/auth', tokenUrl: 'https://idp/token', scopes: {}}
    code:
      type: oauth2
      flows:
        authorizationCode: {authorizationUrl: 'https://idp/auth', tokenUrl: 'https://idp/token', scopes: {read: r}}
    device:
      type: oauth2
      flows:
        deviceAuthorization: {deviceAuthorizationUrl: 'https://idp/device', tokenUrl: 'https://idp/token', scopes: {}}
    implicit:
      type: oauth2
      flows:
        implicit: {authorizationUrl: 'https://idp/auth', scopes: {}}
`))
	if err != nil {
		t.Fatal(err)
	}
	client := rest.OAuth{ClientID: "{{clientId}}", ClientSecret: "{{clientSecret}}"}
	want := map[string]rest.Auth{
		"client":   {Type: rest.AuthOAuth2, OAuth: &rest.OAuth{TokenURL: "https://idp/token", ClientID: client.ClientID, ClientSecret: client.ClientSecret, Scopes: "read write"}},
		"code":     {Type: rest.AuthOAuth2, OAuth: &rest.OAuth{Grant: rest.GrantAuthorizationCode, TokenURL: "https://idp/token", AuthURL: "https://idp/auth", ClientID: client.ClientID, ClientSecret: client.ClientSecret, Scopes: "read"}},
		"device":   {Type: rest.AuthOAuth2, OAuth: &rest.OAuth{Grant: rest.GrantDeviceCode, TokenURL: "https://idp/token", DeviceURL: "https://idp/device", ClientID: client.ClientID, ClientSecret: client.ClientSecret}},
		"implicit": {Type: rest.AuthBearer, Token: "{{token}}"},
	}
	for _, r := range root.All() {
		if got := r.Auth; !reflect.DeepEqual(got, want[r.Name]) {
			t.Errorf("%s: %+v %+v", r.Name, got, got.OAuth)
		}
	}
	if len(root.All()) != len(want) {
		t.Errorf("%d requests", len(root.All()))
	}

	// Swagger 2 has one flow.
	root, err = Import([]byte(`{"swagger": "2.0", "host": "h", "paths": {"/a": {"get": {"security": [{"o": ["x"]}]}}},
		"securityDefinitions": {"o": {"type": "oauth2", "flow": "accessCode", "authorizationUrl": "https://idp/auth", "tokenUrl": "https://idp/token"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := root.All()[0].Auth; got.Type != rest.AuthOAuth2 || got.OAuth.Grant != rest.GrantAuthorizationCode || got.OAuth.AuthURL != "https://idp/auth" || got.OAuth.Scopes != "x" {
		t.Errorf("swagger: %+v %+v", got, got.OAuth)
	}
}

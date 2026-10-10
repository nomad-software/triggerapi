package postman

import (
	"reflect"
	"testing"

	"github.com/nomad-software/postie/internal/collection"
	"github.com/nomad-software/postie/internal/rest"
)

const sample = `{
	"info": {
		"name": "Hotel APIs",
		"schema": "https://schema.getpostman.com/json/collection/v2.1.0/collection.json"
	},
	"auth": {"type": "bearer", "bearer": [{"key": "token", "value": "{{Token}}", "type": "string"}]},
	"event": [{"listen": "test", "script": {"exec": ["ignored"]}}],
	"item": [
		{
			"name": "Accounts",
			"item": [
				{
					"name": "List accounts",
					"event": [{"listen": "prerequest", "script": {"exec": ["ignored"]}}],
					"response": [{"name": "saved example", "body": "ignored"}],
					"request": {
						"method": "GET",
						"header": [
							{"key": "x-app-key", "value": "{{AppKey}}", "type": "text"},
							{"key": "x-off", "value": "1", "disabled": true},
							{"key": "", "value": "no key"}
						],
						"url": {
							"raw": "{{HostName}}/ars/v1/accounts?offset=0&limit=100",
							"host": ["{{HostName}}"],
							"path": ["ars", "v1", "accounts"],
							"query": [
								{"key": "accountId", "value": "{{AccountId}}", "disabled": true},
								{"key": "offset", "value": "0"},
								{"key": "limit", "value": 100}
							]
						}
					}
				},
				{
					"name": "Deeper",
					"auth": {"type": "noauth"},
					"item": [
						{
							"name": "Create account",
							"request": {
								"method": "post",
								"header": [{"key": "Content-Type", "value": "application/json"}],
								"body": {"mode": "raw", "raw": "{\r\n    \"name\": \"a\"\r\n}", "options": {"raw": {"language": "json"}}},
								"url": {"raw": "https://api.example.com/accounts/:accountId/users/:userId", "variable": [{"key": "accountId", "value": "42"}, {"key": "userId"}]}
							}
						}
					]
				},
				{"name": "Empty folder", "item": []}
			]
		},
		{
			"name": "Token",
			"request": {
				"auth": {"type": "basic", "basic": [{"key": "password", "value": "{{Secret}}"}, {"key": "username", "value": "{{Id}}"}]},
				"method": "POST",
				"header": [],
				"body": {"mode": "urlencoded", "urlencoded": [{"key": "grant_type", "value": "password", "type": "text"}, {"key": "scope", "value": "x", "disabled": true}]},
				"url": {"raw": "{{HostName}}/oauth/v1/tokens"}
			}
		},
		{"name": "  ", "request": "https://example.com/plain?a=1"},
		{"name": "Neither a folder nor a request"}
	]
}`

func TestImport(t *testing.T) {
	root, err := Import([]byte(sample))
	if err != nil {
		t.Fatal(err)
	}

	// The folders are as they are in Postman, under one for the collection.
	if root.Name != "Hotel APIs" || len(root.Folders) != 1 || len(root.Requests) != 2 {
		t.Fatalf("root = %q, %d folders, %d requests", root.Name, len(root.Folders), len(root.Requests))
	}
	accounts := root.Folders[0]
	if accounts.Name != "Accounts" || len(accounts.Folders) != 2 || accounts.Folders[0].Name != "Deeper" || accounts.Folders[1].Name != "Empty folder" {
		t.Fatalf("accounts = %+v", accounts)
	}

	check := func(got *collection.Request, name string, want rest.Request) {
		t.Helper()
		if got.Name != name || got.Type != collection.TypeREST || !reflect.DeepEqual(*got.Request, want) {
			t.Errorf("%s: got %q %+v\nwant %+v", name, got.Name, *got.Request, want)
		}
	}

	// Query parameters come from the list, which has those switched off
	// too, and not from the address. With no authorization of its own a
	// request takes that of the collection.
	check(accounts.Requests[0], "List accounts", rest.Request{
		Method: "GET",
		URL:    "{{HostName}}/ars/v1/accounts",
		Params: []rest.KeyValue{
			{Key: "accountId", Value: "{{AccountId}}"},
			{Key: "offset", Value: "0", Enabled: true},
			{Key: "limit", Value: "100", Enabled: true},
		},
		Headers: []rest.KeyValue{
			{Key: "x-app-key", Value: "{{AppKey}}", Enabled: true},
			{Key: "x-off", Value: "1"},
		},
		Auth: rest.Auth{Type: rest.AuthBearer, Token: "{{Token}}"},
	})

	// A folder can turn the authorization off for what is in it. Path
	// variables are filled in, or made placeholders.
	check(accounts.Folders[0].Requests[0], "Create account", rest.Request{
		Method:   "POST",
		URL:      "https://api.example.com/accounts/42/users/{{userId}}",
		Headers:  []rest.KeyValue{{Key: "Content-Type", Value: "application/json", Enabled: true}},
		BodyType: rest.BodyJSON,
		Body:     "{\n    \"name\": \"a\"\n}",
	})

	check(root.Requests[0], "Token", rest.Request{
		Method:   "POST",
		URL:      "{{HostName}}/oauth/v1/tokens",
		Auth:     rest.Auth{Type: rest.AuthBasic, Username: "{{Id}}", Password: "{{Secret}}"},
		BodyType: rest.BodyForm,
		Form:     []rest.KeyValue{{Key: "grant_type", Value: "password", Enabled: true}, {Key: "scope", Value: "x"}},
	})

	// A request can be just an address, and is named for what it does if
	// it has no name.
	check(root.Requests[1], "GET https://example.com/plain", rest.Request{
		Method: "GET",
		URL:    "https://example.com/plain",
		Params: []rest.KeyValue{{Key: "a", Value: "1", Enabled: true}},
		Auth:   rest.Auth{Type: rest.AuthBearer, Token: "{{Token}}"},
	})
}

func TestImportBodies(t *testing.T) {
	body := func(json string) rest.Request {
		t.Helper()
		root, err := Import([]byte(`{"info": {"schema": "https://schema.getpostman.com/json/collection/v2.0.0/collection.json"}, "item": [{"request": ` + json + `}]}`))
		if err != nil {
			t.Fatal(err)
		}
		if root.Name != "Imported Collection" {
			t.Errorf("name = %q", root.Name)
		}
		return *root.Requests[0].Request
	}

	tests := []struct {
		name     string
		request  string
		bodyType rest.BodyType
		body     string
	}{
		{"by header", `{"url": "x", "header": [{"key": "content-type", "value": "application/json; charset=utf-8"}], "body": {"mode": "raw", "raw": "[1]"}}`, rest.BodyJSON, "[1]"},
		{"by its look", `{"url": "x", "body": {"mode": "raw", "raw": "  {\"a\": 1}"}}`, rest.BodyJSON, `  {"a": 1}`},
		{"xml by its look", `{"url": "x", "body": {"mode": "raw", "raw": "<a/>"}}`, rest.BodyXML, "<a/>"},
		{"xml by language", `{"url": "x", "body": {"mode": "raw", "raw": "<a/>", "options": {"raw": {"language": "xml"}}}}`, rest.BodyXML, "<a/>"},
		{"text", `{"url": "x", "body": {"mode": "raw", "raw": "hello"}}`, rest.BodyText, "hello"},
		{"text by header", `{"url": "x", "header": [{"key": "Content-Type", "value": "text/plain"}], "body": {"mode": "raw", "raw": "{not json}"}}`, rest.BodyText, "{not json}"},
		{"graphql", `{"url": "x", "body": {"mode": "graphql", "graphql": {"query": "{ me }", "variables": "{\"a\": 1}"}}}`, rest.BodyJSON, "{\n  \"query\": \"{ me }\",\n  \"variables\": {\n    \"a\": 1\n  }\n}"},
		{"file", `{"url": "x", "body": {"mode": "file", "file": {"src": "a.bin"}}}`, "", ""},
		{"none", `{"url": "x"}`, "", ""},
	}
	for _, tt := range tests {
		if got := body(tt.request); got.BodyType != tt.bodyType || got.Body != tt.body {
			t.Errorf("%s: %q %q, want %q %q", tt.name, got.BodyType, got.Body, tt.bodyType, tt.body)
		}
	}

	// The text fields of a multipart form are kept, as a form.
	form := body(`{"url": "x", "body": {"mode": "formdata", "formdata": [{"key": "title", "value": "a", "type": "text"}, {"key": "upload", "type": "file", "src": "a.bin"}]}}`)
	if form.BodyType != rest.BodyForm || !reflect.DeepEqual(form.Form, []rest.KeyValue{{Key: "title", Value: "a", Enabled: true}}) {
		t.Errorf("form = %+v", form)
	}
}

func TestImportAuth(t *testing.T) {
	request := func(auth string) rest.Request {
		t.Helper()
		root, err := Import([]byte(`{"info": {"schema": "https://schema.getpostman.com/json/collection/v2.0.0/collection.json"}, "item": [{"request": {"url": "x", "auth": ` + auth + `}}]}`))
		if err != nil {
			t.Fatal(err)
		}
		return *root.Requests[0].Request
	}

	// Version 2.0 gives the settings as an object, not a list.
	if got := request(`{"type": "basic", "basic": {"username": "u", "password": "p"}}`).Auth; got != (rest.Auth{Type: rest.AuthBasic, Username: "u", Password: "p"}) {
		t.Errorf("basic = %+v", got)
	}
	if got := request(`{"type": "bearer"}`).Auth; got != (rest.Auth{Type: rest.AuthBearer}) {
		t.Errorf("bearer with no token = %+v", got)
	}
	if got := request(`{"type": "oauth2", "oauth2": [{"key": "accessToken", "value": "abc"}]}`).Auth; got != (rest.Auth{Type: rest.AuthBearer, Token: "abc"}) {
		t.Errorf("oauth2 = %+v", got)
	}
	if got := request(`{"type": "apikey", "apikey": [{"key": "key", "value": "X-Key"}, {"key": "value", "value": "{{k}}"}]}`); !reflect.DeepEqual(got.Headers, []rest.KeyValue{{Key: "X-Key", Value: "{{k}}", Enabled: true}}) || got.Auth != (rest.Auth{}) {
		t.Errorf("apikey in a header = %+v", got)
	}
	if got := request(`{"type": "apikey", "apikey": [{"key": "key", "value": "k"}, {"key": "value", "value": "v"}, {"key": "in", "value": "query"}]}`); !reflect.DeepEqual(got.Params, []rest.KeyValue{{Key: "k", Value: "v", Enabled: true}}) {
		t.Errorf("apikey in the query = %+v", got)
	}
	// Kinds with no equivalent are left out.
	if got := request(`{"type": "awsv4", "awsv4": [{"key": "accessKey", "value": "a"}]}`); got.Auth != (rest.Auth{}) || len(got.Headers) != 0 {
		t.Errorf("awsv4 = %+v", got)
	}
}

func TestImportErrors(t *testing.T) {
	tests := map[string]string{
		"not JSON":              "name: yaml",
		"not a collection":      `{"openapi": "3.0.0", "info": {"title": "x"}}`,
		"another schema":        `{"info": {"schema": "https://example.com/schema.json"}, "item": [{"request": "x"}]}`,
		"no requests":           `{"info": {"schema": "https://schema.getpostman.com/json/collection/v2.1.0/collection.json"}, "item": [{"name": "f", "item": []}]}`,
		"an empty file":         "",
		"a list, not an object": "[]",
	}
	for name, doc := range tests {
		if root, err := Import([]byte(doc)); err == nil {
			t.Errorf("%s: expected an error, got %+v", name, root)
		}
	}
}

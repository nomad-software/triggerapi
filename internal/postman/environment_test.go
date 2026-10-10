package postman

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/nomad-software/postie/internal/env"
)

func TestImportEnvironment(t *testing.T) {
	got, err := ImportEnvironment([]byte(`{
		"id": "82db5abc",
		"name": " Staging ",
		"values": [
			{"key": "host", "value": "api.test", "type": "default", "enabled": true},
			{"key": "token", "value": "abc"},
			{"key": "off", "value": "x", "enabled": false},
			{"key": "port", "value": 8080},
			{"key": "none", "value": null},
			{"key": "", "value": "nameless"}
		],
		"_postman_variable_scope": "environment"
	}`))
	if err != nil {
		t.Fatal(err)
	}

	want := env.Environment{Name: "Staging", Variables: []env.Variable{
		{Key: "host", Value: "api.test", Enabled: true},
		{Key: "token", Value: "abc", Enabled: true},
		{Key: "off", Value: "x"},
		{Key: "port", Value: "8080", Enabled: true},
		{Key: "none", Value: "", Enabled: true},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("environment = %+v\nwant %+v", got, want)
	}
}

func TestImportEnvironmentUnnamed(t *testing.T) {
	got, err := ImportEnvironment([]byte(`{"values": []}`))
	if err != nil || got.Name != "Imported Environment" || len(got.Variables) != 0 {
		t.Errorf("environment = %+v, %v", got, err)
	}
}

func TestImportEnvironmentErrors(t *testing.T) {
	tests := map[string]string{
		`{`:                                   "not valid JSON",
		`[]`:                                  "not valid JSON",
		`{"info": {"name": "A"}, "item": []}`: "not a Postman environment",
		`{"name": "A", "values": {}}`:         "not a Postman environment",
		`{"name": "A", "values": [1]}`:        "not a Postman environment",
	}
	for data, want := range tests {
		if _, err := ImportEnvironment([]byte(data)); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("ImportEnvironment(%s) = %v, want %q", data, err, want)
		}
	}
}

func TestImportEnvironmentSample(t *testing.T) {
	data, err := os.ReadFile("../../sample_imports/oracle-hospitality-distribution.postman_environment.json")
	if err != nil {
		t.Skip(err)
	}
	got, err := ImportEnvironment(data)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "ORACLE Hospitality Distribution APIs" || len(got.Variables) != 61 {
		t.Errorf("name = %q, %d variables", got.Name, len(got.Variables))
	}
	if v := got.Variables[0]; v != (env.Variable{Key: "chainCode", Value: "OH", Enabled: true}) {
		t.Errorf("first variable = %+v", v)
	}
}

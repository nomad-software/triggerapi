package rest

import (
	"reflect"
	"testing"
	"time"
)

func TestParseCurl(t *testing.T) {
	on := func(key, value string) KeyValue { return KeyValue{Key: key, Value: value, Enabled: true} }
	tests := []struct {
		name    string
		command string
		want    Request
	}{
		{
			"get",
			"curl https://api.test/users?q=a+b&n=1",
			Request{Method: "GET", URL: "https://api.test/users", Params: []KeyValue{on("q", "a b"), on("n", "1")}},
		},
		{
			"copied from a browser",
			"curl 'https://api.test/users' \\\n" +
				"  -H 'accept: application/json' \\\n" +
				"  -H 'authorization: Bearer abc' \\\n" +
				"  -H 'content-type: application/json' \\\n" +
				"  --data-raw $'{\"name\":\"O\\'Neil\"}' \\\n" +
				"  --compressed",
			Request{
				Method:   "POST",
				URL:      "https://api.test/users",
				Headers:  []KeyValue{on("accept", "application/json")},
				Auth:     Auth{Type: AuthBearer, Token: "abc"},
				BodyType: BodyJSON,
				Body:     `{"name":"O'Neil"}`,
			},
		},
		{
			"short options run together",
			"curl -sSLXPUT -ume:secret -H'X-A: 1' -d a=1 -d b=x%20y api.test",
			Request{
				Method:   "PUT",
				URL:      "api.test",
				Headers:  []KeyValue{on("X-A", "1")},
				Auth:     Auth{Type: AuthBasic, Username: "me", Password: "secret"},
				BodyType: BodyForm,
				Form:     []KeyValue{on("a", "1"), on("b", "x y")},
			},
		},
		{
			"data in the query",
			"curl -G --data-urlencode 'q=a b' --url https://api.test -o out.json",
			Request{Method: "GET", URL: "https://api.test", Params: []KeyValue{on("q", "a b")}},
		},
		{
			"json",
			`curl --json '{"a": 1}' https://api.test`,
			Request{
				Method:   "POST",
				URL:      "https://api.test",
				Headers:  []KeyValue{on("Accept", "application/json")},
				BodyType: BodyJSON,
				Body:     `{"a": 1}`,
			},
		},
		{
			"a content type of its own",
			`curl -X POST -H 'Content-Type: text/csv' -d 'a,b' -I -A agent -b 'k=v' https://api.test`,
			Request{
				Method:   "POST",
				URL:      "https://api.test",
				Headers:  []KeyValue{on("Content-Type", "text/csv"), on("User-Agent", "agent"), on("Cookie", "k=v")},
				BodyType: BodyText,
				Body:     "a,b",
			},
		},
		{
			"head and a header with no value",
			"curl --head -H 'Accept;' https://api.test",
			Request{Method: "HEAD", URL: "https://api.test", Headers: []KeyValue{on("Accept", "")}},
		},
		{
			"placeholders",
			"curl '{{host}}/users/{{id}}' -H 'Authorization: Bearer {{token}}'",
			Request{Method: "GET", URL: "{{host}}/users/{{id}}", Auth: Auth{Type: AuthBearer, Token: "{{token}}"}},
		},
	}
	for _, test := range tests {
		got, err := ParseCurl(test.command)
		if test.want.Auth.Type == "" {
			test.want.Auth.Type = AuthNone
		}
		if test.want.BodyType == "" {
			test.want.BodyType = BodyNone
		}
		if err != nil || !reflect.DeepEqual(got, test.want) {
			t.Errorf("%s: ParseCurl = %+v, %v, want %+v", test.name, got, err, test.want)
		}
	}
}

// A command made by Curl is read back into a request that makes the same
// command.
func TestParseCurlRoundTrip(t *testing.T) {
	for _, r := range []Request{
		{Method: "GET", URL: "https://api.test/users", Params: []KeyValue{{Key: "q", Value: "a b&c", Enabled: true}}},
		{Method: "DELETE", URL: "https://api.test/users/1", Headers: []KeyValue{{Key: "X-Key", Value: "it's", Enabled: true}, {Key: "Accept", Enabled: true}}},
		{Method: "PATCH", URL: "https://api.test", Auth: Auth{Type: AuthBearer, Token: "abc"}, BodyType: BodyJSON, Body: "{\n  \"name\": \"O'Neil\"\n}"},
		{Method: "PUT", URL: "https://api.test", Auth: Auth{Type: AuthBasic, Username: "me", Password: "p w"}, BodyType: BodyForm, Form: []KeyValue{{Key: "a", Value: "1 2", Enabled: true}}},
		{Method: "POST", URL: "https://api.test", BodyType: BodyXML, Body: "<a/>"},
		{Method: "OPTIONS", URL: "https://api.test", BodyType: BodyText, Body: "@not a file"},
	} {
		r.Options = Options{Timeout: 5 * time.Second}
		want, err := r.Curl()
		if err != nil {
			t.Fatal(err)
		}
		parsed, err := ParseCurl(want)
		if err != nil {
			t.Errorf("ParseCurl(%q): %v", want, err)
			continue
		}
		parsed.Options = r.Options
		if got, _ := parsed.Curl(); got != want {
			t.Errorf("ParseCurl(%q) makes %q", want, got)
		}
	}
}

func TestParseCurlErrors(t *testing.T) {
	for _, command := range []string{
		"",
		"wget https://api.test",
		"curl",
		"curl -s",
		"curl 'https://api.test",
		"curl -X TRACE https://api.test",
		"curl -d @body.json https://api.test",
		"curl -F a=1 https://api.test",
		"curl -T file https://api.test",
		"curl -H",
		"curl --data-urlencode name@file https://api.test",
	} {
		if r, err := ParseCurl(command); err == nil {
			t.Errorf("ParseCurl(%q) = %+v, with no error", command, r)
		}
	}
}

func TestIsCurl(t *testing.T) {
	for text, want := range map[string]bool{
		"curl https://api.test":   true,
		"  curl\\\n  -I x":        true,
		"curl":                    true,
		"curly.test":              false,
		"https://api.test?curl=1": false,
		"":                        false,
	} {
		if got := IsCurl(text); got != want {
			t.Errorf("IsCurl(%q) = %v", text, got)
		}
	}
}

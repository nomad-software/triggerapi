// Package postman turns a collection exported from Postman into saved
// requests. It reads the version 2.0 and 2.1 collection formats. It has no
// dependency on the UI.
package postman

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"

	"github.com/nomad-software/triggerapi/internal/collection"
	"github.com/nomad-software/triggerapi/internal/graphql"
	"github.com/nomad-software/triggerapi/internal/rest"
)

// exported is the part of a collection that requests are made from.
type exported struct {
	Info struct {
		Name   string `json:"name"`
		Schema string `json:"schema"`
	} `json:"info"`
	Item []item `json:"item"`
	Auth *auth  `json:"auth"`
}

// item is a folder, if it has items, or a request.
type item struct {
	Name    string   `json:"name"`
	Item    []item   `json:"item"`
	Request *request `json:"request"`
	Auth    *auth    `json:"auth"`
}

type request struct {
	Method string  `json:"method"`
	URL    address `json:"url"`
	Header []pair  `json:"header"`
	Body   *body   `json:"body"`
	Auth   *auth   `json:"auth"`
}

// UnmarshalJSON reads a request, which is either an object or just the
// address to get.
func (r *request) UnmarshalJSON(data []byte) error {
	var raw string
	if json.Unmarshal(data, &raw) == nil {
		*r = request{URL: address{Raw: raw}}
		return nil
	}

	// The plain type does not have this method, so reading it does not
	// come back here.
	type plain request
	return json.Unmarshal(data, (*plain)(r))
}

// address is the address of a request.
type address struct {
	Raw      string `json:"raw"`
	Query    []pair `json:"query"`
	Variable []pair `json:"variable"`
}

// UnmarshalJSON reads an address, which is either an object or a string.
func (a *address) UnmarshalJSON(data []byte) error {
	var raw string
	if json.Unmarshal(data, &raw) == nil {
		*a = address{Raw: raw}
		return nil
	}

	type plain address
	return json.Unmarshal(data, (*plain)(a))
}

// pair is a header, a query parameter, a form field or a setting of an
// authorization.
type pair struct {
	Key      string `json:"key"`
	Value    any    `json:"value"`
	Disabled bool   `json:"disabled"`
	// Type is "file" for a form field that uploads one.
	Type string `json:"type"`
}

// text returns the value of the pair, which is not always a string.
func (p pair) text() string {
	switch v := p.Value.(type) {
	case nil:
		return ""
	case string:
		return v
	}
	return fmt.Sprint(p.Value)
}

type body struct {
	Mode       string `json:"mode"`
	Raw        string `json:"raw"`
	URLEncoded []pair `json:"urlencoded"`
	FormData   []pair `json:"formdata"`
	GraphQL    *struct {
		Query     string `json:"query"`
		Variables string `json:"variables"`
	} `json:"graphql"`
	Options struct {
		Raw struct {
			Language string `json:"language"`
		} `json:"raw"`
	} `json:"options"`
}

// auth is an authorization. Its settings are under the name of its type, as
// a list of pairs in version 2.1 and as an object in version 2.0.
type auth struct {
	Type     string
	settings map[string]string
}

func (a *auth) UnmarshalJSON(data []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	if err := json.Unmarshal(fields["type"], &a.Type); err != nil {
		return nil
	}

	a.settings = map[string]string{}

	var list []pair
	if json.Unmarshal(fields[a.Type], &list) == nil {
		for _, p := range list {
			a.settings[p.Key] = p.text()
		}
		return nil
	}

	var object map[string]any
	if json.Unmarshal(fields[a.Type], &object) == nil {
		for key, value := range object {
			a.settings[key] = pair{Value: value}.text()
		}
	}
	return nil
}

// Import reads a Postman collection and returns a folder of requests for it,
// named after the collection, with its folders inside as they are in
// Postman.
//
// A request is given its method, address, query parameters, headers, body
// and authorization, which is that of the nearest folder above it if it has
// none of its own. Postman's {{variables}} are kept as they are, to be
// defined in an environment. Scripts, saved responses and descriptions are
// left out, as are requests with a method that is not one of rest.Methods.
// A request whose body is GraphQL becomes a GraphQL request, whatever its
// method.
func Import(data []byte) (*collection.Folder, error) {
	doc := &exported{}
	if err := json.Unmarshal(data, doc); err != nil {
		return nil, fmt.Errorf("the file could not be read as JSON: %w", err)
	}
	if !strings.Contains(doc.Info.Schema, "getpostman.com") && !strings.Contains(doc.Info.Schema, "postman.com") {
		return nil, errors.New("the file is not a Postman collection")
	}

	name := strings.TrimSpace(doc.Info.Name)
	if name == "" {
		name = "Imported Collection"
	}
	root := &collection.Folder{Name: name}

	if fill(root, doc.Item, doc.Auth) == 0 {
		return nil, errors.New("the collection does not have any requests")
	}
	return root, nil
}

// fill adds the items to a folder, and returns the number of requests added
// to it and to the folders inside it. The authorization is the one requests
// use if they have none of their own.
func fill(folder *collection.Folder, items []item, inherited *auth) int {
	count := 0

	for _, it := range items {
		authorization := inherited
		if it.Auth != nil {
			authorization = it.Auth
		}

		switch {
		case it.Request != nil:
			request := convert(it.Request, authorization)
			name := strings.TrimSpace(it.Name)

			// A GraphQL request is one of that type, whatever its method.
			if b := it.Request.Body; b != nil && b.Mode == "graphql" && b.GraphQL != nil {
				query := asGraphQL(request, b)
				if name == "" {
					name = "GraphQL " + query.URL
				}
				folder.Add(collection.NewGraphQL(name, query))
				count++
				continue
			}

			// A request with a method that is not offered cannot be
			// shown or sent as it is, so it is left out.
			if !slices.Contains(rest.Methods, request.Method) {
				continue
			}

			if name == "" {
				name = request.Method + " " + request.URL
			}
			folder.Add(collection.NewREST(name, request))
			count++

		case it.Item != nil:
			name := strings.TrimSpace(it.Name)
			if name == "" {
				name = "Folder"
			}
			count += fill(folder.AddFolder(name), it.Item, authorization)
		}
	}

	return count
}

// pathVariable matches a :name part of a path, which is how Postman writes a
// variable that belongs to one request.
var pathVariable = regexp.MustCompile(`(^|/):([A-Za-z_][A-Za-z0-9_]*)`)

// convert makes a request from one of Postman's.
func convert(r *request, inherited *auth) rest.Request {
	request := rest.Request{Method: strings.ToUpper(strings.TrimSpace(r.Method))}
	if request.Method == "" {
		request.Method = http.MethodGet
	}

	// The query parameters are listed apart from the address, which has
	// only those that are switched on.
	address, typed := rest.SplitURL(strings.TrimSpace(r.URL.Raw))
	request.Params = pairs(r.URL.Query)
	if r.URL.Query == nil {
		request.Params = typed
	}

	// A path variable is given its value, or made a placeholder if it has
	// none.
	values := map[string]string{}
	for _, v := range r.URL.Variable {
		values[v.Key] = v.text()
	}
	scheme, remainder, found := strings.Cut(address, "://")
	if !found {
		scheme, remainder = "", address
	}
	remainder = pathVariable.ReplaceAllStringFunc(remainder, func(match string) string {
		slash, name, _ := strings.Cut(match, ":")
		if value := values[name]; value != "" {
			return slash + value
		}
		return slash + "{{" + name + "}}"
	})
	request.URL = remainder
	if found {
		request.URL = scheme + "://" + remainder
	}

	request.Headers = pairs(r.Header)
	setBody(&request, r.Body)

	authorization := inherited
	if r.Auth != nil {
		authorization = r.Auth
	}
	setAuth(&request, authorization)

	return request
}

// pairs converts Postman's pairs, leaving out any with no key.
func pairs(from []pair) []rest.KeyValue {
	var out []rest.KeyValue
	for _, p := range from {
		if p.Key == "" {
			continue
		}
		out = append(out, rest.KeyValue{Key: p.Key, Value: p.text(), Enabled: !p.Disabled})
	}
	return out
}

// setBody gives the request the body of one of Postman's.
func setBody(request *rest.Request, b *body) {
	if b == nil {
		return
	}

	switch b.Mode {
	case "raw":
		// Line endings are made the same, as collections made on Windows
		// have both kinds.
		request.Body = strings.ReplaceAll(b.Raw, "\r\n", "\n")
		request.BodyType = rawType(b.Options.Raw.Language, request.Headers, request.Body)

	case "urlencoded":
		request.BodyType = rest.BodyForm
		request.Form = pairs(b.URLEncoded)

	case "formdata":
		// There is no multipart body, so the fields that are text are kept
		// as a form. Files to upload are left out.
		request.BodyType = rest.BodyForm
		for _, field := range b.FormData {
			if field.Type != "file" && field.Key != "" {
				request.Form = append(request.Form, rest.KeyValue{Key: field.Key, Value: field.text(), Enabled: !field.Disabled})
			}
		}
	}
}

// asGraphQL makes a GraphQL request of a request whose body is in Postman's
// GraphQL mode: its address, with the query parameters that are switched
// on, its headers and authorization, and the query and variables of the
// body.
func asGraphQL(request rest.Request, b *body) graphql.Request {
	return graphql.Request{
		URL:       rest.JoinURL(request.URL, request.Params),
		Headers:   request.Headers,
		Auth:      request.Auth,
		Query:     strings.ReplaceAll(b.GraphQL.Query, "\r\n", "\n"),
		Variables: strings.ReplaceAll(b.GraphQL.Variables, "\r\n", "\n"),
	}
}

// rawType works out what kind of text a raw body is: from the language
// Postman recorded for it, or else its Content-Type header, or else what it
// looks like.
func rawType(language string, headers []rest.KeyValue, text string) rest.BodyType {
	kind := strings.ToLower(language)
	if kind == "" || kind == "text" {
		for _, h := range headers {
			if h.Enabled && strings.EqualFold(h.Key, "Content-Type") {
				kind = strings.ToLower(h.Value)
				break
			}
		}
	}

	switch trimmed := strings.TrimSpace(text); {
	case strings.Contains(kind, "json"):
		return rest.BodyJSON
	case strings.Contains(kind, "xml"):
		return rest.BodyXML
	case kind != "":
		return rest.BodyText
	case strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "["):
		return rest.BodyJSON
	case strings.HasPrefix(trimmed, "<"):
		return rest.BodyXML
	}
	return rest.BodyText
}

// setAuth gives the request an authorization of Postman's. Kinds there is no
// equivalent for are left out.
func setAuth(request *rest.Request, a *auth) {
	if a == nil {
		return
	}

	switch strings.ToLower(a.Type) {
	case "basic":
		request.Auth = rest.Auth{Type: rest.AuthBasic, Username: a.settings["username"], Password: a.settings["password"]}

	case "bearer":
		request.Auth = rest.Auth{Type: rest.AuthBearer, Token: a.settings["token"]}

	case "oauth2":
		if o, ok := oauthSettings(a.settings); ok {
			request.Auth = rest.Auth{Type: rest.AuthOAuth2, OAuth: o}
			return
		}
		// A grant the program does not have, or one without its URLs, is
		// taken for the token it gave.
		token := a.settings["accessToken"]
		if token == "" {
			token = "{{token}}"
		}
		request.Auth = rest.Auth{Type: rest.AuthBearer, Token: token}

	case "apikey":
		key := rest.KeyValue{Key: a.settings["key"], Value: a.settings["value"], Enabled: true}
		if key.Key == "" {
			return
		}
		if strings.EqualFold(a.settings["in"], "query") {
			request.Params = append(request.Params, key)
		} else {
			request.Headers = append(request.Headers, key)
		}
	}
}

// oauthSettings returns how a token is got for Postman's settings of OAuth
// 2.0, which are for the client credentials grant or the authorization code
// one (which is Postman's default, and which the program always gives PKCE).
// A redirect URL is kept only if it is of this machine, as that is where the
// program is sent back to. It reports false for a grant the program does not
// have, or settings without the URLs of their grant.
func oauthSettings(settings map[string]string) (*rest.OAuth, bool) {
	o := &rest.OAuth{
		TokenURL:     settings["accessTokenUrl"],
		ClientID:     settings["clientId"],
		ClientSecret: settings["clientSecret"],
		Scopes:       settings["scope"],
	}
	switch settings["client_authentication"] {
	case "header":
		o.ClientAuth = rest.ClientAuthHeader
	case "body":
		o.ClientAuth = rest.ClientAuthBody
	}
	switch settings["grant_type"] {
	case "client_credentials":
	case "", "authorization_code", "authorization_code_with_pkce":
		o.Grant, o.AuthURL = rest.GrantAuthorizationCode, settings["authUrl"]
		if redirect, err := url.Parse(settings["redirect_uri"]); err == nil && redirect.Scheme == "http" && rest.LocalHost(redirect.Hostname()) {
			o.RedirectURL = settings["redirect_uri"]
		}
		if o.AuthURL == "" {
			return nil, false
		}
	default:
		return nil, false
	}
	if o.TokenURL == "" {
		return nil, false
	}
	return o, true
}

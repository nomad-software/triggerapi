package rest

import (
	"reflect"
	"strings"
	"testing"
)

func TestOAuthAuth(t *testing.T) {
	o := &OAuth{TokenURL: "{{x}}/token", ClientSecret: "{{x}}", Scopes: " a  b ", Params: "audience={{x}}&resource=r"}
	a := Auth{Type: AuthOAuth2, OAuth: o}

	// Expanding gives a copy, leaving the settings as they were.
	expanded := a.Expand(func(s string) string { return strings.ReplaceAll(s, "{{x}}", "1") })
	if expanded.OAuth == o || o.TokenURL != "{{x}}/token" || expanded.OAuth.TokenURL != "1/token" || expanded.OAuth.ClientSecret != "1" {
		t.Errorf("expanded %+v, original %+v", expanded.OAuth, o)
	}
	if got := expanded.Settings().ScopeList(); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Errorf("scopes = %q", got)
	}
	if params, err := expanded.Settings().ParamValues(); err != nil || params.Get("audience") != "1" || params.Get("resource") != "r" {
		t.Errorf("params = %v, %v", params, err)
	}

	// There is no header until there is a token.
	if _, ok := a.Header(); ok {
		t.Error("a header without a token")
	}
	a.Access = "tok"
	if header, ok := a.Header(); !ok || header != "Bearer tok" {
		t.Errorf("header = %q", header)
	}

	if (Auth{}).Settings() != (OAuth{}) || !(OAuth{}).IsZero() || o.IsZero() {
		t.Error("settings of nothing")
	}
}

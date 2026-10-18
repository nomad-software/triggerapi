package rest

import (
	"net/url"
	"strings"
)

// Grant is the way an OAuth 2.0 token is got.
type Grant string

const (
	// GrantClientCredentials gets a token for the client itself, with its
	// ID and secret. It is what the empty grant is taken as.
	GrantClientCredentials Grant = "client_credentials"
	// GrantAuthorizationCode has the user sign in in their browser, which
	// is sent back to the program with a code that is exchanged for a
	// token, with PKCE.
	GrantAuthorizationCode Grant = "authorization_code"
	// GrantDeviceCode has the user sign in in their browser by entering a
	// code the program is given, while it waits for the token.
	GrantDeviceCode Grant = "device_code"
)

// ClientAuth is how the client ID and secret are given to the token
// endpoint. The empty one has both ways tried.
type ClientAuth string

const (
	ClientAuthDetect ClientAuth = ""
	ClientAuthHeader ClientAuth = "header"
	ClientAuthBody   ClientAuth = "body"
)

// OAuth is what the oauth package gets an OAuth 2.0 token with. Everything
// in it can hold placeholders. Fields the grant does not use are kept, so
// that they are there if the grant is changed back.
type OAuth struct {
	Grant Grant `json:"grant,omitempty"`
	// TokenURL is where every grant gets its token, AuthURL the page the
	// user signs in at for the authorization code grant, and DeviceURL
	// where the device code grant gets its code.
	TokenURL  string `json:"tokenUrl,omitempty"`
	AuthURL   string `json:"authUrl,omitempty"`
	DeviceURL string `json:"deviceUrl,omitempty"`
	// RedirectURL is where the browser is sent back to after signing in,
	// which is an address of this machine, as some servers accept only one
	// that was registered with them. Empty is a free port of 127.0.0.1.
	RedirectURL  string `json:"redirectUrl,omitempty"`
	ClientID     string `json:"clientId,omitempty"`
	ClientSecret string `json:"clientSecret,omitempty"`
	// Scopes are separated by spaces, as they are sent.
	Scopes string `json:"scopes,omitempty"`
	// Params are the parameters the server wants besides those of OAuth,
	// such as audience or resource, in the form of a query string. They are
	// given when the user signs in and when a token is asked for.
	Params     string     `json:"params,omitempty"`
	ClientAuth ClientAuth `json:"clientAuth,omitempty"`
}

// LocalHost reports if a host is one of this machine, as the redirect URL of
// the authorization code grant has to be, for the program to be sent back
// to: 127.0.0.1, ::1 or localhost.
func LocalHost(host string) bool {
	switch host {
	case "127.0.0.1", "::1", "localhost":
		return true
	}
	return false
}

// Expand returns a copy of the settings with f applied to each of them.
func (o OAuth) Expand(f func(string) string) OAuth {
	o.TokenURL = f(o.TokenURL)
	o.AuthURL = f(o.AuthURL)
	o.DeviceURL = f(o.DeviceURL)
	o.RedirectURL = f(o.RedirectURL)
	o.ClientID = f(o.ClientID)
	o.ClientSecret = f(o.ClientSecret)
	o.Scopes = f(o.Scopes)
	o.Params = f(o.Params)
	return o
}

// IsZero reports if nothing has been filled in.
func (o OAuth) IsZero() bool {
	return o == OAuth{}
}

// ScopeList returns the scopes, one by one.
func (o OAuth) ScopeList() []string {
	return strings.Fields(o.Scopes)
}

// ParamValues returns the parameters besides those of OAuth.
func (o OAuth) ParamValues() (url.Values, error) {
	return url.ParseQuery(strings.TrimSpace(o.Params))
}

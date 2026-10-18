package oauth_test

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/nomad-software/triggerapi/internal/oauth"
	"github.com/nomad-software/triggerapi/internal/oauth/oauthtest"
	"github.com/nomad-software/triggerapi/internal/rest"
)

// browse is a SignIn which does what the user would in their browser: goes
// to the page, which for the test server signs them in at once.
func browse(t *testing.T) (oauth.SignIn, *[]string) {
	var codes []string
	return func(page, code string) error {
		codes = append(codes, code)
		res, err := http.Get(page)
		if err != nil {
			t.Errorf("browsing %s: %v", page, err)
			return nil
		}
		res.Body.Close()
		return nil
	}, &codes
}

func settings(s *oauthtest.Server, g rest.Grant) rest.OAuth {
	return rest.OAuth{
		Grant:        g,
		TokenURL:     s.URL + "/token",
		AuthURL:      s.URL + "/authorize",
		DeviceURL:    s.URL + "/device",
		ClientID:     oauthtest.ClientID,
		ClientSecret: oauthtest.ClientSecret,
		Scopes:       "read  write",
	}
}

func TestClientCredentials(t *testing.T) {
	s := oauthtest.New(t)
	c := oauth.NewCache()
	o := settings(s, "")
	o.Params = "audience=api"

	token, err := c.Token(context.Background(), nil, o, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !s.Valid(token.AccessToken) {
		t.Errorf("token %q was not given by the server", token.AccessToken)
	}
	if got := oauth.Scopes(token); got != "read write" {
		t.Errorf("scopes = %q", got)
	}
	form := s.Requests()[0]
	if form.Get("audience") != "api" || form.Get("grant_type") != "client_credentials" {
		t.Errorf("form = %v", form)
	}

	// It is kept, and used again.
	again, err := c.Token(context.Background(), nil, o, nil)
	if err != nil || again.AccessToken != token.AccessToken || s.Issued() != 1 {
		t.Errorf("again = %v, %v; issued %d", again, err, s.Issued())
	}
	if c.Kept(o) == nil {
		t.Error("the token is not kept")
	}

	// The secret is not part of what it is kept by.
	o.ClientSecret = "other"
	if c.Kept(o) == nil {
		t.Error("a change of secret lost the token")
	}

	// Fetch gets a new one.
	o.ClientSecret = oauthtest.ClientSecret
	fresh, err := c.Fetch(context.Background(), nil, o, nil)
	if err != nil || fresh.AccessToken == token.AccessToken {
		t.Errorf("fetch = %v, %v", fresh, err)
	}

	c.Forget(o)
	if c.Kept(o) != nil {
		t.Error("the token was not forgotten")
	}
}

func TestClientAuth(t *testing.T) {
	s := oauthtest.New(t)
	for _, style := range []rest.ClientAuth{rest.ClientAuthDetect, rest.ClientAuthHeader, rest.ClientAuthBody} {
		o := settings(s, rest.GrantClientCredentials)
		o.ClientAuth = style
		if _, err := oauth.NewCache().Token(context.Background(), nil, o, nil); err != nil {
			t.Errorf("%q: %v", style, err)
		}
	}
	form := s.Requests()[len(s.Requests())-1]
	if form.Get("client_secret") != oauthtest.ClientSecret {
		t.Errorf("the secret was not in the body: %v", form)
	}
}

func TestRefused(t *testing.T) {
	s := oauthtest.New(t)
	o := settings(s, "")
	o.ClientSecret = "wrong"
	o.ClientAuth = rest.ClientAuthBody
	_, err := oauth.NewCache().Token(context.Background(), nil, o, nil)
	if err == nil || !strings.Contains(err.Error(), "invalid_client (unknown client)") {
		t.Errorf("err = %v", err)
	}
}

func TestSettingsChecked(t *testing.T) {
	for _, test := range []struct {
		o    rest.OAuth
		want string
	}{
		{rest.OAuth{ClientID: "x"}, "token URL is empty"},
		{rest.OAuth{TokenURL: "http://x"}, "client ID is empty"},
		{rest.OAuth{TokenURL: "http://x", ClientID: "x", Grant: rest.GrantAuthorizationCode}, "authorization URL is empty"},
		{rest.OAuth{TokenURL: "http://x", ClientID: "x", Grant: rest.GrantDeviceCode}, "device authorization URL is empty"},
		{rest.OAuth{TokenURL: "http://x", ClientID: "x", Grant: "implicit"}, "not a grant"},
		{rest.OAuth{TokenURL: "http://x", ClientID: "x", Params: "a=%zz"}, "not a query string"},
	} {
		_, err := oauth.NewCache().Token(context.Background(), nil, test.o, nil)
		if err == nil || !strings.Contains(err.Error(), test.want) {
			t.Errorf("%+v: err = %v, want %q", test.o, err, test.want)
		}
	}
}

func TestAuthorizationCode(t *testing.T) {
	s := oauthtest.New(t)
	c := oauth.NewCache()
	o := settings(s, rest.GrantAuthorizationCode)

	// Without a way to sign in it cannot be had.
	if _, err := c.Token(context.Background(), nil, o, nil); !errors.Is(err, oauth.ErrSignIn) {
		t.Fatalf("err = %v", err)
	}

	signIn, codes := browse(t)
	token, err := c.Token(context.Background(), nil, o, signIn)
	if err != nil {
		t.Fatal(err)
	}
	if !s.Valid(token.AccessToken) || token.RefreshToken == "" {
		t.Errorf("token = %+v", token)
	}
	if len(*codes) != 1 || (*codes)[0] != "" {
		t.Errorf("codes = %q", *codes)
	}
	redirect, _ := url.Parse(s.Requests()[0].Get("redirect_uri"))
	if redirect.Hostname() != "127.0.0.1" || redirect.Port() == "" || redirect.Path != "/callback" {
		t.Errorf("redirect = %v", redirect)
	}

	// Once it has, it is not asked again.
	if _, err := c.Token(context.Background(), nil, o, nil); err != nil {
		t.Error(err)
	}
}

func TestAuthorizationDenied(t *testing.T) {
	s := oauthtest.New(t)
	s.Deny = true
	signIn, _ := browse(t)
	_, err := oauth.NewCache().Token(context.Background(), nil, settings(s, rest.GrantAuthorizationCode), signIn)
	if err == nil || !strings.Contains(err.Error(), "access_denied (The user said no)") {
		t.Errorf("err = %v", err)
	}
}

func TestAuthorizationCancelled(t *testing.T) {
	s := oauthtest.New(t)
	ctx, cancel := context.WithCancel(context.Background())
	// The user never signs in.
	signIn := func(page, code string) error { cancel(); return nil }
	_, err := oauth.NewCache().Token(ctx, nil, settings(s, rest.GrantAuthorizationCode), signIn)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v", err)
	}
}

func TestBrowserFails(t *testing.T) {
	s := oauthtest.New(t)
	signIn := func(page, code string) error { return errors.New("no browser") }
	_, err := oauth.NewCache().Token(context.Background(), nil, settings(s, rest.GrantAuthorizationCode), signIn)
	if err == nil || err.Error() != "no browser" {
		t.Errorf("err = %v", err)
	}
}

func TestRedirectURL(t *testing.T) {
	s := oauthtest.New(t)
	signIn, _ := browse(t)
	o := settings(s, rest.GrantAuthorizationCode)
	o.RedirectURL = "http://localhost/back"
	if _, err := oauth.NewCache().Token(context.Background(), nil, o, signIn); err != nil {
		t.Fatal(err)
	}
	redirect, _ := url.Parse(s.Requests()[0].Get("redirect_uri"))
	if redirect.Hostname() != "localhost" || redirect.Path != "/back" {
		t.Errorf("redirect = %v", redirect)
	}

	o.RedirectURL = "http://example.com/back"
	if _, err := oauth.NewCache().Token(context.Background(), nil, o, signIn); err == nil || !strings.Contains(err.Error(), "not of this machine") {
		t.Errorf("err = %v", err)
	}
}

func TestDeviceCode(t *testing.T) {
	s := oauthtest.New(t)
	signIn, codes := browse(t)
	token, err := oauth.NewCache().Token(context.Background(), nil, settings(s, rest.GrantDeviceCode), signIn)
	if err != nil {
		t.Fatal(err)
	}
	if !s.Valid(token.AccessToken) {
		t.Errorf("token = %+v", token)
	}
	if len(*codes) != 1 || (*codes)[0] != "USER-1" {
		t.Errorf("codes = %q", *codes)
	}
}

func TestRefresh(t *testing.T) {
	s := oauthtest.New(t)
	s.ExpiresIn = -time.Minute
	c := oauth.NewCache()
	o := settings(s, rest.GrantAuthorizationCode)
	signIn, codes := browse(t)
	first, err := c.Token(context.Background(), nil, o, signIn)
	if err != nil {
		t.Fatal(err)
	}

	// The token has expired, so it is refreshed, without signing in.
	second, err := c.Token(context.Background(), nil, o, nil)
	if err != nil {
		t.Fatal(err)
	}
	if second.AccessToken == first.AccessToken || s.Requests()[1].Get("grant_type") != "refresh_token" {
		t.Errorf("second = %+v", second)
	}

	// A refresh that fails has the user sign in again.
	s.Revoke()
	if _, err := c.Token(context.Background(), nil, o, nil); !errors.Is(err, oauth.ErrSignIn) {
		t.Errorf("err = %v", err)
	}
	if _, err := c.Token(context.Background(), nil, o, signIn); err != nil || len(*codes) != 2 {
		t.Errorf("err = %v, codes %q", err, *codes)
	}
}

func TestAuthorize(t *testing.T) {
	s := oauthtest.New(t)
	c := oauth.NewCache()

	other := rest.Auth{Type: rest.AuthBearer, Token: "x"}
	if err := c.Authorize(context.Background(), nil, &other, nil); err != nil || other.Access != "" {
		t.Errorf("bearer: %v, %+v", err, other)
	}

	o := settings(s, "")
	auth := rest.Auth{Type: rest.AuthOAuth2, OAuth: &o}
	if err := c.Authorize(context.Background(), nil, &auth, nil); err != nil {
		t.Fatal(err)
	}
	header, ok := auth.Header()
	if !ok || !s.Valid(strings.TrimPrefix(header, "Bearer ")) {
		t.Errorf("header = %q, %v", header, ok)
	}

	req, err := rest.Request{Method: "GET", URL: s.URL + "/resource", Auth: auth}.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Errorf("status = %d", res.StatusCode)
	}
}

func TestOneAtATime(t *testing.T) {
	s := oauthtest.New(t)
	c := oauth.NewCache()
	o := settings(s, "")
	errs := make(chan error)
	for range 5 {
		go func() {
			_, err := c.Token(context.Background(), nil, o, nil)
			errs <- err
		}()
	}
	for range 5 {
		if err := <-errs; err != nil {
			t.Error(err)
		}
	}
	if s.Issued() != 1 {
		t.Errorf("issued %d tokens", s.Issued())
	}
}

// Package oauth gets OAuth 2.0 access tokens for requests, by the client
// credentials, authorization code (with PKCE) and device code grants, and
// keeps them for as long as they last, refreshing them when they can be. It
// has no dependency on the UI: what the user has to do to sign in is handed
// to a SignIn.
package oauth

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/clientcredentials"

	"github.com/nomad-software/triggerapi/internal/rest"
)

// ErrSignIn is the error of a token that only the user can get, by signing
// in, when there is no SignIn to have them do it.
var ErrSignIn = errors.New("the OAuth 2.0 token needs you to sign in: press Get Token on the Authorization tab, or send the request")

// SignIn has the user sign in, at a page in their browser: for the device
// code grant by entering the code given there, which is empty for the
// authorization code grant. It returns once the page has been opened, or
// with the reason it could not be; the token is then waited for.
type SignIn func(page, code string) error

// DefaultRedirect is where the browser is sent back to after the user signs
// in, unless the settings say otherwise: the port is a free one, chosen each
// time.
const DefaultRedirect = "http://127.0.0.1/callback"

// Cache keeps the tokens that have been got, for each of the settings they
// were got with (see key), for the session. It is safe to use from more than
// one goroutine.
type Cache struct {
	mu     sync.Mutex
	tokens map[string]*oauth2.Token
	// busy has a channel for each of the settings a token is being got for,
	// which is held while it is, so that one is got at a time.
	busy map[string]chan struct{}
}

// NewCache returns a cache with no tokens.
func NewCache() *Cache {
	return &Cache{tokens: map[string]*oauth2.Token{}, busy: map[string]chan struct{}{}}
}

// key is what the tokens of settings are kept by: everything that makes a
// difference to the token given, which leaves out the client secret and how
// it is given.
func key(o rest.OAuth) string {
	data, _ := json.Marshal([]string{
		string(grant(o)), strings.TrimSpace(o.TokenURL), strings.TrimSpace(o.AuthURL), strings.TrimSpace(o.DeviceURL),
		strings.TrimSpace(o.ClientID), strings.Join(o.ScopeList(), " "), strings.TrimSpace(o.Params),
	})
	return string(data)
}

// grant returns the grant of the settings, which is the client credentials
// grant if it is not given.
func grant(o rest.OAuth) rest.Grant {
	if o.Grant == "" {
		return rest.GrantClientCredentials
	}
	return o.Grant
}

// Kept returns the token kept for the settings, which have had their
// placeholders replaced, whether or not it has expired, or nil if there is
// none. It asks for nothing.
func (c *Cache) Kept(o rest.OAuth) *oauth2.Token {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.tokens[key(o)]
}

// Forget drops the token kept for the settings.
func (c *Cache) Forget(o rest.OAuth) {
	c.mu.Lock()
	delete(c.tokens, key(o))
	c.mu.Unlock()
}

// Authorize gives an authorization of the OAuth 2.0 type its access token
// (see Token), and leaves any other as it is. Its placeholders must have been
// replaced.
func (c *Cache) Authorize(ctx context.Context, client *http.Client, auth *rest.Auth, signIn SignIn) error {
	if auth.Type != rest.AuthOAuth2 {
		return nil
	}
	token, err := c.Token(ctx, client, auth.Settings(), signIn)
	if err != nil {
		return err
	}
	auth.Access = token.AccessToken
	return nil
}

// Token returns a token for the settings, which have had their placeholders
// replaced: the one kept if it has not expired, or else that one refreshed
// if it can be, or else a new one, which for a grant that needs the user to
// sign in is got through signIn. Without one, that is ErrSignIn. The HTTP
// requests are made with client, nil being http.DefaultClient.
func (c *Cache) Token(ctx context.Context, client *http.Client, o rest.OAuth, signIn SignIn) (*oauth2.Token, error) {
	return c.get(ctx, client, o, signIn, false)
}

// Fetch returns a new token for the settings, as Token does when it has none
// kept, whatever is kept.
func (c *Cache) Fetch(ctx context.Context, client *http.Client, o rest.OAuth, signIn SignIn) (*oauth2.Token, error) {
	return c.get(ctx, client, o, signIn, true)
}

func (c *Cache) get(ctx context.Context, client *http.Client, o rest.OAuth, signIn SignIn, fresh bool) (*oauth2.Token, error) {
	k := key(o)
	release, err := c.hold(ctx, k)
	if err != nil {
		return nil, err
	}
	defer release()

	if client != nil {
		ctx = context.WithValue(ctx, oauth2.HTTPClient, client)
	}
	g, err := newGetter(o)
	if err != nil {
		return nil, err
	}

	if kept := c.Kept(o); kept != nil && !fresh {
		if kept.Valid() {
			return kept, nil
		}
		if kept.RefreshToken != "" {
			// A refresh that fails, as it does once the refresh token
			// has expired too, is followed by asking for a new token.
			if token, err := g.config.TokenSource(ctx, kept).Token(); err == nil {
				c.keep(k, token)
				return token, nil
			}
		}
	}

	token, err := g.token(ctx, signIn)
	if err != nil {
		return nil, describe(err)
	}
	c.keep(k, token)
	return token, nil
}

// hold waits until no token is being got for the settings of a key, or the
// context is done, and returns what lets the next one go ahead.
func (c *Cache) hold(ctx context.Context, k string) (func(), error) {
	c.mu.Lock()
	ch, ok := c.busy[k]
	if !ok {
		ch = make(chan struct{}, 1)
		c.busy[k] = ch
	}
	c.mu.Unlock()

	select {
	case ch <- struct{}{}:
		return func() { <-ch }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (c *Cache) keep(k string, token *oauth2.Token) {
	c.mu.Lock()
	c.tokens[k] = token
	c.mu.Unlock()
}

// getter gets a new token by the grant of settings.
type getter struct {
	settings rest.OAuth
	config   *oauth2.Config
	params   url.Values
}

// newGetter checks the settings and returns what gets a token with them.
func newGetter(o rest.OAuth) (*getter, error) {
	if strings.TrimSpace(o.TokenURL) == "" {
		return nil, errors.New("the OAuth 2.0 token URL is empty")
	}
	if strings.TrimSpace(o.ClientID) == "" {
		return nil, errors.New("the OAuth 2.0 client ID is empty")
	}
	switch grant(o) {
	case rest.GrantClientCredentials:
	case rest.GrantAuthorizationCode:
		if strings.TrimSpace(o.AuthURL) == "" {
			return nil, errors.New("the OAuth 2.0 authorization URL is empty")
		}
	case rest.GrantDeviceCode:
		if strings.TrimSpace(o.DeviceURL) == "" {
			return nil, errors.New("the OAuth 2.0 device authorization URL is empty")
		}
	default:
		return nil, fmt.Errorf("%q is not a grant of OAuth 2.0 that is supported", o.Grant)
	}
	params, err := o.ParamValues()
	if err != nil {
		return nil, fmt.Errorf("the extra parameters of OAuth 2.0 are not a query string: %w", err)
	}

	style := oauth2.AuthStyleAutoDetect
	switch o.ClientAuth {
	case rest.ClientAuthHeader:
		style = oauth2.AuthStyleInHeader
	case rest.ClientAuthBody:
		style = oauth2.AuthStyleInParams
	}
	config := &oauth2.Config{
		ClientID:     strings.TrimSpace(o.ClientID),
		ClientSecret: o.ClientSecret,
		Endpoint: oauth2.Endpoint{
			AuthURL:       strings.TrimSpace(o.AuthURL),
			DeviceAuthURL: strings.TrimSpace(o.DeviceURL),
			TokenURL:      strings.TrimSpace(o.TokenURL),
			AuthStyle:     style,
		},
		Scopes: o.ScopeList(),
	}
	return &getter{settings: o, config: config, params: params}, nil
}

// options returns the extra parameters as options of the requests.
func (g *getter) options() []oauth2.AuthCodeOption {
	var options []oauth2.AuthCodeOption
	for name, values := range g.params {
		for _, value := range values {
			options = append(options, oauth2.SetAuthURLParam(name, value))
		}
	}
	return options
}

// token gets a new token by the grant.
func (g *getter) token(ctx context.Context, signIn SignIn) (*oauth2.Token, error) {
	switch grant(g.settings) {
	case rest.GrantAuthorizationCode:
		if signIn == nil {
			return nil, ErrSignIn
		}
		return g.authorizationCode(ctx, signIn)
	case rest.GrantDeviceCode:
		if signIn == nil {
			return nil, ErrSignIn
		}
		return g.deviceCode(ctx, signIn)
	}
	config := clientcredentials.Config{
		ClientID:       g.config.ClientID,
		ClientSecret:   g.config.ClientSecret,
		TokenURL:       g.config.Endpoint.TokenURL,
		Scopes:         g.config.Scopes,
		EndpointParams: g.params,
		AuthStyle:      g.config.Endpoint.AuthStyle,
	}
	return config.Token(ctx)
}

// deviceCode gets a token by the device code grant: the user is given a code
// to enter at a page, and the token endpoint is asked until they have.
func (g *getter) deviceCode(ctx context.Context, signIn SignIn) (*oauth2.Token, error) {
	options := g.options()
	device, err := g.config.DeviceAuth(ctx, options...)
	if err != nil {
		return nil, err
	}
	page := device.VerificationURIComplete
	if page == "" {
		page = device.VerificationURI
	}
	if err := signIn(page, device.UserCode); err != nil {
		return nil, err
	}
	return g.config.DeviceAccessToken(ctx, device, options...)
}

// authorizationCode gets a token by the authorization code grant, with PKCE:
// the user signs in at the authorization URL in their browser, which is then
// sent to a server of this program on the redirect URL for as long as it
// takes, with the code to exchange for the token.
func (g *getter) authorizationCode(ctx context.Context, signIn SignIn) (*oauth2.Token, error) {
	redirect, listener, err := listen(g.settings.RedirectURL)
	if err != nil {
		return nil, err
	}
	config := *g.config
	config.RedirectURL = redirect.String()

	state := rand.Text()
	verifier := oauth2.GenerateVerifier()
	options := g.options()
	page := config.AuthCodeURL(state, append(options, oauth2.S256ChallengeOption(verifier))...)

	codes := make(chan callback, 1)
	server := &http.Server{
		Handler:           callbackHandler(redirect.Path, state, codes),
		ReadHeaderTimeout: 10 * time.Second,
	}
	go server.Serve(listener)
	defer server.Close()

	if err := signIn(page, ""); err != nil {
		return nil, err
	}

	var got callback
	select {
	case got = <-codes:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if got.err != nil {
		return nil, got.err
	}
	return config.Exchange(ctx, got.code, append(options, oauth2.VerifierOption(verifier))...)
}

// listen opens the server of the redirect URL, which has to be of this
// machine, and returns the URL with its port, which is a free one if it has
// none.
func listen(redirect string) (*url.URL, net.Listener, error) {
	redirect = strings.TrimSpace(redirect)
	if redirect == "" {
		redirect = DefaultRedirect
	}
	u, err := url.Parse(redirect)
	if err != nil {
		return nil, nil, fmt.Errorf("the OAuth 2.0 redirect URL is not valid: %w", err)
	}
	if u.Scheme != "http" {
		return nil, nil, fmt.Errorf("the OAuth 2.0 redirect URL %q is not http, as an address of this machine is", redirect)
	}
	host := u.Hostname()
	if !rest.LocalHost(host) {
		return nil, nil, fmt.Errorf("the OAuth 2.0 redirect URL %q is not of this machine: its host has to be 127.0.0.1, [::1] or localhost", redirect)
	}
	if host == "localhost" {
		host = "127.0.0.1"
	}
	port := u.Port()
	if port == "" {
		port = "0"
	}
	listener, err := net.Listen("tcp", net.JoinHostPort(host, port))
	if err != nil {
		return nil, nil, fmt.Errorf("the OAuth 2.0 redirect URL %q cannot be listened on: %w", redirect, err)
	}
	if u.Port() == "" {
		_, actual, _ := net.SplitHostPort(listener.Addr().String())
		u.Host = net.JoinHostPort(u.Hostname(), actual)
	}
	if u.Path == "" {
		u.Path = "/"
	}
	return u, listener, nil
}

// callback is what the browser was sent back with: a code, or the reason
// there is none.
type callback struct {
	code string
	err  error
}

// callbackHandler answers the browser when it is sent back to the redirect
// URL, handing on the first code or error it was given with the state that
// was sent.
func callbackHandler(path, state string, codes chan<- callback) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != path {
			http.NotFound(w, r)
			return
		}
		query := r.URL.Query()
		var got callback
		switch {
		case query.Get("state") != state:
			// Not an answer to what was asked, so it is not taken.
			http.Error(w, "This is not the sign-in TriggerAPI is waiting for.", http.StatusBadRequest)
			return
		case query.Get("error") != "":
			got.err = &signInError{query.Get("error"), query.Get("error_description")}
		case query.Get("code") == "":
			got.err = errors.New("the browser was sent back without an authorization code")
		default:
			got.code = query.Get("code")
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if got.err != nil {
			fmt.Fprintf(w, page, "Signing in failed", "TriggerAPI was not given a token. You can close this tab.")
		} else {
			fmt.Fprintf(w, page, "Signed in", "TriggerAPI has been given the code for its token. You can close this tab.")
		}
		select {
		case codes <- got:
		default:
		}
	})
}

// page is the page the browser is shown once it has been sent back.
const page = `<!doctype html><html><head><meta charset="utf-8"><title>TriggerAPI</title></head>` +
	`<body style="font-family: sans-serif; margin: 3em"><h1>%s</h1><p>%s</p></body></html>`

// signInError is the error the authorization server sent the browser back
// with.
type signInError struct {
	code, description string
}

func (e *signInError) Error() string {
	if e.description != "" {
		return fmt.Sprintf("signing in failed: %s (%s)", e.code, e.description)
	}
	return "signing in failed: " + e.code
}

// describe makes the error of a token endpoint readable: the error the
// endpoint gave, or the status it answered with.
func describe(err error) error {
	var retrieve *oauth2.RetrieveError
	if !errors.As(err, &retrieve) {
		return err
	}
	switch {
	case retrieve.ErrorCode != "" && retrieve.ErrorDescription != "":
		return fmt.Errorf("the OAuth 2.0 server refused: %s (%s)", retrieve.ErrorCode, retrieve.ErrorDescription)
	case retrieve.ErrorCode != "":
		return fmt.Errorf("the OAuth 2.0 server refused: %s", retrieve.ErrorCode)
	case retrieve.Response != nil:
		body := strings.TrimSpace(string(retrieve.Body))
		if len(body) > 200 {
			body = body[:200] + "…"
		}
		if body != "" {
			return fmt.Errorf("the OAuth 2.0 server answered %s: %s", retrieve.Response.Status, body)
		}
		return fmt.Errorf("the OAuth 2.0 server answered %s", retrieve.Response.Status)
	}
	return err
}

// Scopes returns the scopes a token was given, if the server said.
func Scopes(token *oauth2.Token) string {
	scope, _ := token.Extra("scope").(string)
	return scope
}

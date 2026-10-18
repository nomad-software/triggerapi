package ui

import (
	"context"
	"errors"
	"fmt"
	"html"
	"net/http"
	"time"

	qt "github.com/mappu/miqt/qt6"
	"github.com/mappu/miqt/qt6/mainthread"

	"golang.org/x/oauth2"

	"github.com/nomad-software/triggerapi/internal/collection"
	"github.com/nomad-software/triggerapi/internal/network"
	"github.com/nomad-software/triggerapi/internal/oauth"
	"github.com/nomad-software/triggerapi/internal/rest"
)

// How the window gets the OAuth 2.0 tokens of requests, which are kept in
// Window.tokens for the session, and shows what token there is on the
// Authorization tab of a request UI.

// openBrowser opens a page in the user's browser, reporting false if it
// could not. It is called from a goroutine, never the main thread, and is a
// variable so that tests can go to the page themselves.
var openBrowser = func(page string) bool {
	var opened bool
	mainthread.Wait(func() {
		u := qt.NewQUrl3(page)
		opened = qt.QDesktopServices_OpenUrl(u)
		u.Delete()
	})
	return opened
}

// tokenClient returns the client tokens are got with: the one requests are
// sent with, which each request to the authorization server is given the
// time limit of the settings by. Signing in has none, as it takes as long as
// the user does.
func (w *Window) tokenClient() *http.Client {
	client := *w.client
	client.Timeout = w.timeout()
	return &client
}

// authorizer returns what gives an authorization of OAuth 2.0 its token
// before the request of a request UI is sent, having the user sign in if
// need be, which its Authorization tab tells them how to do. It is nil for
// any other authorization. The authorization has had its placeholders
// replaced.
func (w *Window) authorizer(ui *requestUI, auth *rest.Auth) func(context.Context) error {
	if auth.Type != rest.AuthOAuth2 {
		return nil
	}
	client, signIn := w.tokenClient(), w.signIn(ui)
	return func(ctx context.Context) error {
		err := w.tokens.Authorize(ctx, client, auth, signIn)
		mainthread.Start(func() { w.showToken(ui) })
		return err
	}
}

// backgroundAuthorizer returns what gives an authorization of OAuth 2.0 the
// token there is, or one that can be got without the user, as is done for
// what is asked of a server in the background. A token that needs the user
// to sign in is oauth.ErrSignIn. Any other authorization is left as it is.
func (w *Window) backgroundAuthorizer() func(context.Context, *rest.Auth) error {
	client := w.tokenClient()
	return func(ctx context.Context, auth *rest.Auth) error {
		return w.tokens.Authorize(ctx, client, auth, nil)
	}
}

// signIn returns what has the user sign in for a token, at a page in their
// browser, which the Authorization tab of a request UI links to as well, in
// case it could not be opened.
func (w *Window) signIn(ui *requestUI) oauth.SignIn {
	editor := ui.editor.authorization()
	return func(page, code string) error {
		opened := openBrowser(page)
		mainthread.Start(func() { editor.showToken(signInText(page, code, opened), false) })
		return nil
	}
}

// signInText says what the user has to do to sign in at a page, as rich
// text: enter the code there, unless it is empty. It says whether the page
// was opened in the browser.
func signInText(page, code string, opened bool) string {
	link := `<a href="` + html.EscapeString(page) + `">`
	var text string
	if code != "" {
		text = "Enter the code <b>" + html.EscapeString(code) + "</b> at " + link + html.EscapeString(page) + "</a>"
	} else {
		text = "Sign in at " + link + "the sign-in page</a>"
	}
	if opened {
		return text + ", which has been opened in your browser."
	}
	return text + ". Your browser could not be opened."
}

// oauthSettings returns the settings of OAuth 2.0 of the request on show in a
// request UI, with the variables of the active environment in place by
// expand, and reports false if its authorization is not of OAuth 2.0.
func (w *Window) oauthSettings(ui *requestUI, expand func(string) string) (rest.OAuth, bool) {
	auth := ui.editor.authorization().auth()
	if auth.Type != rest.AuthOAuth2 {
		return rest.OAuth{}, false
	}
	return auth.Settings().Expand(expand), true
}

// showToken says on the Authorization tab of a request UI what token there
// is for the request on show, and has the curl command of a REST request
// show it.
func (w *Window) showToken(ui *requestUI) {
	if ui.typ == collection.TypeREST {
		w.request.updateCurl()
	}
	o, ok := w.oauthSettings(ui, w.expand)
	if !ok {
		return
	}
	token := w.tokens.Kept(o)
	ui.editor.authorization().showToken(tokenText(token, time.Now()), token != nil)
}

// tokenText says what a token is, which is nil if there is none, at a time:
// how long it lasts and what scopes it was given, or that it has expired.
// It is rich text.
func tokenText(token *oauth2.Token, now time.Time) string {
	switch {
	case token == nil:
		return "There is no token yet. One is got when the request is sent, or with Get Token."
	case !token.Valid():
		if token.RefreshToken != "" {
			return "The token has expired. It is refreshed when the request is sent."
		}
		return "The token has expired. Another is got when the request is sent."
	}
	text := "There is a token, which does not expire."
	if !token.Expiry.IsZero() {
		text = fmt.Sprintf("There is a token until %s (%s).", token.Expiry.Format("15:04"), lasting(token.Expiry.Sub(now)))
	}
	if scopes := oauth.Scopes(token); scopes != "" {
		text += " Its scopes are " + html.EscapeString(scopes) + "."
	}
	return text
}

// lasting says how long there is left of a time, in minutes or hours.
func lasting(left time.Duration) string {
	switch {
	case left < time.Minute:
		return "less than a minute left"
	case left < 2*time.Hour:
		return plural(int(left/time.Minute), "minute") + " left"
	}
	return plural(int(left/time.Hour), "hour") + " left"
}

// getToken gets a new token for the request on show in a request UI, having
// the user sign in if need be, or cancels the one being got.
func (w *Window) getToken(ui *requestUI) {
	editor := ui.editor.authorization()
	if editor.cancel != nil {
		editor.cancel()
		return
	}
	// A value made by a function is made once, for the whole token.
	o, ok := w.oauthSettings(ui, w.expander())
	if !ok {
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	editor.fetching(cancel)
	editor.problem.set("")
	client, signIn := w.tokenClient(), w.signIn(ui)
	go func() {
		_, err := w.tokens.Fetch(ctx, client, o, signIn)
		err = network.Explain(err)
		mainthread.Start(func() {
			cancel()
			editor.fetching(nil)
			w.showToken(ui)
			if err != nil && !errors.Is(err, context.Canceled) {
				editor.problem.set(err.Error())
			}
		})
	}()
}

// clearToken forgets the token of the request on show in a request UI.
func (w *Window) clearToken(ui *requestUI) {
	if o, ok := w.oauthSettings(ui, w.expand); ok {
		w.tokens.Forget(o)
	}
	w.showToken(ui)
}

// keptToken gives an authorization of OAuth 2.0, which has had its
// placeholders replaced, the token there is for it if it has not expired,
// without getting one, as the curl command shows it.
func (w *Window) keptToken(auth *rest.Auth) {
	if auth.Type != rest.AuthOAuth2 {
		return
	}
	if token := w.tokens.Kept(auth.Settings()); token != nil && token.Valid() {
		auth.Access = token.AccessToken
	}
}

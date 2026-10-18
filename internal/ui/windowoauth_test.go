package ui

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/mappu/miqt/qt6/mainthread"
	"golang.org/x/oauth2"

	"github.com/nomad-software/triggerapi/internal/collection"
	"github.com/nomad-software/triggerapi/internal/mcp/mcptest"
	"github.com/nomad-software/triggerapi/internal/oauth/oauthtest"
	"github.com/nomad-software/triggerapi/internal/rest"
)

// oauthAuth returns an authorization of OAuth 2.0 by a grant of the test
// server.
func oauthAuth(s *oauthtest.Server, grant rest.Grant) rest.Auth {
	return rest.Auth{Type: rest.AuthOAuth2, OAuth: &rest.OAuth{
		Grant:        grant,
		TokenURL:     s.URL + "/token",
		AuthURL:      s.URL + "/authorize",
		DeviceURL:    s.URL + "/device",
		ClientID:     oauthtest.ClientID,
		ClientSecret: oauthtest.ClientSecret,
		Scopes:       "read",
	}}
}

// browseForUser has the pages the user is sent to sign in at gone to, as
// they would be in their browser, until the test ends.
func browseForUser(t *testing.T) {
	prev := openBrowser
	openBrowser = func(page string) bool {
		if res, err := http.Get(page); err == nil {
			res.Body.Close()
		}
		return true
	}
	t.Cleanup(func() { openBrowser = prev })
}

func tokenState(w *Window, a *authEditor) string {
	return mainthread.Wait2(a.tokenState.Text)
}

func TestOAuthClientCredentials(t *testing.T) {
	s := oauthtest.New(t)
	var w *Window
	mainthread.Wait(func() {
		w = NewWindow(testFiles(t).Files)
		w.Show()
		w.request.url.SetText(s.URL + "/resource")
		w.request.auth.load(oauthAuth(s, ""))
		if !strings.Contains(w.request.auth.tokenState.Text(), "no token yet") || w.request.auth.clearToken.IsEnabled() {
			t.Errorf("before: %q", w.request.auth.tokenState.Text())
		}
		w.request.send.Click()
	})
	waitFor(t, "the response", func() bool { return fieldValue(w.response, "Status") == "200 OK" })
	if got := tokenState(w, w.request.auth); !strings.Contains(got, "There is a token until") || !strings.Contains(got, "Its scopes are read.") {
		t.Errorf("after: %q", got)
	}

	// The token is used again.
	mainthread.Wait(func() { w.request.send.Click() })
	waitFor(t, "the second response", func() bool {
		return mainthread.Wait2(func() bool { return w.cancel == nil }) && fieldValue(w.response, "Status") == "200 OK"
	})
	if s.Issued() != 1 {
		t.Errorf("issued %d tokens", s.Issued())
	}

	mainthread.Wait(func() {
		if !w.request.auth.clearToken.IsEnabled() {
			t.Error("Clear Token is not enabled")
		}
		w.request.auth.clearToken.Click()
		if !strings.Contains(w.request.auth.tokenState.Text(), "no token yet") || w.request.auth.clearToken.IsEnabled() {
			t.Errorf("cleared: %q", w.request.auth.tokenState.Text())
		}
	})
}

func TestOAuthSignIn(t *testing.T) {
	browseForUser(t)
	for _, grant := range []rest.Grant{rest.GrantAuthorizationCode, rest.GrantDeviceCode} {
		t.Run(string(grant), func(t *testing.T) {
			s := oauthtest.New(t)
			var w *Window
			mainthread.Wait(func() {
				w = NewWindow(testFiles(t).Files)
				w.Show()
				w.request.url.SetText(s.URL + "/resource")
				w.request.auth.load(oauthAuth(s, grant))
				w.request.send.Click()
			})
			waitFor(t, "the response", func() bool { return fieldValue(w.response, "Status") == "200 OK" })
			waitFor(t, "the token", func() bool { return strings.Contains(tokenState(w, w.request.auth), "There is a token") })
		})
	}
}

func TestGetToken(t *testing.T) {
	s := oauthtest.New(t)
	var w *Window
	a := func() *authEditor { return w.request.auth }
	mainthread.Wait(func() {
		w = NewWindow(testFiles(t).Files)
		auth := oauthAuth(s, "")
		auth.OAuth.ClientSecret = "wrong"
		auth.OAuth.ClientAuth = rest.ClientAuthBody
		a().load(auth)
		a().getToken.Click()
		if a().getToken.Text() != "Cancel" {
			t.Errorf("button = %q", a().getToken.Text())
		}
	})
	waitFor(t, "the problem", func() bool {
		return mainthread.Wait2(func() bool {
			return strings.Contains(a().problem.Text(), "invalid_client") && a().getToken.Text() == "Get Token"
		})
	})

	mainthread.Wait(func() {
		a().clientSecret.SetText(oauthtest.ClientSecret)
		if a().problem.IsVisible() {
			t.Error("the problem is still shown")
		}
		a().getToken.Click()
	})
	waitFor(t, "the token", func() bool { return strings.Contains(tokenState(w, a()), "There is a token") })

	// Get Token gets a new one, whatever there is.
	mainthread.Wait(func() { a().getToken.Click() })
	waitFor(t, "another token", func() bool { return s.Issued() == 2 && mainthread.Wait2(func() bool { return a().cancel == nil }) })
}

func TestOAuthCurl(t *testing.T) {
	s := oauthtest.New(t)
	var w *Window
	command := func() string { return mainthread.Wait2(w.request.command.ToPlainText) }
	mainthread.Wait(func() {
		w = NewWindow(testFiles(t).Files)
		w.request.url.SetText(s.URL + "/resource")
		w.request.auth.load(oauthAuth(s, ""))
		w.request.curl.Click()
	})
	if got := command(); !strings.HasPrefix(got, "# There is no OAuth 2.0 token yet") || strings.Contains(got, "Authorization") {
		t.Errorf("without a token: %q", got)
	}

	mainthread.Wait(func() { w.request.auth.getToken.Click() })
	waitFor(t, "the token in the command", func() bool { return strings.Contains(command(), "Authorization: Bearer token-1") })
	if strings.HasPrefix(command(), "#") {
		t.Errorf("with a token: %q", command())
	}
}

// What is asked of a server in the background does not have the user sign
// in, but says they have to.
func TestOAuthInBackground(t *testing.T) {
	s := oauthtest.New(t)
	srv := mcptest.NewServer(t)
	var w *Window
	mainthread.Wait(func() {
		w = NewWindow(testFiles(t).Files)
		w.Show()
		defer func(prev collection.Type) { testRequestType = prev }(testRequestType)
		testRequestType = collection.TypeMCP
		w.requests.addRequest()
		w.mcp.auth.load(oauthAuth(s, rest.GrantAuthorizationCode))
		w.mcp.url.SetText(srv.URL + "/mcp")
		w.mcp.reload.Click()
	})
	waitFor(t, "the reason", func() bool {
		return mainthread.Wait2(func() bool { return strings.Contains(w.mcp.status.Text(), "needs you to sign in") })
	})

	// Once there is a token it is used.
	browseForUser(t)
	mainthread.Wait(func() { w.mcp.auth.getToken.Click() })
	waitFor(t, "the token", func() bool { return strings.Contains(tokenState(w, w.mcp.auth), "There is a token") })
	mainthread.Wait(func() { w.mcp.reload.Click() })
	waitFor(t, "the capabilities", func() bool { return mainthread.Wait2(w.mcp.endpoint.count) == 4 })
}

func TestOAuthWebSocket(t *testing.T) {
	s := oauthtest.New(t)
	srv := wsGreeter(t)
	var w *Window
	mainthread.Wait(func() {
		w = NewWindow(testFiles(t).Files)
		w.Show()
		defer func(prev collection.Type) { testRequestType = prev }(testRequestType)
		testRequestType = collection.TypeWebSocket
		w.requests.addRequest()
		w.ws.url.SetText(srv.URL)
		w.ws.auth.load(oauthAuth(s, ""))
		w.ws.connect.Click()
	})
	waitFor(t, "the greeting", func() bool {
		return mainthread.Wait2(func() bool { return strings.Contains(w.wsResponse.body.ToPlainText(), "hello Bearer token-1") })
	})
	mainthread.Wait(w.disconnectWS)
}

// wsGreeter accepts WebSocket connections, greeting each with the
// Authorization header of its handshake.
func wsGreeter(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		conn.Write(ctx, websocket.MessageText, []byte("hello "+r.Header.Get("Authorization")))
		conn.Read(ctx)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestTokenText(t *testing.T) {
	now := time.Now()
	until := func(d time.Duration) time.Time { return now.Add(d) }
	scoped := (&oauth2.Token{AccessToken: "a", Expiry: until(42*time.Minute + 30*time.Second)}).WithExtra(map[string]any{"scope": "read <write>"})
	for _, test := range []struct {
		token *oauth2.Token
		want  string
	}{
		{nil, "There is no token yet. One is got when the request is sent, or with Get Token."},
		{&oauth2.Token{AccessToken: "a"}, "There is a token, which does not expire."},
		{scoped, fmt.Sprintf("There is a token until %s (42 minutes left). Its scopes are read &lt;write&gt;.", scoped.Expiry.Format("15:04"))},
		{&oauth2.Token{AccessToken: "a", Expiry: until(5 * time.Hour)}, fmt.Sprintf("There is a token until %s (5 hours left).", until(5*time.Hour).Format("15:04"))},
		{&oauth2.Token{AccessToken: "a", Expiry: until(5 * time.Second)}, "The token has expired. Another is got when the request is sent."},
		{&oauth2.Token{AccessToken: "a", Expiry: until(-time.Hour), RefreshToken: "r"}, "The token has expired. It is refreshed when the request is sent."},
	} {
		if got := tokenText(test.token, now); got != test.want {
			t.Errorf("tokenText = %q, want %q", got, test.want)
		}
	}
	if got := lasting(90 * time.Second); got != "1 minute left" {
		t.Errorf("lasting = %q", got)
	}
	if got := lasting(10 * time.Second); got != "less than a minute left" {
		t.Errorf("lasting = %q", got)
	}
}

func TestSignInText(t *testing.T) {
	if got := signInText("https://x/verify?a=1&b=2", "AB-CD", true); got != `Enter the code <b>AB-CD</b> at <a href="https://x/verify?a=1&amp;b=2">https://x/verify?a=1&amp;b=2</a>, which has been opened in your browser.` {
		t.Errorf("device = %q", got)
	}
	if got := signInText("https://x/auth", "", false); got != `Sign in at <a href="https://x/auth">the sign-in page</a>. Your browser could not be opened.` {
		t.Errorf("code = %q", got)
	}
}

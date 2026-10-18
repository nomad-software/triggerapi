package ui

import (
	"context"

	qt "github.com/mappu/miqt/qt6"

	"github.com/nomad-software/triggerapi/internal/oauth"
	"github.com/nomad-software/triggerapi/internal/rest"
)

// authTypes are the entries of the auth type selector, in the order they are
// shown.
var authTypes = []authTypeEntry{
	{"No Auth", rest.AuthNone},
	{"Basic Auth", rest.AuthBasic},
	{"Bearer Token", rest.AuthBearer},
	{"OAuth 2.0", rest.AuthOAuth2},
}

// grants are the entries of the selector of the OAuth 2.0 grant, in the order
// they are shown. The client credentials grant is the empty one, which is
// what it is taken as, so that a request with nothing of OAuth filled in is
// saved without it.
var grants = []grantEntry{
	{"Client Credentials", ""},
	{"Authorization Code (PKCE)", rest.GrantAuthorizationCode},
	{"Device Code", rest.GrantDeviceCode},
}

// clientAuths are the entries of the selector of how the client ID and
// secret are given to the token endpoint.
var clientAuths = []clientAuthEntry{
	{"Detect", rest.ClientAuthDetect},
	{"Basic Auth Header", rest.ClientAuthHeader},
	{"Request Body", rest.ClientAuthBody},
}

type authTypeEntry struct {
	label string
	typ   rest.AuthType
}

type grantEntry struct {
	label string
	grant rest.Grant
}

type clientAuthEntry struct {
	label string
	auth  rest.ClientAuth
}

// authEditor edits the authorization of a request.
type authEditor struct {
	root *qt.QWidget

	typ      *qt.QComboBox
	username *qt.QLineEdit
	password *qt.QLineEdit
	token    *qt.QLineEdit

	// The fields of OAuth 2.0, those of URLs being shown only for the grants
	// that use them.
	grant        *qt.QComboBox
	tokenURL     *qt.QLineEdit
	authURL      *qt.QLineEdit
	deviceURL    *qt.QLineEdit
	redirectURL  *qt.QLineEdit
	clientID     *qt.QLineEdit
	clientSecret *qt.QLineEdit
	scopes       *qt.QLineEdit
	params       *qt.QLineEdit
	clientAuth   *qt.QComboBox
	oauthForm    *qt.QFormLayout
	// tokenState says what token there is, or what the user has to do to
	// sign in, and problem why one could not be got. getToken gets a new
	// token, and reads Cancel while it does (cancel is then set), and
	// clearToken forgets the one there is.
	tokenState *qt.QLabel
	problem    statusLine
	getToken   *qt.QPushButton
	clearToken *qt.QPushButton
	cancel     context.CancelFunc

	// onChange is called after any of the fields change, as is onEdited,
	// which the window has show the token for what is in them.
	onChange func()
	onEdited func()
}

// newAuthEditor creates the editor. The placeholders in its fields are
// coloured by the highlighter. The password is shown as it is typed, like the
// token, so that a placeholder in it can be read.
func newAuthEditor(highlight *placeholderHighlighter) *authEditor {
	a := &authEditor{
		typ:          qt.NewQComboBox2(),
		username:     qt.NewQLineEdit2(),
		password:     qt.NewQLineEdit2(),
		token:        qt.NewQLineEdit2(),
		grant:        qt.NewQComboBox2(),
		tokenURL:     qt.NewQLineEdit2(),
		authURL:      qt.NewQLineEdit2(),
		deviceURL:    qt.NewQLineEdit2(),
		redirectURL:  qt.NewQLineEdit2(),
		clientID:     qt.NewQLineEdit2(),
		clientSecret: qt.NewQLineEdit2(),
		scopes:       qt.NewQLineEdit2(),
		params:       qt.NewQLineEdit2(),
		clientAuth:   qt.NewQComboBox2(),
		tokenState:   qt.NewQLabel2(),
		problem:      newStatusLine(),
		getToken:     qt.NewQPushButton3("Get Token"),
		clearToken:   qt.NewQPushButton3("Clear Token"),
	}

	for _, t := range authTypes {
		a.typ.AddItem(t.label)
	}
	for _, g := range grants {
		a.grant.AddItem(g.label)
	}
	for _, c := range clientAuths {
		a.clientAuth.AddItem(c.label)
	}

	for _, edit := range a.edits() {
		edit.OnTextChanged(func(string) { a.changed() })
		highlight.lineEdit(edit)
	}
	a.redirectURL.SetPlaceholderText(oauth.DefaultRedirect + " on a free port")
	a.scopes.SetPlaceholderText("Separated by spaces")
	a.params.SetPlaceholderText("audience=…&resource=…")

	basic := qt.NewQWidget2()
	basicForm := qt.NewQFormLayout(basic)
	basicForm.AddRow3("Username", a.username.QWidget)
	basicForm.AddRow3("Password", a.password.QWidget)

	bearer := qt.NewQWidget2()
	bearerForm := qt.NewQFormLayout(bearer)
	bearerForm.AddRow3("Token", a.token.QWidget)

	// Page order matches authTypes.
	stack := qt.NewQStackedWidget2()
	stack.AddWidget(newHint("This request does not use any authorization."))
	stack.AddWidget(basic)
	stack.AddWidget(bearer)
	stack.AddWidget(a.newOAuthPage())

	a.typ.OnCurrentIndexChanged(func(index int) {
		stack.SetCurrentIndex(index)
		a.changed()
	})
	a.grant.OnCurrentIndexChanged(func(int) {
		a.showGrantFields()
		a.changed()
	})
	a.clientAuth.OnCurrentIndexChanged(func(int) { a.changed() })
	a.showGrantFields()

	a.root = newSelectorTab("Type", a.typ, stack)

	return a
}

// edits returns the fields that are typed into.
func (a *authEditor) edits() []*qt.QLineEdit {
	return []*qt.QLineEdit{a.username, a.password, a.token,
		a.tokenURL, a.authURL, a.deviceURL, a.redirectURL, a.clientID, a.clientSecret, a.scopes, a.params}
}

// newOAuthPage returns the page of OAuth 2.0: its settings, and beneath them
// the token there is and the buttons which get and clear it. It scrolls, as
// it is taller than the room a request is often given.
func (a *authEditor) newOAuthPage() *qt.QWidget {
	page := qt.NewQWidget2()
	a.oauthForm = qt.NewQFormLayout2()
	a.oauthForm.AddRow3("Grant", a.grant.QWidget)
	a.oauthForm.AddRow3("Token URL", a.tokenURL.QWidget)
	a.oauthForm.AddRow3("Authorization URL", a.authURL.QWidget)
	a.oauthForm.AddRow3("Device Authorization URL", a.deviceURL.QWidget)
	a.oauthForm.AddRow3("Redirect URL", a.redirectURL.QWidget)
	a.oauthForm.AddRow3("Client ID", a.clientID.QWidget)
	a.oauthForm.AddRow3("Client Secret", a.clientSecret.QWidget)
	a.oauthForm.AddRow3("Scopes", a.scopes.QWidget)
	a.oauthForm.AddRow3("Extra Parameters", a.params.QWidget)
	a.oauthForm.AddRow3("Client Authentication", a.clientAuth.QWidget)

	// The state of the token is rich text, for the link to the page the
	// user signs in at, which opens in the browser.
	a.tokenState.SetWordWrap(true)
	a.tokenState.SetTextFormat(qt.RichText)
	a.tokenState.SetOpenExternalLinks(true)
	a.tokenState.SetTextInteractionFlags(qt.TextBrowserInteraction)

	buttons := qt.NewQHBoxLayout2()
	buttons.AddWidget(a.getToken.QWidget)
	buttons.AddWidget(a.clearToken.QWidget)
	buttons.AddStretch()

	// The state of the token is beneath the form, across the whole page:
	// a style which keeps the fields of a form at their size hint (Breeze,
	// for one) would squeeze it, as wrapped text, into a narrow column cut
	// off after its first line.
	layout := qt.NewQVBoxLayout(page)
	layout.AddLayout(a.oauthForm.QLayout)
	layout.AddWidget(a.tokenState.QWidget)
	layout.AddWidget(a.problem.QWidget)
	layout.AddLayout(buttons.QLayout)
	layout.AddStretch()

	scroll := qt.NewQScrollArea2()
	scroll.SetFrameShape(qt.QFrame__NoFrame)
	scroll.SetWidgetResizable(true)
	scroll.SetWidget(plainPage(page))
	plainPage(scroll.Viewport())
	return scroll.QWidget
}

// showGrantFields shows the fields of URLs the grant chosen uses, and hides
// the others.
func (a *authEditor) showGrantFields() {
	grant := grants[a.grant.CurrentIndex()].grant
	a.oauthForm.SetRowVisible2(a.authURL.QWidget, grant == rest.GrantAuthorizationCode)
	a.oauthForm.SetRowVisible2(a.redirectURL.QWidget, grant == rest.GrantAuthorizationCode)
	a.oauthForm.SetRowVisible2(a.deviceURL.QWidget, grant == rest.GrantDeviceCode)
}

func (a *authEditor) changed() {
	if a.onChange != nil {
		a.onChange()
	}
	if a.onEdited != nil {
		a.onEdited()
	}
}

// auth reads the fields. Those the chosen type does not use are included, so
// they are kept if the type is changed back. The settings of OAuth are left
// out while there is nothing in them, as in a request from before there was
// OAuth.
func (a *authEditor) auth() rest.Auth {
	auth := rest.Auth{
		Type:     authTypes[a.typ.CurrentIndex()].typ,
		Username: a.username.Text(),
		Password: a.password.Text(),
		Token:    a.token.Text(),
	}
	o := rest.OAuth{
		Grant:        grants[a.grant.CurrentIndex()].grant,
		TokenURL:     a.tokenURL.Text(),
		AuthURL:      a.authURL.Text(),
		DeviceURL:    a.deviceURL.Text(),
		RedirectURL:  a.redirectURL.Text(),
		ClientID:     a.clientID.Text(),
		ClientSecret: a.clientSecret.Text(),
		Scopes:       a.scopes.Text(),
		Params:       a.params.Text(),
		ClientAuth:   clientAuths[a.clientAuth.CurrentIndex()].auth,
	}
	if !o.IsZero() {
		auth.OAuth = &o
	}
	return auth
}

// load shows the authorization in the fields. An unknown type falls back to
// the first entry of the selector, as do an unknown grant and way of giving
// the client secret.
func (a *authEditor) load(auth rest.Auth) {
	o := auth.Settings()
	a.typ.SetCurrentIndex(selectorIndex(authTypes, func(t authTypeEntry) bool { return t.typ == auth.Type }))
	a.username.SetText(auth.Username)
	a.password.SetText(auth.Password)
	a.token.SetText(auth.Token)
	a.grant.SetCurrentIndex(selectorIndex(grants, func(g grantEntry) bool { return g.grant == o.Grant }))
	a.tokenURL.SetText(o.TokenURL)
	a.authURL.SetText(o.AuthURL)
	a.deviceURL.SetText(o.DeviceURL)
	a.redirectURL.SetText(o.RedirectURL)
	a.clientID.SetText(o.ClientID)
	a.clientSecret.SetText(o.ClientSecret)
	a.scopes.SetText(o.Scopes)
	a.params.SetText(o.Params)
	a.clientAuth.SetCurrentIndex(selectorIndex(clientAuths, func(c clientAuthEntry) bool { return c.auth == o.ClientAuth }))
}

// showToken says what token there is, which is rich text, and whether there
// is one to clear, taking away any problem shown.
func (a *authEditor) showToken(state string, kept bool) {
	a.tokenState.SetText(state)
	a.clearToken.SetEnabled(kept)
	a.problem.set("")
}

// fetching turns Get Token into Cancel while a token is got, which cancel
// stops, or back again when it is nil.
func (a *authEditor) fetching(cancel context.CancelFunc) {
	a.cancel = cancel
	if cancel != nil {
		a.getToken.SetText("Cancel")
	} else {
		a.getToken.SetText("Get Token")
	}
}

// recolour gives the problem the colour of errors in the settings.
func (a *authEditor) recolour() {
	a.problem.recolour()
}

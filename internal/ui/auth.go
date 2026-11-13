package ui

import (
	"github.com/nomad-software/qute/qtcore"
	"github.com/nomad-software/qute/qtwidgets"

	"context"

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
	root *qtwidgets.QWidget

	typ      *qtwidgets.QComboBox
	username *qtwidgets.QLineEdit
	password *qtwidgets.QLineEdit
	token    *qtwidgets.QLineEdit

	// The fields of OAuth 2.0, those of URLs being shown only for the grants
	// that use them.
	grant        *qtwidgets.QComboBox
	tokenURL     *qtwidgets.QLineEdit
	authURL      *qtwidgets.QLineEdit
	deviceURL    *qtwidgets.QLineEdit
	redirectURL  *qtwidgets.QLineEdit
	clientID     *qtwidgets.QLineEdit
	clientSecret *qtwidgets.QLineEdit
	scopes       *qtwidgets.QLineEdit
	params       *qtwidgets.QLineEdit
	clientAuth   *qtwidgets.QComboBox
	oauthForm    *qtwidgets.QFormLayout
	// tokenState says what token there is, or what the user has to do to
	// sign in, and problem why one could not be got. getToken gets a new
	// token, and reads Cancel while it does (cancel is then set), and
	// clearToken forgets the one there is.
	tokenState *qtwidgets.QLabel
	problem    statusLine
	getToken   *qtwidgets.QPushButton
	clearToken *qtwidgets.QPushButton
	cancel     context.CancelFunc

	// onChange is called after the type, grant or client authentication
	// is chosen; what is typed into the fields is reported through the
	// state of the panel, once it is committed. onEdited is called after
	// any of them change, typed or not, and has the window show the token
	// for what is in them.
	onChange func()
	onEdited func()
}

// newAuthEditor creates the editor of the panel whose state is given. The
// placeholders in its fields are coloured by the highlighter of the state.
// The password is shown as it is typed, like the token, so that a
// placeholder in it can be read.
func newAuthEditor(state *panelState) *authEditor {
	a := &authEditor{
		typ:          qtwidgets.NewQComboBox(nil),
		username:     qtwidgets.NewQLineEdit(nil),
		password:     qtwidgets.NewQLineEdit(nil),
		token:        qtwidgets.NewQLineEdit(nil),
		grant:        qtwidgets.NewQComboBox(nil),
		tokenURL:     qtwidgets.NewQLineEdit(nil),
		authURL:      qtwidgets.NewQLineEdit(nil),
		deviceURL:    qtwidgets.NewQLineEdit(nil),
		redirectURL:  qtwidgets.NewQLineEdit(nil),
		clientID:     qtwidgets.NewQLineEdit(nil),
		clientSecret: qtwidgets.NewQLineEdit(nil),
		scopes:       qtwidgets.NewQLineEdit(nil),
		params:       qtwidgets.NewQLineEdit(nil),
		clientAuth:   qtwidgets.NewQComboBox(nil),
		tokenState:   qtwidgets.NewQLabel(nil),
		problem:      newStatusLine(),
		getToken:     qtwidgets.NewQPushButtonWithText("Get Token", nil),
		clearToken:   qtwidgets.NewQPushButtonWithText("Clear Token", nil),
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
		state.typedLine(edit)
		edit.OnTextChanged(func(string) { a.edited() })
		state.highlight.lineEdit(edit)
	}
	a.redirectURL.SetPlaceholderText(oauth.DefaultRedirect + " on a free port")
	a.scopes.SetPlaceholderText("Separated by spaces")
	a.params.SetPlaceholderText("audience=…&resource=…")

	basic := qtwidgets.NewQWidget(nil)
	basicForm := qtwidgets.NewQFormLayout(basic)
	basicForm.AddRowText("Username", a.username.AsQWidget())
	basicForm.AddRowText("Password", a.password.AsQWidget())

	bearer := qtwidgets.NewQWidget(nil)
	bearerForm := qtwidgets.NewQFormLayout(bearer)
	bearerForm.AddRowText("Token", a.token.AsQWidget())

	// Page order matches authTypes.
	stack := qtwidgets.NewQStackedWidget(nil)
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
func (a *authEditor) edits() []*qtwidgets.QLineEdit {
	return []*qtwidgets.QLineEdit{a.username, a.password, a.token,
		a.tokenURL, a.authURL, a.deviceURL, a.redirectURL, a.clientID, a.clientSecret, a.scopes, a.params}
}

// newOAuthPage returns the page of OAuth 2.0: its settings, and beneath them
// the token there is and the buttons which get and clear it. It scrolls, as
// it is taller than the room a request is often given.
func (a *authEditor) newOAuthPage() *qtwidgets.QWidget {
	page := qtwidgets.NewQWidget(nil)
	a.oauthForm = qtwidgets.NewQFormLayout(nil)
	a.oauthForm.AddRowText("Grant", a.grant.AsQWidget())
	a.oauthForm.AddRowText("Token URL", a.tokenURL.AsQWidget())
	a.oauthForm.AddRowText("Authorization URL", a.authURL.AsQWidget())
	a.oauthForm.AddRowText("Device Authorization URL", a.deviceURL.AsQWidget())
	a.oauthForm.AddRowText("Redirect URL", a.redirectURL.AsQWidget())
	a.oauthForm.AddRowText("Client ID", a.clientID.AsQWidget())
	a.oauthForm.AddRowText("Client Secret", a.clientSecret.AsQWidget())
	a.oauthForm.AddRowText("Scopes", a.scopes.AsQWidget())
	a.oauthForm.AddRowText("Extra Parameters", a.params.AsQWidget())
	a.oauthForm.AddRowText("Client Authentication", a.clientAuth.AsQWidget())

	// The state of the token is rich text, for the link to the page the
	// user signs in at, which opens in the browser.
	a.tokenState.SetWordWrap(true)
	a.tokenState.SetTextFormat(qtcore.RichText)
	a.tokenState.SetOpenExternalLinks(true)
	a.tokenState.SetTextInteractionFlags(qtcore.TextBrowserInteraction)

	buttons := qtwidgets.NewQHBoxLayout(nil)
	buttons.AddWidget(a.getToken.AsQWidget())
	buttons.AddWidget(a.clearToken.AsQWidget())
	buttons.AddStretch()

	// The state of the token is beneath the form, across the whole page:
	// a style which keeps the fields of a form at their size hint (Breeze,
	// for one) would squeeze it, as wrapped text, into a narrow column cut
	// off after its first line.
	layout := qtwidgets.NewQVBoxLayout(page)
	layout.AddLayout(a.oauthForm.AsQLayout())
	layout.AddWidget(a.tokenState.AsQWidget())
	layout.AddWidget(a.problem.AsQWidget())
	layout.AddLayout(buttons.AsQLayout())
	layout.AddStretch()

	scroll := qtwidgets.NewQScrollArea(nil)
	scroll.SetFrameShape(qtwidgets.QFrameNoFrame)
	scroll.SetWidgetResizable(true)
	scroll.SetWidget(plainPage(page))
	plainPage(scroll.Viewport())
	return scroll.AsQWidget()
}

// showGrantFields shows the fields of URLs the grant chosen uses, and hides
// the others.
func (a *authEditor) showGrantFields() {
	grant := grants[a.grant.CurrentIndex()].grant
	a.oauthForm.SetRowVisibleOf(a.authURL.AsQWidget(), grant == rest.GrantAuthorizationCode)
	a.oauthForm.SetRowVisibleOf(a.redirectURL.AsQWidget(), grant == rest.GrantAuthorizationCode)
	a.oauthForm.SetRowVisibleOf(a.deviceURL.AsQWidget(), grant == rest.GrantDeviceCode)
}

// changed reports a choice of the type, the grant or the client
// authentication.
func (a *authEditor) changed() {
	if a.onChange != nil {
		a.onChange()
	}
	a.edited()
}

// edited has the token shown for what is in the fields.
func (a *authEditor) edited() {
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

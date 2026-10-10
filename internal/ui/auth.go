package ui

import (
	qt "github.com/mappu/miqt/qt6"

	"github.com/nomad-software/triggerapi/internal/rest"
)

// authTypes are the entries of the auth type selector, in the order they are
// shown.
var authTypes = []struct {
	label string
	typ   rest.AuthType
}{
	{"No Auth", rest.AuthNone},
	{"Basic Auth", rest.AuthBasic},
	{"Bearer Token", rest.AuthBearer},
}

// authEditor edits the authorization of a request.
type authEditor struct {
	root *qt.QWidget

	typ      *qt.QComboBox
	username *qt.QLineEdit
	password *qt.QLineEdit
	token    *qt.QLineEdit

	// onChange is called after any of the fields change.
	onChange func()
}

// newAuthEditor creates the editor. The placeholders in its fields are
// coloured by the highlighter.
func newAuthEditor(highlight *placeholderHighlighter) *authEditor {
	a := &authEditor{
		typ:      qt.NewQComboBox2(),
		username: qt.NewQLineEdit2(),
		password: qt.NewQLineEdit2(),
		token:    qt.NewQLineEdit2(),
	}

	for _, t := range authTypes {
		a.typ.AddItem(t.label)
	}
	a.password.SetEchoMode(qt.QLineEdit__Password)

	for _, edit := range []*qt.QLineEdit{a.username, a.password, a.token} {
		edit.OnTextChanged(func(string) { a.changed() })
	}
	// The password is masked, so there is nothing to colour in it.
	highlight.lineEdit(a.username)
	highlight.lineEdit(a.token)

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

	a.typ.OnCurrentIndexChanged(func(index int) {
		stack.SetCurrentIndex(index)
		a.changed()
	})

	a.root = newSelectorTab("Type", a.typ, stack)

	return a
}

func (a *authEditor) changed() {
	if a.onChange != nil {
		a.onChange()
	}
}

// auth reads the fields. Those the chosen type does not use are included, so
// they are kept if the type is changed back.
func (a *authEditor) auth() rest.Auth {
	return rest.Auth{
		Type:     authTypes[a.typ.CurrentIndex()].typ,
		Username: a.username.Text(),
		Password: a.password.Text(),
		Token:    a.token.Text(),
	}
}

// load shows the authorization in the fields. An unknown type falls back to
// the first entry of the selector.
func (a *authEditor) load(auth rest.Auth) {
	index := 0
	for i, t := range authTypes {
		if t.typ == auth.Type {
			index = i
		}
	}
	a.typ.SetCurrentIndex(index)
	a.username.SetText(auth.Username)
	a.password.SetText(auth.Password)
	a.token.SetText(auth.Token)
}

package ui

import (
	"reflect"
	"testing"

	qt "github.com/mappu/miqt/qt6"
	"github.com/mappu/miqt/qt6/mainthread"

	"github.com/nomad-software/triggerapi/internal/rest"
)

func TestAuthEditor(t *testing.T) {
	mainthread.Wait(func() {
		a := newAuthEditor(new(newPanelState()))

		// With nothing of OAuth filled in, there are no settings of it,
		// as in a request from before there was OAuth.
		bearer := rest.Auth{Type: rest.AuthBearer, Token: "t"}
		a.load(bearer)
		if got := a.auth(); !reflect.DeepEqual(got, bearer) {
			t.Errorf("bearer = %+v", got)
		}

		o := rest.OAuth{
			Grant: rest.GrantDeviceCode, TokenURL: "{{idp}}/token", AuthURL: "{{idp}}/auth", DeviceURL: "{{idp}}/device",
			RedirectURL: "http://localhost:8080/back", ClientID: "app", ClientSecret: "{{secret}}",
			Scopes: "read write", Params: "audience=api", ClientAuth: rest.ClientAuthHeader,
		}
		auth := rest.Auth{Type: rest.AuthOAuth2, Username: "kept", OAuth: &o}
		a.load(auth)
		if got := a.auth(); !reflect.DeepEqual(got, auth) {
			t.Errorf("oauth = %+v %+v", got, got.OAuth)
		}

		// Only the URLs the grant uses are shown.
		for i, want := range []struct{ auth, redirect, device bool }{
			{false, false, false},
			{true, true, false},
			{false, false, true},
		} {
			a.grant.SetCurrentIndex(i)
			got := struct{ auth, redirect, device bool }{
				a.oauthForm.IsRowVisibleWithWidget(a.authURL.QWidget),
				a.oauthForm.IsRowVisibleWithWidget(a.redirectURL.QWidget),
				a.oauthForm.IsRowVisibleWithWidget(a.deviceURL.QWidget),
			}
			if got != want {
				t.Errorf("grant %d shows %+v", i, got)
			}
		}

		// The client credentials grant is the one with no name, which is
		// what an unknown one is taken as.
		a.load(rest.Auth{Type: rest.AuthOAuth2, OAuth: &rest.OAuth{Grant: "implicit", TokenURL: "x"}})
		if got := a.auth().OAuth; a.grant.CurrentIndex() != 0 || got.Grant != "" {
			t.Errorf("unknown grant = %q at %d", got.Grant, a.grant.CurrentIndex())
		}
	})
}

// The state of the token is as wide as the page, even in a style which keeps
// the fields of a form at their size hint, where it would otherwise be
// squeezed into a narrow column and cut off.
func TestTokenStateWidth(t *testing.T) {
	var a *authEditor
	mainthread.Wait(func() {
		a = newAuthEditor(new(newPanelState()))
		a.oauthForm.SetFieldGrowthPolicy(qt.QFormLayout__FieldsStayAtSizeHint)
		a.typ.SetCurrentIndex(3) // OAuth 2.0
		a.showToken("There is no token yet. One is got when the request is sent, or with Get Token.", false)
		a.root.Resize(900, 600)
		a.root.Show()
	})
	waitFor(t, "the layout", func() bool {
		return mainthread.Wait2(func() bool { return a.tokenState.Width() > 0 })
	})
	mainthread.Wait(func() {
		page := a.tokenState.ParentWidget()
		if a.tokenState.Width() < page.Width()*3/4 {
			t.Errorf("the token state is %d wide on a page of %d", a.tokenState.Width(), page.Width())
		}
		a.root.Hide()
	})
}

package ui

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	qt "github.com/mappu/miqt/qt6"
	"github.com/mappu/miqt/qt6/mainthread"

	"github.com/nomad-software/triggerapi/internal/network"
	"github.com/nomad-software/triggerapi/internal/rest"
	"github.com/nomad-software/triggerapi/internal/settings"
)

// replaceNetworkSecret has the passphrases and passwords the network settings
// ask for given as answer gives them, counting how often each is asked for.
func replaceNetworkSecret(t *testing.T, answer func(network.Secret) string) map[network.Secret]int {
	asked := map[network.Secret]int{}
	previous := askNetworkSecret
	askNetworkSecret = func(_ *qt.QWidget, s network.Secret) (string, error) {
		asked[s]++
		return answer(s), nil
	}
	t.Cleanup(func() { askNetworkSecret = previous })
	return asked
}

func TestNetworkSettings(t *testing.T) {
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, password, _ := strings.Cut(r.Header.Get("Proxy-Authorization"), " ")
		w.Write([]byte("proxied for " + r.URL.Host + " " + user + " " + password))
	}))
	defer proxy.Close()
	secure := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("secure"))
	}))
	defer secure.Close()
	asked := replaceNetworkSecret(t, func(network.Secret) string { return "pw" })

	var w *Window
	mainthread.Wait(func() {
		w = NewWindow(testFiles(t).Files)
		w.Show()

		// The server's certificate is not one the system trusts, and the
		// error says what can be done.
		w.request.load(rest.Request{Method: "GET", URL: secure.URL})
		w.request.send.Click()
	})
	waitFor(t, "the failure", func() bool { return fieldValue(w.response, fieldError) != "" })

	mainthread.Wait(func() {
		if got := fieldValue(w.response, fieldError); !strings.Contains(got, "signed by an authority that is not trusted") {
			t.Errorf("error = %q", got)
		}

		// A proxy with a user, which the server is reached through, and a
		// host whose certificate is not checked, reached directly.
		s := w.config
		s.Network = network.Config{
			Proxy:   strings.Replace(proxy.URL, "http://", "http://gary@", 1),
			NoProxy: []string{"127.0.0.1"},
			Hosts:   []network.Host{{Pattern: "127.0.0.1", Insecure: true}},
		}
		if err := w.applySettings(s); err != nil {
			t.Fatal(err)
		}
		if len(asked) != 1 {
			t.Errorf("asked %v", asked)
		}
		if saved := settings.Load(w.files.Settings).Network; saved.Proxy != s.Network.Proxy {
			t.Errorf("saved %+v", saved)
		}
		w.request.send.Click()
	})
	waitFor(t, "the response", func() bool { return fieldValue(w.response, fieldCode) == "200 OK" })

	mainthread.Wait(func() {
		// The command is made while it is on show, and stays on show.
		w.request.curl.Click()
		if !strings.Contains(w.response.timing.details.Text(), "Not checked") || !strings.Contains(w.request.command.ToPlainText(), "--insecure") {
			t.Errorf("details %q, command %s", w.response.timing.details.Text(), w.request.command.ToPlainText())
		}

		w.request.load(rest.Request{Method: "GET", URL: "http://example.invalid/"})
		w.request.send.Click()
	})
	waitFor(t, "the proxied response", func() bool { return strings.Contains(w.response.body.ToPlainText(), "proxied") })

	mainthread.Wait(func() {
		if got := w.response.body.ToPlainText(); !strings.Contains(got, "example.invalid Basic") {
			t.Errorf("body = %q", got)
		}
		if details := w.response.timing.details.Text(); !strings.Contains(details, proxy.URL) {
			t.Errorf("details = %q", details)
		}
		if command := w.request.command.ToPlainText(); !strings.Contains(command, "--proxy-user gary") || strings.Contains(command, "pw") {
			t.Errorf("command = %s", command)
		}

		// Settings which change are loaded again, without the password
		// being asked for again in the session, and the same settings are
		// not loaded again.
		s := w.config
		s.Network.MinTLS = network.TLS13
		before := w.net
		if err := w.applySettings(s); err != nil || w.net == before || len(asked) != 1 {
			t.Errorf("applied: %v, asked %v", err, asked)
		}
		before = w.net
		if err := w.applySettings(s); err != nil || w.net != before {
			t.Errorf("the same settings were loaded again: %v", err)
		}

		// Settings that cannot all be used are refused, and those there
		// were stay.
		bad := s
		bad.Network.Hosts = []network.Host{{Pattern: "localhost", CA: filepath.Join(t.TempDir(), "missing.pem")}}
		err := w.applySettings(bad)
		if err == nil || !strings.Contains(err.Error(), "network settings") || w.net != before || w.config.Network.Hosts[0].Pattern != "127.0.0.1" {
			t.Errorf("error %v, settings %+v", err, w.config.Network)
		}
	})
}

func TestNetworkSettingsAtStartup(t *testing.T) {
	files := testFiles(t)
	s := settings.Load(files.Settings)
	s.Network = network.Config{Hosts: []network.Host{{Pattern: "example.invalid", CA: filepath.Join(t.TempDir(), "missing.pem")}}}
	if err := s.Save(files.Settings); err != nil {
		t.Fatal(err)
	}

	mainthread.Wait(func() {
		// Settings that cannot all be used are reported, and a request to
		// a host they are for fails with the reason.
		w := NewWindow(files.Files)
		if w.netErr == nil || w.net == nil {
			t.Fatalf("netErr %v", w.netErr)
		}
		if _, err := w.client.Get("http://example.invalid/"); err == nil || !strings.Contains(err.Error(), "could not be read") {
			t.Errorf("error = %v", err)
		}
	})
}

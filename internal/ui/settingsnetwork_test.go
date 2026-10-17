package ui

import (
	"reflect"
	"testing"

	"github.com/mappu/miqt/qt6/mainthread"

	"github.com/nomad-software/triggerapi/internal/network"
)

func TestNetworkTab(t *testing.T) {
	current := network.Config{
		Proxy:   "http://gary@proxy:3128",
		NoProxy: []string{"localhost", ".lan"},
		MinTLS:  network.TLS13,
		Hosts: []network.Host{
			{Pattern: "*.example.com", CA: "/ca.pem", Insecure: true},
			{Pattern: "pay.example.com", Cert: "/c.p12", Address: "10.0.0.1"},
		},
	}

	mainthread.Wait(func() {
		w := NewWindow(testFiles(t).Files)
		d := newSettingsDialog(w.QWidget, w.config, w.files)
		defer d.DeleteLater()
		if !d.tabs.Widget(settingsTabNetwork).IsAncestorOf(d.network.proxy.QWidget) {
			t.Error("the network settings are not on their tab")
		}

		// With none chosen, the settings are the default, and nothing is
		// selected to edit.
		if c := d.network.config(); !c.IsZero() || d.network.form.IsEnabled() || d.network.remove.IsEnabled() {
			t.Errorf("config = %+v", c)
		}

		// What the settings hold is shown, and given back unchanged.
		tab := newNetworkTab(w.QWidget, current)
		if got := tab.config(); !reflect.DeepEqual(got, current) {
			t.Errorf("config = %+v, want %+v", got, current)
		}
		if tab.list.Count() != 2 || tab.list.CurrentRow() != 0 || tab.pattern.Text() != "*.example.com" || !tab.insecure.IsChecked() || !tab.proxy.IsEnabled() {
			t.Errorf("shown: %d hosts, row %d, %q", tab.list.Count(), tab.list.CurrentRow(), tab.pattern.Text())
		}

		// The form edits the host selected, whose name the list shows.
		tab.list.SetCurrentRow(1)
		tab.pattern.SetText("shop.example.com")
		tab.key.SetText(" /c.key ")
		if got := tab.config().Hosts[1]; got != (network.Host{Pattern: "shop.example.com", Cert: "/c.p12", Key: "/c.key", Address: "10.0.0.1"}) || tab.list.Item(1).Text() != "shop.example.com" {
			t.Errorf("host = %+v, listed as %q", got, tab.list.Item(1).Text())
		}

		// A file is chosen by browsing.
		tab.choose = func(caption, start string) string {
			if caption != "Certificate Authority" {
				t.Errorf("caption %q", caption)
			}
			return "/chosen.pem"
		}
		tab.list.SetCurrentRow(0)
		tab.browse[tab.ca].Click()
		if got := tab.config().Hosts[0].CA; got != "/chosen.pem" {
			t.Errorf("CA = %q", got)
		}

		// A host added is selected, and left empty it is not kept.
		tab.add()
		if tab.list.Count() != 3 || tab.list.CurrentRow() != 2 || tab.list.Item(2).Text() != noHostName || len(tab.config().Hosts) != 2 {
			t.Errorf("added: %d hosts, row %d", tab.list.Count(), tab.list.CurrentRow())
		}
		tab.pattern.SetText("*")
		if len(tab.config().Hosts) != 3 {
			t.Error("the host added is not kept")
		}

		// The host selected is removed.
		tab.list.SetCurrentRow(0)
		tab.removeSelected()
		if hosts := tab.config().Hosts; len(hosts) != 2 || hosts[0].Pattern != "shop.example.com" || tab.list.Count() != 2 {
			t.Errorf("hosts = %+v", hosts)
		}

		// No proxy, an address of none, and TLS 1.2.
		tab.proxyMode.SetCurrentIndex(proxyNone)
		tab.minTLS.SetCurrentIndex(0)
		tab.noProxy.SetText("a, b  c")
		if c := tab.config(); c.Proxy != network.ProxyNone || c.MinTLS != "" || !reflect.DeepEqual(c.NoProxy, []string{"a", "b", "c"}) || tab.proxy.IsEnabled() {
			t.Errorf("config = %+v", c)
		}
		tab.proxyMode.SetCurrentIndex(proxyAddress)
		tab.proxy.SetText("  ")
		if c := tab.config(); c.Proxy != network.ProxyEnvironment {
			t.Errorf("an empty address is %q", c.Proxy)
		}

		// The dialog gives the settings what the tab holds.
		d.network = tab
		if got := d.settings(w.config).Network; !reflect.DeepEqual(got, tab.config()) {
			t.Errorf("settings = %+v", got)
		}
	})
}

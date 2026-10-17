package network

import (
	"strings"
	"testing"
)

func TestMatch(t *testing.T) {
	for _, c := range []struct {
		pattern, host string
		want          bool
	}{
		{"api.example.com", "api.example.com", true},
		{"API.example.com", "api.EXAMPLE.com", true},
		{"api.example.com", "example.com", false},
		{"*.example.com", "api.example.com", true},
		{"*.example.com", "a.b.example.com", true},
		{"*.example.com", "example.com", false},
		{"*.example.com", "badexample.com", false},
		{"*", "anything", true},
		{"", "anything", false},
	} {
		if got := Match(c.pattern, c.host); got != c.want {
			t.Errorf("Match(%q, %q) = %v", c.pattern, c.host, got)
		}
	}
}

func TestHost(t *testing.T) {
	c := Config{Hosts: []Host{
		{Pattern: "*", Insecure: true},
		{Pattern: "*.example.com", CA: "wide"},
		{Pattern: "*.api.example.com", CA: "narrow"},
		{Pattern: "one.api.example.com", CA: "exact"},
		{Pattern: "*.example.com", CA: "second"},
	}}
	for host, want := range map[string]string{
		"one.api.example.com": "exact",
		"two.api.example.com": "narrow",
		"www.example.com":     "wide",
		"other.org":           "",
	} {
		h, ok := c.Host(host)
		if !ok || h.CA != want {
			t.Errorf("Host(%q) = %+v, %v; want CA %q", host, h, ok, want)
		}
	}
	if _, ok := (Config{}).Host("a"); ok {
		t.Error("an empty configuration has an entry")
	}
}

func TestBypasses(t *testing.T) {
	c := Config{NoProxy: []string{"internal.net", ".corp.com", "*.lan", " Local "}}
	for host, want := range map[string]bool{
		"internal.net":      true,
		"api.internal.net":  true,
		"corp.com":          true,
		"x.corp.com":        true,
		"printer.lan":       true,
		"local":             true,
		"notinternal.net":   false,
		"example.com":       false,
		"internal.net.evil": false,
	} {
		if got := c.Bypasses(host); got != want {
			t.Errorf("Bypasses(%q) = %v", host, got)
		}
	}
	if !(Config{NoProxy: []string{"*"}}).Bypasses("anything") {
		t.Error("* does not bypass every host")
	}
}

func TestCheck(t *testing.T) {
	valid := Config{Proxy: "socks5://gary@proxy:1080", NoProxy: []string{"a"}, MinTLS: TLS13, Hosts: []Host{
		{Pattern: "*.example.com", Address: "10.0.0.1"},
		{Pattern: "api.example.com", Address: "[::1]:8443", Cert: "c.p12"},
	}}
	if err := valid.Check(); err != nil {
		t.Errorf("valid: %v", err)
	}
	if err := (Config{}).Check(); err != nil {
		t.Errorf("zero: %v", err)
	}

	for _, c := range []struct {
		config Config
		want   string
	}{
		{Config{Proxy: "ftp://proxy"}, "not http, https or socks5"},
		{Config{Proxy: "http://gary:secret@proxy"}, "has a password in it"},
		{Config{Proxy: "http://"}, "has no host"},
		{Config{MinTLS: "1.1"}, "not a version of TLS"},
		{Config{Hosts: []Host{{Pattern: " "}}}, "has no name"},
		{Config{Hosts: []Host{{Pattern: "api.*.com"}}}, "can have a * only"},
		{Config{Hosts: []Host{{Pattern: "https://api.com"}}}, "a name alone"},
		{Config{Hosts: []Host{{Pattern: "api.com", Key: "k"}}}, "a key but no certificate"},
		{Config{Hosts: []Host{{Pattern: "api.com", Address: "a b"}}}, "not an address"},
	} {
		if err := c.config.Check(); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("Check(%+v) = %v, want %q", c.config, err, c.want)
		}
	}
}

func TestProxyURL(t *testing.T) {
	for proxy, want := range map[string]string{
		"proxy:3128":              "http://proxy:3128",
		"https://gary@proxy:3128": "https://gary@proxy:3128",
		ProxyNone:                 "",
		ProxyEnvironment:          "",
	} {
		u, err := Config{Proxy: proxy}.ProxyURL()
		got := ""
		if u != nil {
			got = u.String()
		}
		if err != nil || got != want {
			t.Errorf("ProxyURL(%q) = %q, %v", proxy, got, err)
		}
	}
}

func TestRedirect(t *testing.T) {
	for _, c := range []struct{ address, dialled, want string }{
		{"10.0.0.1", "api.example.com:443", "10.0.0.1:443"},
		{"10.0.0.1:8443", "api.example.com:443", "10.0.0.1:8443"},
		{"::1", "api.example.com:80", "[::1]:80"},
		{"[::1]", "api.example.com:80", "[::1]:80"},
		{"[::1]:9000", "api.example.com:80", "[::1]:9000"},
		{"staging.internal", "api.example.com:80", "staging.internal:80"},
		{"", "api.example.com:80", "api.example.com:80"},
	} {
		if got := (Host{Address: c.address}).redirect(c.dialled); got != c.want {
			t.Errorf("redirect of %q to %q = %q, want %q", c.dialled, c.address, got, c.want)
		}
	}
}

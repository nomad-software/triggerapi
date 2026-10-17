// Package network says how requests reach their servers: through which proxy,
// with which version of TLS, trusting which certificates, with which client
// certificate and at which address. It makes the transports every kind of
// request is sent with, and the options of curl that do the same. It has no
// dependency on the UI.
package network

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"slices"
	"strings"
)

// Config is how requests reach their servers, as the settings keep it. The
// zero value is what there was before there were settings for it: the proxy
// of the environment, and TLS as Go has it.
type Config struct {
	// Proxy is ProxyEnvironment (empty) for the proxy the environment
	// names, ProxyNone for none, or the address of a proxy, http, https or
	// socks5, which can name a user but not a password: the password is
	// asked for when it is needed and never kept.
	Proxy string `json:"proxy,omitempty"`
	// NoProxy names the hosts reached without the proxy. See Bypasses.
	NoProxy []string `json:"noProxy,omitempty"`
	// MinTLS is the oldest version of TLS a server may use: "", for Go's
	// own least, TLS12 or TLS13.
	MinTLS string `json:"minTLS,omitempty"`
	// Hosts are what is particular to some servers. See Config.Host.
	Hosts []Host `json:"hosts,omitempty"`
}

// The values of Config.Proxy that are not an address.
const (
	ProxyEnvironment = ""
	ProxyNone        = "none"
)

// The values of Config.MinTLS.
const (
	TLS12 = "1.2"
	TLS13 = "1.3"
)

// Host is what is particular to the servers whose names match Pattern.
type Host struct {
	// Pattern is a host name, *.name for any name ending in .name, or * for
	// every host. Case does not matter.
	Pattern string `json:"host"`
	// CA is the file of the certificates of the authorities trusted for
	// these hosts, in place of those of the system.
	CA string `json:"ca,omitempty"`
	// Cert is the file of the client certificate to give these hosts, in
	// PEM with its private key in Key, or a PKCS #12 file holding both, in
	// which case Key is empty.
	Cert string `json:"cert,omitempty"`
	Key  string `json:"key,omitempty"`
	// Insecure has the certificates of these hosts not checked.
	Insecure bool `json:"insecure,omitempty"`
	// Address is where to connect for these hosts in place of the address
	// their names have: a host name or IP address, with or without a port,
	// which is otherwise that of the request.
	Address string `json:"address,omitempty"`
}

// IsZero reports whether the configuration is the default.
func (c Config) IsZero() bool {
	return c.Proxy == "" && len(c.NoProxy) == 0 && c.MinTLS == "" && len(c.Hosts) == 0
}

// Match reports whether a host name matches a pattern of Host.
func Match(pattern, host string) bool {
	pattern, host = strings.ToLower(strings.TrimSpace(pattern)), strings.ToLower(host)
	if pattern == "*" {
		return true
	}
	if suffix, ok := strings.CutPrefix(pattern, "*."); ok {
		return strings.HasSuffix(host, "."+suffix)
	}
	return pattern != "" && pattern == host
}

// specificity orders patterns from the least particular to the most: *, then
// *.name by the length of name, then a name itself.
func specificity(pattern string) int {
	pattern = strings.TrimSpace(pattern)
	switch {
	case pattern == "*":
		return 0
	case strings.HasPrefix(pattern, "*."):
		return 1 + len(pattern)
	}
	return 1 << 20
}

// Host returns the entry of Hosts for a host name, and reports whether there
// is one. Where several match, the most particular is used, and of those
// alike the first: a name itself before *.name, and *.a.b before *.b.
func (c Config) Host(host string) (Host, bool) {
	i := c.index(host)
	if i < 0 {
		return Host{}, false
	}
	return c.Hosts[i], true
}

// index returns the place in Hosts of the entry for a host name, which is -1
// if there is none. See Host.
func (c Config) index(host string) int {
	best, chosen := -1, -1
	for i, h := range c.Hosts {
		if Match(h.Pattern, host) && specificity(h.Pattern) > best {
			best, chosen = specificity(h.Pattern), i
		}
	}
	return chosen
}

// Bypasses reports whether a host is reached without the proxy: whether it
// is, or is inside, a domain NoProxy names. A leading . or *. on a name
// changes nothing, and * names every host, as with curl's --noproxy and the
// NO_PROXY of the environment.
func (c Config) Bypasses(host string) bool {
	host = strings.ToLower(host)
	for _, name := range c.NoProxy {
		name = noProxyName(name)
		if name == "*" || host == name || strings.HasSuffix(host, "."+name) {
			return true
		}
	}
	return false
}

// noProxyName returns a name of NoProxy as a domain, without what may come
// before it.
func noProxyName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "*" {
		return name
	}
	name = strings.TrimPrefix(name, "*")
	return strings.TrimPrefix(name, ".")
}

// proxySchemes are the kinds of proxy there can be.
var proxySchemes = []string{"http", "https", "socks5", "socks5h"}

// ProxyURL returns the address of the proxy of the configuration, which is
// nil for the environment's or none.
func (c Config) ProxyURL() (*url.URL, error) {
	if c.Proxy == ProxyEnvironment || c.Proxy == ProxyNone {
		return nil, nil
	}
	address := strings.TrimSpace(c.Proxy)
	if !strings.Contains(address, "://") {
		address = "http://" + address
	}
	u, err := url.Parse(address)
	if err != nil {
		return nil, fmt.Errorf("the proxy %q is not an address: %w", c.Proxy, err)
	}
	if !slices.Contains(proxySchemes, u.Scheme) {
		return nil, fmt.Errorf("the proxy %q is not http, https or socks5", c.Proxy)
	}
	if u.Hostname() == "" {
		return nil, fmt.Errorf("the proxy %q has no host", c.Proxy)
	}
	if _, ok := u.User.Password(); ok {
		return nil, fmt.Errorf("the proxy %q has a password in it, which would be kept in the settings: give only the user, and the password is asked for", c.Proxy)
	}
	return u, nil
}

// Check returns what is wrong with the configuration, other than files that
// cannot be read, which loading it finds.
func (c Config) Check() error {
	var problems []error
	if _, err := c.ProxyURL(); err != nil {
		problems = append(problems, err)
	}
	if c.MinTLS != "" && c.MinTLS != TLS12 && c.MinTLS != TLS13 {
		problems = append(problems, fmt.Errorf("%q is not a version of TLS that can be required", c.MinTLS))
	}
	for _, h := range c.Hosts {
		if err := h.check(); err != nil {
			problems = append(problems, err)
		}
	}
	return errors.Join(problems...)
}

// check returns what is wrong with the entry.
func (h Host) check() error {
	pattern := strings.TrimSpace(h.Pattern)
	switch {
	case pattern == "":
		return errors.New("a host has no name")
	case pattern != "*" && strings.Contains(strings.TrimPrefix(pattern, "*."), "*"):
		return fmt.Errorf("the host %q can have a * only as all of it or at its start, as in *.example.com", h.Pattern)
	case strings.ContainsAny(pattern, ":/ "):
		return fmt.Errorf("the host %q is to be a name alone, with no scheme, port or path", h.Pattern)
	case h.Key != "" && h.Cert == "":
		return fmt.Errorf("the host %q has a key but no certificate", h.Pattern)
	}
	if h.Address != "" {
		if _, _, err := splitAddress(h.Address); err != nil {
			return fmt.Errorf("the host %q is to be reached at %q, which is not an address: %w", h.Pattern, h.Address, err)
		}
	}
	return nil
}

// splitAddress splits a host name or IP address, with or without a port,
// into the two, the port being empty if there is none.
func splitAddress(address string) (host, port string, err error) {
	address = strings.TrimSpace(address)
	if address == "" {
		return "", "", errors.New("it is empty")
	}
	if host, port, err := net.SplitHostPort(address); err == nil {
		return host, port, nil
	}
	// An IPv6 address without a port can be written with or without its
	// brackets.
	host = strings.TrimSuffix(strings.TrimPrefix(address, "["), "]")
	if strings.ContainsAny(host, "/ []") {
		return "", "", errors.New("it has characters an address cannot")
	}
	return host, "", nil
}

// redirect returns the address to dial in place of address, which is a host
// and port, for the entry: its Address, with the port of address unless it
// names its own.
func (h Host) redirect(address string) string {
	if h.Address == "" {
		return address
	}
	_, port, err := net.SplitHostPort(address)
	if err != nil {
		return address
	}
	host, own, err := splitAddress(h.Address)
	if err != nil {
		return address
	}
	if own != "" {
		port = own
	}
	return net.JoinHostPort(host, port)
}

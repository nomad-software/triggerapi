package network

import (
	"strings"

	"github.com/nomad-software/triggerapi/internal/homedir"
)

// CurlOptions returns the options of curl that send a request to a host as
// the client does, each with its value. A passphrase or password is never
// among them; CurlConfig gives those.
//
// A proxy of the environment is left to curl, which reads the same
// variables. curl trusts only the authority of a host's CA, as the client
// does, and is told of the address to reach a host at with --connect-to,
// which leaves the name it is asked for as it is.
func (c *Client) CurlOptions(host string) [][]string {
	var options [][]string
	add := func(option ...string) { options = append(options, option) }

	switch {
	case c.config.Proxy == ProxyNone:
		add("--noproxy", "*")
	case c.proxy != nil:
		shown := *c.proxy
		shown.User = nil
		add("--proxy", shown.String())
		if user := c.proxy.User; user != nil {
			// The password is given in the configuration.
			add("--proxy-user", user.Username())
		}
	}
	if c.config.Proxy != ProxyNone && len(c.config.NoProxy) > 0 {
		var names []string
		for _, name := range c.config.NoProxy {
			if name = noProxyName(name); name != "" {
				names = append(names, name)
			}
		}
		add("--noproxy", strings.Join(names, ","))
	}

	switch c.config.MinTLS {
	case TLS12:
		add("--tlsv1.2")
	case TLS13:
		add("--tlsv1.3")
	}

	h, ok := c.config.Host(host)
	if !ok {
		return options
	}
	if h.CA != "" {
		add("--cacert", homedir.Expand(h.CA))
	}
	if h.Insecure {
		add("--insecure")
	}
	if h.Cert != "" {
		add("--cert", homedir.Expand(h.Cert))
		if h.Key != "" {
			add("--key", homedir.Expand(h.Key))
		} else {
			add("--cert-type", "P12")
		}
	}
	if h.Address != "" {
		if address, port, err := splitAddress(h.Address); err == nil {
			if strings.Contains(address, ":") {
				address = "[" + address + "]"
			}
			add("--connect-to", host+"::"+address+":"+port)
		}
	}
	return options
}

// CurlConfig returns the passphrase and password the options of CurlOptions
// need, as the lines of a curl configuration file, which is empty if there
// are none. It is given to curl on its standard input, so that they are not
// among the arguments of a program, which other users can see.
func (c *Client) CurlConfig(host string) string {
	var lines []string
	if c.proxy != nil && c.proxy.User != nil && c.config.Proxy != ProxyNone {
		if password, ok := c.proxy.User.Password(); ok {
			lines = append(lines, "proxy-user = "+curlQuote(c.proxy.User.Username()+":"+password))
		}
	}
	if h, ok := c.config.Host(host); ok && h.Cert != "" {
		path := h.Key
		if path == "" {
			path = h.Cert
		}
		if passphrase, ok := c.secrets[Secret{Path: path}]; ok {
			lines = append(lines, "pass = "+curlQuote(passphrase))
		}
	}
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n") + "\n"
}

// curlQuote returns a value of a curl configuration file in quotes.
func curlQuote(value string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`, "\r", `\r`, "\t", `\t`).Replace(value) + `"`
}

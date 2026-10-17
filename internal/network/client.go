package network

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"software.sslmate.com/src/go-pkcs12"

	"github.com/nomad-software/triggerapi/internal/homedir"
)

// Secret is a passphrase or password that Load asks for, as it is never kept
// in the settings.
type Secret struct {
	// Path is the file of the key it opens, or the address of the proxy,
	// with its user, for a proxy's password.
	Path string
	// Proxy says that it is the password of a proxy, not the passphrase of
	// a key.
	Proxy bool
}

// Ask returns a secret, or an error if the user would not give it.
type Ask func(Secret) (string, error)

// Client sends requests as a Config says. It is an http.RoundTripper, so it
// is the Transport of the http.Client requests are sent with, and it keeps a
// transport for each entry of Hosts that is used, so that connections are
// reused as with one. It is safe to use from several goroutines.
type Client struct {
	config Config
	proxy  *url.URL
	// secret is the passphrase of each key that has one, and the password of
	// the proxy, by Secret, as Load was given them.
	secrets map[Secret]string
	// hosts is what was loaded for each entry of Hosts, by its place.
	hosts []loaded

	mu         sync.Mutex
	transports map[int]*http.Transport
}

// loaded is what the files of an entry of Hosts were made into, or the error
// there was in reading them, which a request to its hosts fails with.
type loaded struct {
	roots *x509.CertPool
	cert  *tls.Certificate
	err   error
}

// noHost is the key in Client.transports of the transport of the hosts that
// no entry matches, as Config.index gives it.
const noHost = -1

// Load reads the files the configuration names, asking for any passphrase
// and the password of the proxy, and returns the Client. It is returned with
// the errors there were too, as a request to a host whose files could not be
// read fails with the reason, and others are sent as they would be.
func Load(c Config, ask Ask) (*Client, error) {
	client := &Client{config: c, secrets: map[Secret]string{}, transports: map[int]*http.Transport{}}
	failed := []error{c.Check()}

	proxy, err := c.ProxyURL()
	if err == nil && proxy != nil && proxy.User != nil {
		secret := Secret{Path: proxy.String(), Proxy: true}
		password, err := ask(secret)
		if err != nil {
			failed = append(failed, fmt.Errorf("the password of the proxy %s: %w", proxy.Redacted(), err))
		} else {
			client.secrets[secret] = password
			proxy.User = url.UserPassword(proxy.User.Username(), password)
		}
	}
	client.proxy = proxy

	for _, h := range c.Hosts {
		l := client.load(h, ask)
		if l.err != nil {
			l.err = fmt.Errorf("the settings for %s: %w", h.Pattern, l.err)
			failed = append(failed, l.err)
		}
		client.hosts = append(client.hosts, l)
	}
	return client, errors.Join(failed...)
}

// Default returns a client of the default configuration: the proxy of the
// environment, and TLS as Go has it.
func Default() *Client {
	// There is nothing to read or ask for, so there is no error.
	client, _ := Load(Config{}, nil)
	return client
}

// load reads the files of an entry of Hosts.
func (c *Client) load(h Host, ask Ask) loaded {
	var l loaded
	if h.CA != "" {
		data, err := os.ReadFile(homedir.Expand(h.CA))
		if err != nil {
			return loaded{err: fmt.Errorf("the certificate authority could not be read: %w", err)}
		}
		l.roots = x509.NewCertPool()
		if !l.roots.AppendCertsFromPEM(data) {
			return loaded{err: fmt.Errorf("%s has no certificate in PEM", h.CA)}
		}
	}
	if h.Cert != "" {
		cert, err := c.loadCert(h, ask)
		if err != nil {
			return loaded{err: fmt.Errorf("the client certificate could not be read: %w", err)}
		}
		l.cert = cert
	}
	return l
}

// loadCert reads the client certificate of an entry: a PKCS #12 file, or a
// certificate in PEM and its key, which may be encrypted.
func (c *Client) loadCert(h Host, ask Ask) (*tls.Certificate, error) {
	data, err := os.ReadFile(homedir.Expand(h.Cert))
	if err != nil {
		return nil, err
	}
	// A file of PEM blocks starts with text; PKCS #12 is binary.
	if h.Key == "" && !strings.Contains(string(data), "-----BEGIN") {
		return c.loadPKCS12(h.Cert, data, ask)
	}
	if h.Key == "" {
		return nil, fmt.Errorf("%s is a certificate in PEM, whose key is to be given too", h.Cert)
	}
	key, err := os.ReadFile(homedir.Expand(h.Key))
	if err != nil {
		return nil, err
	}
	key, err = c.decryptKey(h.Key, key, ask)
	if err != nil {
		return nil, err
	}
	cert, err := tls.X509KeyPair(data, key)
	if err != nil {
		return nil, err
	}
	return &cert, nil
}

// loadPKCS12 reads a PKCS #12 file, asking for its passphrase. One without a
// passphrase is tried first, so that none is asked for it.
func (c *Client) loadPKCS12(path string, data []byte, ask Ask) (*tls.Certificate, error) {
	key, cert, chain, err := pkcs12.DecodeChain(data, "")
	if errors.Is(err, pkcs12.ErrIncorrectPassword) {
		secret := Secret{Path: path}
		passphrase, askErr := ask(secret)
		if askErr != nil {
			return nil, askErr
		}
		if key, cert, chain, err = pkcs12.DecodeChain(data, passphrase); err != nil {
			return nil, fmt.Errorf("%s could not be opened with this passphrase: %w", path, err)
		}
		c.secrets[secret] = passphrase
	}
	if err != nil {
		return nil, fmt.Errorf("%s is not a PKCS #12 file that can be read: %w", path, err)
	}
	certificate := &tls.Certificate{Certificate: [][]byte{cert.Raw}, PrivateKey: key, Leaf: cert}
	for _, ca := range chain {
		certificate.Certificate = append(certificate.Certificate, ca.Raw)
	}
	return certificate, nil
}

// decryptKey returns a private key in PEM decrypted, asking for its
// passphrase, if it is encrypted.
func (c *Client) decryptKey(path string, data []byte, ask Ask) ([]byte, error) {
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("%s is not a key in PEM", path)
	}
	if block.Type == "ENCRYPTED PRIVATE KEY" {
		return nil, fmt.Errorf("%s is an encrypted PKCS #8 key, which cannot be read: put it with its certificate in a PKCS #12 file (openssl pkcs12 -export -in cert.pem -inkey key.pem -out client.p12) and give that as the certificate", path)
	}
	//lint:ignore SA1019 the legacy encryption of PEM is weak, but is what an encrypted key made by older tools has.
	if !x509.IsEncryptedPEMBlock(block) {
		return data, nil
	}
	secret := Secret{Path: path}
	passphrase, err := ask(secret)
	if err != nil {
		return nil, err
	}
	//lint:ignore SA1019 as above.
	der, err := x509.DecryptPEMBlock(block, []byte(passphrase))
	if err != nil {
		return nil, fmt.Errorf("%s could not be decrypted with this passphrase: %w", path, err)
	}
	c.secrets[secret] = passphrase
	return pem.EncodeToMemory(&pem.Block{Type: block.Type, Bytes: der}), nil
}

// Config returns the configuration the client was loaded from.
func (c *Client) Config() Config {
	return c.config
}

// HTTP returns an http.Client which sends with the client.
func (c *Client) HTTP() *http.Client {
	return &http.Client{Transport: c}
}

// RoundTrip sends a request with the transport of its host.
func (c *Client) RoundTrip(req *http.Request) (*http.Response, error) {
	t, err := c.transport(req.URL.Hostname())
	if err != nil {
		return nil, err
	}
	return t.RoundTrip(req)
}

// transport returns the transport of a host, which is made when it is first
// needed and kept.
func (c *Client) transport(host string) (*http.Transport, error) {
	i := c.config.index(host)
	if i != noHost && c.hosts[i].err != nil {
		return nil, c.hosts[i].err
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if t, ok := c.transports[i]; ok {
		return t, nil
	}
	t := c.newTransport(i, nil)
	c.transports[i] = t
	return t, nil
}

// Transport returns a new transport for a host, speaking the protocols given,
// or those of http.DefaultTransport if they are nil. Its caller closes its
// connections.
func (c *Client) Transport(host string, protocols *http.Protocols) (*http.Transport, error) {
	i := c.config.index(host)
	if i != noHost && c.hosts[i].err != nil {
		return nil, c.hosts[i].err
	}
	return c.newTransport(i, protocols), nil
}

// newTransport makes the transport of an entry of Hosts, or of the hosts no
// entry matches.
func (c *Client) newTransport(i int, protocols *http.Protocols) *http.Transport {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.Proxy = c.proxyFor
	if protocols != nil {
		t.Protocols = protocols
	}
	config := &tls.Config{}
	switch c.config.MinTLS {
	case TLS12:
		config.MinVersion = tls.VersionTLS12
	case TLS13:
		config.MinVersion = tls.VersionTLS13
	}
	if i != noHost {
		h, l := c.config.Hosts[i], c.hosts[i]
		config.RootCAs = l.roots
		config.InsecureSkipVerify = h.Insecure
		if l.cert != nil {
			config.Certificates = []tls.Certificate{*l.cert}
		}
		if h.Address != "" {
			dialer := &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}
			t.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
				// The proxy is dialled where it is.
				if host, _, err := net.SplitHostPort(address); err == nil && Match(h.Pattern, host) {
					address = h.redirect(address)
				}
				return dialer.DialContext(ctx, network, address)
			}
		}
	}
	t.TLSClientConfig = config
	return t
}

// proxyFor returns the proxy a request goes through, which is nil for none.
func (c *Client) proxyFor(req *http.Request) (*url.URL, error) {
	if c.config.Bypasses(req.URL.Hostname()) {
		return nil, nil
	}
	switch c.config.Proxy {
	case ProxyEnvironment:
		return http.ProxyFromEnvironment(req)
	case ProxyNone:
		return nil, nil
	}
	return c.proxy, nil
}

// Proxy returns the address of the proxy a request goes through, without
// its user, which is empty for none.
func (c *Client) Proxy(req *http.Request) string {
	u, err := c.proxyFor(req)
	if err != nil || u == nil {
		return ""
	}
	shown := *u
	shown.User = nil
	return shown.String()
}

// Unverified reports whether the certificate of a host is not checked.
func (c *Client) Unverified(host string) bool {
	h, ok := c.config.Host(host)
	return ok && h.Insecure
}

// CloseIdleConnections closes the connections kept open by the transports,
// as when the client is replaced.
func (c *Client) CloseIdleConnections() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, t := range c.transports {
		t.CloseIdleConnections()
	}
}

package network

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"software.sslmate.com/src/go-pkcs12"
)

// pki is an authority and certificates it signed, kept in files.
type pki struct {
	dir string
	ca  *x509.Certificate
	key *ecdsa.PrivateKey
	// caFile is the authority's certificate in PEM.
	caFile string
}

func newPKI(t *testing.T) *pki {
	t.Helper()
	p := &pki{dir: t.TempDir()}
	p.ca, p.key = p.sign(t, &x509.Certificate{
		Subject:               pkix.Name{CommonName: "Test CA"},
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}, nil, nil)
	p.caFile = p.write(t, "ca.pem", pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: p.ca.Raw}))
	return p
}

// sign makes a certificate from a template, signed by parent and its key, or
// by itself if they are nil.
func (p *pki) sign(t *testing.T, template, parent *x509.Certificate, parentKey *ecdsa.PrivateKey) (*x509.Certificate, *ecdsa.PrivateKey) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template.SerialNumber = big.NewInt(time.Now().UnixNano())
	template.NotBefore = time.Now().Add(-time.Hour)
	template.NotAfter = time.Now().Add(time.Hour)
	if parent == nil {
		parent, parentKey = template, key
	}
	der, err := x509.CreateCertificate(rand.Reader, template, parent, &key.PublicKey, parentKey)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return cert, key
}

func (p *pki) write(t *testing.T, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(p.dir, name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// server returns a certificate for localhost, for a server.
func (p *pki) server(t *testing.T) tls.Certificate {
	cert, key := p.sign(t, &x509.Certificate{
		Subject:     pkix.Name{CommonName: "localhost"},
		DNSNames:    []string{"localhost"},
		IPAddresses: []net.IP{net.IPv4(127, 0, 0, 1)},
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}, p.ca, p.key)
	return tls.Certificate{Certificate: [][]byte{cert.Raw}, PrivateKey: key, Leaf: cert}
}

// client returns a client certificate, and the files of it in PEM and of its
// key.
func (p *pki) client(t *testing.T) (*x509.Certificate, *ecdsa.PrivateKey, string, string) {
	cert, key := p.sign(t, &x509.Certificate{
		Subject:     pkix.Name{CommonName: "client"},
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}, p.ca, p.key)
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	certFile := p.write(t, "client.pem", pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw}))
	keyFile := p.write(t, "client.key", pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
	return cert, key, certFile, keyFile
}

// tlsServer starts a server for localhost with a certificate of the pki,
// which answers with the common name of the client's certificate, if any.
func tlsServer(t *testing.T, p *pki, configure func(*tls.Config)) *httptest.Server {
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := "anonymous"
		if len(r.TLS.PeerCertificates) > 0 {
			name = r.TLS.PeerCertificates[0].Subject.CommonName
		}
		w.Write([]byte(name))
	}))
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{p.server(t)}}
	if configure != nil {
		configure(srv.TLS)
	}
	srv.StartTLS()
	t.Cleanup(srv.Close)
	return srv
}

// localURL returns the address of a server with localhost as its host, which
// its certificate is for.
func localURL(srv *httptest.Server) string {
	return strings.Replace(srv.URL, "127.0.0.1", "localhost", 1)
}

// noSecrets is an Ask for a configuration that needs none.
func noSecrets(t *testing.T) Ask {
	return func(s Secret) (string, error) {
		t.Errorf("asked for %+v", s)
		return "", errors.New("no")
	}
}

// get sends a GET with a client and returns the body.
func get(c *Client, address string) (string, error) {
	res, err := c.HTTP().Get(address)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	return string(body), err
}

func load(t *testing.T, config Config, ask Ask) *Client {
	t.Helper()
	c, err := Load(config, ask)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.CloseIdleConnections)
	return c
}

func TestTrustedCA(t *testing.T) {
	p := newPKI(t)
	srv := tlsServer(t, p, nil)

	// The authority is not one of the system's.
	_, err := get(load(t, Config{}, noSecrets(t)), localURL(srv))
	if err == nil || !strings.Contains(Explain(err).Error(), "signed by an authority that is not trusted") {
		t.Errorf("error = %v", Explain(err))
	}

	// Trusted for the host, it is used, and not for other hosts.
	c := load(t, Config{Hosts: []Host{{Pattern: "localhost", CA: p.caFile}}}, noSecrets(t))
	if body, err := get(c, localURL(srv)); err != nil || body != "anonymous" {
		t.Errorf("body %q, %v", body, err)
	}
	if _, err := get(c, srv.URL); err == nil {
		t.Error("the authority is trusted for a host it is not given for")
	}

	// Not checked, the certificate is accepted.
	c = load(t, Config{Hosts: []Host{{Pattern: "*", Insecure: true}}}, noSecrets(t))
	if body, err := get(c, localURL(srv)); err != nil || body != "anonymous" || !c.Unverified("localhost") {
		t.Errorf("body %q, %v", body, err)
	}

	// An authority that cannot be read fails the requests to its hosts.
	missing := Config{Hosts: []Host{{Pattern: "localhost", CA: filepath.Join(p.dir, "missing.pem")}}}
	c, err = Load(missing, noSecrets(t))
	if err == nil || !strings.Contains(err.Error(), "the settings for localhost") {
		t.Errorf("load error = %v", err)
	}
	if _, err := get(c, localURL(srv)); err == nil || !strings.Contains(err.Error(), "could not be read") {
		t.Errorf("request error = %v", err)
	}
}

func TestClientCertificate(t *testing.T) {
	p := newPKI(t)
	roots := x509.NewCertPool()
	roots.AddCert(p.ca)
	srv := tlsServer(t, p, func(c *tls.Config) {
		c.ClientCAs = roots
		c.ClientAuth = tls.RequireAndVerifyClientCert
	})
	cert, key, certFile, keyFile := p.client(t)

	// Without a certificate the server refuses.
	trust := Host{Pattern: "localhost", CA: p.caFile}
	_, err := get(load(t, Config{Hosts: []Host{trust}}, noSecrets(t)), localURL(srv))
	if err == nil || !strings.Contains(Explain(err).Error(), "wants a client certificate") {
		t.Errorf("error = %v", Explain(err))
	}

	// A certificate in PEM with its key.
	withPEM := trust
	withPEM.Cert, withPEM.Key = certFile, keyFile
	if body, err := get(load(t, Config{Hosts: []Host{withPEM}}, noSecrets(t)), localURL(srv)); err != nil || body != "client" {
		t.Errorf("body %q, %v", body, err)
	}

	// A key encrypted the old way of PEM, whose passphrase is asked for.
	der, _ := x509.MarshalECPrivateKey(key)
	//lint:ignore SA1019 the old encryption is what is being read.
	encrypted, err := x509.EncryptPEMBlock(rand.Reader, "EC PRIVATE KEY", der, []byte("secret"), x509.PEMCipherAES256)
	if err != nil {
		t.Fatal(err)
	}
	withEncrypted := trust
	withEncrypted.Cert, withEncrypted.Key = certFile, p.write(t, "encrypted.key", pem.EncodeToMemory(encrypted))
	var asked []Secret
	ask := func(s Secret) (string, error) { asked = append(asked, s); return "secret", nil }
	c := load(t, Config{Hosts: []Host{withEncrypted}}, ask)
	if body, err := get(c, localURL(srv)); err != nil || body != "client" || len(asked) != 1 || asked[0].Path != withEncrypted.Key {
		t.Errorf("body %q, %v, asked %+v", body, err, asked)
	}
	if got := c.CurlConfig("localhost"); got != "pass = \"secret\"\n" {
		t.Errorf("curl configuration = %q", got)
	}
	// A wrong passphrase, or none, is an error.
	if _, err := Load(Config{Hosts: []Host{withEncrypted}}, func(Secret) (string, error) { return "wrong", nil }); err == nil {
		t.Error("no error for a wrong passphrase")
	}
	if _, err := Load(Config{Hosts: []Host{withEncrypted}}, func(Secret) (string, error) { return "", errors.New("cancelled") }); err == nil || !strings.Contains(err.Error(), "cancelled") {
		t.Errorf("error = %v", err)
	}

	// A PKCS #12 file holds both, with a passphrase or without.
	for _, passphrase := range []string{"", "secret"} {
		data, err := pkcs12.Modern.Encode(key, cert, []*x509.Certificate{p.ca}, passphrase)
		if err != nil {
			t.Fatal(err)
		}
		withP12 := trust
		withP12.Cert = p.write(t, "client.p12", data)
		asked = nil
		if body, err := get(load(t, Config{Hosts: []Host{withP12}}, ask), localURL(srv)); err != nil || body != "client" || len(asked) != len(passphrase)/len("secret") {
			t.Errorf("passphrase %q: body %q, %v, asked %+v", passphrase, body, err, asked)
		}
	}

	// An encrypted PKCS #8 key cannot be read, and says what to do.
	withPKCS8 := trust
	withPKCS8.Cert, withPKCS8.Key = certFile, p.write(t, "pkcs8.key", pem.EncodeToMemory(&pem.Block{Type: "ENCRYPTED PRIVATE KEY", Bytes: []byte{1}}))
	if _, err := Load(Config{Hosts: []Host{withPKCS8}}, noSecrets(t)); err == nil || !strings.Contains(err.Error(), "openssl pkcs12 -export") {
		t.Errorf("error = %v", err)
	}
	// A certificate in PEM without its key.
	if _, err := Load(Config{Hosts: []Host{{Pattern: "localhost", Cert: certFile}}}, noSecrets(t)); err == nil || !strings.Contains(err.Error(), "whose key is to be given") {
		t.Errorf("error = %v", err)
	}
}

func TestMinTLS(t *testing.T) {
	p := newPKI(t)
	srv := tlsServer(t, p, func(c *tls.Config) { c.MaxVersion = tls.VersionTLS12 })
	trust := Host{Pattern: "localhost", CA: p.caFile}

	if _, err := get(load(t, Config{MinTLS: TLS12, Hosts: []Host{trust}}, noSecrets(t)), localURL(srv)); err != nil {
		t.Errorf("TLS 1.2: %v", err)
	}
	_, err := get(load(t, Config{MinTLS: TLS13, Hosts: []Host{trust}}, noSecrets(t)), localURL(srv))
	if err == nil || !strings.Contains(Explain(err).Error(), "no version in common") {
		t.Errorf("error = %v", Explain(err))
	}
}

func TestAddress(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("reached " + r.Host))
	}))
	defer srv.Close()
	_, port, _ := net.SplitHostPort(srv.Listener.Addr().String())

	// A name that does not exist is reached at the server's address, and
	// keeps its name as the host of the request.
	c := load(t, Config{Hosts: []Host{{Pattern: "*.example.invalid", Address: "127.0.0.1"}}}, noSecrets(t))
	body, err := get(c, "http://api.example.invalid:"+port+"/")
	if err != nil || body != "reached api.example.invalid:"+port {
		t.Errorf("body %q, %v", body, err)
	}

	// A port of its own replaces that of the request.
	c = load(t, Config{Hosts: []Host{{Pattern: "api.example.invalid", Address: "127.0.0.1:" + port}}}, noSecrets(t))
	if body, err := get(c, "http://api.example.invalid:1/"); err != nil || !strings.HasPrefix(body, "reached") {
		t.Errorf("body %q, %v", body, err)
	}
}

func TestProxy(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("direct"))
	}))
	defer target.Close()
	// The proxy answers in place of the server, saying who asked.
	var auth string
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Proxy-Authorization")
		w.Write([]byte("proxied " + r.URL.Host))
	}))
	defer proxy.Close()
	host := strings.TrimPrefix(target.URL, "http://")

	// A proxy with a user is given the password asked for.
	config := Config{Proxy: strings.Replace(proxy.URL, "http://", "http://gary@", 1)}
	var asked []Secret
	c := load(t, config, func(s Secret) (string, error) { asked = append(asked, s); return "pw", nil })
	body, err := get(c, target.URL)
	if err != nil || body != "proxied "+host || len(asked) != 1 || !asked[0].Proxy {
		t.Errorf("body %q, %v, asked %+v", body, err, asked)
	}
	if want := "Basic " + base64.StdEncoding.EncodeToString([]byte("gary:pw")); auth != want {
		t.Errorf("Proxy-Authorization = %q", auth)
	}
	req, _ := http.NewRequest("GET", target.URL, nil)
	if got := c.Proxy(req); got != proxy.URL {
		t.Errorf("Proxy = %q, want %q (with no user)", got, proxy.URL)
	}

	// A host it bypasses is reached directly, as is every host with none.
	config.NoProxy = []string{"127.0.0.1"}
	if body, err := get(load(t, config, func(Secret) (string, error) { return "pw", nil }), target.URL); err != nil || body != "direct" {
		t.Errorf("bypassed: body %q, %v", body, err)
	}
	t.Setenv("HTTP_PROXY", proxy.URL)
	if body, err := get(load(t, Config{Proxy: ProxyNone}, noSecrets(t)), target.URL); err != nil || body != "direct" {
		t.Errorf("none: body %q, %v", body, err)
	}

	// A proxy that cannot be reached says so.
	closed := httptest.NewServer(nil)
	closed.Close()
	_, err = get(load(t, Config{Proxy: closed.URL}, noSecrets(t)), target.URL)
	if err == nil || !strings.Contains(Explain(err).Error(), "The proxy could not be used") {
		t.Errorf("error = %v", Explain(err))
	}
}

func TestTransport(t *testing.T) {
	p := newPKI(t)
	srv := tlsServer(t, p, nil)
	c := load(t, Config{Hosts: []Host{{Pattern: "localhost", CA: p.caFile}}}, noSecrets(t))

	// A transport of its own, as gRPC has, follows the configuration.
	transport, err := c.Transport("localhost", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer transport.CloseIdleConnections()
	res, err := (&http.Client{Transport: transport}).Get(localURL(srv))
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
}

func TestCurlOptions(t *testing.T) {
	config := Config{
		Proxy:   "http://gary@proxy:3128",
		NoProxy: []string{"*.lan", ".corp.com"},
		MinTLS:  TLS13,
		Hosts: []Host{
			{Pattern: "*.example.com", CA: "/ca.pem", Insecure: true, Address: "::1"},
			{Pattern: "pay.example.com", Cert: "/c.p12"},
			{Pattern: "api.internal", Cert: "/c.pem", Key: "/c.key", Address: "10.0.0.1:8443"},
		},
	}
	c, err := Load(config, func(Secret) (string, error) { return `p"w`, nil })
	// The certificate files do not exist.
	if err == nil {
		t.Fatal("no error for files that do not exist")
	}

	join := func(options [][]string) string {
		var words []string
		for _, o := range options {
			words = append(words, o...)
		}
		return strings.Join(words, " ")
	}
	for host, want := range map[string]string{
		"www.example.com": "--proxy http://proxy:3128 --proxy-user gary --noproxy lan,corp.com --tlsv1.3 --cacert /ca.pem --insecure --connect-to www.example.com::[::1]:",
		"pay.example.com": "--proxy http://proxy:3128 --proxy-user gary --noproxy lan,corp.com --tlsv1.3 --cert /c.p12 --cert-type P12",
		"api.internal":    "--proxy http://proxy:3128 --proxy-user gary --noproxy lan,corp.com --tlsv1.3 --cert /c.pem --key /c.key --connect-to api.internal::10.0.0.1:8443",
	} {
		if got := join(c.CurlOptions(host)); got != want {
			t.Errorf("%s:\n got %s\nwant %s", host, got, want)
		}
	}
	// The password is given in the configuration, quoted.
	if got := c.CurlConfig("other"); got != "proxy-user = \"gary:p\\\"w\"\n" {
		t.Errorf("configuration = %q", got)
	}

	c = load(t, Config{Proxy: ProxyNone, NoProxy: []string{"x"}, MinTLS: TLS12}, noSecrets(t))
	if got := join(c.CurlOptions("a")); got != "--noproxy * --tlsv1.2" {
		t.Errorf("none: %s", got)
	}
	if got := join(load(t, Config{}, noSecrets(t)).CurlOptions("a")); got != "" || c.CurlConfig("a") != "" {
		t.Errorf("zero: %q", got)
	}
}

func TestExplain(t *testing.T) {
	if Explain(nil) != nil {
		t.Error("nil is explained")
	}
	plain := errors.New("connection refused")
	if Explain(plain) != plain {
		t.Error("an error with nothing to add is changed")
	}
	wrapped := Explain(x509.UnknownAuthorityError{})
	if !errors.As(wrapped, new(x509.UnknownAuthorityError)) {
		t.Error("the explained error does not wrap the error")
	}
}

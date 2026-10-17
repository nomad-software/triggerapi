package network

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"strings"
)

// settingsPlace is where the settings of this package are in the program.
const settingsPlace = "the Network tab of the settings"

// Explain returns an error that says what may be done about one which came
// of reaching a server, ahead of the error itself, or the error as it is if
// there is nothing to add. It is nil for nil.
func Explain(err error) error {
	if hint := explain(err); hint != "" {
		return &explained{hint: hint, err: err}
	}
	return err
}

// explained is an error with what may be done about it.
type explained struct {
	hint string
	err  error
}

func (e *explained) Error() string { return e.hint + " (" + e.err.Error() + ")" }
func (e *explained) Unwrap() error { return e.err }

// explain returns what may be done about an error, which is empty if there is
// nothing to say.
func explain(err error) string {
	if err == nil {
		return ""
	}
	var unknown x509.UnknownAuthorityError
	var hostname x509.HostnameError
	var invalid x509.CertificateInvalidError
	var record tls.RecordHeaderError
	message := strings.ToLower(err.Error())

	switch {
	case errors.As(err, &unknown):
		return "The server's certificate is signed by an authority that is not trusted. " +
			"Give the certificate of the authority for this host in " + settingsPlace + ", or have its certificate not checked"
	case errors.As(err, &hostname):
		return "The server's certificate is not for this host name. " +
			"Use the name it is for, or have its certificate not checked in " + settingsPlace
	case errors.As(err, &invalid) && invalid.Reason == x509.Expired:
		return "The server's certificate has expired or is not yet valid"
	case errors.As(err, &record):
		return "The server did not answer in TLS: it may want http and not https"
	case strings.Contains(message, "certificate required"):
		return "The server wants a client certificate. Give one for this host in " + settingsPlace
	case strings.Contains(message, "tls: bad certificate") || strings.Contains(message, "tls: unknown certificate authority") ||
		strings.Contains(message, "tls: certificate expired") || strings.Contains(message, "tls: unsupported certificate"):
		return "The server refused the client certificate"
	case strings.Contains(message, "tls: protocol version not supported"):
		return fmt.Sprintf("The server and the least version of TLS in %s have no version in common", settingsPlace)
	case strings.Contains(message, "proxyconnect"):
		return "The proxy could not be used. Check it in " + settingsPlace
	}
	return ""
}

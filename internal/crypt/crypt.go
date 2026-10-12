// Package crypt encrypts and decrypts the saved files with a pair of SSH
// keys, using age (filippo.io/age). It has no dependency on the UI.
package crypt

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"filippo.io/age"
	"filippo.io/age/agessh"
	"filippo.io/age/armor"
	"golang.org/x/crypto/ssh"
)

// intros are what a file that age has encrypted can start with: armoured,
// as they are written here, or not.
var intros = [][]byte{
	[]byte(armor.Header),
	[]byte("age-encryption.org/v1\n"),
}

// Keys are a pair of SSH keys: the public one, which data is encrypted to,
// and the private one, which decrypts it. Nil keys are those of settings
// which name none: they leave data that is not encrypted as it is, and
// cannot decrypt data that is.
type Keys struct {
	recipient age.Recipient
	identity  age.Identity
	// err is why the keys cannot be used, if they cannot.
	err error
}

// Open reads a pair of keys from the files of the public and of the private
// key, whose paths can start with ~ for the home folder of the user, and
// checks that one decrypts what the other encrypts. The passphrase
// is asked for if the private key is protected by one, and not again
// afterwards.
func Open(public, private string, passphrase func() ([]byte, error)) (*Keys, error) {
	if public == "" || private == "" {
		return nil, errors.New("both a public and a private key are needed")
	}

	public, private = home(public), home(private)

	line, err := os.ReadFile(public)
	if err != nil {
		return nil, fmt.Errorf("the public key could not be read: %w", err)
	}
	recipient, err := agessh.ParseRecipient(strings.TrimSpace(string(line)))
	if err != nil {
		return nil, fmt.Errorf("%s is not a public key that can be used: %w", public, err)
	}

	pem, err := os.ReadFile(private)
	if err != nil {
		return nil, fmt.Errorf("the private key could not be read: %w", err)
	}
	identity, err := agessh.ParseIdentity(pem)
	if _, protected := errors.AsType[*ssh.PassphraseMissingError](err); protected {
		// The recipient has been made of the same line, so it is known to
		// parse.
		key, _, _, _, _ := ssh.ParseAuthorizedKey(line)
		identity, err = agessh.NewEncryptedSSHIdentity(key, pem, passphrase)
	}
	if err != nil {
		return nil, fmt.Errorf("%s is not a private key that can be used: %w", private, err)
	}

	keys := &Keys{recipient: recipient, identity: identity}
	if err := keys.check(); err != nil {
		return nil, err
	}
	return keys, nil
}

// Recipient reads the public key of someone else from its file. The keys
// returned encrypt to it, and decrypt nothing, as that takes the private
// key, which only they have.
func Recipient(public string) (*Keys, error) {
	if public == "" {
		return nil, errors.New("a public key is needed")
	}
	line, err := os.ReadFile(home(public))
	if err != nil {
		return nil, fmt.Errorf("the public key could not be read: %w", err)
	}
	recipient, err := agessh.ParseRecipient(strings.TrimSpace(string(line)))
	if err != nil {
		return nil, fmt.Errorf("%s is not a public key that can be used: %w", public, err)
	}
	return &Keys{recipient: recipient}, nil
}

// home returns a path with the ~ it starts with, if it does, replaced by the
// home folder of the user.
func home(path string) string {
	rest, ok := strings.CutPrefix(path, "~")
	if !ok || (rest != "" && !os.IsPathSeparator(rest[0])) {
		return path
	}
	dir, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	return filepath.Join(dir, rest)
}

// Unusable returns keys which fail, for the reason given, to encrypt or
// decrypt anything. They stand for keys the settings name which could not
// be opened, so that nothing is then saved without being encrypted.
func Unusable(err error) *Keys {
	return &Keys{err: err}
}

// Err returns why the keys cannot be used, which is nil if they can be, or
// if there are none.
func (k *Keys) Err() error {
	if k == nil {
		return nil
	}
	return k.err
}

// check encrypts something and decrypts it again, which fails if the keys
// are not a pair, and is when the passphrase of the private key is asked
// for.
func (k *Keys) check() error {
	const sample = "triggerapi"

	encrypted, err := k.Encrypt([]byte(sample))
	if err != nil {
		return err
	}
	decrypted, err := k.Decrypt(encrypted)
	if _, mismatch := errors.AsType[*age.NoIdentityMatchError](err); mismatch {
		return errors.New("the private key is not the one that goes with the public key")
	}
	if err != nil {
		return err
	}
	if string(decrypted) != sample {
		return errors.New("the keys do not decrypt what they encrypt")
	}
	return nil
}

// Encrypted reports if data is encrypted.
func Encrypted(data []byte) bool {
	for _, intro := range intros {
		if bytes.HasPrefix(data, intro) {
			return true
		}
	}
	return false
}

// Encrypt returns data encrypted to the public key, as text. Nil keys return
// it as it is.
func (k *Keys) Encrypt(data []byte) ([]byte, error) {
	if k == nil {
		return data, nil
	}
	if k.err != nil {
		return nil, k.err
	}

	var out bytes.Buffer
	armoured := armor.NewWriter(&out)
	w, err := age.Encrypt(armoured, k.recipient)
	if err != nil {
		return nil, fmt.Errorf("encrypting: %w", err)
	}
	if _, err := w.Write(data); err != nil {
		return nil, fmt.Errorf("encrypting: %w", err)
	}
	if err := errors.Join(w.Close(), armoured.Close()); err != nil {
		return nil, fmt.Errorf("encrypting: %w", err)
	}
	return out.Bytes(), nil
}

// Decrypt returns what data was before it was encrypted. Data that is not
// encrypted is returned as it is, so that files from before there were keys
// can still be read.
func (k *Keys) Decrypt(data []byte) ([]byte, error) {
	if k != nil && k.err != nil {
		return nil, k.err
	}
	if !Encrypted(data) {
		return data, nil
	}
	if k == nil {
		return nil, errors.New("it is encrypted, and the settings have no keys to decrypt it with")
	}
	if k.identity == nil {
		return nil, errors.New("it is encrypted, and there is no private key to decrypt it with")
	}

	var src io.Reader = bytes.NewReader(data)
	if bytes.HasPrefix(data, []byte(armor.Header)) {
		src = armor.NewReader(src)
	}
	r, err := age.Decrypt(src, k.identity)
	if err != nil {
		return nil, fmt.Errorf("decrypting: %w", err)
	}
	decrypted, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("decrypting: %w", err)
	}
	return decrypted, nil
}

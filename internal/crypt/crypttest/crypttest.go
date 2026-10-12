// Package crypttest makes SSH keys for tests to encrypt with.
package crypttest

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/ssh"
)

// Pair writes a new pair of Ed25519 keys to files named name and name.pub in
// a folder, and returns the paths of the public and of the private key. The
// private key is protected by the passphrase unless it is empty.
func Pair(t testing.TB, dir, name, passphrase string) (public, private string) {
	t.Helper()

	public, private, err := Write(dir, name, passphrase)
	if err != nil {
		t.Fatal(err)
	}
	return public, private
}

// Write does what Pair does, for where there is no test to fail.
func Write(dir, name, passphrase string) (public, private string, err error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", "", err
	}
	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		return "", "", err
	}

	var block *pem.Block
	if passphrase == "" {
		block, err = ssh.MarshalPrivateKey(priv, "")
	} else {
		block, err = ssh.MarshalPrivateKeyWithPassphrase(priv, "", []byte(passphrase))
	}
	if err != nil {
		return "", "", err
	}

	private = filepath.Join(dir, name)
	public = private + ".pub"
	if err := os.WriteFile(private, pem.EncodeToMemory(block), 0o600); err != nil {
		return "", "", err
	}
	if err := os.WriteFile(public, ssh.MarshalAuthorizedKey(sshPub), 0o644); err != nil {
		return "", "", err
	}
	return public, private, nil
}

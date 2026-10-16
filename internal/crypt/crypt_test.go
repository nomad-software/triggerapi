package crypt_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nomad-software/triggerapi/internal/crypt"
	"github.com/nomad-software/triggerapi/internal/crypt/crypttest"
)

// noPassphrase fails a test which asks for a passphrase.
func noPassphrase(t *testing.T) func() ([]byte, error) {
	return func() ([]byte, error) {
		t.Error("a passphrase was asked for")
		return nil, errors.New("none")
	}
}

func TestRoundTrip(t *testing.T) {
	public, private := crypttest.Pair(t, t.TempDir(), "id", "")
	keys, err := crypt.Open(public, private, noPassphrase(t))
	if err != nil {
		t.Fatal(err)
	}

	const text = `{"name": "secret"}`
	encrypted, err := keys.Encrypt([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	if !crypt.Encrypted(encrypted) || strings.Contains(string(encrypted), "secret") {
		t.Errorf("encrypted = %s", encrypted)
	}
	if decrypted, err := keys.Decrypt(encrypted); err != nil || string(decrypted) != text {
		t.Errorf("decrypted = %q, %v", decrypted, err)
	}

	// What is not encrypted is read as it is.
	if got, err := keys.Decrypt([]byte(text)); err != nil || string(got) != text {
		t.Errorf("plain = %q, %v", got, err)
	}

	// Other keys cannot read it.
	otherPublic, otherPrivate := crypttest.Pair(t, t.TempDir(), "id", "")
	other, err := crypt.Open(otherPublic, otherPrivate, noPassphrase(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := other.Decrypt(encrypted); err == nil {
		t.Error("other keys decrypted it")
	}
}

func TestNoKeys(t *testing.T) {
	var none *crypt.Keys

	if got, err := none.Encrypt([]byte("text")); err != nil || string(got) != "text" {
		t.Errorf("encrypted = %q, %v", got, err)
	}
	if got, err := none.Decrypt([]byte("text")); err != nil || string(got) != "text" {
		t.Errorf("decrypted = %q, %v", got, err)
	}
	if none.Err() != nil {
		t.Errorf("err = %v", none.Err())
	}

	public, private := crypttest.Pair(t, t.TempDir(), "id", "")
	keys, err := crypt.Open(public, private, noPassphrase(t))
	if err != nil {
		t.Fatal(err)
	}
	encrypted, _ := keys.Encrypt([]byte("text"))
	if _, err := none.Decrypt(encrypted); err == nil || !strings.Contains(err.Error(), "no keys") {
		t.Errorf("err = %v", err)
	}
}

func TestPassphrase(t *testing.T) {
	public, private := crypttest.Pair(t, t.TempDir(), "id", "hunter2")

	asked := 0
	keys, err := crypt.Open(public, private, func() ([]byte, error) {
		asked++
		return []byte("hunter2"), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	encrypted, _ := keys.Encrypt([]byte("text"))
	if got, err := keys.Decrypt(encrypted); err != nil || string(got) != "text" {
		t.Errorf("decrypted = %q, %v", got, err)
	}
	if asked != 1 {
		t.Errorf("the passphrase was asked for %d times", asked)
	}

	if _, err := crypt.Open(public, private, func() ([]byte, error) { return []byte("wrong"), nil }); err == nil {
		t.Error("the wrong passphrase was accepted")
	}
	if _, err := crypt.Open(public, private, func() ([]byte, error) { return nil, errors.New("cancelled") }); err == nil {
		t.Error("no passphrase was accepted")
	}
}

func TestOpenFailures(t *testing.T) {
	dir := t.TempDir()
	public, private := crypttest.Pair(t, dir, "id", "")
	otherPublic, _ := crypttest.Pair(t, dir, "other", "")
	junk := filepath.Join(dir, "junk")
	os.WriteFile(junk, []byte("not a key"), 0o600)

	tests := []struct {
		name, public, private, want string
	}{
		{"one key", public, "", "both"},
		{"missing public", filepath.Join(dir, "none.pub"), private, "public key could not be read"},
		{"missing private", public, filepath.Join(dir, "none"), "private key could not be read"},
		{"junk public", junk, private, "not a public key"},
		{"junk private", public, junk, "not a private key"},
		{"not a pair", otherPublic, private, "not the one that goes with"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := crypt.Open(tt.public, tt.private, noPassphrase(t)); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("err = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestHomePath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	crypttest.Pair(t, home, "id", "")

	if _, err := crypt.Open("~/id.pub", "~/id", noPassphrase(t)); err != nil {
		t.Error(err)
	}
}

func TestUnusable(t *testing.T) {
	reason := errors.New("no good")
	keys := crypt.Unusable(reason)

	if _, err := keys.Encrypt([]byte("text")); !errors.Is(err, reason) {
		t.Errorf("encrypt err = %v", err)
	}
	// Not even what is not encrypted is read, as it could not be saved.
	if _, err := keys.Decrypt([]byte("text")); !errors.Is(err, reason) {
		t.Errorf("decrypt err = %v", err)
	}
	if !errors.Is(keys.Err(), reason) {
		t.Errorf("err = %v", keys.Err())
	}
}

func TestRecipient(t *testing.T) {
	public, private := crypttest.Pair(t, t.TempDir(), "id", "")
	recipient, err := crypt.Recipient(public)
	if err != nil {
		t.Fatal(err)
	}

	// What is encrypted for someone is read with their private key, and
	// not with the keys that encrypted it.
	encrypted, err := recipient.Encrypt([]byte("for them"))
	if err != nil || !crypt.Encrypted(encrypted) {
		t.Fatalf("encrypted %s, %v", encrypted, err)
	}
	theirs, err := crypt.Open(public, private, noPassphrase(t))
	if err != nil {
		t.Fatal(err)
	}
	if text, err := theirs.Decrypt(encrypted); err != nil || string(text) != "for them" {
		t.Errorf("decrypted by them %q, %v", text, err)
	}
	if _, err := recipient.Decrypt(encrypted); err == nil {
		t.Error("a public key decrypted")
	}

	for _, path := range []string{"", filepath.Join(t.TempDir(), "none.pub"), private} {
		if _, err := crypt.Recipient(path); err == nil {
			t.Errorf("%q was taken for a public key", path)
		}
	}
}

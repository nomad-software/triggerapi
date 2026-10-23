package jsonfile

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/nomad-software/triggerapi/internal/crypt"
	"github.com/nomad-software/triggerapi/internal/crypt/crypttest"
)

func openKeys(t *testing.T, name string) *crypt.Keys {
	t.Helper()
	public, private := crypttest.Pair(t, t.TempDir(), name, "")
	keys, err := crypt.Open(public, private, nil)
	if err != nil {
		t.Fatal(err)
	}
	return keys
}

type document struct {
	Name string `json:"name"`
}

func TestEncryptedRoundTrip(t *testing.T) {
	keys := openKeys(t, "id")
	path := filepath.Join(t.TempDir(), "doc.json")

	if err := SaveWith(path, document{Name: "secret"}, keys); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(path); !crypt.Encrypted(data) || strings.Contains(string(data), "secret") {
		t.Errorf("file = %s", data)
	}

	var got document
	if err := LoadWith(path, &got, keys); err != nil || got.Name != "secret" {
		t.Errorf("loaded %+v, %v", got, err)
	}
	// It cannot be read without the keys, or with others.
	if err := Load(path, &got); err == nil {
		t.Error("loaded without keys")
	}
	if err := LoadWith(path, &got, openKeys(t, "other")); err == nil {
		t.Error("loaded with other keys")
	}

	// A file that is not encrypted is read whatever the keys.
	if err := Save(path, document{Name: "plain"}); err != nil {
		t.Fatal(err)
	}
	if err := LoadWith(path, &got, keys); err != nil || got.Name != "plain" {
		t.Errorf("loaded %+v, %v", got, err)
	}
}

func TestConvert(t *testing.T) {
	first, second := openKeys(t, "first"), openKeys(t, "second")
	path := filepath.Join(t.TempDir(), "doc.json")

	load := func(keys *crypt.Keys) string {
		t.Helper()
		var got document
		if err := LoadWith(path, &got, keys); err != nil {
			t.Fatal(err)
		}
		return got.Name
	}
	encrypted := func() bool {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return crypt.Encrypted(data)
	}

	// A file that is missing is left so.
	if err := Convert(path, nil, first); err != nil {
		t.Error(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("a file was made")
	}

	if err := Save(path, document{Name: "kept"}); err != nil {
		t.Fatal(err)
	}

	// Encrypted, with the same keys given for both as when it is loaded.
	if err := Convert(path, first, first); err != nil {
		t.Fatal(err)
	}
	if !encrypted() || load(first) != "kept" {
		t.Error("not encrypted with the first keys")
	}
	before, _ := os.ReadFile(path)
	if err := Convert(path, first, first); err != nil {
		t.Fatal(err)
	}
	if after, _ := os.ReadFile(path); string(after) != string(before) {
		t.Error("a file already encrypted was written again")
	}

	// Given other keys.
	if err := Convert(path, first, second); err != nil {
		t.Fatal(err)
	}
	if !encrypted() || load(second) != "kept" {
		t.Error("not encrypted with the second keys")
	}
	// A file which already has the keys it is to have is not a failure.
	if err := Convert(path, first, second); err != nil {
		t.Error(err)
	}
	// One which neither can read is, and is left alone.
	if err := Convert(path, first, nil); err == nil {
		t.Error("decrypted with the wrong keys")
	}
	if !encrypted() || load(second) != "kept" {
		t.Error("the file was changed")
	}

	// Decrypted.
	if err := Convert(path, second, nil); err != nil {
		t.Fatal(err)
	}
	if encrypted() || load(nil) != "kept" {
		t.Error("not decrypted")
	}

	// Keys that cannot be used encrypt nothing.
	if err := Convert(path, nil, crypt.Unusable(os.ErrInvalid)); err == nil {
		t.Error("converted with keys that cannot be used")
	}
	if encrypted() || load(nil) != "kept" {
		t.Error("the file was changed")
	}
}

func TestFilePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permissions are not kept this way on Windows")
	}
	path := filepath.Join(t.TempDir(), "doc.json")

	// A file anyone could read is replaced by one only the user can.
	if err := os.WriteFile(path, []byte(`{"name":"old"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Save(path, document{Name: "new"}); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, %v", info.Mode().Perm(), err)
	}

	if err := Convert(path, nil, openKeys(t, "id")); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("mode after converting = %v, %v", info.Mode().Perm(), err)
	}
}

// A file is written as json.MarshalIndent makes it, with a newline at the
// end, although it is indented apart from being made into JSON.
func TestSaveFormat(t *testing.T) {
	type nested struct {
		Name  string            `json:"name"`
		Items []document        `json:"items"`
		Map   map[string]string `json:"map"`
		Empty []string          `json:"empty"`
		HTML  string            `json:"html"`
	}
	v := nested{Name: "n", Items: []document{{Name: "a"}, {}}, Map: map[string]string{"b": "2", "a": "1"}, Empty: []string{}, HTML: "<a href=\"x\">&</a>"}
	path := filepath.Join(t.TempDir(), "doc.json")
	if err := Save(path, v); err != nil {
		t.Fatal(err)
	}
	want, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); string(got) != string(want)+"\n" {
		t.Errorf("file = %s, want %s", got, want)
	}
}

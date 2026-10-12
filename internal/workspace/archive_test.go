package workspace

import (
	"archive/zip"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/nomad-software/triggerapi/internal/crypt"
	"github.com/nomad-software/triggerapi/internal/crypt/crypttest"
)

func testKeys(t *testing.T, name string) (keys *crypt.Keys, public string) {
	t.Helper()
	public, private := crypttest.Pair(t, t.TempDir(), name, "")
	keys, err := crypt.Open(public, private, nil)
	if err != nil {
		t.Fatal(err)
	}
	return keys, public
}

// writeEncrypted writes text to a file of a workspace, encrypted.
func writeEncrypted(t *testing.T, path, text string, keys *crypt.Keys) {
	t.Helper()
	data, err := keys.Encrypt([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

// readDecrypted returns what is in an encrypted file.
func readDecrypted(t *testing.T, path string, keys *crypt.Keys) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !crypt.Encrypted(data) {
		t.Errorf("%s is not encrypted", path)
	}
	plain, err := keys.Decrypt(data)
	if err != nil {
		t.Fatal(err)
	}
	return string(plain)
}

func TestExportAndUnpack(t *testing.T) {
	mine, _ := testKeys(t, "mine")
	theirs, theirPublic := testKeys(t, "theirs")
	recipient, err := crypt.Recipient(theirPublic)
	if err != nil {
		t.Fatal(err)
	}

	from := filepath.Join(t.TempDir(), "workspaces")
	for _, name := range []string{"Hotel", "Empty"} {
		if err := Create(from, name); err != nil {
			t.Fatal(err)
		}
	}
	writeEncrypted(t, Environments(from, "Hotel"), `{"environments":"hotel"}`, mine)
	writeEncrypted(t, Requests(from, "Hotel"), `{"folders":"hotel"}`, mine)
	before, _ := os.ReadFile(Requests(from, "Hotel"))

	archive := filepath.Join(t.TempDir(), "export.zip")
	if err := Export(from, archive, mine, recipient); err != nil {
		t.Fatal(err)
	}
	// What was exported is left as it was.
	if after, _ := os.ReadFile(Requests(from, "Hotel")); string(after) != string(before) {
		t.Error("the exported workspace was changed")
	}

	// It cannot be read back with the keys it was exported from.
	to := filepath.Join(t.TempDir(), "workspaces")
	if _, err := Unpack(to, archive, mine); err == nil || !strings.Contains(err.Error(), "cannot be read with the keys") {
		t.Errorf("err = %v", err)
	}
	if names, _ := List(to); len(names) != 0 {
		t.Errorf("workspaces were added: %q", names)
	}

	// Whoever it was exported for can, and a name that is taken is not
	// written over.
	if err := Create(to, "Hotel"); err != nil {
		t.Fatal(err)
	}
	added, err := Unpack(to, archive, theirs)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"Empty", "Hotel 2"}; !reflect.DeepEqual(added, want) {
		t.Errorf("added = %q, want %q", added, want)
	}
	if got := readDecrypted(t, Environments(to, "Hotel 2"), theirs); got != `{"environments":"hotel"}` {
		t.Errorf("environments = %s", got)
	}
	if got := readDecrypted(t, Requests(to, "Hotel 2"), theirs); got != `{"folders":"hotel"}` {
		t.Errorf("requests = %s", got)
	}
	if entries, _ := os.ReadDir(filepath.Join(to, "Hotel")); len(entries) != 0 {
		t.Error("the workspace already there was written to")
	}
	if entries, err := os.ReadDir(filepath.Join(to, "Empty")); err != nil || len(entries) != 0 {
		t.Errorf("the empty workspace has %d files, %v", len(entries), err)
	}
}

func TestExportFailures(t *testing.T) {
	mine, _ := testKeys(t, "mine")
	other, _ := testKeys(t, "other")
	dir := filepath.Join(t.TempDir(), "workspaces")
	archive := filepath.Join(t.TempDir(), "export.zip")

	if err := Export(dir, archive, mine, other); err == nil {
		t.Error("exported no workspaces")
	}
	if err := Create(dir, "Hotel"); err != nil {
		t.Fatal(err)
	}
	if err := Export(dir, archive, nil, other); err == nil {
		t.Error("exported without keys")
	}

	// A file the keys cannot read is not exported, and no archive is left.
	writeEncrypted(t, Requests(dir, "Hotel"), "{}", other)
	if err := Export(dir, archive, mine, mine); err == nil {
		t.Error("exported a file that could not be read")
	}
	if entries, _ := os.ReadDir(filepath.Dir(archive)); len(entries) != 0 {
		t.Errorf("%d files were left", len(entries))
	}
}

func TestUnpackRefusesOtherFiles(t *testing.T) {
	keys, _ := testKeys(t, "mine")
	dir := filepath.Join(t.TempDir(), "workspaces")

	for _, name := range []string{"../escape/requests.json", "Hotel/other.txt", "Hotel/sub/requests.json", "requests.json", "/Hotel/requests.json"} {
		archive := filepath.Join(t.TempDir(), "bad.zip")
		file, err := os.Create(archive)
		if err != nil {
			t.Fatal(err)
		}
		w := zip.NewWriter(file)
		entry, _ := w.Create(name)
		entry.Write([]byte("{}"))
		w.Close()
		file.Close()

		if added, err := Unpack(dir, archive, keys); err == nil || len(added) != 0 {
			t.Errorf("%s: added %q, err %v", name, added, err)
		}
	}
	if names, _ := List(dir); len(names) != 0 {
		t.Errorf("workspaces were added: %q", names)
	}

	notZip := filepath.Join(t.TempDir(), "not.zip")
	os.WriteFile(notZip, []byte("not a zip"), 0o600)
	if _, err := Unpack(dir, notZip, keys); err == nil {
		t.Error("unpacked a file that is not a zip")
	}
}

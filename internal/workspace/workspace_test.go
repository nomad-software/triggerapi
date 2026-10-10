package workspace

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func list(t *testing.T, dir string) []string {
	t.Helper()
	names, err := List(dir)
	if err != nil {
		t.Fatal(err)
	}
	return names
}

func TestWorkspaces(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "workspaces")

	// A folder that does not exist has no workspaces, and is made for the
	// first.
	if names := list(t, dir); len(names) != 0 {
		t.Fatalf("workspaces = %q", names)
	}
	for _, name := range []string{"beta", "Alpha", "Gamma"} {
		if err := Create(dir, name); err != nil {
			t.Fatal(err)
		}
	}
	// Files and hidden folders are not workspaces.
	os.WriteFile(filepath.Join(dir, "notes.txt"), nil, 0o644)
	os.Mkdir(filepath.Join(dir, ".git"), 0o755)
	if got, want := list(t, dir), []string{"Alpha", "beta", "Gamma"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("workspaces = %q, want %q", got, want)
	}

	if got := Environments(dir, "Alpha"); got != filepath.Join(dir, "Alpha", "environments.json") {
		t.Errorf("environments = %q", got)
	}
	if got := Requests(dir, "Alpha"); got != filepath.Join(dir, "Alpha", "requests.json") {
		t.Errorf("requests = %q", got)
	}

	// Names are unique whatever the case of their letters.
	if err := Create(dir, "ALPHA"); err == nil || !strings.Contains(err.Error(), "already") {
		t.Errorf("created a duplicate: %v", err)
	}
	if got := Unique(dir, "alpha"); got != "alpha 2" {
		t.Errorf("unique = %q", got)
	}
	if got := Unique(dir, "Delta"); got != "Delta" {
		t.Errorf("unique = %q", got)
	}

	// A workspace keeps what is in it when it is renamed.
	os.WriteFile(Requests(dir, "beta"), []byte("{}"), 0o644)
	if err := Rename(dir, "beta", "Gamma"); err == nil {
		t.Error("renamed to a name that is taken")
	}
	if err := Rename(dir, "beta", "a/b"); err == nil {
		t.Error("renamed to a name that is not valid")
	}
	if err := Rename(dir, "beta", "Zeta"); err != nil {
		t.Fatal(err)
	}
	if err := Rename(dir, "Zeta", "ZETA"); err != nil {
		t.Errorf("could not change the case of a name: %v", err)
	}
	if data, _ := os.ReadFile(Requests(dir, "ZETA")); string(data) != "{}" {
		t.Errorf("requests after rename = %q", data)
	}
	if got, want := list(t, dir), []string{"Alpha", "Gamma", "ZETA"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("workspaces = %q, want %q", got, want)
	}

	// Removing one deletes its files and its folder.
	os.WriteFile(Environments(dir, "ZETA"), []byte("{}"), 0o644)
	if err := Remove(dir, "ZETA"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "ZETA")); err == nil {
		t.Error("the folder was not removed")
	}

	// One with anything else in its folder is left alone.
	os.WriteFile(Requests(dir, "Gamma"), []byte("{}"), 0o644)
	os.WriteFile(filepath.Join(dir, "Gamma", "mine.txt"), []byte("keep"), 0o644)
	if err := Remove(dir, "Gamma"); err == nil || !strings.Contains(err.Error(), "mine.txt") {
		t.Errorf("removed a folder with other files: %v", err)
	}
	if data, _ := os.ReadFile(Requests(dir, "Gamma")); string(data) != "{}" {
		t.Error("requests were deleted from a folder that was left")
	}
	if err := Remove(dir, ".."); err == nil {
		t.Error("removed the folder above")
	}
}

func TestValid(t *testing.T) {
	for _, name := range []string{"Default", "My API (v2)", "données", "a.b"} {
		if err := Valid(name); err != nil {
			t.Errorf("Valid(%q) = %v", name, err)
		}
	}
	for _, name := range []string{"", " ", " a", "a ", ".", "..", ".hidden", "a.", "a/b", `a\b`, "a:b", "a\nb", "a?", strings.Repeat("a", 101)} {
		if err := Valid(name); err == nil {
			t.Errorf("Valid(%q) = nil", name)
		}
	}
}

func TestImport(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "workspaces")
	environments := filepath.Join(root, "environments.json")
	requests := filepath.Join(root, "requests.json")
	os.WriteFile(environments, []byte("envs"), 0o644)

	if err := Create(dir, Default); err != nil {
		t.Fatal(err)
	}
	// A file that is missing is skipped.
	if err := Import(dir, Default, environments, requests); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(Environments(dir, Default)); string(data) != "envs" {
		t.Errorf("environments = %q", data)
	}
	if _, err := os.Stat(Requests(dir, Default)); err == nil {
		t.Error("requests were made from nothing")
	}
	// The files copied from are left where they were.
	if data, _ := os.ReadFile(environments); string(data) != "envs" {
		t.Error("the file copied from was changed")
	}

	// Nothing already there is replaced.
	os.WriteFile(environments, []byte("newer"), 0o644)
	os.WriteFile(requests, []byte("reqs"), 0o644)
	if err := Import(dir, Default, environments, requests); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(Environments(dir, Default)); string(data) != "envs" {
		t.Errorf("environments = %q", data)
	}
	if data, _ := os.ReadFile(Requests(dir, Default)); string(data) != "reqs" {
		t.Errorf("requests = %q", data)
	}
}

func TestCopyAll(t *testing.T) {
	root := t.TempDir()
	from, to := filepath.Join(root, "from"), filepath.Join(root, "to")
	Create(from, "A")
	Create(from, "B")
	os.WriteFile(Environments(from, "A"), []byte("a envs"), 0o644)
	os.WriteFile(Requests(from, "B"), []byte("b reqs"), 0o644)

	if err := CopyAll(from, to); err != nil {
		t.Fatal(err)
	}
	if got, want := list(t, to), []string{"A", "B"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("workspaces = %q, want %q", got, want)
	}
	a, _ := os.ReadFile(Environments(to, "A"))
	b, _ := os.ReadFile(Requests(to, "B"))
	if string(a) != "a envs" || string(b) != "b reqs" || len(list(t, from)) != 2 {
		t.Errorf("copied %q and %q", a, b)
	}
}

func TestDuplicate(t *testing.T) {
	dir := t.TempDir()
	Create(dir, "Hotel")
	os.WriteFile(Environments(dir, "Hotel"), []byte("envs"), 0o644)
	os.WriteFile(Requests(dir, "Hotel"), []byte("reqs"), 0o644)

	name, err := Duplicate(dir, "Hotel")
	if err != nil || name != "Hotel copy" {
		t.Fatalf("duplicate = %q, %v", name, err)
	}
	envs, _ := os.ReadFile(Environments(dir, name))
	reqs, _ := os.ReadFile(Requests(dir, name))
	if string(envs) != "envs" || string(reqs) != "reqs" {
		t.Errorf("copied %q and %q", envs, reqs)
	}

	// Another copy has another name, and one with nothing in it copies.
	if name, err := Duplicate(dir, "Hotel"); err != nil || name != "Hotel copy 2" {
		t.Errorf("duplicate = %q, %v", name, err)
	}
	Create(dir, "Empty")
	if name, err := Duplicate(dir, "Empty"); err != nil || name != "Empty copy" {
		t.Errorf("duplicate = %q, %v", name, err)
	}
	if got, want := list(t, dir), []string{"Empty", "Empty copy", "Hotel", "Hotel copy", "Hotel copy 2"}; !reflect.DeepEqual(got, want) {
		t.Errorf("workspaces = %q, want %q", got, want)
	}

	if _, err := Duplicate(dir, "Missing"); err == nil {
		t.Error("duplicated a workspace that is not there")
	}
	long := strings.Repeat("a", 100)
	Create(dir, long)
	if name, err := Duplicate(dir, long); err != nil || Valid(name) != nil {
		t.Errorf("duplicate of a long name = %q, %v", name, err)
	}
}

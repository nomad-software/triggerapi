package homedir

import (
	"path/filepath"
	"testing"
)

func TestExpand(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	for path, want := range map[string]string{
		"~":           home,
		"~/.ssh/id":   filepath.Join(home, ".ssh", "id"),
		"~user/a":     "~user/a",
		"/etc/a~":     "/etc/a~",
		"relative/~a": "relative/~a",
	} {
		if got := Expand(path); got != want {
			t.Errorf("Expand(%q) = %q, want %q", path, got, want)
		}
	}
}

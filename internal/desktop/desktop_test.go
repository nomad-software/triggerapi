package desktop

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestEntry(t *testing.T) {
	entry := Entry("/home/me/bin/triggerapi", "triggerapi")
	for _, line := range []string{
		"[Desktop Entry]",
		"Type=Application",
		"Name=TriggerAPI",
		"Exec=/home/me/bin/triggerapi",
		"TryExec=/home/me/bin/triggerapi",
		"Icon=triggerapi",
		"StartupWMClass=triggerapi",
	} {
		if !strings.Contains(entry, line+"\n") {
			t.Errorf("no %q in\n%s", line, entry)
		}
	}
	if !strings.HasPrefix(entry, "[Desktop Entry]\n") {
		t.Errorf("entry does not start with its group:\n%s", entry)
	}
}

func TestExecQuoting(t *testing.T) {
	for path, want := range map[string]string{
		"/opt/triggerapi":                      "/opt/triggerapi",
		"/home/me/My Apps/TriggerAPI.AppImage": `"/home/me/My Apps/TriggerAPI.AppImage"`,
		`/tmp/a"b`:                             `"/tmp/a\\"b"`,
		"/tmp/$HOME/x":                         `"/tmp/\\$HOME/x"`,
		"/tmp/100%/x":                          "/tmp/100%%/x",
		`/tmp/back\slash`:                      `"/tmp/back\\\\slash"`,
	} {
		entry := Entry(path, "x")
		if !strings.Contains(entry, "\nExec="+want+"\n") {
			t.Errorf("Exec for %q: want %q in\n%s", path, want, entry)
		}
	}
}

func TestExecutable(t *testing.T) {
	// An AppImage is started again through the AppImage itself.
	t.Setenv("APPIMAGE", "/home/me/Apps/TriggerAPI-x86_64.AppImage")
	if got, err := Executable(); err != nil || got != "/home/me/Apps/TriggerAPI-x86_64.AppImage" {
		t.Errorf("in an AppImage: %q, %v", got, err)
	}

	// Otherwise it is the program itself, with any links followed.
	t.Setenv("APPIMAGE", "")
	got, err := Executable()
	if err != nil || !filepath.IsAbs(got) {
		t.Fatalf("Executable() = %q, %v", got, err)
	}
	if resolved, _ := filepath.EvalSymlinks(got); resolved != got {
		t.Errorf("links not followed: %q is %q", got, resolved)
	}
}

func TestDir(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "/data/home")
	if got, err := Dir(); err != nil || got != "/data/home" {
		t.Errorf("with XDG_DATA_HOME: %q, %v", got, err)
	}
	// A relative one is ignored, as the specification says.
	t.Setenv("XDG_DATA_HOME", "relative")
	t.Setenv("HOME", "/home/me")
	if got, err := Dir(); err != nil || got != "/home/me/.local/share" {
		t.Errorf("without XDG_DATA_HOME: %q, %v", got, err)
	}
}

func TestWrite(t *testing.T) {
	data := t.TempDir()
	entryPath := filepath.Join(data, "applications", "triggerapi.desktop")
	iconPath := filepath.Join(data, "icons", "hicolor", "256x256", "apps", "triggerapi.png")

	if err := Write(data, "/opt/one/triggerapi", "triggerapi", []byte("png")); err != nil {
		t.Fatal(err)
	}
	entry, err := os.ReadFile(entryPath)
	if err != nil || !strings.Contains(string(entry), "Exec=/opt/one/triggerapi\n") {
		t.Fatalf("entry %q, %v", entry, err)
	}
	if icon, err := os.ReadFile(iconPath); err != nil || string(icon) != "png" {
		t.Errorf("icon %q, %v", icon, err)
	}
	if info, err := os.Stat(entryPath); err != nil || info.Mode().Perm() != 0o644 {
		t.Errorf("entry mode %v, %v", info.Mode(), err)
	}

	// The same again leaves the files alone.
	old := time.Now().Add(-time.Hour)
	os.Chtimes(entryPath, old, old)
	if err := Write(data, "/opt/one/triggerapi", "triggerapi", []byte("png")); err != nil {
		t.Fatal(err)
	}
	if info, _ := os.Stat(entryPath); !info.ModTime().Equal(old) {
		t.Error("an entry that was already right was written again")
	}

	// Run from elsewhere, or under another name, it is rewritten.
	if err := Write(data, "/home/me/Apps/TriggerAPI.AppImage", "triggerapi", []byte("png")); err != nil {
		t.Fatal(err)
	}
	entry, _ = os.ReadFile(entryPath)
	if !strings.Contains(string(entry), "Exec=/home/me/Apps/TriggerAPI.AppImage\n") || strings.Contains(string(entry), "/opt/one") {
		t.Errorf("entry not rewritten:\n%s", entry)
	}
	// No temporary files are left behind.
	if files, _ := os.ReadDir(filepath.Dir(entryPath)); len(files) != 1 {
		t.Errorf("files left in applications: %v", files)
	}
}

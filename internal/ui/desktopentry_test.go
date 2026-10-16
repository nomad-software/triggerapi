package ui

import (
	"bytes"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mappu/miqt/qt6/mainthread"
)

func TestInstallDesktopEntry(t *testing.T) {
	data := t.TempDir()
	t.Setenv("XDG_DATA_HOME", data)
	t.Setenv("APPIMAGE", "/home/me/Apps/My TriggerAPI.AppImage")

	var err error
	mainthread.Wait(func() { err = InstallDesktopEntry() })
	if err != nil {
		t.Fatal(err)
	}

	// The entry starts the AppImage, quoted as its name has a space.
	entry, err := os.ReadFile(filepath.Join(data, "applications", "triggerapi.desktop"))
	if err != nil {
		t.Fatal(err)
	}
	self, _ := os.Executable()
	// The icon is named by its path, not looked for in the theme.
	image := filepath.Join(data, "icons", "hicolor", "256x256", "apps", "triggerapi.png")
	for _, line := range []string{`Exec="/home/me/Apps/My TriggerAPI.AppImage"`, "Icon=" + image, "StartupWMClass=" + filepath.Base(self)} {
		if !strings.Contains(string(entry), line+"\n") {
			t.Errorf("no %q in\n%s", line, entry)
		}
	}

	// The icon is the program's, at 256 pixels.
	icon, err := os.ReadFile(image)
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(icon))
	if err != nil || img.Bounds().Dx() != 256 || img.Bounds().Dy() != 256 {
		t.Errorf("icon %v, %v", img.Bounds(), err)
	}
}

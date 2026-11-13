package ui

import (
	"github.com/nomad-software/qute"

	"bytes"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/nomad-software/triggerapi/internal/appicon"
)

// iconsEnv names the folder of the repository, which TestWriteIcons writes
// the icons into. The test is skipped without it; make icons sets it.
const iconsEnv = "TRIGGERAPI_ICONS"

// TestWriteIcons draws the program's icon into
// internal/appicon/appicon.png, which is built into the program and is the
// icon of the desktop entry and the AppImage. Run make icons again whenever
// the icon changes.
func TestWriteIcons(t *testing.T) {
	dir := os.Getenv(iconsEnv)
	if dir == "" {
		t.Skip(iconsEnv + " is not set")
	}
	var data []byte
	qute.PostSync(func() { data = appIconPNG(appicon.Size) })
	if data == nil {
		t.Fatal("the icon could not be drawn")
	}
	path := filepath.Join(dir, "internal", "appicon", "appicon.png")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestAppIconPNG(t *testing.T) {
	var data []byte
	qute.PostSync(func() { data = appIconPNG(appicon.Size) })
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil || img.Bounds().Dx() != appicon.Size || img.Bounds().Dy() != appicon.Size {
		t.Fatalf("icon %v, %v", img, err)
	}
	// The corners are clear, as the icon is rounded, and the middle is not.
	if _, _, _, a := img.At(0, 0).RGBA(); a != 0 {
		t.Error("the corner is not clear")
	}
	if _, _, _, a := img.At(appicon.Size/2, appicon.Size/2).RGBA(); a == 0 {
		t.Error("the middle is clear")
	}
}

package ui

import (
	"bytes"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/mappu/miqt/qt6/mainthread"
)

// iconsEnv names the folder TestWriteIcons writes the icons of the packages
// into. The test is skipped without it; make icons sets it to packaging.
const iconsEnv = "TRIGGERAPI_ICONS"

// macIconSize and macIconMargin are the size of the icon of the macOS app
// and the margin Apple's grid gives the shape of an icon inside it, which
// the icons of the Dock are drawn with.
const (
	macIconSize   = 1024
	macIconMargin = 100
)

// TestWriteIcons draws the program's icon into the files the packages are
// made from: triggerapi.png for Linux, as the desktop entry has it, and
// macos/triggerapi.png for macOS, from which the .icns of the app is made.
// Run make icons again whenever the icon changes.
func TestWriteIcons(t *testing.T) {
	dir := os.Getenv(iconsEnv)
	if dir == "" {
		t.Skip(iconsEnv + " is not set")
	}
	for _, icon := range []struct {
		path         string
		size, margin int
	}{
		{"triggerapi.png", desktopIconSize, 0},
		{filepath.Join("macos", "triggerapi.png"), macIconSize, macIconMargin},
	} {
		var data []byte
		mainthread.Wait(func() { data = appIconPNG(icon.size, icon.margin) })
		if data == nil {
			t.Fatalf("%s could not be drawn", icon.path)
		}
		path := filepath.Join(dir, icon.path)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestAppIconPNG(t *testing.T) {
	var data []byte
	mainthread.Wait(func() { data = appIconPNG(macIconSize, macIconMargin) })
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil || img.Bounds().Dx() != macIconSize || img.Bounds().Dy() != macIconSize {
		t.Fatalf("icon %v, %v", img, err)
	}
	// The margin is clear, and the shape inside it is not.
	if _, _, _, a := img.At(macIconMargin/2, macIconSize/2).RGBA(); a != 0 {
		t.Error("the margin is not clear")
	}
	if _, _, _, a := img.At(macIconSize/2, macIconSize/2).RGBA(); a == 0 {
		t.Error("the middle is clear")
	}
}

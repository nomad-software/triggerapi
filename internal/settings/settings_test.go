package settings

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")

	want := Settings{Width: 900, Height: 600, Maximized: true, ResponseFont: "Hack"}
	if err := want.Save(path); err != nil {
		t.Fatal(err)
	}
	if got := Load(path); got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestLoadDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	want := Settings{Width: defaultWidth, Height: defaultHeight, ResponseFont: defaultResponseFont}

	if got := Load(path); got != want {
		t.Errorf("missing file: got %+v", got)
	}

	os.WriteFile(path, []byte("{oops"), 0o644)
	if got := Load(path); got != want {
		t.Errorf("invalid file: got %+v", got)
	}

	os.WriteFile(path, []byte(`{"width": 5, "height": 9000, "maximized": true}`), 0o644)
	if got := Load(path); got != (Settings{Width: defaultWidth, Height: defaultHeight, Maximized: true, ResponseFont: defaultResponseFont}) {
		t.Errorf("bad size: got %+v", got)
	}
}

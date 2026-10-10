package settings

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSaveLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")

	want := Settings{Width: 900, Height: 600, Maximized: true, SidebarWidth: 310, ResponseHeight: 240, ResponseFont: "Hack", EnvironmentsFile: "/a/e.json", RequestsFile: "r.json", JSONColours: JSONColours{Key: "#A31515", Number: "#098658"}}
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

	os.WriteFile(path, []byte(`{"width": 5, "height": 9000, "maximized": true, "sidebarWidth": -4, "responseHeight": -1}`), 0o644)
	if got := Load(path); got != (Settings{Width: defaultWidth, Height: defaultHeight, Maximized: true, ResponseFont: defaultResponseFont}) {
		t.Errorf("bad size: got %+v", got)
	}
}

func TestLoadInvalidColours(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	os.WriteFile(path, []byte(`{"jsonColours": {"key": "red", "string": "#12345", "number": "#00ff00", "literal": "#gggggg"}}`), 0o644)

	if got, want := Load(path).JSONColours, (JSONColours{Number: "#00ff00"}); got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestDefaultColoursNotSaved(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := (Settings{Width: 900, Height: 600}).Save(path); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(path); strings.Contains(string(data), "jsonColours") {
		t.Errorf("default colours were saved: %s", data)
	}
}

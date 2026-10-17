package settings

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestSaveLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")

	want := Settings{Width: 900, Height: 600, Maximized: true, SidebarWidth: 310, ResponseHeight: 240, ResponseFont: "Hack", WorkspacesDir: "/a/workspaces", Workspace: "Default", JSONColours: JSONColours{Key: "#A31515", Number: "#098658"}, Colours: Colours{Workspaces: "#112233", GRPC: "#445566"}}
	if err := want.Save(path); err != nil {
		t.Fatal(err)
	}
	if got := Load(path); !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestLoadDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	want := Settings{Width: defaultWidth, Height: defaultHeight, ResponseFont: defaultResponseFont}

	if got := Load(path); !reflect.DeepEqual(got, want) {
		t.Errorf("missing file: got %+v", got)
	}

	os.WriteFile(path, []byte("{oops"), 0o644)
	if got := Load(path); !reflect.DeepEqual(got, want) {
		t.Errorf("invalid file: got %+v", got)
	}

	os.WriteFile(path, []byte(`{"width": 5, "height": 9000, "maximized": true, "sidebarWidth": -4, "responseHeight": -1}`), 0o644)
	if got := Load(path); !reflect.DeepEqual(got, Settings{Width: defaultWidth, Height: defaultHeight, Maximized: true, ResponseFont: defaultResponseFont}) {
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

func TestLoadInvalidIconColours(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	os.WriteFile(path, []byte(`{"colours": {"workspaces": "orange", "requests": "#17807c", "remove": "#f00", "action": "#0B4F6C", "found": "#abcdef", "notFound": "nope"}}`), 0o644)

	if got, want := Load(path).Colours, (Colours{Requests: "#17807c", Action: "#0B4F6C", Found: "#abcdef"}); got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestDefaultColoursNotSaved(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := (Settings{Width: 900, Height: 600}).Save(path); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(path); strings.Contains(strings.ToLower(string(data)), "colours") {
		t.Errorf("default colours were saved: %s", data)
	}
}

func TestPath(t *testing.T) {
	config := t.TempDir()
	// These are where the configuration of programs is kept on Linux and
	// on Windows. On macOS it is in the home folder.
	t.Setenv("XDG_CONFIG_HOME", config)
	t.Setenv("AppData", config)
	t.Setenv("HOME", config)

	path, err := Path()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(path, config) || !strings.HasSuffix(path, filepath.Join("triggerapi", "settings.json")) {
		t.Errorf("path = %q", path)
	}
}

func TestPrepare(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config", "triggerapi", "settings.json")

	// The folder is made, and no settings file.
	if err := Prepare(path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err == nil {
		t.Error("a settings file was made from nothing")
	}

	// Settings can then be saved there, and making the folder again leaves
	// them alone.
	want := Settings{Width: 900, Height: 600, ResponseFont: "Hack", Workspace: "Mine"}
	if err := want.Save(path); err != nil {
		t.Fatal(err)
	}
	if err := Prepare(path); err != nil {
		t.Fatal(err)
	}
	if got := Load(path); !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestRequestSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")

	// Redirects are followed and requests are given as long as they need
	// unless the settings say otherwise.
	if got := Load(path); got.Timeout != 0 || got.NoRedirects {
		t.Errorf("defaults: %+v", got)
	}

	s := Load(path)
	s.Timeout, s.NoRedirects = 30, true
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	if got := Load(path); got.Timeout != 30 || !got.NoRedirects {
		t.Errorf("saved: %+v", got)
	}

	// A time that cannot be is put within what can.
	for timeout, want := range map[int]int{-5: 0, MaxTimeout + 1: MaxTimeout} {
		s.Timeout = timeout
		if err := s.Save(path); err != nil {
			t.Fatal(err)
		}
		if got := Load(path).Timeout; got != want {
			t.Errorf("timeout %d loaded as %d, want %d", timeout, got, want)
		}
	}
}

func TestHistorySettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")

	// Requests are kept, as many as the default, unless the settings say
	// otherwise.
	if got := Load(path); got.NoHistory || got.HistoryLimit != 0 || got.History() != DefaultHistoryLimit {
		t.Errorf("defaults: %+v", got)
	}

	s := Load(path)
	s.NoHistory, s.HistoryLimit = true, 50
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	if got := Load(path); !got.NoHistory || got.History() != 50 {
		t.Errorf("saved: %+v", got)
	}

	// A number that cannot be is put within what can.
	for limit, want := range map[int]int{-5: DefaultHistoryLimit, MaxHistoryLimit + 1: MaxHistoryLimit} {
		s.HistoryLimit = limit
		if err := s.Save(path); err != nil {
			t.Fatal(err)
		}
		if got := Load(path).History(); got != want {
			t.Errorf("limit %d loaded as %d, want %d", limit, got, want)
		}
	}
}

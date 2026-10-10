// Package settings stores program state that is kept between runs. It has no
// dependency on the UI.
package settings

import (
	"os"
	"path/filepath"
	"regexp"

	"github.com/nomad-software/triggerapi/internal/jsonfile"
)

const (
	defaultWidth  = 1024
	defaultHeight = 768
	minSize       = 200

	defaultResponseFont = "Iosevka Term Curly"
)

// Settings is the state that is saved to disk.
type Settings struct {
	// Width and Height are the size of the window when it is not maximised.
	Width     int  `json:"width"`
	Height    int  `json:"height"`
	Maximized bool `json:"maximized"`

	// SidebarWidth and ResponseHeight are the sizes the sidebar and the
	// response were dragged to. The default layout is used if one is zero.
	SidebarWidth   int `json:"sidebarWidth,omitempty"`
	ResponseHeight int `json:"responseHeight,omitempty"`

	// ResponseFont is the font family the response body is shown in. The
	// system's fixed width font is used if it is not installed.
	ResponseFont string `json:"responseFont"`

	// WorkspacesDir is the folder the workspaces are kept in, each in a
	// folder of its own. A relative path is relative to the directory the
	// program is run from. The default location is used if it is empty.
	WorkspacesDir string `json:"workspacesDir,omitempty"`
	// Workspace is the name of the workspace in use.
	Workspace string `json:"workspace,omitempty"`

	// Pinned are the requests that are kept open in tabs of their own.
	Pinned []Pin `json:"pinned,omitempty"`

	// JSONColours are the colours JSON is highlighted in.
	JSONColours JSONColours `json:"jsonColours,omitzero"`
}

// Pin names a request that is kept open in a tab.
type Pin struct {
	// Workspace is the name of the workspace the request is in.
	Workspace string `json:"workspace"`
	// Path is the names of the folders the request is in, outermost first,
	// followed by its own name.
	Path []string `json:"path"`
}

// JSONColours are the colours of the parts of JSON, as #rrggbb. The default
// colour for the desktop's theme is used for a part that is empty.
type JSONColours struct {
	Key     string `json:"key,omitempty"`
	String  string `json:"string,omitempty"`
	Number  string `json:"number,omitempty"`
	Literal string `json:"literal,omitempty"`
}

var hexColour = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// valid returns the colours with any that are not of the form #rrggbb
// emptied.
func (c JSONColours) valid() JSONColours {
	for _, colour := range []*string{&c.Key, &c.String, &c.Number, &c.Literal} {
		if !hexColour.MatchString(*colour) {
			*colour = ""
		}
	}
	return c
}

// appName is the name of the folder the settings are kept in.
const appName = "triggerapi"

// fileName is the name of the settings file.
const fileName = "settings.json"

// Path returns where the settings file is kept: in a folder for the program
// in the place the system has for the configuration of programs, which is
// ~/.config on Linux (or $XDG_CONFIG_HOME), ~/Library/Application Support on
// macOS and %AppData% on Windows.
func Path() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, appName, fileName), nil
}

// Prepare makes the folder the settings file at path is kept in.
func Prepare(path string) error {
	return os.MkdirAll(filepath.Dir(path), 0o755)
}

// Load reads the settings from the JSON file at path. Defaults are used if
// the file is missing or unreadable, and for any value that is unusable.
func Load(path string) Settings {
	var s Settings
	if err := jsonfile.Load(path, &s); err != nil {
		s = Settings{}
	}
	if s.Width < minSize || s.Height < minSize {
		s.Width, s.Height = defaultWidth, defaultHeight
	}
	s.SidebarWidth = max(s.SidebarWidth, 0)
	s.ResponseHeight = max(s.ResponseHeight, 0)
	s.JSONColours = s.JSONColours.valid()
	if s.ResponseFont == "" {
		s.ResponseFont = defaultResponseFont
	}
	return s
}

// Save writes the settings to the JSON file at path.
func (s Settings) Save(path string) error {
	return jsonfile.Save(path, s)
}

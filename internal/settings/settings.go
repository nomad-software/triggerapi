// Package settings stores program state that is kept between runs. It has no
// dependency on the UI.
package settings

import (
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

	// EnvironmentsFile and RequestsFile are where the environments and the
	// requests were kept before there were workspaces. They are only read,
	// to bring those into the first workspace, and are then emptied.
	EnvironmentsFile string `json:"environmentsFile,omitempty"`
	RequestsFile     string `json:"requestsFile,omitempty"`

	// JSONColours are the colours JSON is highlighted in.
	JSONColours JSONColours `json:"jsonColours,omitzero"`
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

// Package settings stores program state that is kept between runs. It has no
// dependency on the UI.
package settings

import "github.com/nomad-software/postie/internal/jsonfile"

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

	// EnvironmentsFile and RequestsFile are the paths of the files the
	// environments and the requests are kept in. A relative path is relative
	// to the directory the program is run from. The default location is used
	// if one is empty.
	EnvironmentsFile string `json:"environmentsFile,omitempty"`
	RequestsFile     string `json:"requestsFile,omitempty"`
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
	if s.ResponseFont == "" {
		s.ResponseFont = defaultResponseFont
	}
	return s
}

// Save writes the settings to the JSON file at path.
func (s Settings) Save(path string) error {
	return jsonfile.Save(path, s)
}

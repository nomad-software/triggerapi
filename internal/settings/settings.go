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

// MaxTimeout is the longest a request can be given, in seconds.
const MaxTimeout = 3600

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

	// Timeout is how long a request can take, in seconds. There is no
	// limit if it is zero.
	Timeout int `json:"timeout,omitempty"`
	// NoRedirects has a REST response that redirects to another address
	// shown as it is. Such an address is otherwise followed.
	NoRedirects bool `json:"noRedirects,omitempty"`

	// WorkspacesDir is the folder the workspaces are kept in, each in a
	// folder of its own. A relative path is relative to the directory the
	// program is run from. The default location is used if it is empty.
	WorkspacesDir string `json:"workspacesDir,omitempty"`
	// Workspace is the name of the workspace in use.
	Workspace string `json:"workspace,omitempty"`

	// PublicKey and PrivateKey are the paths of the files of a pair of SSH
	// keys, which the environments and requests of every workspace are
	// encrypted with. Nothing is encrypted if both are empty, and nothing
	// that is encrypted can be read.
	PublicKey  string `json:"publicKey,omitempty"`
	PrivateKey string `json:"privateKey,omitempty"`

	// JSONColours are the colours JSON is highlighted in.
	JSONColours JSONColours `json:"jsonColours,omitzero"`

	// Colours are the colours of the icons and of placeholders.
	Colours Colours `json:"colours,omitzero"`
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
	emptyInvalid(&c.Key, &c.String, &c.Number, &c.Literal)
	return c
}

// Colours are the colours of the icons and of placeholders, as #rrggbb. The default colour is
// used for one that is empty.
type Colours struct {
	// Workspaces, Environments and Requests are the colours of the tabs of
	// the sidebar, of the buttons of each and of what they list.
	Workspaces   string `json:"workspaces,omitempty"`
	Environments string `json:"environments,omitempty"`
	Requests     string `json:"requests,omitempty"`
	// REST and GRPC are the colours of the requests of each type.
	REST string `json:"rest,omitempty"`
	GRPC string `json:"grpc,omitempty"`
	// WebSocket is the colour of WebSocket requests.
	WebSocket string `json:"websocket,omitempty"`
	// GraphQL is the colour of GraphQL requests.
	GraphQL string `json:"graphql,omitempty"`
	// Remove is the colour of the buttons which remove something.
	Remove string `json:"remove,omitempty"`
	// Action is the colour of the buttons beside the tabs of a panel, which
	// show the curl command and save the response, and of the pin of a
	// request tab.
	Action string `json:"action,omitempty"`
	// Send is the colour of the buttons which send a request.
	Send string `json:"send,omitempty"`
	// Found and NotFound are the colours of placeholders: those the active
	// environment defines, and those it does not.
	Found    string `json:"found,omitempty"`
	NotFound string `json:"notFound,omitempty"`
	// Error is the colour of the text of errors and of failed statuses.
	Error string `json:"error,omitempty"`
	// Captured and NotCaptured are the colours of the marks which say that
	// a value was captured from a response, and that one could not be.
	Captured    string `json:"captured,omitempty"`
	NotCaptured string `json:"notCaptured,omitempty"`
	// Match is the colour that marks the places text looked for in a
	// response was found.
	Match string `json:"match,omitempty"`
	// Sets is the colour of the sets, of their tab in the sidebar and of
	// their buttons.
	Sets string `json:"sets,omitempty"`
}

// valid returns the colours with any that are not of the form #rrggbb
// emptied.
func (c Colours) valid() Colours {
	emptyInvalid(&c.Workspaces, &c.Environments, &c.Requests, &c.REST, &c.GRPC, &c.WebSocket, &c.GraphQL, &c.Remove, &c.Action, &c.Send, &c.Found, &c.NotFound, &c.Error, &c.Captured, &c.NotCaptured, &c.Match, &c.Sets)
	return c
}

// emptyInvalid empties the colours that are not of the form #rrggbb.
func emptyInvalid(colours ...*string) {
	for _, colour := range colours {
		if !hexColour.MatchString(*colour) {
			*colour = ""
		}
	}
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
	s.Timeout = min(max(s.Timeout, 0), MaxTimeout)
	s.SidebarWidth = max(s.SidebarWidth, 0)
	s.ResponseHeight = max(s.ResponseHeight, 0)
	s.JSONColours = s.JSONColours.valid()
	s.Colours = s.Colours.valid()
	if s.ResponseFont == "" {
		s.ResponseFont = defaultResponseFont
	}
	return s
}

// Save writes the settings to the JSON file at path.
func (s Settings) Save(path string) error {
	return jsonfile.Save(path, s)
}

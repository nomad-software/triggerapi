package ui

import (
	qt "github.com/mappu/miqt/qt6"

	"github.com/nomad-software/triggerapi/internal/collection"
	"github.com/nomad-software/triggerapi/internal/settings"
)

// colourKind is one of the colours of the icons, buttons and text that the
// user can choose.
type colourKind int

const (
	colourWorkspaces colourKind = iota
	colourEnvironments
	colourRequests
	colourREST
	colourGRPC
	colourRemove
	colourAction
	colourSend
	colourFound
	colourNotFound
	colourError
	colourCaptured
	colourNotCaptured
	colourMatch
	colourSets
)

// colourKinds are the colours, with what each is called in the settings and
// the colour it is until another is chosen. The workspaces have the colour
// of the Send button, which is that of Invoke and of Execute beside the curl
// command too. The action buttons are those beside the tabs of a panel, curl
// and save, and the pin of a request tab. A placeholder that the active
// environment defines is green, and one that it does not is the colour of an
// error. Captured and not captured are of the tick and the cross which say
// how capturing from a response went, and the last is what marks each place
// that text looked for in a response was found. Sets is the colour of the
// sets of the Requests tab, their tab and their buttons.
var colourKinds = []struct {
	kind     colourKind
	label    string
	fallback string
}{
	{colourWorkspaces, "Workspaces", primaryColour},
	{colourEnvironments, "Environments", "#8e6fd8"},
	{colourRequests, "Requests", "#17807c"},
	{colourREST, "REST", "#2e9e5b"},
	{colourGRPC, "gRPC", "#2f7fd1"},
	{colourRemove, "Delete", "#e03131"},
	{colourAction, "Action", "#0b4f6c"},
	{colourSend, "Send", primaryColour},
	{colourFound, "Placeholders found", "#2e9e5b"},
	{colourNotFound, "Placeholders not found", failureColour},
	{colourError, "Errors", failureColour},
	{colourCaptured, "Captured", "#2e9e5b"},
	{colourNotCaptured, "Not captured", failureColour},
	{colourMatch, "Search matches", "#ffd43b"},
	{colourSets, "Sets", "#b8508c"},
}

// customColours are the colours the user has chosen. See setColours.
var customColours settings.Colours

// customIconColour returns the place in the settings that holds the colour
// the user has chosen for a kind.
func customIconColour(custom *settings.Colours, kind colourKind) *string {
	switch kind {
	case colourWorkspaces:
		return &custom.Workspaces
	case colourEnvironments:
		return &custom.Environments
	case colourRequests:
		return &custom.Requests
	case colourREST:
		return &custom.REST
	case colourGRPC:
		return &custom.GRPC
	case colourRemove:
		return &custom.Remove
	case colourSend:
		return &custom.Send
	case colourFound:
		return &custom.Found
	case colourNotFound:
		return &custom.NotFound
	case colourError:
		return &custom.Error
	case colourCaptured:
		return &custom.Captured
	case colourNotCaptured:
		return &custom.NotCaptured
	case colourMatch:
		return &custom.Match
	case colourSets:
		return &custom.Sets
	default:
		return &custom.Action
	}
}

// colourLabel returns what a kind is called in the settings.
func colourLabel(kind colourKind) string {
	for _, k := range colourKinds {
		if k.kind == kind {
			return k.label
		}
	}
	return ""
}

// defaultColour returns the colour of a kind when none is chosen.
func defaultColour(kind colourKind) string {
	for _, k := range colourKinds {
		if k.kind == kind {
			return k.fallback
		}
	}
	return primaryColour
}

// themeColour returns the colour of a kind: the one the user has chosen, or
// the default.
func themeColour(kind colourKind) string {
	if chosen := *customIconColour(&customColours, kind); chosen != "" {
		return chosen
	}
	return defaultColour(kind)
}

// painted holds, for each button whose icon is in one of the colours, what
// draws that icon again.
var painted = map[*qt.QToolButton]func(){}

// followColours gives a button its icon with paint, which is called again
// whenever the colours change, for as long as there is the button.
func followColours(button *qt.QToolButton, paint func()) {
	paint()
	painted[button] = paint
	button.OnDestroyed(func() { delete(painted, button) })
}

// setColours changes the colours the user has chosen. The icons that are
// kept once drawn are forgotten, and the buttons are given theirs again;
// icons of items in lists and tabs have to be set again by their owners,
// which Window.applyColours sees to.
func setColours(custom settings.Colours) {
	if custom == customColours {
		return
	}
	customColours = custom

	requestTypeIcons = map[collection.Type]*qt.QIcon{}
	folderIcon, workspaceIconCache, environmentIconCache, removeIcon = nil, nil, nil, nil
	capturedIcon, notCapturedIcon, setIconCache = nil, nil, nil

	for _, paint := range painted {
		paint()
	}
}

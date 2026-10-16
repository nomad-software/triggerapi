package ui

import (
	qt "github.com/mappu/miqt/qt6"

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
	colourWebSocket
	colourGraphQL
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
	// setting returns the place in the settings that holds the colour the
	// user has chosen.
	setting func(*settings.Colours) *string
}{
	{colourWorkspaces, "Workspaces", primaryColour, func(c *settings.Colours) *string { return &c.Workspaces }},
	{colourEnvironments, "Environments", "#8e6fd8", func(c *settings.Colours) *string { return &c.Environments }},
	{colourRequests, "Requests", "#17807c", func(c *settings.Colours) *string { return &c.Requests }},
	{colourREST, "REST", "#2e9e5b", func(c *settings.Colours) *string { return &c.REST }},
	{colourGRPC, "gRPC", "#2f7fd1", func(c *settings.Colours) *string { return &c.GRPC }},
	{colourWebSocket, "WebSocket", "#c8871e", func(c *settings.Colours) *string { return &c.WebSocket }},
	{colourGraphQL, "GraphQL", "#e10098", func(c *settings.Colours) *string { return &c.GraphQL }},
	{colourRemove, "Delete", "#e03131", func(c *settings.Colours) *string { return &c.Remove }},
	{colourAction, "Action", "#0b4f6c", func(c *settings.Colours) *string { return &c.Action }},
	{colourSend, "Send", primaryColour, func(c *settings.Colours) *string { return &c.Send }},
	{colourFound, "Placeholders found", "#2e9e5b", func(c *settings.Colours) *string { return &c.Found }},
	{colourNotFound, "Placeholders not found", failureColour, func(c *settings.Colours) *string { return &c.NotFound }},
	{colourError, "Errors", failureColour, func(c *settings.Colours) *string { return &c.Error }},
	{colourCaptured, "Captured", "#2e9e5b", func(c *settings.Colours) *string { return &c.Captured }},
	{colourNotCaptured, "Not captured", failureColour, func(c *settings.Colours) *string { return &c.NotCaptured }},
	{colourMatch, "Search matches", "#ffd43b", func(c *settings.Colours) *string { return &c.Match }},
	{colourSets, "Sets", "#b8508c", func(c *settings.Colours) *string { return &c.Sets }},
}

// customColours are the colours the user has chosen. See setColours.
var customColours settings.Colours

// customIconColour returns the place in the settings that holds the colour
// the user has chosen for a kind.
func customIconColour(custom *settings.Colours, kind colourKind) *string {
	for _, k := range colourKinds {
		if k.kind == kind {
			return k.setting(custom)
		}
	}
	return &custom.Action
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

// icons holds the icons drawn in the colours of the settings, by what they
// are of, once they have been drawn. setColours forgets them.
var icons = map[string]*qt.QIcon{}

// cachedIcon returns the icon kept under a key, drawing it if there is none.
func cachedIcon(key string, draw func() *qt.QIcon) *qt.QIcon {
	icon, ok := icons[key]
	if !ok {
		icon = draw()
		icons[key] = icon
	}
	return icon
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

	icons = map[string]*qt.QIcon{}

	for _, paint := range painted {
		paint()
	}
}

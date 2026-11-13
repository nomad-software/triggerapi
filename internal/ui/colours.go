package ui

import (
	"github.com/nomad-software/qute/qtcore"
	"github.com/nomad-software/qute/qtgui"
	"github.com/nomad-software/qute/qtwidgets"

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
	colourMCP
	colourRemove
	colourAction
	colourSend
	colourFound
	colourNotFound
	colourError
	colourMatch
	colourSets
	colourHistory
	colourTiming
	colourPassed
	colourFailed
)

// colourKinds are the colours, with what each is called in the settings and
// the colour it is until another is chosen, which is one of the accents of
// the Solarized palette. Each has a default of its own,
// which no other follows, so that changing one changes nothing else. Send
// is the colour of every button which sends: Invoke, Call, Connect, and
// Execute beside the curl command. The action buttons are those beside the tabs of a panel,
// curl and save, and the pin of a request tab. A placeholder that the active
// environment defines is green, and one that it does not is the colour of an
// error. Search matches is what marks each place that text looked for in a
// response was found. Sets is the colour of the
// sets of the Requests tab, their tab and their buttons, and History that of
// the History tab and its buttons. Timing is that of the bars of the Timing
// tab of a response. Passed and failed are of the tick and the cross which
// say how capturing from a response and its assertions went.
var colourKinds = []struct {
	kind     colourKind
	label    string
	fallback string
	// setting returns the place in the settings that holds the colour the
	// user has chosen.
	setting func(*settings.Colours) *string
}{
	{colourWorkspaces, "Workspaces", "#859900", func(c *settings.Colours) *string { return &c.Workspaces }},
	{colourEnvironments, "Environments", "#268bd2", func(c *settings.Colours) *string { return &c.Environments }},
	{colourRequests, "Requests", "#2aa198", func(c *settings.Colours) *string { return &c.Requests }},
	{colourREST, "REST", "#859900", func(c *settings.Colours) *string { return &c.REST }},
	{colourGRPC, "gRPC", "#268bd2", func(c *settings.Colours) *string { return &c.GRPC }},
	{colourWebSocket, "WebSocket", "#b58900", func(c *settings.Colours) *string { return &c.WebSocket }},
	{colourGraphQL, "GraphQL", "#d33682", func(c *settings.Colours) *string { return &c.GraphQL }},
	{colourMCP, "MCP", "#6c71c4", func(c *settings.Colours) *string { return &c.MCP }},
	{colourRemove, "Delete", "#dc322f", func(c *settings.Colours) *string { return &c.Remove }},
	{colourAction, "Action", "#6c71c4", func(c *settings.Colours) *string { return &c.Action }},
	{colourSend, "Send", "#b58900", func(c *settings.Colours) *string { return &c.Send }},
	{colourFound, "Placeholders found", "#859900", func(c *settings.Colours) *string { return &c.Found }},
	{colourNotFound, "Placeholders not found", "#dc322f", func(c *settings.Colours) *string { return &c.NotFound }},
	{colourError, "Errors", "#dc322f", func(c *settings.Colours) *string { return &c.Error }},
	{colourMatch, "Search matches", "#b58900", func(c *settings.Colours) *string { return &c.Match }},
	{colourSets, "Sets", "#d33682", func(c *settings.Colours) *string { return &c.Sets }},
	{colourHistory, "History", "#cb4b16", func(c *settings.Colours) *string { return &c.History }},
	{colourTiming, "Timing bars", "#b58900", func(c *settings.Colours) *string { return &c.Timing }},
	{colourPassed, "Passed", "#859900", func(c *settings.Colours) *string { return &c.Passed }},
	{colourFailed, "Failed", "#dc322f", func(c *settings.Colours) *string { return &c.Failed }},
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
	// A kind that is not listed has a colour of its own, which no setting
	// holds.
	return new(string)
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
	return ""
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
var icons = map[string]*qtgui.QIcon{}

// cachedIcon returns the icon kept under a key, drawing it if there is none.
func cachedIcon(key string, draw func() *qtgui.QIcon) *qtgui.QIcon {
	icon, ok := icons[key]
	if !ok {
		icon = draw()
		icons[key] = icon
	}
	return icon
}

// painted holds, for each button whose icon is in one of the colours, what
// draws that icon again.
var painted = map[*qtwidgets.QToolButton]func(){}

// followColours gives a button its icon with paint, which is called again
// whenever the colours change, for as long as there is the button.
func followColours(button *qtwidgets.QToolButton, paint func()) {
	paint()
	painted[button] = paint
	button.OnDestroyed(func(_ *qtcore.QObject) { delete(painted, button) })
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

	icons = map[string]*qtgui.QIcon{}

	for _, paint := range painted {
		paint()
	}
}

// qcolour returns the colour a name such as #rrggbb stands for, or the
// invalid colour if it stands for none.
func qcolour(name string) qtgui.QColor {
	colour, err := qtgui.ParseColor(name)
	if err != nil {
		return qtgui.QColor{}
	}
	return colour
}

package ui

import (
	"errors"
	"fmt"
	"path/filepath"

	qt "github.com/mappu/miqt/qt6"

	"github.com/nomad-software/triggerapi/internal/settings"
)

// What the window does with the settings: keeping them, editing them and
// putting them into effect.

// settings returns the state to remember for the next run. The size of a
// maximised window is that of the window before it was maximised, falling
// back to the previous setting if the platform does not report one.
func (w *Window) settings() settings.Settings {
	s := w.config

	// The layout is only known once the window has been shown. The response
	// is measured on the page in view, as resizing the window changes its
	// height without it being dragged.
	if w.laidOut {
		if width := w.split.Sizes()[0]; width > 0 {
			s.SidebarWidth = width
		}
		s.ResponseHeight = w.responseHeight
		if split, ok := w.splits[w.stack.CurrentIndex()]; ok && split.Sizes()[1] > 0 {
			s.ResponseHeight = split.Sizes()[1]
		}
	}
	s.Width, s.Height, s.Maximized = w.Width(), w.Height(), w.IsMaximized()
	if s.Maximized {
		normal := w.NormalGeometry()
		s.Width, s.Height = normal.Width(), normal.Height()
		if s.Width <= 0 || s.Height <= 0 {
			s.Width, s.Height = w.config.Width, w.config.Height
		}
	}
	return s
}

// openSettings lets the user edit the settings in a dialog.
func (w *Window) openSettings() {
	dialog := newSettingsDialog(w.QWidget, w.config, w.files)
	// A dialog is not destroyed by being closed.
	defer dialog.DeleteLater()
	dialog.onExport = w.chooseExport
	dialog.onImport = w.chooseImport

	if dialog.Exec() != int(qt.QDialog__Accepted) {
		return
	}
	if err := w.applySettings(dialog.settings(w.config)); err != nil {
		qt.QMessageBox_Warning(w.QWidget, "Settings", err.Error())
	}
}

// applySettings puts the settings into effect and saves them. A setting that
// cannot be applied is left as it was, and reported in the error.
//
// Moving the workspaces to a folder that has some shows those. Moving them
// to a folder that has none copies the ones there are into it.
func (w *Window) applySettings(s settings.Settings) error {
	var failed []error

	w.config.ResponseFont = s.ResponseFont
	for _, ui := range w.uis {
		ui.editor.setCodeFont(s.ResponseFont)
		ui.response.setBodyFont(s.ResponseFont)
	}

	// The curl command says how long the request can take and whether it
	// follows redirects.
	w.config.Timeout, w.config.NoRedirects = s.Timeout, s.NoRedirects
	w.request.updateCurl()

	w.config.JSONColours = s.JSONColours
	w.applyJSONColours()

	if s.Colours != w.config.Colours {
		w.config.Colours = s.Colours
		w.applyColours()
	}

	// The files are given their new keys where they are, before they are
	// moved.
	// Keys that could not be opened are tried again, as the passphrase may
	// not have been given.
	if s.PublicKey != w.config.PublicKey || s.PrivateKey != w.config.PrivateKey || w.keys.Err() != nil {
		if keys, err := openKeys(w.QWidget, s); err != nil {
			failed = append(failed, err)
		} else {
			w.config.PublicKey, w.config.PrivateKey = s.PublicKey, s.PrivateKey
			if err := w.changeKeys(keys); err != nil {
				failed = append(failed, fmt.Errorf("not every file could be given the keys: %w", err))
			}
		}
	}

	setting, previous := w.config.WorkspacesDir, w.workspacesDir()
	w.config.WorkspacesDir = s.WorkspacesDir
	if dir := w.workspacesDir(); filepath.Clean(dir) != filepath.Clean(previous) {
		if err := w.moveWorkspaces(previous); err != nil {
			w.config.WorkspacesDir = setting
			failed = append(failed, fmt.Errorf("the workspaces were not moved to %s: %w", dir, err))
		}
	}

	if err := w.settings().Save(w.files.Settings); err != nil {
		failed = append(failed, fmt.Errorf("the settings were not saved: %w", err))
	}

	return errors.Join(failed...)
}

// applyJSONColours shows JSON in the colours of the settings, everywhere it
// is highlighted.
func (w *Window) applyJSONColours() {
	colours := jsonColours(w.config.JSONColours)
	for _, ui := range w.uis {
		ui.editor.highlighter().json.setColours(colours)
		ui.editor.refreshPlaceholders()
		ui.response.setJSONColours(colours)
	}
}

// drawSidebarIcons gives the tabs of the sidebar their icons.
func (w *Window) drawSidebarIcons() {
	w.sidebar.SetTabIcon(tabWorkspaces, drawTabIcon(themeColour(colourWorkspaces), drawMonitor))
	w.sidebar.SetTabIcon(tabEnvironments, drawTabIcon(themeColour(colourEnvironments), drawGlobe))
	w.sidebar.SetTabIcon(tabRequests, drawTabIcon(themeColour(colourRequests), drawFolder))
}

// applyColours shows the icons, the buttons which send, the placeholders and
// the errors in the colours of the settings, everywhere they are shown.
func (w *Window) applyColours() {
	setColours(w.config.Colours)

	w.drawSidebarIcons()
	w.workspaces.recolour()
	w.environments.recolour()
	w.requests.draw(w.requests.selection())
	w.requests.drawViewIcons()
	w.requests.sets.draw(w.requests.sets.selected())
	w.tabs.recolour()

	for _, ui := range w.uis {
		ui.editor.recolour()
		ui.response.recolour()
	}
}

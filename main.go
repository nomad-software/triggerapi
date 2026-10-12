package main

import (
	"fmt"
	"os"

	qt "github.com/mappu/miqt/qt6"

	"github.com/nomad-software/triggerapi/internal/settings"
	"github.com/nomad-software/triggerapi/internal/ui"
	"github.com/nomad-software/triggerapi/internal/workspace"
)

// The workspaces used to be kept in the directory the program is run from,
// as were the environments and requests files from before there were
// workspaces.
var files = ui.Files{
	OldWorkspaces: "workspaces",
	Environments:  "environments.json",
	Requests:      "requests.json",
}

func main() {
	// The settings are kept where the system keeps the configuration of
	// programs, or in the current directory if it has no such place.
	files.Settings = "settings.json"
	if path, err := settings.Path(); err != nil {
		fmt.Fprintln(os.Stderr, "triggerapi: the settings are kept in the current directory:", err)
	} else if err := settings.Prepare(path); err != nil {
		fmt.Fprintln(os.Stderr, "triggerapi: the settings are kept in the current directory:", err)
	} else {
		files.Settings = path
	}

	// The workspaces are kept where the system keeps the data of programs,
	// unless the settings name another folder.
	files.Workspaces = files.OldWorkspaces
	if dir, err := workspace.DefaultDir(); err != nil {
		fmt.Fprintln(os.Stderr, "triggerapi: the workspaces are kept in the current directory:", err)
	} else {
		files.Workspaces = dir
	}

	qt.NewQApplication(os.Args)
	// Under Wayland this is the name a window goes by, which a desktop
	// matches to the desktop file of that name for its icon and its place
	// in the task bar. It is the name of packaging/triggerapi.desktop.
	qt.QGuiApplication_SetDesktopFileName("triggerapi")

	window := ui.NewWindow(files)
	window.Show()

	qt.QApplication_Exec()
}

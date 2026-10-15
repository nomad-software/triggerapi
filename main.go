package main

import (
	"fmt"
	"os"
	"runtime"

	qt "github.com/mappu/miqt/qt6"

	"github.com/nomad-software/triggerapi/internal/settings"
	"github.com/nomad-software/triggerapi/internal/ui"
	"github.com/nomad-software/triggerapi/internal/workspace"
)

var files ui.Files

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
	files.Workspaces = "workspaces"
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

	// On Linux the desktop is given an entry for the program, which starts
	// the copy that is running now, wherever it is and whatever it is
	// called, so that it can be started from the desktop's own menus and
	// kept in a dock or sidebar.
	if runtime.GOOS == "linux" {
		if err := ui.InstallDesktopEntry(); err != nil {
			fmt.Fprintln(os.Stderr, "triggerapi: the desktop entry could not be written:", err)
		}
	}

	window := ui.NewWindow(files)
	window.Show()

	qt.QApplication_Exec()
}

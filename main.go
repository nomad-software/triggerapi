package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/nomad-software/qute"
	"github.com/nomad-software/qute/qtgui"

	"github.com/nomad-software/triggerapi/internal/instance"
	"github.com/nomad-software/triggerapi/internal/settings"
	"github.com/nomad-software/triggerapi/internal/ui"
	"github.com/nomad-software/triggerapi/internal/workspace"
)

func main() {
	os.Exit(run())
}

// run runs the program, and returns what it exits with.
func run() int {
	// One copy runs at a time: one started while another runs, as from the
	// desktop, has that one's window shown and leaves, passing on the
	// desktop's leave to take the focus. Nothing else is done before, so
	// that it leaves at once.
	var claim *instance.Instance
	if dir, err := instance.Dir(); err == nil {
		claim, err = instance.Claim(dir, os.Getenv("XDG_ACTIVATION_TOKEN"))
		switch {
		case errors.Is(err, instance.ErrRunning):
			return 0
		case errors.Is(err, instance.ErrNoAnswer):
			fmt.Fprintln(os.Stderr, "triggerapi:", err)
			return 1
		case err != nil:
			// The program runs, as it would without the check.
			fmt.Fprintln(os.Stderr, "triggerapi: another copy could be started beside this one:", err)
		default:
			defer claim.Close()
		}
	} else {
		fmt.Fprintln(os.Stderr, "triggerapi: another copy could be started beside this one:", err)
	}

	var files ui.Files

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

	return qute.Main(func() {
		// Under Wayland this is the name a window goes by, which a desktop
		// matches to the desktop file of that name for its icon and its
		// place in the task bar. It is the name of
		// packaging/triggerapi.desktop.
		qtgui.QGuiApplicationSetDesktopFileName("triggerapi")

		// The desktop is given an entry for the program, which starts the
		// copy that is running now, wherever it is and whatever it is
		// called, so that it can be started from the desktop's own menus
		// and kept in a dock or sidebar.
		if err := ui.InstallDesktopEntry(); err != nil {
			fmt.Fprintln(os.Stderr, "triggerapi: the desktop entry could not be written:", err)
		}

		window := ui.NewWindow(files)
		window.Show()
		if claim != nil {
			claim.Serve(func(token string) {
				qute.Post(func() { window.Activate(token) })
			})
		}
	})
}

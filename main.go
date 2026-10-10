package main

import (
	"os"

	qt "github.com/mappu/miqt/qt6"

	"github.com/nomad-software/triggerapi/internal/ui"
)

// The saved data lives in the directory the program is run from. The
// environments and requests files are from before there were workspaces.
var files = ui.Files{
	Settings:     "settings.json",
	Workspaces:   "workspaces",
	Environments: "environments.json",
	Requests:     "requests.json",
}

func main() {
	qt.NewQApplication(os.Args)

	window := ui.NewWindow(files)
	window.Show()

	qt.QApplication_Exec()
}

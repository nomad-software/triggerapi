package main

import (
	"os"

	qt "github.com/mappu/miqt/qt6"

	"github.com/nomad-software/postie/internal/ui"
)

// The saved data lives in the directory the program is run from.
var files = ui.Files{
	Environments: "environments.json",
	Requests:     "requests.json",
	Settings:     "settings.json",
}

func main() {
	qt.NewQApplication(os.Args)

	window := ui.NewWindow(files)
	window.Show()

	qt.QApplication_Exec()
}

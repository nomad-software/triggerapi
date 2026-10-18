package ui

import (
	"os"
	"path/filepath"

	"github.com/nomad-software/triggerapi/internal/desktop"
)

// desktopIconSize is the size of the icon of the desktop entry, as
// packaging/triggerapi.png is.
const desktopIconSize = 256

// InstallDesktopEntry writes the desktop entry and icon that let a Linux
// desktop list TriggerAPI with its applications, so that it can be started
// from there and kept in a dock or sidebar. It is called at every start, and
// rewrites the entry whenever the program is run from somewhere else or under
// another name, so that the entry always starts the copy last run: an
// AppImage, or else the program itself. It must be called on the main thread
// after the QApplication has been created, as the icon is drawn with Qt.
func InstallDesktopEntry() error {
	exec, err := desktop.Executable()
	if err != nil {
		return err
	}
	data, err := desktop.Dir()
	if err != nil {
		return err
	}
	// Windows are known under X11 by the name of the program file that is
	// running, which inside an AppImage is the program and not the image.
	class := desktop.Name
	if self, err := os.Executable(); err == nil {
		class = filepath.Base(self)
	}
	return desktop.Write(data, exec, class, appIconPNG(desktopIconSize, 0))
}

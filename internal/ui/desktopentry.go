package ui

import (
	"os"
	"path/filepath"

	qt "github.com/mappu/miqt/qt6"

	"github.com/nomad-software/triggerapi/internal/desktop"
)

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
	return desktop.Write(data, exec, class, appIconPNG())
}

// appIconPNG returns the program's icon as a PNG of 256 pixels, as
// packaging/triggerapi.png is.
func appIconPNG() []byte {
	pixmap := drawPixmap(256, appIconColour, drawRevolver)
	buffer := qt.NewQBuffer()
	defer buffer.Delete()
	if !buffer.Open(qt.QIODeviceBase__WriteOnly) {
		return nil
	}
	if !pixmap.Save4(buffer.QIODevice, "PNG") {
		return nil
	}
	return buffer.Data()
}

// Package desktop writes the desktop entry which lets a Linux desktop list
// TriggerAPI with its other applications, to be started from there and kept
// in a dock or sidebar. It has no dependency on the UI.
package desktop

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/nomad-software/triggerapi/internal/atomicfile"
)

// Name is the name of the desktop entry, without its extension, and of the
// icon. It is the name the program gives Qt (QGuiApplication_SetDesktopFileName
// in main.go), by which a Wayland desktop matches a window to its entry.
const Name = "triggerapi"

// Executable returns the file that starts the program as it is being run: the
// AppImage it is in, if it is in one, and otherwise the program itself. Inside
// an AppImage the program is in a folder that is only there while it runs, so
// the AppImage, which its runtime names in $APPIMAGE, is what starts it again.
func Executable() (string, error) {
	if image := os.Getenv("APPIMAGE"); image != "" {
		return image, nil
	}
	path, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	return path, nil
}

// Dir returns the folder desktop entries of the user's own are kept in:
// applications in $XDG_DATA_HOME, or in ~/.local/share without it.
func Dir() (string, error) {
	data := os.Getenv("XDG_DATA_HOME")
	if data == "" || !filepath.IsAbs(data) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		data = filepath.Join(home, ".local", "share")
	}
	return data, nil
}

// Entry returns the desktop entry for a program started by the file at exec.
// The class is what the program's windows are known by under X11, the name
// of the program file, so that its windows are matched to the entry. The
// icon is the path of its image file: a path and not the name of an icon of
// the theme, which a desktop may not find in a folder it has a stale cache
// of (as GNOME on Ubuntu 22.04 does), leaving the windows matched to the
// entry with no icon at all.
func Entry(exec, class, icon string) string {
	var b strings.Builder
	b.WriteString("[Desktop Entry]\n")
	b.WriteString("Type=Application\n")
	b.WriteString("Name=TriggerAPI\n")
	b.WriteString("Comment=Make REST API, gRPC and WebSocket requests\n")
	b.WriteString("Exec=" + escape(quote(exec)) + "\n")
	b.WriteString("TryExec=" + escape(exec) + "\n")
	b.WriteString("Icon=" + escape(icon) + "\n")
	b.WriteString("Categories=Development;Network;\n")
	b.WriteString("Terminal=false\n")
	b.WriteString("StartupWMClass=" + escape(class) + "\n")
	return b.String()
}

// quote makes a path one argument of an Exec key: in double quotes if it has
// anything the desktop entry specification reserves, with ", `, $ and \
// escaped inside them, and with % doubled, which would otherwise begin a
// field code.
func quote(path string) string {
	path = strings.ReplaceAll(path, "%", "%%")
	if !strings.ContainsAny(path, " \t\n\"'\\><~|&;$*?#()`") {
		return path
	}
	escaped := strings.NewReplacer(`"`, `\"`, "`", "\\`", `$`, `\$`, `\`, `\\`).Replace(path)
	return `"` + escaped + `"`
}

// escape escapes a value of a desktop entry key, in which a backslash and
// the characters it could stand for are written with one.
func escape(value string) string {
	return strings.NewReplacer(`\`, `\\`, "\n", `\n`, "\t", `\t`, "\r", `\r`).Replace(value)
}

// Write writes the desktop entry and the icon, as PNG data, into the data
// folder given: the entry to applications/triggerapi.desktop and the icon to
// icons/hicolor/256x256/apps/triggerapi.png. Each is written only if it is
// not already so, and in a way that leaves the old file whole if it fails.
func Write(data, exec, class string, icon []byte) error {
	entry := filepath.Join(data, "applications", Name+".desktop")
	image := filepath.Join(data, "icons", "hicolor", "256x256", "apps", Name+".png")
	return errors.Join(
		writeFile(image, icon),
		writeFile(entry, []byte(Entry(exec, class, image))),
	)
}

// writeFile writes data to a file atomically, unless it already holds that
// data.
func writeFile(path string, data []byte) error {
	if len(data) == 0 {
		return nil
	}
	if old, err := os.ReadFile(path); err == nil && bytes.Equal(old, data) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return atomicfile.Write(path, data, 0o644)
}

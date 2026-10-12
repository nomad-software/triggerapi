#!/bin/sh
# Builds TriggerAPI and packs it, with the Qt libraries it needs, into an
# AppImage: one file that runs on most Linux systems without installing
# anything. The result is build/TriggerAPI-<version>-x86_64.AppImage.
#
# It is meant to be run on an old distribution (the GitHub workflow uses
# Ubuntu 22.04, with Qt's own build of Qt), as an AppImage runs on systems as
# new as the one it was built on or newer, not on older ones. It needs Go, a
# C++ compiler, pkg-config, the Qt 6 development files with qmake, curl and
# file. The Qt that pkg-config finds is the one built with, and QMAKE, if
# set, says which qmake belongs to it.
set -eu

cd "$(dirname "$0")/.."

VERSION="${VERSION:-$(git describe --tags --always 2>/dev/null || echo dev)}"
ARCH=x86_64
BUILD=build
APPDIR="$BUILD/AppDir"
TOOLS="$BUILD/tools"
OUTPUT="TriggerAPI-$VERSION-$ARCH.AppImage"

rm -rf "$APPDIR"
mkdir -p "$TOOLS"

echo "Building TriggerAPI $VERSION"
go build -ldflags "-s -w" -o "$BUILD/triggerapi" .

# linuxdeploy copies the program and every library it uses into a folder
# laid out as an AppImage needs, and its Qt plugin adds the parts of Qt that
# are loaded while running, which cannot be found by looking at the program.
fetch() {
	if [ ! -x "$TOOLS/$1" ]; then
		curl -fsSL -o "$TOOLS/$1" "$2"
		chmod +x "$TOOLS/$1"
	fi
}
fetch "linuxdeploy-$ARCH.AppImage" \
	"https://github.com/linuxdeploy/linuxdeploy/releases/download/continuous/linuxdeploy-$ARCH.AppImage"
fetch "linuxdeploy-plugin-qt-$ARCH.AppImage" \
	"https://github.com/linuxdeploy/linuxdeploy-plugin-qt/releases/download/continuous/linuxdeploy-plugin-qt-$ARCH.AppImage"

# These tools are AppImages themselves, which unpack rather than mount where
# there is no FUSE, as in a container.
export APPIMAGE_EXTRACT_AND_RUN=1
export QMAKE="${QMAKE:-$(command -v qmake6 || command -v qmake)}"
# The AppImage has to run under both X11 and Wayland. The Qt plugin takes
# what X11 needs (xcb) by default, and Wayland has to be asked for: its
# platform plugins here, and the module which has the plugin bring the rest
# of what Wayland loads (shell integration, decorations, graphics). The
# offscreen platform is for the check below, which has no display.
export EXTRA_PLATFORM_PLUGINS="libqwayland-egl.so;libqwayland-generic.so;libqoffscreen.so"
export EXTRA_QT_MODULES="waylandcompositor"
# The name of the file made, which the newer and the older versions of the
# tool read from different variables.
export LDAI_OUTPUT="$OUTPUT" OUTPUT
export LINUXDEPLOY_OUTPUT_VERSION="$VERSION"

"$TOOLS/linuxdeploy-$ARCH.AppImage" \
	--appdir "$APPDIR" \
	--executable "$BUILD/triggerapi" \
	--desktop-file packaging/triggerapi.desktop \
	--icon-file packaging/triggerapi.png \
	--plugin qt

# Without one of these the AppImage would be made, and then not start under
# X11 or under Wayland, so it is not made at all.
missing=0
for plugin in \
	platforms/libqxcb.so \
	platforms/libqwayland-generic.so \
	platforms/libqwayland-egl.so \
	wayland-shell-integration/libxdg-shell.so; do
	if [ ! -e "$APPDIR/usr/plugins/$plugin" ]; then
		echo "Missing from the AppImage: $plugin" >&2
		missing=1
	fi
done
if [ "$missing" -ne 0 ]; then
	echo "The Qt plugins that were put in it:" >&2
	find "$APPDIR/usr/plugins" -name '*.so' | sort >&2
	exit 1
fi

"$TOOLS/linuxdeploy-$ARCH.AppImage" --appdir "$APPDIR" --output appimage
mv "$OUTPUT" "$BUILD/$OUTPUT"

# It is started without a display, with its settings and data in a scratch
# folder, and has to still be running when it is stopped a few seconds
# later, which timeout reports as 124. Anything else is a library that is
# missing or a crash.
scratch="$(mktemp -d)"
status=0
XDG_CONFIG_HOME="$scratch/config" XDG_DATA_HOME="$scratch/data" QT_QPA_PLATFORM=offscreen \
	timeout 5 "$BUILD/$OUTPUT" || status=$?
rm -rf "$scratch"
if [ "$status" -ne 124 ]; then
	echo "The AppImage did not start: exit code $status" >&2
	exit 1
fi

echo "Made $BUILD/$OUTPUT"

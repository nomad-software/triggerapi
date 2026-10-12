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
# The strip the tool carries is too old for the libraries of a recent
# system, and fails on them. The program is built without symbols already,
# and libraries come without them, so nothing is lost by leaving it out.
export NO_STRIP=1
export QMAKE="${QMAKE:-$(command -v qmake6 || command -v qmake)}"
# On a desktop system the folder of Qt's plugins also has those of the
# desktop (KDE's image formats, theme and style, for one), which would all go
# into the AppImage with everything they use, and stop it being made if one
# of them is broken. Qt's own are all named lib*.so, so where there are
# others a copy of the folder with only Qt's is made, and the tool is given
# a qmake which says that is where the plugins are. Qt's own build, as the
# GitHub workflow uses, has no others.
PLUGINS="$("$QMAKE" -query QT_INSTALL_PLUGINS)"
if [ -n "$(find "$PLUGINS" -mindepth 2 -maxdepth 2 -name '*.so' ! -name 'lib*' | head -n 1)" ]; then
	STAGE="$PWD/$BUILD/qt-plugins"
	rm -rf "$STAGE"
	for dir in "$PLUGINS"/*/; do
		name="$(basename "$dir")"
		if ls "$dir"lib*.so >/dev/null 2>&1; then
			mkdir -p "$STAGE/$name"
			cp "$dir"lib*.so "$STAGE/$name/"
		fi
	done

	REAL_QMAKE="$QMAKE"
	QMAKE="$PWD/$TOOLS/qmake"
	cat > "$QMAKE" <<WRAPPER
#!/bin/sh
"$REAL_QMAKE" "\$@" | sed "s|$PLUGINS|$STAGE|"
WRAPPER
	chmod +x "$QMAKE"
	export QMAKE
	echo "Using only Qt's own plugins, from $STAGE"
fi

# The AppImage has to run under both X11 and Wayland. The Qt plugin takes
# what X11 needs (xcb) by default, and Wayland has to be asked for: its
# platform plugins here, and the module which has the plugin bring the rest
# of what Wayland loads (shell integration, decorations, graphics). The
# offscreen platform is for the check below, which has no display. Older
# versions of Qt have two platform plugins for Wayland (libqwayland-egl.so
# and libqwayland-generic.so) and newer ones a single libqwayland.so, so
# those the Qt in use has are the ones asked for.
PLATFORMS="$("$QMAKE" -query QT_INSTALL_PLUGINS)/platforms"
EXTRA_PLATFORM_PLUGINS="libqoffscreen.so"
for plugin in "$PLATFORMS"/libqwayland*.so; do
	if [ -e "$plugin" ]; then
		EXTRA_PLATFORM_PLUGINS="$EXTRA_PLATFORM_PLUGINS;$(basename "$plugin")"
	fi
done
export EXTRA_PLATFORM_PLUGINS
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
	--custom-apprun packaging/AppRun \
	--plugin qt

# Without one of these the AppImage would be made, and then not start under
# X11 or under Wayland, so it is not made at all.
missing=0
for plugin in \
	platforms/libqxcb.so \
	'platforms/libqwayland*.so' \
	wayland-shell-integration/libxdg-shell.so; do
	# The pattern is left to match here, where it is not in quotes.
	if ! ls "$APPDIR/usr/plugins/"$plugin >/dev/null 2>&1; then
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

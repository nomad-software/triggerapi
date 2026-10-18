#!/bin/sh
# Builds TriggerAPI as a macOS app for Apple Silicon, with the Qt it needs
# inside it, and puts it in a disk image to drag into Applications. The
# result is build/TriggerAPI-<version>-arm64.dmg.
#
# It runs on a Mac with Apple Silicon, with Go, the Xcode command line tools
# and the base of Homebrew's Qt (brew install go pkgconf qtbase), as the
# GitHub workflow has. Only the base: macdeployqt puts in the app every
# plugin of the kinds it uses that the Qt it is given has, and with all of
# Homebrew's qt that is those of other modules too (the virtual keyboard,
# PDF, SVG, WebP), with what they use. The Qt that pkg-config finds is the
# one built with, and MACDEPLOYQT, if set, names its macdeployqt (the one on
# the path otherwise, or in the folder of Homebrew's qtbase).
#
# The app is signed ad hoc, which Apple Silicon needs to run it at all, and
# not with a Developer ID, so macOS warns that it cannot check it the first
# time it is opened: see the README.
set -eu

cd "$(dirname "$0")/.."

VERSION="${VERSION:-$(git describe --tags --always 2>/dev/null || echo dev)}"
ARCH=arm64
BUILD=build
APP="$BUILD/TriggerAPI.app"
OUTPUT="TriggerAPI-$VERSION-$ARCH.dmg"
MACDEPLOYQT="${MACDEPLOYQT:-$(command -v macdeployqt || echo "$(brew --prefix qtbase)/bin/macdeployqt")}"
BREW_LIB="$(brew --prefix)/lib"

if [ ! -x "$MACDEPLOYQT" ]; then
	echo "There is no macdeployqt at $MACDEPLOYQT: install Homebrew's qtbase, or qttools if it is not in that." >&2
	exit 1
fi
if [ "$(uname -m)" != "$ARCH" ]; then
	echo "This builds for Apple Silicon, and has to run on it." >&2
	exit 1
fi

rm -rf "$APP" "$BUILD/$OUTPUT" "$BUILD/dmg"
mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Resources"

echo "Building TriggerAPI $VERSION"
# The program is linked with room to name the libraries it uses by longer
# paths, as macdeployqt names them by where they are in the app, which is
# longer than where Homebrew has them; without it the last few are left as
# they were. It is only given to the last link, so that the bindings are
# not compiled again.
go build -ldflags "-s -w -extldflags=-Wl,-headerpad_max_install_names" -o "$APP/Contents/MacOS/triggerapi" .

# The icon is made from packaging/macos/triggerapi.png (make icons) at each
# of the sizes macOS looks for, and those made into an .icns.
ICONSET="$BUILD/triggerapi.iconset"
rm -rf "$ICONSET"
mkdir -p "$ICONSET"
for size in 16 32 128 256 512; do
	sips -z "$size" "$size" packaging/macos/triggerapi.png --out "$ICONSET/icon_${size}x${size}.png" >/dev/null
	double=$((size * 2))
	sips -z "$double" "$double" packaging/macos/triggerapi.png --out "$ICONSET/icon_${size}x${size}@2x.png" >/dev/null
done
iconutil -c icns "$ICONSET" -o "$APP/Contents/Resources/triggerapi.icns"

# The version is that of the tag, without its v, which macOS wants as
# numbers; a build of no tag is 0.0.0. The app is given its Info.plist
# before macdeployqt signs it, and the oldest macOS it runs on once what is
# in it is known, below.
SHORT="$(echo "$VERSION" | sed -n 's/^v\{0,1\}\([0-9][0-9.]*\)$/\1/p')"
SHORT="${SHORT:-0.0.0}"
sed -e "s/VERSION/$SHORT/g" packaging/macos/Info.plist > "$APP/Contents/Info.plist"

# macdeployqt copies into the app the Qt frameworks and plugins the program
# uses, and the libraries of Homebrew those use in turn, and has the program
# find them there instead of where they were built with. Some of Homebrew's
# libraries name others through @rpath, which it finds in Homebrew's folder
# of libraries, where all of them are linked.
"$MACDEPLOYQT" "$APP" -verbose=1 -libpath="$BREW_LIB"

# machO lists the programs and libraries in the app, one to a line.
machO() {
	find "$APP/Contents" -type f -exec sh -c 'file -b "$1" | grep -q Mach-O' _ {} \; -print
}

# Nothing in the app may still look outside it for what macOS does not
# have itself, or it would only start on a Mac with Homebrew's Qt.
outside="$(machO | while read -r file; do
	otool -L "$file" | grep -E '^[[:space:]]+/(opt/homebrew|usr/local)/' | sed "s|^[[:space:]]*|${file#"$APP"/}: |"
done)"
if [ -n "$outside" ]; then
	echo "The app still uses libraries from outside it:" >&2
	echo "$outside" >&2
	exit 1
fi

# The oldest macOS the app runs on is the oldest that what is in it was built
# for, which for Homebrew's Qt is the macOS of the machine it was built on.
MINIMUM="$(machO | while read -r file; do otool -l "$file"; done |
	awk '$1 == "minos" { print $2 }' | sort -t . -k1,1n -k2,2n | tail -n 1)"
MINIMUM="${MINIMUM:-14.0}"
plutil -replace LSMinimumSystemVersion -string "$MINIMUM" "$APP/Contents/Info.plist"
plutil -lint "$APP/Contents/Info.plist"
echo "The app needs macOS $MINIMUM or later"

# Signed ad hoc, everything in it and then the app as a whole, after the
# last change to it, in place of the signature macdeployqt gave it before
# the minimum was put in.
codesign --force --deep --sign - "$APP"
codesign --verify --deep --strict "$APP"

# It is started, with its settings and data in a scratch home, and has to
# still be running a few seconds later. Anything else is a library that is
# missing or a crash. macOS has no timeout, so it is waited for by hand.
scratch="$(mktemp -d)"
HOME="$scratch" "$APP/Contents/MacOS/triggerapi" &
pid=$!
sleep 5
if ! kill -0 "$pid" 2>/dev/null; then
	wait "$pid" || true
	rm -rf "$scratch"
	echo "The app did not start" >&2
	exit 1
fi
kill "$pid"
wait "$pid" 2>/dev/null || true
rm -rf "$scratch"

# The disk image holds the app beside a link to Applications, to drag it
# onto.
mkdir -p "$BUILD/dmg"
cp -R "$APP" "$BUILD/dmg/"
ln -s /Applications "$BUILD/dmg/Applications"
hdiutil create -volname TriggerAPI -srcfolder "$BUILD/dmg" -fs HFS+ -format UDZO -ov "$BUILD/$OUTPUT"
rm -rf "$BUILD/dmg"

echo "Made $BUILD/$OUTPUT"

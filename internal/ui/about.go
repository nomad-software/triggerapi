package ui

import (
	"runtime/debug"
	"strconv"
	"strings"

	qt "github.com/mappu/miqt/qt6"
)

// aboutWidth is the narrowest the about box is, so that the notices of the
// licences are set out in lines of a comfortable length.
const aboutWidth = 640

// aboutIconSize is the size of the program's icon in the about box.
const aboutIconSize = 96

// appVersion returns the version of the program, which Go records from git
// when it is built, as plain major.minor.patch.
func appVersion() string {
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return plainVersion(info.Main.Version)
	}
	return "dev"
}

// plainVersion returns the release a version recorded by Go is, or follows.
// Between tags Go records a version made from the commit, whose patch is one
// more than the tag before it, as v1.2.4-0.20261027184954-00c19a9187bb is
// after v1.2.3.
func plainVersion(version string) string {
	version, _, _ = strings.Cut(version, "+")
	version, pre, _ := strings.Cut(version, "-")
	if !strings.HasPrefix(pre, "0.") {
		return version
	}
	i := strings.LastIndex(version, ".") + 1
	patch, err := strconv.Atoi(version[i:])
	if err != nil || patch == 0 {
		return version
	}
	return version[:i] + strconv.Itoa(patch-1)
}

// aboutText returns what the about box says of the program.
func aboutText() string {
	return "<p>A desktop client for making REST API, GraphQL and gRPC calls, calling the tools of " +
		"MCP servers and talking to WebSocket servers.</p>" +
		"<p>Copyright © 2026 Gary Willoughby. All rights reserved.</p>"
}

// newAboutTitle returns the name of the program with its version to the
// right of it, smaller and half transparent, on one baseline, and the label
// of the version.
func newAboutTitle() (*qt.QHBoxLayout, *qt.QLabel) {
	name := qt.NewQLabel3("TriggerAPI")
	font := name.Font()
	font.SetBold(true)
	font.SetPointSizeF(font.PointSizeF() * 1.5)
	name.SetFont(font)

	version := qt.NewQLabel3(appVersion())
	version.SetTextFormat(qt.PlainText)
	small := version.Font()
	small.SetPointSizeF(small.PointSizeF() * 0.8)
	version.SetFont(small)
	version.SetContentsMargins(0, 0, 0, name.FontMetrics().Descent()-version.FontMetrics().Descent())
	faded := qt.NewQGraphicsOpacityEffect()
	faded.SetOpacity(0.5)
	version.SetGraphicsEffect(faded.QGraphicsEffect)

	title := qt.NewQHBoxLayout2()
	title.AddWidget3(name.QWidget, 0, qt.AlignBottom)
	title.AddWidget3(version.QWidget, 0, qt.AlignBottom)
	title.AddStretch()
	return title, version
}

// licenceText returns the notices of the licences of what the program is
// built with, which carry the notice the LGPL asks of programs that use Qt.
func licenceText() string {
	return "<p><b>Third party licences</b></p>" +
		"<p>This program uses Qt version " + qt.QLibraryInfo_Version().ToString() + ".</p>" +
		"<p>Qt is a C++ toolkit for cross-platform application development, used here under the " +
		"terms of the <a href=\"https://www.gnu.org/licenses/lgpl-3.0.html\">GNU Lesser General " +
		"Public License version 3</a> (LGPLv3). Qt is dynamically linked, so it can be replaced " +
		"with a different or modified build of the library.</p>" +
		"<p>Qt and its source code are available from <a href=\"https://www.qt.io\">www.qt.io</a>. " +
		"See <a href=\"https://www.qt.io/licensing\">www.qt.io/licensing</a> for an overview of Qt " +
		"licensing.</p>" +
		"<p>Copyright (C) The Qt Company Ltd. and other contributors. Qt and the Qt logo are " +
		"trademarks of The Qt Company Ltd.</p>" +
		"<p>The Go bindings for Qt are provided by <a href=\"https://github.com/mappu/miqt\">miqt</a>, " +
		"used under the MIT licence.</p>" +
		"<p>It is also built with the Go libraries " +
		"<a href=\"https://connectrpc.com\">Connect</a> (Apache 2.0) for gRPC, " +
		"<a href=\"https://github.com/modelcontextprotocol/go-sdk\">the MCP Go SDK</a> (Apache 2.0 and MIT) " +
		"and <a href=\"https://github.com/google/jsonschema-go\">jsonschema-go</a> (MIT) for MCP, " +
		"<a href=\"https://github.com/coder/websocket\">websocket</a> (ISC), " +
		"<a href=\"https://age-encryption.org\">age</a> (BSD), " +
		"<a href=\"https://github.com/protocolbuffers/protobuf-go\">Go protobuf</a> (BSD), " +
		"<a href=\"https://pkg.go.dev/golang.org/x/crypto\">x/crypto</a> (BSD), " +
		"<a href=\"https://pkg.go.dev/golang.org/x/oauth2\">x/oauth2</a> (BSD), " +
		"<a href=\"https://github.com/SSLMate/go-pkcs12\">go-pkcs12</a> (BSD) and " +
		"<a href=\"https://github.com/go-yaml/yaml\">yaml</a> (MIT and Apache 2.0).</p>"
}

// aboutDialog is the about box: the program's icon beside what it is, and
// the notices of the licences beneath in a frame of the style's own, in the
// colours of the theme.
type aboutDialog struct {
	*qt.QDialog
	// notices is the frame around the notices of the licences.
	notices *qt.QFrame
	// version is the label of the version of the program.
	version *qt.QLabel
}

func newAboutDialog(parent *qt.QWidget) *aboutDialog {
	d := &aboutDialog{QDialog: qt.NewQDialog(parent)}
	d.SetWindowTitle("About TriggerAPI")
	d.SetMinimumWidth(aboutWidth)

	icon := qt.NewQLabel2()
	icon.SetPixmap(appIcon().PixmapWithExtent(aboutIconSize))
	icon.SetAlignment(qt.AlignTop)

	title, version := newAboutTitle()
	d.version = version

	about := qt.NewQVBoxLayout2()
	about.SetSpacing(12)
	about.AddLayout(title.QLayout)
	about.AddWidget(newRichLabel(aboutText()).QWidget)
	about.AddStretch()

	top := qt.NewQHBoxLayout2()
	top.SetSpacing(20)
	top.AddWidget(icon.QWidget)
	top.AddLayout2(about.QLayout, 1)

	// The notices are smaller than the rest, as on a label of the style.
	licences := newRichLabel(licenceText())
	font := licences.Font()
	font.SetPointSizeF(font.PointSizeF() * 0.9)
	licences.SetFont(font)

	d.notices = qt.NewQFrame2()
	d.notices.SetFrameShape(qt.QFrame__StyledPanel)
	d.notices.SetFrameShadow(qt.QFrame__Sunken)
	frameLayout := qt.NewQVBoxLayout(d.notices.QWidget)
	frameLayout.SetContentsMargins(12, 8, 12, 8)
	frameLayout.AddWidget(licences.QWidget)

	buttons := qt.NewQDialogButtonBox4(qt.QDialogButtonBox__Ok)
	buttons.OnAccepted(d.Accept)

	layout := qt.NewQVBoxLayout(d.QWidget)
	layout.SetContentsMargins(20, 20, 20, 16)
	layout.SetSpacing(16)
	layout.AddLayout(top.QLayout)
	layout.AddWidget(d.notices.QWidget)
	layout.AddWidget(buttons.QWidget)

	return d
}

// newRichLabel returns a label of rich text which wraps, and whose links
// open in the browser.
func newRichLabel(text string) *qt.QLabel {
	label := qt.NewQLabel3(text)
	label.SetTextFormat(qt.RichText)
	label.SetWordWrap(true)
	label.SetOpenExternalLinks(true)
	label.SetTextInteractionFlags(qt.TextBrowserInteraction)
	return label
}

// showAbout shows the about box until it is closed.
func showAbout(parent *qt.QWidget) {
	d := newAboutDialog(parent)
	// A dialog is not destroyed by being closed.
	defer d.DeleteLater()
	d.Exec()
}

package ui

import (
	"github.com/nomad-software/qute/qtcore"
	"github.com/nomad-software/qute/qtwidgets"

	"runtime/debug"
	"strconv"
	"strings"
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
func newAboutTitle() (*qtwidgets.QHBoxLayout, *qtwidgets.QLabel) {
	name := qtwidgets.NewQLabelWithText("TriggerAPI", nil)
	font := name.Font()
	font.SetBold(true)
	font.SetPointSizeF(font.PointSizeF() * 1.5)
	name.SetFont(font)

	version := qtwidgets.NewQLabelWithText(appVersion(), nil)
	version.SetTextFormat(qtcore.PlainText)
	small := version.Font()
	small.SetPointSizeF(small.PointSizeF() * 0.8)
	version.SetFont(small)
	version.SetContentsMargins(qtcore.QMargins{Left: 0, Top: 0, Right: 0, Bottom: name.FontMetrics().Descent() - version.FontMetrics().Descent()})
	faded := qtwidgets.NewQGraphicsOpacityEffect(nil)
	faded.SetOpacity(0.5)
	version.SetGraphicsEffect(faded.AsQGraphicsEffect())

	title := qtwidgets.NewQHBoxLayout(nil)
	title.AddWidgetFull(name.AsQWidget(), 0, qtcore.AlignBottom)
	title.AddWidgetFull(version.AsQWidget(), 0, qtcore.AlignBottom)
	title.AddStretch()
	return title, version
}

// licenceText returns the notices of the licences of what the program is
// built with, which carry the notice the LGPL asks of programs that use Qt.
func licenceText() string {
	return "<p><b>Third party licences</b></p>" +
		"<p>This program uses Qt version " + qtcore.QLibraryInfoVersion().ToString() + ".</p>" +
		"<p>Qt is a C++ toolkit for cross-platform application development, used here under the " +
		"terms of the <a href=\"https://www.gnu.org/licenses/lgpl-3.0.html\">GNU Lesser General " +
		"Public License version 3</a> (LGPLv3). Qt is dynamically linked, so it can be replaced " +
		"with a different or modified build of the library.</p>" +
		"<p>Qt and its source code are available from <a href=\"https://www.qt.io\">www.qt.io</a>. " +
		"See <a href=\"https://www.qt.io/licensing\">www.qt.io/licensing</a> for an overview of Qt " +
		"licensing.</p>" +
		"<p>Copyright (C) The Qt Company Ltd. and other contributors. Qt and the Qt logo are " +
		"trademarks of The Qt Company Ltd.</p>" +
		"<p>The Go bindings for Qt are provided by <a href=\"https://github.com/nomad-software/qute\">qute</a>, " +
		"used under the GNU General Public License version 3.</p>" +
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
	*qtwidgets.QDialog
	// notices is the frame around the notices of the licences.
	notices *qtwidgets.QFrame
	// version is the label of the version of the program.
	version *qtwidgets.QLabel
}

func newAboutDialog(parent *qtwidgets.QWidget) *aboutDialog {
	d := &aboutDialog{QDialog: qtwidgets.NewQDialog(parent)}
	d.SetWindowTitle("About TriggerAPI")
	d.SetMinimumWidth(aboutWidth)

	icon := qtwidgets.NewQLabel(nil)
	icon.SetPixmap(appIcon().PixmapExtent(aboutIconSize))
	icon.SetAlignment(qtcore.AlignTop)

	title, version := newAboutTitle()
	d.version = version

	about := qtwidgets.NewQVBoxLayout(nil)
	about.SetSpacing(12)
	about.AddLayout(title.AsQLayout())
	about.AddWidget(newRichLabel(aboutText()).AsQWidget())
	about.AddStretch()

	top := qtwidgets.NewQHBoxLayout(nil)
	top.SetSpacing(20)
	top.AddWidget(icon.AsQWidget())
	top.AddLayoutFull(about.AsQLayout(), 1)

	// The notices are smaller than the rest, as on a label of the style.
	licences := newRichLabel(licenceText())
	font := licences.Font()
	font.SetPointSizeF(font.PointSizeF() * 0.9)
	licences.SetFont(font)

	d.notices = qtwidgets.NewQFrame(nil)
	d.notices.SetFrameShape(qtwidgets.QFrameStyledPanel)
	d.notices.SetFrameShadow(qtwidgets.QFrameSunken)
	frameLayout := qtwidgets.NewQVBoxLayout(d.notices.AsQWidget())
	frameLayout.SetContentsMargins(qtcore.QMargins{Left: 12, Top: 8, Right: 12, Bottom: 8})
	frameLayout.AddWidget(licences.AsQWidget())

	buttons := qtwidgets.NewQDialogButtonBoxWithStandardButton(qtwidgets.QDialogButtonBoxOk, nil)
	buttons.OnAccepted(d.Accept)

	layout := qtwidgets.NewQVBoxLayout(d.AsQWidget())
	layout.SetContentsMargins(qtcore.QMargins{Left: 20, Top: 20, Right: 20, Bottom: 16})
	layout.SetSpacing(16)
	layout.AddLayout(top.AsQLayout())
	layout.AddWidget(d.notices.AsQWidget())
	layout.AddWidget(buttons.AsQWidget())

	return d
}

// newRichLabel returns a label of rich text which wraps, and whose links
// open in the browser.
func newRichLabel(text string) *qtwidgets.QLabel {
	label := qtwidgets.NewQLabelWithText(text, nil)
	label.SetTextFormat(qtcore.RichText)
	label.SetWordWrap(true)
	label.SetOpenExternalLinks(true)
	label.SetTextInteractionFlags(qtcore.TextBrowserInteraction)
	return label
}

// showAbout shows the about box until it is closed.
func showAbout(parent *qtwidgets.QWidget) {
	d := newAboutDialog(parent)
	// A dialog is not destroyed by being closed.
	defer d.DeleteLater()
	d.Exec()
}

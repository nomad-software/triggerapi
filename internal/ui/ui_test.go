package ui

// The tests of the user interface. This file runs them (TestMain) and makes
// what a test window needs: its files, with the keys of the tests. Helpers
// that tests of several features share are in helpers_test.go, and the tests
// of each feature are in the file named after the source they test, as
// rest_test.go for rest.go and requesttreesearch_test.go for
// requesttreesearch.go.

import (
	"os"
	"path/filepath"
	"testing"

	qt "github.com/mappu/miqt/qt6"
	"github.com/mappu/miqt/qt6/mainthread"

	"github.com/nomad-software/triggerapi/internal/collection"
	"github.com/nomad-software/triggerapi/internal/crypt"
	"github.com/nomad-software/triggerapi/internal/crypt/crypttest"
	"github.com/nomad-software/triggerapi/internal/env"
	"github.com/nomad-software/triggerapi/internal/history"
	"github.com/nomad-software/triggerapi/internal/settings"
	"github.com/nomad-software/triggerapi/internal/workspace"
)

// TestMain runs the Qt event loop on the main thread and the tests on another
// goroutine. Tests must touch widgets through mainthread.Wait.
func TestMain(m *testing.M) {
	os.Setenv("QT_QPA_PLATFORM", "offscreen")
	qt.NewQApplication(os.Args[:1])
	// Tests close windows, which must not end the event loop.
	qt.QGuiApplication_SetQuitOnLastWindowClosed(false)
	// Answer the request type dialog rather than show it.
	askRequestType = func(*qt.QWidget) (collection.Type, bool) {
		return testRequestType, testRequestType != ""
	}

	// The dialog which says keys are needed is counted rather than shown.
	adviseKeys = func(*qt.QWidget) { advised++ }

	// Every window is given the keys it needs to read and save anything.
	keyDir, err := os.MkdirTemp("", "triggerapi-keys-")
	if err == nil {
		testPublic, testPrivate, err = crypttest.Write(keyDir, "id", "")
	}
	if err == nil {
		testKeys, err = crypt.Open(testPublic, testPrivate, nil)
	}
	if err != nil {
		panic(err)
	}

	code := 0
	go func() {
		code = m.Run()
		os.RemoveAll(keyDir)
		mainthread.Start(qt.QCoreApplication_Quit)
	}()

	qt.QApplication_Exec()
	os.Exit(code)
}

// testPublic and testPrivate are the paths of the files of the keys the
// settings of a test window name, and testKeys the keys themselves.
var (
	testPublic, testPrivate string
	testKeys                *crypt.Keys
)

// advised counts the times the user was told that keys are needed.
var advised int

// loadEnvs reads the environments a test window saved.
func loadEnvs(path string) (*env.Store, error) {
	writer.Flush()
	return env.LoadWith(path, testKeys)
}

// loadHistory reads the history a test window saved.
func loadHistory(path string) (*history.Store, error) {
	writer.Flush()
	return history.LoadWith(path, testKeys)
}

// loadRequests reads the requests a test window saved.
func loadRequests(path string) (*collection.Store, error) {
	writer.Flush()
	return collection.LoadWith(path, testKeys)
}

// saveTestKeys makes a settings file which names the keys of the tests and
// nothing else, as a window without keys reads and saves nothing.
func saveTestKeys(t *testing.T, path string) {
	t.Helper()
	if err := settings.Prepare(path); err != nil {
		t.Fatal(err)
	}
	if err := (settings.Settings{PublicKey: testPublic, PrivateKey: testPrivate}).Save(path); err != nil {
		t.Fatal(err)
	}
}

// testRequestType is the type given to requests added by a test. An empty
// type stands for the user cancelling the dialog.
var testRequestType = collection.TypeREST

// testPaths are the places a test window keeps its data, with the paths of
// the files of its one workspace.
type testPaths struct {
	Files
	Environments string
	Requests     string
	History      string
}

// closeWindows has every window closed when the test ends. A window saves
// its settings when it is closed, which the ones left open would otherwise
// do when the tests are over and the folder of their settings is gone. It is
// called after the folder is made, so that it is still there.
func closeWindows(t *testing.T) {
	t.Cleanup(func() {
		mainthread.Wait(func() {
			for _, widget := range qt.QApplication_TopLevelWidgets() {
				if widget.IsVisible() {
					widget.Close()
				}
			}
		})
	})
}

// testFiles returns places in a temporary folder for a window to keep its
// data in, with a workspace named Default that has nothing in it yet.
func testFiles(t *testing.T) testPaths {
	dir := t.TempDir()
	closeWindows(t)
	workspaces := filepath.Join(dir, "workspaces")
	if err := workspace.Create(workspaces, workspace.Default); err != nil {
		t.Fatal(err)
	}
	settingsPath := filepath.Join(dir, "settings.json")
	saveTestKeys(t, settingsPath)
	return testPaths{
		Files:        Files{Settings: filepath.Join(dir, "settings.json"), Workspaces: workspaces},
		Environments: workspace.Environments(workspaces, workspace.Default),
		Requests:     workspace.Requests(workspaces, workspace.Default),
		History:      workspace.History(workspaces, workspace.Default),
	}
}

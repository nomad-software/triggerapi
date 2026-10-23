package ui

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mappu/miqt/qt6/mainthread"
)

// The stores are written in the background, and a failure is shown in the
// status bar once it is known.
func TestSaveStoreInBackground(t *testing.T) {
	files := testFiles(t)
	var w *Window
	mainthread.Wait(func() {
		w = NewWindow(files.Files)
		w.Show()
		w.request.url.SetText("one.test")
		w.requests.addRequest()
	})
	saved, err := loadRequests(files.Requests)
	if err != nil || saved.Folders[0].Requests[0].URL != "one.test" {
		t.Fatalf("saved %+v, %v", saved, err)
	}

	// A file that cannot be written is reported on the GUI thread.
	mainthread.Wait(func() {
		w.requests.path = filepath.Join(t.TempDir(), "gone", "requests.json")
		w.requests.save()
	})
	waitFor(t, "the failure", func() bool {
		return mainthread.Wait2(func() bool {
			return strings.Contains(w.StatusBar().CurrentMessage(), "Changes could not be saved")
		})
	})
	// It is what is not on disk until the file is written.
	var path string
	mainthread.Wait(func() { path = w.requests.path })
	if err := mainthread.Wait2(w.writeFiles); err == nil || !strings.Contains(err.Error(), path) {
		t.Errorf("writeFiles = %v", err)
	}
	if err := os.Mkdir(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	mainthread.Wait(func() { w.requests.save() })
	if err := writer.Flush(); err != nil {
		t.Errorf("Flush = %v", err)
	}
}

// A workspace is removed once what is waiting to be written to it has been,
// so that nothing is written into its folder after it has gone.
func TestRemoveWorkspaceWithWrites(t *testing.T) {
	files := testFiles(t)
	dir := files.Workspaces
	var w *Window
	mainthread.Wait(func() {
		w = NewWindow(files.Files)
		w.Show()
		ws := w.workspaces
		ws.warn = func(message string) { t.Error(message) }
		ws.confirm = func(string) bool { return true }

		ws.add()
		// Many changes are waiting to be written when it is removed.
		for range 20 {
			w.requests.addRequest()
			w.environments.add()
		}
		ws.remove()
		if !reflect.DeepEqual(ws.names, []string{"Default"}) {
			t.Errorf("workspaces = %q", ws.names)
		}
	})
	writer.Flush()
	if _, err := os.Stat(filepath.Join(dir, "New Workspace")); !os.IsNotExist(err) {
		t.Errorf("the folder is there: %v", err)
	}
}

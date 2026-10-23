package ui

import (
	"os"
	"reflect"
	"strings"
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

func TestEncryption(t *testing.T) {
	files := testFiles(t)
	dir := files.Workspaces
	keyDir := t.TempDir()
	public, private := crypttest.Pair(t, keyDir, "id", "hunter2")
	otherPublic, otherPrivate := crypttest.Pair(t, keyDir, "other", "")

	// The passphrase is given rather than asked for in a dialog.
	asked := 0
	previous := askPassphrase
	askPassphrase = func(*qt.QWidget, string) ([]byte, error) {
		asked++
		return []byte("hunter2"), nil
	}
	t.Cleanup(func() { askPassphrase = previous })

	// Two workspaces saved before there were keys, and settings without
	// any, as on the first run of a version with encryption.
	if err := (settings.Settings{}).Save(files.Settings); err != nil {
		t.Fatal(err)
	}
	if err := workspace.Create(dir, "Other"); err != nil {
		t.Fatal(err)
	}
	envs := &env.Store{Environments: []env.Environment{{Name: "Secret"}}, Active: 0}
	requests := &collection.Store{Folders: []*collection.Folder{{Name: "Hidden"}}}
	paths := []string{
		files.Environments, files.Requests,
		workspace.Environments(dir, "Other"), workspace.Requests(dir, "Other"),
	}
	for _, path := range []string{paths[0], paths[2]} {
		if err := envs.Save(path); err != nil {
			t.Fatal(err)
		}
	}
	for _, path := range []string{paths[1], paths[3]} {
		if err := requests.Save(path); err != nil {
			t.Fatal(err)
		}
	}
	// A history, which is encrypted with the rest.
	sent := &history.Store{}
	sent.Add(&history.Entry{Request: collection.New(collection.TypeREST, "Sent"), Error: "refused"}, 10)
	paths = append(paths, workspace.History(dir, "Other"))
	if err := sent.SaveWith(paths[4], nil); err != nil {
		t.Fatal(err)
	}

	contents := func() []string {
		writer.Flush()
		var all []string
		for _, path := range paths {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Error(err)
			}
			all = append(all, string(data))
		}
		return all
	}
	allEncrypted := func() {
		t.Helper()
		for i, data := range contents() {
			if !crypt.Encrypted([]byte(data)) {
				t.Errorf("%s is not encrypted", paths[i])
			}
		}
	}
	// loaded reports if the window shows the workspace and saves to it.
	loaded := func(w *Window, environments int) bool {
		return w.environments.list.Count() == environments && topCount(w.requests.tree) == 1 &&
			topFolder(w.requests.tree, 0).Text(colName) == "Hidden" &&
			w.environments.path == files.Environments && w.requests.path == files.Requests
	}
	// empty reports if the window shows nothing and saves nowhere.
	empty := func(w *Window) bool {
		return w.environments.list.Count() == 0 && topCount(w.requests.tree) == 0 &&
			w.environments.path == "" && w.requests.path == "" &&
			strings.Contains(w.StatusBar().CurrentMessage(), "needs a pair of SSH keys")
	}

	// Without keys nothing is read or saved, and the user is told so once
	// the window is shown.
	before := contents()
	advised = 0
	var w *Window
	mainthread.Wait(func() {
		w = NewWindow(files.Files)
		w.Show()
		if !empty(w) {
			t.Errorf("without keys: status %q", w.StatusBar().CurrentMessage())
		}
		w.environments.add()
	})
	waitFor(t, "being told that keys are needed", func() bool { return advised == 1 })
	if !reflect.DeepEqual(contents(), before) {
		t.Error("files were written without keys")
	}

	mainthread.Wait(func() {
		// Naming the keys in the settings encrypts the files of every
		// workspace and shows the one in use.
		s := w.config
		s.PublicKey, s.PrivateKey = public, private
		if err := w.applySettings(s); err != nil {
			t.Error(err)
		}
		if asked != 1 {
			t.Errorf("the passphrase was asked for %d times", asked)
		}
		allEncrypted()
		if !loaded(w, 1) || w.StatusBar().CurrentMessage() != "" {
			t.Errorf("the workspace was not loaded: status %q", w.StatusBar().CurrentMessage())
		}

		// What is saved afterwards is encrypted too.
		w.environments.add()
		allEncrypted()
		if !loaded(w, 2) {
			t.Error("an environment was not added")
		}

		// Keys that are not a pair are refused, and the ones there are kept.
		s = w.config
		s.PublicKey = otherPublic
		if err := w.applySettings(s); err == nil || !strings.Contains(err.Error(), "cannot be used") {
			t.Errorf("err = %v", err)
		}
		if w.config.PublicKey != public || w.keys.Err() != nil || !loaded(w, 2) {
			t.Errorf("settings changed: %+v", w.config)
		}
		w.Close()
	})
	if saved := settings.Load(files.Settings); saved.PublicKey != public || saved.PrivateKey != private {
		t.Errorf("saved settings = %+v", saved)
	}

	// The next run asks for the passphrase and reads the files.
	asked, advised = 0, 0
	mainthread.Wait(func() {
		w = NewWindow(files.Files)
		w.Show()
		if asked != 1 {
			t.Errorf("the passphrase was asked for %d times", asked)
		}
		if !loaded(w, 2) {
			t.Error("the encrypted workspace was not loaded")
		}

		// Other keys encrypt every file again.
		s := w.config
		s.PublicKey, s.PrivateKey = otherPublic, otherPrivate
		if err := w.applySettings(s); err != nil {
			t.Error(err)
		}
		allEncrypted()
		other, err := crypt.Open(otherPublic, otherPrivate, nil)
		if err != nil {
			t.Error(err)
			return
		}
		for _, path := range []string{paths[0], paths[2]} {
			if got, err := env.LoadWith(path, other); err != nil || len(got.Environments) == 0 {
				t.Errorf("%s: %+v, %v", path, got, err)
			}
		}
		if advised != 0 {
			t.Errorf("told that keys are needed %d times", advised)
		}

		// Taking the keys away leaves the files as they are, shows
		// nothing, and tells the user.
		before = contents()
		s = w.config
		s.PublicKey, s.PrivateKey = "", ""
		if err := w.applySettings(s); err != nil {
			t.Error(err)
		}
		if advised != 1 || !empty(w) {
			t.Errorf("keys taken away: advised %d times, status %q", advised, w.StatusBar().CurrentMessage())
		}
		w.environments.add()
		if !reflect.DeepEqual(contents(), before) {
			t.Error("files were changed when the keys were taken away")
		}

		// Putting them back shows what is in the files.
		s = w.config
		s.PublicKey, s.PrivateKey = otherPublic, otherPrivate
		if err := w.applySettings(s); err != nil {
			t.Error(err)
		}
		if !loaded(w, 2) {
			t.Error("the workspace was not loaded once the keys were given")
		}
		w.Close()
	})
}

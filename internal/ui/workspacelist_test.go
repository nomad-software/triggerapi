package ui

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mappu/miqt/qt6/mainthread"

	"github.com/nomad-software/triggerapi/internal/crypt/crypttest"
	"github.com/nomad-software/triggerapi/internal/settings"
	"github.com/nomad-software/triggerapi/internal/workspace"
)

func TestWorkspaces(t *testing.T) {
	files := testFiles(t)
	dir := files.Workspaces

	var w *Window
	var warned []string
	mainthread.Wait(func() {
		w = NewWindow(files.Files)
		w.Show()
		ws := w.workspaces
		ws.warn = func(message string) { warned = append(warned, message) }

		// There is one workspace to begin with, which is the one in use.
		if !reflect.DeepEqual(ws.names, []string{"Default"}) || ws.current() != "Default" || ws.list.CurrentRow() != 0 {
			t.Fatalf("workspaces = %q, current %q", ws.names, ws.current())
		}

		// It is given an environment and a request.
		w.environments.add()
		w.editor.name.SetText("Local")
		w.closeEditor()
		w.request.url.SetText("default.test")
		w.requests.addRequest()

		// The last workspace cannot be removed.
		ws.confirm = func(string) bool { t.Error("asked about the last workspace"); return true }
		ws.remove()
		if len(warned) != 1 || !strings.Contains(warned[0], "last workspace") || len(ws.names) != 1 {
			t.Errorf("warned %q with workspaces %q", warned, ws.names)
		}
		warned = nil

		// A new workspace becomes the one in use, and has nothing in it.
		ws.add()
		if !reflect.DeepEqual(ws.names, []string{"Default", "New Workspace"}) || ws.current() != "New Workspace" || w.config.Workspace != "New Workspace" {
			t.Fatalf("workspaces = %q, current %q", ws.names, ws.current())
		}
		if w.environments.list.Count() != 0 || topCount(w.requests.tree) != 0 || w.open != nil || w.request.url.Text() != "" {
			t.Errorf("new workspace has %d environments and %d folders, open %+v", w.environments.list.Count(), topCount(w.requests.tree), w.open)
		}

		// What is added now goes into it.
		w.environments.add()
		w.editor.name.SetText("Staging")
		w.closeEditor()
		w.request.url.SetText("new.test")
		w.requests.addRequest()

		// It is renamed, which renames its folder and moves it in the list.
		ws.list.CurrentItem().SetText("  Alpha ")
	})

	waitFor(t, "the workspace to be renamed", func() bool {
		return reflect.DeepEqual(w.workspaces.names, []string{"Alpha", "Default"}) && w.workspaces.current() == "Alpha"
	})

	mainthread.Wait(func() {
		ws := w.workspaces
		if w.config.Workspace != "Alpha" || w.environments.path != workspace.Environments(dir, "Alpha") || w.requests.path != workspace.Requests(dir, "Alpha") {
			t.Errorf("after rename: workspace %q, paths %q and %q", w.config.Workspace, w.environments.path, w.requests.path)
		}
		if _, err := os.Stat(filepath.Join(dir, "New Workspace")); err == nil {
			t.Error("the old folder is still there")
		}

		// Later changes are saved under the new name.
		w.environments.edit()
		w.editor.variables.add("host", "alpha.test")
		w.closeEditor()

		// A name that cannot be used is refused.
		ws.list.CurrentItem().SetText("default")
	})

	waitFor(t, "the name to be refused", func() bool { return len(warned) == 1 })

	mainthread.Wait(func() {
		ws := w.workspaces
		if !strings.Contains(warned[0], "already a workspace") || !reflect.DeepEqual(ws.names, []string{"Alpha", "Default"}) || ws.list.Item(0).Text() != "Alpha" {
			t.Errorf("warned %q, workspaces %q, shown %q", warned, ws.names, ws.list.Item(0).Text())
		}
		warned = nil

		// Selecting the other workspace shows what is in it.
		ws.list.SetCurrentRow(1)
		if ws.current() != "Default" || w.environments.list.Count() != 1 || w.environments.list.Item(0).Text() != "Local" {
			t.Fatalf("current %q with %d environments", ws.current(), w.environments.list.Count())
		}
		tree := w.requests.tree
		if topCount(tree) != 1 || w.open != nil {
			t.Fatalf("%d folders, open %+v", topCount(tree), w.open)
		}
		tree.SetCurrentItem(topFolder(tree, 0).Child(0))
		if w.request.url.Text() != "default.test" {
			t.Errorf("url = %q", w.request.url.Text())
		}

		// And back again.
		ws.list.SetCurrentRow(0)
		if w.environments.list.Item(0).Text() != "Staging" || w.expand("{{host}}") != "alpha.test" || w.open != nil {
			t.Errorf("environment %q, host %q", w.environments.list.Item(0).Text(), w.expand("{{host}}"))
		}

		w.Close()
	})

	// Each workspace has its own files.
	alpha, err := loadRequests(workspace.Requests(dir, "Alpha"))
	if err != nil || len(alpha.Folders) != 1 || alpha.Folders[0].Requests[0].URL != "new.test" {
		t.Errorf("requests of Alpha = %+v, %v", alpha, err)
	}
	original, err := loadRequests(files.Requests)
	if err != nil || len(original.Folders) != 1 || original.Folders[0].Requests[0].URL != "default.test" {
		t.Errorf("requests of Default = %+v, %v", original, err)
	}
	if envs, _ := loadEnvs(workspace.Environments(dir, "Alpha")); len(envs.Environments) != 1 || len(envs.Environments[0].Variables) != 1 {
		t.Errorf("environments of Alpha = %+v", envs)
	}
	if saved := settings.Load(files.Settings); saved.Workspace != "Alpha" {
		t.Errorf("saved workspace = %q", saved.Workspace)
	}

	// The next run opens the workspace that was in use.
	mainthread.Wait(func() {
		w = NewWindow(files.Files)
		w.Show()
		ws := w.workspaces
		ws.warn = func(message string) { warned = append(warned, message) }

		if ws.current() != "Alpha" || w.environments.list.Count() != 1 || w.environments.list.Item(0).Text() != "Staging" {
			t.Fatalf("current %q with %d environments", ws.current(), w.environments.list.Count())
		}
		tree := w.requests.tree
		tree.SetCurrentItem(topFolder(tree, 0).Child(0))

		// It is not removed unless the user agrees to it.
		var asked string
		ws.confirm = func(question string) bool { asked = question; return false }
		ws.remove()
		if len(ws.names) != 2 || !strings.Contains(asked, `"Alpha"`) {
			t.Errorf("workspaces %q after being asked %q", ws.names, asked)
		}

		// Removing it deletes its files, and the other becomes the one in
		// use, with the request that was open closed.
		ws.confirm = func(string) bool { return true }
		ws.remove()
		if !reflect.DeepEqual(ws.names, []string{"Default"}) || ws.current() != "Default" || w.config.Workspace != "Default" || len(warned) != 0 {
			t.Fatalf("workspaces = %q, current %q, warned %q", ws.names, ws.current(), warned)
		}
		if w.open != nil || w.environments.list.Item(0).Text() != "Local" || w.requests.path != files.Requests {
			t.Errorf("after remove: open %+v, requests at %q", w.open, w.requests.path)
		}
	})
	if _, err := os.Stat(filepath.Join(dir, "Alpha")); err == nil {
		t.Error("the folder of the removed workspace is still there")
	}

	// A workspace whose folder has other files in it is not removed.
	mainthread.Wait(func() {
		ws := w.workspaces
		ws.add()
		os.WriteFile(filepath.Join(dir, "New Workspace", "notes.txt"), []byte("mine"), 0o644)
		ws.remove()
		if len(warned) != 1 || !strings.Contains(warned[0], "notes.txt") || len(ws.names) != 2 || ws.current() != "New Workspace" {
			t.Errorf("warned %q, workspaces %q", warned, ws.names)
		}
	})
}

func TestFirstRun(t *testing.T) {
	dir := t.TempDir()
	files := Files{
		Settings:   filepath.Join(dir, "settings.json"),
		Workspaces: filepath.Join(dir, "workspaces"),
	}
	saveTestKeys(t, files.Settings)

	// With nothing saved at all there is still a workspace to work in.
	mainthread.Wait(func() {
		w := NewWindow(files)
		if !reflect.DeepEqual(w.workspaces.names, []string{"Default"}) || w.StatusBar().CurrentMessage() != "" {
			t.Fatalf("workspaces = %q, status %q", w.workspaces.names, w.StatusBar().CurrentMessage())
		}
		w.request.url.SetText("first.test")
		w.requests.addRequest()
	})
	if saved, err := loadRequests(workspace.Requests(files.Workspaces, "Default")); err != nil || len(saved.Folders) != 1 {
		t.Errorf("requests = %+v, %v", saved, err)
	}
}

func TestExportAndImportWorkspaces(t *testing.T) {
	files := testFiles(t)
	keyDir := t.TempDir()
	theirPublic, theirPrivate := crypttest.Pair(t, keyDir, "theirs", "")
	archive := filepath.Join(t.TempDir(), "export.zip")

	// Someone else has a window of their own, with their own keys.
	theirFiles := testFiles(t)
	if err := (settings.Settings{PublicKey: theirPublic, PrivateKey: theirPrivate}).Save(theirFiles.Settings); err != nil {
		t.Fatal(err)
	}

	mainthread.Wait(func() {
		w := NewWindow(files.Files)
		w.environments.add()
		w.requests.addFolder()

		// The workspaces are exported for their public key alone.
		if err := w.exportWorkspaces(theirPublic, archive); err != nil {
			t.Error(err)
			return
		}
		// They are not encrypted for the keys they came from.
		if _, err := w.importWorkspaces(archive); err == nil {
			t.Error("imported workspaces that were exported for someone else")
		}
		if !reflect.DeepEqual(w.workspaces.names, []string{"Default"}) {
			t.Errorf("workspaces = %q", w.workspaces.names)
		}
		if err := w.exportWorkspaces(filepath.Join(keyDir, "none.pub"), archive); err == nil {
			t.Error("exported for a key that is not there")
		}
	})

	mainthread.Wait(func() {
		w := NewWindow(theirFiles.Files)
		added, err := w.importWorkspaces(archive)
		if err != nil || !reflect.DeepEqual(added, []string{"Default 2"}) {
			t.Errorf("added = %q, %v", added, err)
			return
		}
		// The workspace in use stays so, and the new one is listed.
		if !reflect.DeepEqual(w.workspaces.names, []string{"Default", "Default 2"}) || w.workspaces.current() != "Default" || w.environments.list.Count() != 0 {
			t.Errorf("workspaces = %q, current %q", w.workspaces.names, w.workspaces.current())
		}

		// It can be read with their keys.
		w.workspaces.open("Default 2")
		if w.environments.list.Count() != 1 || topCount(w.requests.tree) != 1 || w.StatusBar().CurrentMessage() != "" {
			t.Errorf("the imported workspace was not loaded: status %q", w.StatusBar().CurrentMessage())
		}
	})
}

package ui

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	qt "github.com/mappu/miqt/qt6"

	"github.com/nomad-software/triggerapi/internal/collection"
	"github.com/nomad-software/triggerapi/internal/crypt"
	"github.com/nomad-software/triggerapi/internal/env"
	"github.com/nomad-software/triggerapi/internal/history"
	"github.com/nomad-software/triggerapi/internal/jsonfile"
	"github.com/nomad-software/triggerapi/internal/settings"
	"github.com/nomad-software/triggerapi/internal/workspace"
)

// What the window does with the workspaces, their files and the keys they
// are encrypted with.

// workspacesDir returns the folder the workspaces are kept in.
func (w *Window) workspacesDir() string {
	if w.config.WorkspacesDir != "" {
		return w.config.WorkspacesDir
	}
	return w.files.Workspaces
}

// firstWorkspace makes the first workspace, named Default, if there are
// none, so that there is always one to work in.
func (w *Window) firstWorkspace() {
	dir := w.workspacesDir()
	if names, err := workspace.List(dir); err != nil || len(names) > 0 {
		return
	}
	if err := workspace.Create(dir, workspace.Default); err != nil {
		w.status("The first workspace could not be made: " + err.Error())
	}
}

// loaded is what loadWorkspace read of a workspace: its environments,
// requests and history, each with the path it is to be saved to.
type loaded struct {
	envs         *env.Store
	envPath      string
	requests     *collection.Store
	requestsPath string
	history      *history.Store
	historyPath  string
}

// loadWorkspace reads the environments, requests and history of a
// workspace. A file that could not be read is never saved to, so it is not
// overwritten: its path is empty, as are all of them if there is no
// workspace.
func (w *Window) loadWorkspace(name string) loaded {
	none := loaded{envs: &env.Store{Active: -1}, requests: &collection.Store{}, history: &history.Store{}}
	if name == "" {
		w.status("There is no workspace, so changes will not be saved.")
		return none
	}
	// Nothing is read or saved without keys to encrypt it with.
	if w.keys == nil {
		w.status(needKeys)
		return none
	}

	var failed []error
	dir := w.workspacesDir()
	l := loaded{
		envPath:      workspace.Environments(dir, name),
		requestsPath: workspace.Requests(dir, name),
		historyPath:  workspace.History(dir, name),
	}
	// A file from before there were keys is encrypted once it has been
	// read, which leaves alone one that cannot be.
	read := func(path *string, err error) {
		if err == nil {
			err = w.encrypt(*path)
		}
		if err != nil {
			*path = ""
			failed = append(failed, err)
		}
	}
	var err error
	l.envs, err = env.LoadWith(l.envPath, w.keys)
	read(&l.envPath, err)
	l.requests, err = collection.LoadWith(l.requestsPath, w.keys)
	read(&l.requestsPath, err)
	l.history, err = history.LoadWith(l.historyPath, w.keys)
	read(&l.historyPath, err)

	if len(failed) > 0 {
		w.status("Saved data could not be loaded, changes will not be saved: " + errors.Join(failed...).Error())
	} else {
		w.status("")
	}
	return l
}

// encrypt has a file that is not encrypted, as one from before there were
// keys, made so.
func (w *Window) encrypt(path string) error {
	return jsonfile.Convert(path, w.keys, w.keys)
}

// needKeys says what is wrong when the settings have no keys.
const needKeys = "TriggerAPI needs a pair of SSH keys to encrypt its files with. " +
	"Until they are added under Encryption in File > Settings, nothing is loaded or saved."

// adviseKeys tells the user that the program cannot work until the settings
// are given keys. Tests replace it rather than let the dialog open.
var adviseKeys = func(parent *qt.QWidget) {
	qt.QMessageBox_Warning(parent, "Keys Needed", needKeys)
}

// askPassphrase asks for the passphrase of the private key in the file at
// path. Tests replace it rather than let the dialog open.
var askPassphrase = func(parent *qt.QWidget, path string) ([]byte, error) {
	ok := false
	passphrase := qt.QInputDialog_GetText4(parent, "Private Key", "Passphrase of "+path, qt.QLineEdit__Password, "", &ok)
	if !ok {
		return nil, errors.New("no passphrase was given")
	}
	return []byte(passphrase), nil
}

// openKeys returns the keys the settings name, which are nil if they name
// none. Keys that cannot be opened are returned, with the reason, as ones
// that cannot be used, so that nothing is saved without them.
func openKeys(parent *qt.QWidget, s settings.Settings) (*crypt.Keys, error) {
	if s.PublicKey == "" && s.PrivateKey == "" {
		return nil, nil
	}
	keys, err := crypt.Open(s.PublicKey, s.PrivateKey, func() ([]byte, error) {
		return askPassphrase(parent, s.PrivateKey)
	})
	if err != nil {
		err = fmt.Errorf("the keys of the settings cannot be used: %w", err)
		return crypt.Unusable(err), err
	}
	return keys, nil
}

// changeKeys has the environments, requests and history of every workspace
// encrypted with other keys from now on: each file is decrypted with the
// keys there were and encrypted with those given. The workspace in use is
// read again if it could not be before. Taking the keys away leaves the
// files encrypted as they are, and nothing can be read or saved until there
// are keys again, which the user is told.
func (w *Window) changeKeys(keys *crypt.Keys) error {
	// The history is written with the keys it was read with before the
	// files are converted.
	w.history.flush()
	if keys == nil {
		w.keys = nil
		w.workspaces.open(w.config.Workspace)
		adviseKeys(w.QWidget)
		return nil
	}

	dir := w.workspacesDir()
	names, err := workspace.List(dir)

	failed := []error{err}
	for _, name := range names {
		for _, path := range workspace.Files(dir, name) {
			if err := jsonfile.Convert(path, w.keys, keys); err != nil {
				failed = append(failed, fmt.Errorf("%s: %w", path, err))
			}
		}
	}
	w.keys = keys

	if w.environments.path == "" || w.requests.path == "" || w.history.path == "" {
		w.workspaces.open(w.config.Workspace)
	}
	return errors.Join(failed...)
}

// openWorkspace makes a workspace the one in use, showing its environments
// and requests in place of those of the one before.
func (w *Window) openWorkspace(name string) {
	// The pinned requests of the workspace being left are noted while its
	// requests are still there to be found, and its history is written.
	w.keepSet()
	w.history.flush()

	l := w.loadWorkspace(name)
	w.config.Workspace = name

	// The environment being edited belongs to the workspace being left, as
	// do the requests in the tabs, which are replaced by those that are
	// pinned in this one.
	w.closeEditor()

	w.environments.load(l.envs, l.envPath)
	w.requests.clearSearch()
	w.requests.load(l.requests, l.requestsPath)
	w.history.load(l.history, l.historyPath)
	w.restoreTabs()
	w.environmentChanged()
}

// keepSet records the set chosen in the workspace in use, for its tabs to be
// opened again when the workspace is gone back to. Tabs that are not in a
// set are not remembered.
func (w *Window) keepSet() {
	if w.chosenSets == nil {
		w.chosenSets = map[string]string{}
	}
	if set := w.requests.sets.active; set != nil {
		w.chosenSets[w.config.Workspace] = set.Name
	} else {
		delete(w.chosenSets, w.config.Workspace)
	}
}

// renamedWorkspace follows the workspace in use to its new name, which is
// also the name of the folder its files are in.
func (w *Window) renamedWorkspace(name string) {
	w.config.Workspace = name
	if w.environments.path != "" {
		w.environments.path = workspace.Environments(w.workspacesDir(), name)
	}
	if w.requests.path != "" {
		w.requests.path = workspace.Requests(w.workspacesDir(), name)
	}
	if w.history.path != "" {
		w.history.path = workspace.History(w.workspacesDir(), name)
	}
}

// moveWorkspaces keeps the workspaces in another folder from now on. If
// there are none in it yet, the ones there are now are copied there.
func (w *Window) moveWorkspaces(from string) error {
	// The history is written where it is before it is copied.
	w.history.flush()
	to := w.workspacesDir()

	names, err := workspace.List(to)
	if err != nil {
		return err
	}
	if len(names) == 0 {
		if err := workspace.CopyAll(from, to); err != nil {
			return err
		}
	}

	w.workspaces.open(w.config.Workspace)
	return nil
}

// exportName is the name offered for the file the workspaces are exported
// to.
const exportName = "triggerapi-workspaces.zip"

// chooseExport asks whose public key the workspaces are to be exported for
// and where to, and exports them.
func (w *Window) chooseExport(parent *qt.QWidget) {
	if w.keys == nil {
		adviseKeys(parent)
		return
	}
	public := qt.QFileDialog_GetOpenFileName3(parent, "Public Key To Export For", filepath.Join(qt.QDir_HomePath(), ".ssh"))
	if public == "" {
		return
	}
	archive := qt.QFileDialog_GetSaveFileName4(parent, "Export Workspaces", filepath.Join(qt.QDir_HomePath(), exportName), "Zip files (*.zip)")
	if archive == "" {
		return
	}

	if err := w.exportWorkspaces(public, archive); err != nil {
		qt.QMessageBox_Warning(parent, "Export Workspaces", fmt.Sprintf("The workspaces were not exported: %v.", err))
		return
	}
	qt.QMessageBox_Information(parent, "Export Workspaces", "The workspaces were exported to "+archive+
		", which can be read only with the private key that goes with "+public+".")
}

// exportWorkspaces writes every workspace to a zip file, encrypted for the
// public key in a file.
func (w *Window) exportWorkspaces(public, archive string) error {
	recipient, err := crypt.Recipient(public)
	if err != nil {
		return err
	}
	return workspace.Export(w.workspacesDir(), archive, w.keys, recipient)
}

// chooseImport asks for a file of exported workspaces and adds them.
func (w *Window) chooseImport(parent *qt.QWidget) {
	if w.keys == nil {
		adviseKeys(parent)
		return
	}
	archive := qt.QFileDialog_GetOpenFileName4(parent, "Import Workspaces", qt.QDir_HomePath(), "Zip files (*.zip);;All files (*)")
	if archive == "" {
		return
	}

	added, err := w.importWorkspaces(archive)
	if err != nil {
		qt.QMessageBox_Warning(parent, "Import Workspaces", fmt.Sprintf("The workspaces were not all imported: %v.", err))
		return
	}
	qt.QMessageBox_Information(parent, "Import Workspaces", "Imported "+strings.Join(added, ", ")+".")
}

// importWorkspaces adds the workspaces of a zip file made by exporting, which
// have to be encrypted for the keys of the settings, and returns the names
// they were given. The workspace in use stays the one in use.
func (w *Window) importWorkspaces(archive string) ([]string, error) {
	added, err := workspace.Unpack(w.workspacesDir(), archive, w.keys)
	if len(added) > 0 {
		if _, listErr := w.workspaces.load(w.config.Workspace); listErr != nil {
			err = errors.Join(err, listErr)
		}
	}
	return added, err
}

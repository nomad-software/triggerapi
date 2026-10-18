package ui

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/nomad-software/triggerapi/internal/collection"
	"github.com/nomad-software/triggerapi/internal/history"
)

// How the window keeps the requests it sends in the history of the
// workspace in use, and opens them from it again.

// historyEntry returns an entry of the history for the request on show in a
// request UI, which is being sent to url (with its placeholders replaced),
// or nil if the settings keep no history. What came of it is added once it
// is known.
func (w *Window) historyEntry(ui *requestUI, url string) *history.Entry {
	if w.config.NoHistory {
		return nil
	}
	name := ""
	var path []string
	if w.open != nil {
		name = w.open.Name
		path = w.requests.store.Path(w.open)
	}
	e := &history.Entry{Time: time.Now(), Request: ui.editor.compose(name), Path: path, URL: strings.TrimSpace(url)}
	if environment, ok := w.environments.store.Current(); ok {
		e.Environment = environment.Name
	}
	return e
}

// openEntry shows the request of an entry of the history, as a request that
// is not saved, in the tab that is not pinned, with the response it was
// given.
func (w *Window) openEntry(e *history.Entry) {
	w.stash(w.tabs.current())
	tab := w.tabs.open(nil)
	tab.draft, tab.replay = e.Request.Clone(), w.replayEntry(e)
	w.showTab(tab, true)
	w.closeEditor()
}

// replayEntry returns what shows the response of an entry of the history in
// the response panel of the type of its request, which is nil if it has
// none.
func (w *Window) replayEntry(e *history.Entry) func() {
	ui := w.requestUI(e.Request.Type)
	r := ui.response
	var show func()
	switch {
	case e.Error != "":
		err := errors.New(e.Error)
		show = func() { r.showError(err, e.Duration) }
	case e.HTTP != nil && ui.typ == collection.TypeGraphQL:
		show = func() { r.showGraphQL(e.HTTP) }
	case e.HTTP != nil:
		show = func() { r.show(e.HTTP) }
	case e.GRPC != nil:
		show = func() { r.showRPC(e.GRPC) }
	case e.MCP != nil:
		show = func() { r.showMCP(e.MCP) }
	case e.WebSocket != nil:
		show = func() { w.showWSRecord(e.WebSocket) }
	default:
		return nil
	}
	return func() {
		show()
		r.showAsserted(e.Assertions)
		// It is saved under the name and time it was sent with.
		r.entry = e
		if e.Omitted > 0 {
			r.noteOmitted(e.Omitted)
		}
	}
}

// diffEntries compares the responses of entries of the history with the
// diff tool of the settings, oldest first. The files it is given are kept
// until the window closes, as some tools return before they are read.
func (w *Window) diffEntries(entries []*history.Entry) error {
	command := strings.TrimSpace(w.config.DiffCommand)
	if command == "" {
		return errNoDiffCommand
	}
	if len(entries) < 2 {
		return errors.New("tick at least two entries to compare their responses")
	}
	if len(entries) > maxDiff {
		return fmt.Errorf("%d entries are ticked, and no more than %d responses can be compared at once, as diff tools compare up to %d files", len(entries), maxDiff, maxDiff)
	}
	dir, paths, err := writeDiffFiles(entries)
	if err != nil {
		return fmt.Errorf("the responses could not be written to compare: %w", err)
	}
	if err := startDiff(command, paths); err != nil {
		os.RemoveAll(dir)
		return err
	}
	w.diffDirs = append(w.diffDirs, dir)
	return nil
}

// removeDiffFiles deletes the files the responses were compared in.
func (w *Window) removeDiffFiles() {
	for _, dir := range w.diffDirs {
		os.RemoveAll(dir)
	}
	w.diffDirs = nil
}

// goToEntry opens the saved request an entry of the history was sent from,
// if it is still where it was, and shows it in the Requests tab.
func (w *Window) goToEntry(e *history.Entry) {
	if e.Path == nil {
		return
	}
	r := w.requests.store.Find(e.Path)
	if r == nil {
		return
	}
	w.sidebar.SetCurrentIndex(tabRequests)
	w.requests.showView(viewRequests)
	w.openRequest(r)
	w.requests.selectRequest(r)
}

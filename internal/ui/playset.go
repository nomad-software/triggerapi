package ui

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/nomad-software/triggerapi/internal/assert"
	"github.com/nomad-software/triggerapi/internal/collection"
	"github.com/nomad-software/triggerapi/internal/mcp"
	"github.com/nomad-software/triggerapi/internal/rest"
	"github.com/nomad-software/triggerapi/internal/rpc"
)

// Playing the pinned tabs: the request of each is sent in turn, from left to
// right, with its tab shown, until one does not go as it should.

// setPlay is the playing of the pinned tabs that is under way.
type setPlay struct {
	// tabs are those to play, in order, and next the place of the one to
	// be played next.
	tabs []*requestTab
	next int
	// waiting is set while a request that was in flight when the playing
	// began is being cancelled, which has to end before any is played.
	waiting bool
}

// playable reports whether the request of a tab can be played: one whose
// connection stays open, as a WebSocket's does, has no result to go on from.
func playable(tab *requestTab) bool {
	return tab.pinned && tab.request != nil && tab.request.Type != collection.TypeWebSocket
}

// playSet plays the pinned tabs, or stops them being played if they are.
func (w *Window) playSet() {
	if w.playing != nil {
		w.stopPlaying()
		return
	}
	tabs := slices.DeleteFunc(slices.Clone(w.tabs.tabs), func(tab *requestTab) bool { return !playable(tab) })
	if len(tabs) == 0 {
		return
	}
	w.closeEditor()
	w.playing = &setPlay{tabs: tabs}
	w.updatePlay()
	if w.cancel != nil {
		w.playing.waiting = true
		w.cancel()
		return
	}
	w.playNext()
}

// playNext shows the next tab to play and sends its request, or ends the
// playing if there is none left. A tab closed since the playing began is
// passed over.
func (w *Window) playNext() {
	p := w.playing
	for p.next < len(p.tabs) {
		tab := p.tabs[p.next]
		p.next++
		if !slices.Contains(w.tabs.tabs, tab) || !playable(tab) {
			continue
		}
		if from := w.tabs.current(); from != tab {
			w.stash(from)
			w.tabs.show(tab)
			w.showTab(tab, true)
		}
		w.requestUI(tab.request.Type).send()
		return
	}
	w.stopPlaying()
}

// played is told how the request played last went, as run finishes with it:
// the playing goes on to the next tab if the problem is "", and otherwise
// stops, with the tab of the request on show.
func (w *Window) played(problem string) {
	if w.playing == nil {
		return
	}
	if problem != "" {
		w.stopPlaying()
		return
	}
	w.playNext()
}

// stopPlaying ends the playing of the tabs, cancelling the request in
// flight, if there is one.
func (w *Window) stopPlaying() {
	if w.playing == nil {
		return
	}
	w.playing = nil
	if w.cancel != nil {
		w.cancel()
	}
	w.updatePlay()
}

// updatePlay shows on the play button whether the tabs are being played, and
// whether there are any to play.
func (w *Window) updatePlay() {
	w.environment.setPlaying(w.playing != nil, slices.ContainsFunc(w.tabs.tabs, playable))
}

// runProblem says why a request that was played did not go as it should, or
// is "" if it did: an error instead of a response (err, with the time limit
// of the settings in seconds), a response that is a failure, a value that
// was not captured or an assertion that failed.
func runProblem(err error, seconds int, failure string, captured []captureResult, asserted []assert.Result) string {
	switch {
	case errors.Is(err, context.Canceled):
		return "cancelled"
	case errors.Is(err, context.DeadlineExceeded):
		return timeoutError(seconds).Error()
	case err != nil:
		return err.Error()
	case failure != "":
		return failure
	}
	for _, c := range captured {
		if c.err != nil {
			return fmt.Sprintf("%s was not captured: %v", c.variable, c.err)
		}
	}
	if failed := assert.Failed(asserted); failed > 0 {
		return plural(failed, "assertion") + " failed"
	}
	return ""
}

// httpFailure says why an HTTP response is a failure, as its status is 400
// or more, or is "" if it is not.
// A stream of events broken off by an error is one too.
func httpFailure(res *rest.Response) string {
	switch {
	case res == nil:
		return ""
	case res.StatusCode >= 400:
		return "status " + res.Status
	case res.Interrupted != "" && res.Interrupted != rest.StreamStopped:
		return res.Interrupted
	}
	return ""
}

// rpcFailure says why a gRPC call is a failure, as it did not end with OK,
// or is "" if it is not.
func rpcFailure(res *rpc.Response) string {
	if res == nil || res.OK() {
		return ""
	}
	return fmt.Sprintf("status %d %s", res.Code, res.CodeName)
}

// mcpFailure says why an MCP call is a failure, or is "" if it is not.
func mcpFailure(res *mcp.Response) string {
	if res == nil {
		return ""
	}
	if status, _, failed := res.Outcome(); failed {
		return "status " + status
	}
	return ""
}

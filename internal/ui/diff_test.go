package ui

import (
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mappu/miqt/qt6/mainthread"

	"github.com/nomad-software/triggerapi/internal/collection"
	"github.com/nomad-software/triggerapi/internal/history"
	"github.com/nomad-software/triggerapi/internal/rest"
	"github.com/nomad-software/triggerapi/internal/rpc"
)

func TestWriteDiffFiles(t *testing.T) {
	at := time.Date(2026, 10, 17, 9, 30, 0, 0, time.Local)
	newer := &history.Entry{
		Time:    at.Add(time.Minute),
		Request: collection.NewREST("List rooms", rest.Request{}),
		HTTP:    &rest.Response{Headers: http.Header{"Content-Type": {"application/json"}}, Body: []byte(`{"rooms":[]}`)},
	}
	older := &history.Entry{Time: at, Request: collection.NewGRPC("", rpc.Request{}), GRPC: &rpc.Response{Messages: []string{"{}"}}}
	failed := &history.Entry{Time: at.Add(2 * time.Minute), Request: collection.NewREST("List rooms", rest.Request{}), Error: "refused"}

	dir, paths, err := writeDiffFiles([]*history.Entry{newer, failed, older})
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	// Oldest first, named as saved responses are, with the extension of
	// what they hold.
	var names []string
	for _, path := range paths {
		if filepath.Dir(path) != dir {
			t.Errorf("%s is not in %s", path, dir)
		}
		names = append(names, filepath.Base(path))
	}
	want := []string{"1_grpc_20261017_093000_resp.json", "2_list_rooms_20261017_093100_resp.json", "3_list_rooms_20261017_093200_resp.txt"}
	if !slices.Equal(names, want) {
		t.Errorf("names = %q, want %q", names, want)
	}
	for i, text := range []string{"{}", "{\n  \"rooms\": []\n}", "refused"} {
		if data, _ := os.ReadFile(paths[i]); string(data) != text {
			t.Errorf("%s holds %q, want %q", names[i], data, text)
		}
	}

	// Only the user can read them, as responses can hold secrets.
	if info, err := os.Stat(dir); err != nil || info.Mode().Perm() != 0o700 {
		t.Errorf("folder mode %v, %v", info.Mode(), err)
	}
	if info, err := os.Stat(paths[0]); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("file mode %v, %v", info.Mode(), err)
	}
}

func TestDiffHistory(t *testing.T) {
	dir := t.TempDir()
	// The diff tool is a script which writes down the files it is given.
	out := filepath.Join(dir, "compared.txt")
	script := filepath.Join(dir, "my diff.sh")
	if err := os.WriteFile(script, []byte("for f in \"$@\"; do printf '%s|' \"$(basename \"$f\")\" >> '"+out+"'; cat \"$f\" >> '"+out+"'; echo >> '"+out+"'; done\n"), 0o700); err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	var w *Window
	var warned []string
	mainthread.Wait(func() {
		w = NewWindow(testFiles(t).Files)
		w.Show()
		w.history.warn = func(message string) { warned = append(warned, message) }
		for i, body := range []string{"old", "new", "other"} {
			w.history.add(&history.Entry{
				Time:    now.Add(time.Duration(i) * time.Second),
				Request: collection.NewREST(body, rest.Request{}),
				HTTP:    &rest.Response{StatusCode: 200, Body: []byte(body)},
			})
		}
		today := w.history.tree.TopLevelItem(0)

		// Two entries have to be ticked to compare them.
		if w.history.diff.IsEnabled() {
			t.Error("comparing is possible with nothing ticked")
		}
		w.history.toggle(today.Child(2))
		if w.history.diff.IsEnabled() {
			t.Error("comparing is possible with one entry ticked")
		}
		w.history.toggle(today.Child(1))
		if !w.history.diff.IsEnabled() {
			t.Fatal("two entries cannot be compared")
		}

		// Without a diff tool in the settings the user is told to set one.
		w.history.diff.Click()
		if len(warned) != 1 || !strings.Contains(warned[0], "File > Settings") || len(w.diffDirs) != 0 {
			t.Errorf("warned %q", warned)
		}

		// One that cannot be started is reported, and leaves no files.
		w.config.DiffCommand = "triggerapi-no-such-diff-tool"
		w.history.diff.Click()
		if len(warned) != 2 || !strings.Contains(warned[1], "could not be started") || len(w.diffDirs) != 0 {
			t.Errorf("warned %q", warned)
		}

		w.config.DiffCommand = `sh "` + script + `"`
		w.history.diff.Click()
		if len(warned) != 2 || len(w.diffDirs) != 1 {
			t.Errorf("warned %q, %d folders", warned, len(w.diffDirs))
		}
	})

	// It is given the responses of the ticked entries, oldest first.
	waitFor(t, "the diff tool", func() bool {
		data, _ := os.ReadFile(out)
		return strings.Count(string(data), "\n") == 2
	})
	data, _ := os.ReadFile(out)
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 || !strings.HasPrefix(lines[0], "1_old_") || !strings.HasSuffix(lines[0], "|old") || !strings.HasPrefix(lines[1], "2_new_") || !strings.HasSuffix(lines[1], "|new") {
		t.Errorf("compared %q", lines)
	}

	// The files are deleted when the window closes.
	var folder string
	mainthread.Wait(func() {
		folder = w.diffDirs[0]
		w.Close()
	})
	if _, err := os.Stat(folder); err == nil {
		t.Error("the files compared were left")
	}
}

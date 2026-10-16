package ui

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/mappu/miqt/qt6/mainthread"

	"github.com/nomad-software/triggerapi/internal/rest"
	"github.com/nomad-software/triggerapi/internal/rpc"
	"github.com/nomad-software/triggerapi/internal/settings"
)

func TestResponseFont(t *testing.T) {
	files := testFiles(t)
	settings.Settings{Width: 800, Height: 600, ResponseFont: "Some Font", PublicKey: testPublic, PrivateKey: testPrivate}.Save(files.Settings)

	mainthread.Wait(func() {
		w := NewWindow(files.Files)
		w.Show()

		families := w.response.body.Font().Families()
		if len(families) < 2 || families[0] != "Some Font" {
			t.Errorf("families = %v", families)
		}

		// The setting survives the settings being saved on close.
		w.Close()
	})

	if got := settings.Load(files.Settings).ResponseFont; got != "Some Font" {
		t.Errorf("saved font = %q", got)
	}
}

func TestSaveResponse(t *testing.T) {
	dir := t.TempDir()

	mainthread.Wait(func() {
		w := NewWindow(testFiles(t).Files)
		w.Show()

		for _, p := range []*responsePanel{w.response, w.grpcResponse} {
			var offered []string
			var warned []string
			target := filepath.Join(dir, "saved")
			p.chooseSave = func(name string) (string, bool) { offered = append(offered, name); return target, true }
			p.warn = func(message string) { warned = append(warned, message) }
			p.now = func() time.Time { return time.Date(2026, time.October, 11, 17, 30, 5, 0, time.Local) }

			// There is nothing to save until there is a response.
			if p.save.IsEnabled() || !p.root.IsAncestorOf(p.save.QWidget) {
				t.Fatal("the save button is enabled without a response, or is not in the panel")
			}

			// JSON is saved formatted, as it is shown.
			raw := []byte(`{"a":1}`)
			p.show(&rest.Response{Status: "200 OK", StatusCode: 200, Headers: http.Header{"Content-Type": {"application/json"}}, Body: raw})
			if !p.save.IsEnabled() {
				t.Fatal("the save button is not enabled")
			}
			p.save.Click()
			if got, err := os.ReadFile(target); err != nil || string(got) != "{\n  \"a\": 1\n}" || string(got) != p.body.ToPlainText() || !reflect.DeepEqual(offered, []string{"untitled_20261011_173005_resp.json"}) {
				t.Errorf("saved %q, %v, offered %q", got, err, offered)
			}

			// Anything else is saved as it was received, as is JSON that
			// is not valid.
			for contentType, body := range map[string]string{"application/octet-stream": "\x00\xff {\"a\":1}", "application/json": `{"a":`} {
				p.show(&rest.Response{Status: "200 OK", StatusCode: 200, Headers: http.Header{"Content-Type": {contentType}}, Body: []byte(body)})
				p.save.Click()
				if got, _ := os.ReadFile(target); string(got) != body {
					t.Errorf("saved %q, want %q", got, body)
				}
			}

			// The messages of a gRPC call are saved as they are shown.
			p.showRPC(&rpc.Response{Messages: []string{"{\n  \"a\": 1\n}", "{\n  \"b\": 2\n}"}})
			p.save.Click()
			if got, _ := os.ReadFile(target); string(got) != p.body.ToPlainText() || !strings.Contains(string(got), `"b": 2`) {
				t.Errorf("saved %q", got)
			}

			// Nothing is written if no file is chosen.
			os.Remove(target)
			p.chooseSave = func(string) (string, bool) { return "", false }
			p.save.Click()
			if _, err := os.Stat(target); err == nil {
				t.Error("saved without a file being chosen")
			}

			// A file that cannot be written is reported.
			p.chooseSave = func(string) (string, bool) { return filepath.Join(dir, "missing", "file"), true }
			p.save.Click()
			if len(warned) != 1 || !strings.Contains(warned[0], "could not be saved") {
				t.Errorf("warned %q", warned)
			}

			// A response without a body, an error and a new request leave
			// nothing to save.
			p.show(&rest.Response{Status: "204 No Content", StatusCode: 204, Headers: http.Header{}})
			if p.save.IsEnabled() {
				t.Error("an empty response can be saved")
			}
			p.show(&rest.Response{Status: "200 OK", StatusCode: 200, Headers: http.Header{}, Body: raw})
			p.showError(io.EOF, time.Second)
			if p.save.IsEnabled() {
				t.Error("an error can be saved")
			}
		}

		// The file is named after the request that is open, and the time.
		w.requests.addFolder()
		w.requests.addRequest()
		w.open.Name = "Book Room (v2)"
		for _, p := range []*responsePanel{w.response, w.grpcResponse} {
			var offered string
			p.chooseSave = func(name string) (string, bool) { offered = name; return "", false }
			p.show(&rest.Response{Status: "200 OK", StatusCode: 200, Headers: http.Header{"Content-Type": {"text/xml"}}, Body: []byte("<a/>")})
			p.save.Click()
			if want := "book_room_v2_20261011_173005_resp.xml"; offered != want {
				t.Errorf("offered %q, want %q", offered, want)
			}
		}

		at := time.Date(2026, time.January, 2, 3, 4, 5, 0, time.Local)
		for request, want := range map[string]string{
			"":                         "untitled_20260102_030405_resp.json",
			"  ":                       "untitled_20260102_030405_resp.json",
			"../..":                    "untitled_20260102_030405_resp.json",
			"Ping":                     "ping_20260102_030405_resp.json",
			"  List   Rooms  ":         "list_rooms_20260102_030405_resp.json",
			"GET /users/{id}?a=b&c":    "get_users_id_a_b_c_20260102_030405_resp.json",
			"a.b-c_d:e*f\\g\"h'i|j<k>": "a_b_c_d_e_f_g_h_i_j_k_20260102_030405_resp.json",
			"Café Zürich №5":           "café_zürich_5_20260102_030405_resp.json",
			strings.Repeat("Ab ", 60):  strings.TrimSuffix(strings.Repeat("ab_", 40), "_") + "_20260102_030405_resp.json",
		} {
			if got := saveFileName(request, at, ".json"); got != want {
				t.Errorf("saveFileName(%q) = %q, want %q", request, got, want)
			}
		}

		for contentType, want := range map[string]string{
			"application/json; charset=utf-8": "response.json",
			"text/xml":                        "response.xml",
			"text/html":                       "response.html",
			"text/plain":                      "response.txt",
			"":                                "response.bin",
		} {
			if got := responseFileName(contentType); got != want {
				t.Errorf("file name for %q = %q, want %q", contentType, got, want)
			}
		}
	})
}

func TestFormatSize(t *testing.T) {
	for size, want := range map[int]string{
		0:               "0 B",
		1023:            "1023 B",
		1024:            "1.0 KB",
		1536:            "1.5 KB",
		1024*1024 - 1:   "1024.0 KB",
		1024 * 1024:     "1.0 MB",
		5 * 1024 * 1024: "5.0 MB",
	} {
		if got := formatSize(size); got != want {
			t.Errorf("formatSize(%d) = %q, want %q", size, got, want)
		}
	}
}

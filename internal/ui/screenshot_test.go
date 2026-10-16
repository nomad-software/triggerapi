package ui

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mappu/miqt/qt6/mainthread"

	"github.com/nomad-software/triggerapi/internal/capture"
	"github.com/nomad-software/triggerapi/internal/collection"
	"github.com/nomad-software/triggerapi/internal/env"
	"github.com/nomad-software/triggerapi/internal/graphql"
	"github.com/nomad-software/triggerapi/internal/mcp"
	"github.com/nomad-software/triggerapi/internal/rest"
	"github.com/nomad-software/triggerapi/internal/rpc"
	"github.com/nomad-software/triggerapi/internal/settings"
	"github.com/nomad-software/triggerapi/internal/ws"
)

// screenshotEnv names the file TestScreenshot draws the window into. The
// test is skipped without it; make screenshot sets it to docs/screenshot.png.
const screenshotEnv = "TRIGGERAPI_SCREENSHOT"

// bookingBody is the body of the request the screenshot shows.
const bookingBody = `{
  "guest": "Ada Lovelace",
  "nights": 3,
  "breakfast": true,
  "ref": "{{bookingRef}}"
}`

// bookingResponse is what the server of the screenshot answers it with.
const bookingResponse = `{"id": 4821, "ref": "BK-4821", "room": {"number": "204", "type": "double", "floor": 2},` +
	` "guest": {"name": "Ada Lovelace", "email": "ada@example.com"}, "nights": 3, "breakfast": true,` +
	` "total": {"amount": 447.00, "currency": "GBP"}}`

// TestScreenshot draws the window, with sample requests and a response to
// one of them, into the PNG file screenshotEnv names, for the README.
func TestScreenshot(t *testing.T) {
	path := os.Getenv(screenshotEnv)
	if path == "" {
		t.Skip(screenshotEnv + " is not set")
	}
	path, err := filepath.Abs(path)
	if err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		// A response takes a moment, as it would from a real server.
		time.Sleep(25 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		io.WriteString(w, bookingResponse)
	}))
	defer srv.Close()

	files := testFiles(t)
	config := settings.Load(files.Settings)
	config.Width, config.Height, config.SidebarWidth = 1280, 800, 340
	if err := config.Save(files.Settings); err != nil {
		t.Fatal(err)
	}

	envs := &env.Store{Active: 0, Environments: []env.Environment{
		{Name: "Local", Variables: []env.Variable{{Key: "host", Value: srv.URL, Enabled: true}}},
		{Name: "Staging", Variables: []env.Variable{{Key: "host", Value: "https://staging.example.com", Enabled: true}}},
	}}
	if err := envs.SaveWith(files.Environments, testKeys); err != nil {
		t.Fatal(err)
	}

	store := &collection.Store{}
	notifications := store.AddFolder("Notifications")
	notifications.Add(collection.NewGRPC("Subscribe", rpc.Request{URL: "{{grpcHost}}", Method: "notify.v1.Notifications/Subscribe"}))
	health := store.AddFolder("Health")
	health.Add(collection.NewREST("Ping", rest.Request{Method: http.MethodGet, URL: "{{host}}/ping"}))
	store.AddFolder("Payments").Add(collection.NewREST("Refund", rest.Request{Method: http.MethodPost, URL: "{{host}}/refunds"}))
	store.AddFolder("Live").Add(collection.NewWebSocket("Room feed", ws.Request{URL: "{{host}}/feed"}))
	store.AddFolder("Catalogue").Add(collection.NewGraphQL("Search rooms", graphql.Request{URL: "{{host}}/graphql"}))
	store.AddFolder("Assistant").Add(collection.NewMCP("Find booking", mcp.Request{URL: "{{host}}/mcp", Kind: mcp.KindTool, Name: "find_booking"}))
	hotel := store.AddFolder("Hotel")
	guests := hotel.AddFolder("Guests")
	guests.Add(collection.NewREST("Find guest", rest.Request{Method: http.MethodGet, URL: "{{host}}/guests"}))
	rooms := hotel.AddFolder("Rooms")
	book := rooms.Add(collection.NewREST("Book room", rest.Request{
		Method:   http.MethodPost,
		URL:      "{{host}}/rooms/204/bookings",
		BodyType: rest.BodyJSON,
		Body:     bookingBody,
		Captures: []capture.Rule{{Variable: "bookingRef", Path: "ref", Enabled: true}},
	}))
	list := rooms.Add(collection.NewREST("List rooms", rest.Request{Method: http.MethodGet, URL: "{{host}}/rooms"}))
	availability := hotel.Add(collection.NewGRPC("Availability", rpc.Request{URL: "{{grpcHost}}", Method: "hotel.v1.Rooms/Availability"}))
	store.AddSet("Booking", []*collection.Request{availability, list, book})
	if err := store.SaveWith(files.Requests, testKeys); err != nil {
		t.Fatal(err)
	}

	var w *Window
	mainthread.Wait(func() {
		w = NewWindow(files.Files)
		w.Show()
		w.requests.sets.chooseAt(0)
		for _, f := range w.requests.store.Folders {
			f.Open = true
		}
		w.openRequest(w.requests.store.Find([]string{"Hotel", "Rooms", "Book room"}))
		w.send()
	})
	waitFor(t, "the response", func() bool { return fieldValue(w.response, fieldCode) == "201 Created" })

	saved := mainthread.Wait2(func() bool {
		w.requests.draw(w.open)
		return w.Grab().Save(path)
	})
	if !saved {
		t.Fatalf("the screenshot could not be saved to %s", path)
	}
}

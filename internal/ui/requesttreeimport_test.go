package ui

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	qt "github.com/mappu/miqt/qt6"
	"github.com/mappu/miqt/qt6/mainthread"

	"github.com/nomad-software/triggerapi/internal/rest"
)

func TestImportOpenAPI(t *testing.T) {
	files := testFiles(t)
	dir := filepath.Dir(files.Requests)

	spec := filepath.Join(dir, "shop.yaml")
	os.WriteFile(spec, []byte(`
openapi: 3.0.3
info:
  title: Shop
servers:
  - url: https://shop.example.com
paths:
  /orders:
    get:
      tags: [orders]
      summary: List orders
      parameters:
        - name: page
          in: query
          required: true
          example: 2
    post:
      tags: [orders]
      summary: Create order
      requestBody:
        content:
          application/json:
            example: {item: tea, quantity: 2}
  /health:
    get:
      summary: Health
`), 0o644)
	bad := filepath.Join(dir, "bad.yaml")
	os.WriteFile(bad, []byte("just: text"), 0o644)

	mainthread.Wait(func() {
		w := NewWindow(files.Files)
		w.Show()
		rt := w.requests
		tree := rt.tree

		var warned []string
		rt.warn = func(message string) { warned = append(warned, message) }

		// There is something already, which is left closed.
		rt.addRequest()
		topFolder(tree, 0).SetExpanded(false)

		// Deciding against importing, or against choosing a file, does
		// nothing.
		rt.chooseSource = func() (importSource, bool) { return "", false }
		rt.chooseFile = func(importSource) (string, bool) { t.Error("asked for a file"); return "", false }
		rt.importDocument()

		var asked []importSource
		rt.chooseSource = func() (importSource, bool) { return importOpenAPI, true }
		rt.chooseFile = func(source importSource) (string, bool) { asked = append(asked, source); return "", false }
		rt.importDocument()
		if !reflect.DeepEqual(asked, []importSource{importOpenAPI}) {
			t.Errorf("asked for a file of %q", asked)
		}
		if topCount(tree) != 1 || len(warned) != 0 {
			t.Fatalf("imported without a file: %d folders, warned %q", topCount(tree), warned)
		}

		rt.chooseFile = func(importSource) (string, bool) { return spec, true }
		rt.importDocument()
		if len(warned) != 0 || topCount(tree) != 2 {
			t.Fatalf("not imported: %d folders, warned %q", topCount(tree), warned)
		}

		// The folder is added in its place in the order, open and selected.
		shop := topFolder(tree, 1)
		if shop.Text(colName) != "Shop" || !shop.IsExpanded() || topFolder(tree, 0).IsExpanded() || tree.CurrentItem().Text(colName) != "Shop" {
			t.Errorf("folder = %q, open %t", shop.Text(colName), shop.IsExpanded())
		}
		if got, want := treeNames(shop), []string{"orders/", "Health"}; !reflect.DeepEqual(got, want) {
			t.Errorf("shop = %q, want %q", got, want)
		}
		// The folders inside it start off closed.
		if shop.Child(0).IsExpanded() {
			t.Error("imported subfolder is open")
		}
		if got, want := treeNames(shop.Child(0)), []string{"Create order", "List orders"}; !reflect.DeepEqual(got, want) {
			t.Errorf("orders = %q, want %q", got, want)
		}

		// An imported request opens like any other.
		tree.SetCurrentItem(treeItem(tree, "Shop", "orders", "List orders"))
		if w.open == nil || w.request.url.Text() != "https://shop.example.com/orders?page=2" || w.request.method.CurrentText() != "GET" {
			t.Errorf("open = %+v, url %q", w.open, w.request.url.Text())
		}
		tree.SetCurrentItem(treeItem(tree, "Shop", "orders", "Create order"))
		if w.request.method.CurrentText() != "POST" || !strings.Contains(w.request.bodyRaw.ToPlainText(), `"item": "tea"`) || !w.request.bodyIsJSON() {
			t.Errorf("create: %q %q", w.request.method.CurrentText(), w.request.bodyRaw.ToPlainText())
		}

		// A file that cannot be imported is reported, and adds nothing.
		for _, path := range []string{bad, filepath.Join(dir, "missing.yaml")} {
			rt.chooseFile = func(importSource) (string, bool) { return path, true }
			rt.importDocument()
		}
		if len(warned) != 2 || !strings.Contains(warned[0], "bad.yaml could not be imported") || !strings.Contains(warned[0], "not an OpenAPI document") || topCount(tree) != 2 {
			t.Errorf("warned %q, %d folders", warned, topCount(tree))
		}
	})

	saved, err := loadRequests(files.Requests)
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.Folders) != 2 || saved.Folders[1].Name != "Shop" || saved.Folders[1].Folders[0].Requests[0].Name != "Create order" {
		t.Errorf("saved %+v", saved.Folders)
	}
}

func TestImportPostman(t *testing.T) {
	files := testFiles(t)
	dir := filepath.Dir(files.Requests)

	exported := filepath.Join(dir, "hotel.postman_collection.json")
	os.WriteFile(exported, []byte(`{
		"info": {"name": "Hotel", "schema": "https://schema.getpostman.com/json/collection/v2.1.0/collection.json"},
		"item": [
			{"name": "Rooms", "item": [
				{"name": "Deep", "item": [
					{"name": "Book room", "request": {
						"method": "POST",
						"auth": {"type": "bearer", "bearer": [{"key": "token", "value": "{{Token}}"}]},
						"header": [{"key": "x-app-key", "value": "{{AppKey}}"}],
						"body": {"mode": "raw", "raw": "{\"room\": 1}", "options": {"raw": {"language": "json"}}},
						"url": {"raw": "{{HostName}}/rooms?dry=1", "query": [{"key": "dry", "value": "1"}, {"key": "debug", "value": "x", "disabled": true}]}
					}}
				]}
			]},
			{"name": "Ping", "request": {"method": "GET", "url": "{{HostName}}/ping"}}
		]
	}`), 0o644)
	spec := filepath.Join(dir, "spec.json")
	os.WriteFile(spec, []byte(`{"openapi": "3.0.0", "paths": {"/a": {"get": {}}}}`), 0o644)

	mainthread.Wait(func() {
		w := NewWindow(files.Files)
		w.Show()
		rt := w.requests
		tree := rt.tree

		var warned []string
		rt.warn = func(message string) { warned = append(warned, message) }

		// The file is read as the kind that was chosen.
		rt.chooseSource = func() (importSource, bool) { return importPostman, true }
		rt.chooseFile = func(source importSource) (string, bool) {
			if source != importPostman {
				t.Errorf("asked for a file of %q", source)
			}
			return exported, true
		}
		rt.importDocument()
		if len(warned) != 0 || topCount(tree) != 1 {
			t.Fatalf("not imported: %d folders, warned %q", topCount(tree), warned)
		}

		hotel := treeItem(tree, "Hotel")
		if got, want := treeNames(hotel), []string{"Rooms/", "Ping"}; !reflect.DeepEqual(got, want) || !hotel.IsExpanded() || hotel.Child(0).IsExpanded() {
			t.Errorf("hotel = %q, want %q", got, want)
		}

		// A request deep inside it opens with everything it was given.
		tree.SetCurrentItem(treeItem(tree, "Hotel", "Rooms", "Deep", "Book room"))
		got := w.request.request()
		want := rest.Request{
			Method:   "POST",
			URL:      "{{HostName}}/rooms",
			Params:   []rest.KeyValue{{Key: "debug", Value: "x"}, {Key: "dry", Value: "1", Enabled: true}},
			Headers:  []rest.KeyValue{{Key: "x-app-key", Value: "{{AppKey}}", Enabled: true}},
			Auth:     rest.Auth{Type: rest.AuthBearer, Token: "{{Token}}"},
			BodyType: rest.BodyJSON,
			Body:     `{"room": 1}`,
		}
		if !reflect.DeepEqual(got, want) || w.request.url.Text() != "{{HostName}}/rooms?dry=1" {
			t.Errorf("request = %+v\nwant %+v", got, want)
		}

		// A file of the other kind is refused, and adds nothing.
		rt.chooseFile = func(importSource) (string, bool) { return spec, true }
		rt.importDocument()
		if len(warned) != 1 || !strings.Contains(warned[0], "not a Postman collection") || topCount(tree) != 1 {
			t.Errorf("warned %q", warned)
		}
	})
}

func TestImportDialog(t *testing.T) {
	mainthread.Wait(func() {
		w := NewWindow(testFiles(t).Files)

		for source, label := range map[importSource]string{importOpenAPI: "OpenAPI", importPostman: "Postman"} {
			dialog := newImportDialog(w.QWidget)
			dialog.Show()

			button := dialog.buttons[source]
			if button == nil || button.Text() != label {
				t.Fatalf("no button labelled %q", label)
			}

			button.Click()
			if dialog.chosen != source || dialog.Result() != int(qt.QDialog__Accepted) || dialog.IsVisible() {
				t.Errorf("clicking %s chose %q with result %d", label, dialog.chosen, dialog.Result())
			}
		}
	})
}

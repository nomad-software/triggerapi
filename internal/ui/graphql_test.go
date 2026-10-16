package ui

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/mappu/miqt/qt6/mainthread"

	"github.com/nomad-software/triggerapi/internal/capture"
	"github.com/nomad-software/triggerapi/internal/collection"
	"github.com/nomad-software/triggerapi/internal/env"
	"github.com/nomad-software/triggerapi/internal/graphql"
	"github.com/nomad-software/triggerapi/internal/rest"
	"github.com/nomad-software/triggerapi/internal/ws"
)

// graphqlServer answers GraphQL requests: the introspection query with a
// schema of one type, a query of "fail" with an error, and any other with
// the user named in its id variable, saying what its X-Test header was.
func graphqlServer(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var asked struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		data, _ := io.ReadAll(r.Body)
		json.Unmarshal(data, &asked)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(asked.Query, "__schema"):
			io.WriteString(w, `{"data": {"__schema": {"queryType": {"name": "Query"}, "types": [
				{"kind": "OBJECT", "name": "Query", "fields": [{"name": "user", "args": [], "type": {"kind": "SCALAR", "name": "String"}}]}
			]}}}`)
		case strings.Contains(asked.Query, "fail"):
			io.WriteString(w, `{"data": null, "errors": [{"message": "no such field"}]}`)
		default:
			answer, _ := json.Marshal(map[string]any{"data": map[string]any{"user": map[string]any{
				"id": asked.Variables["id"], "query": asked.Query, "header": r.Header.Get("X-Test"),
			}}})
			w.Write(answer)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestGraphQLRequest(t *testing.T) {
	srv := graphqlServer(t)
	files := testFiles(t)
	envs := &env.Store{Active: 0, Environments: []env.Environment{{Name: "Local", Variables: []env.Variable{
		{Key: "host", Value: srv.URL, Enabled: true},
		{Key: "who", Value: "world", Enabled: true},
		{Key: "id", Value: "7", Enabled: true},
	}}}}
	if err := envs.Save(files.Environments); err != nil {
		t.Fatal(err)
	}

	var w *Window
	field := func(label string) string { return fieldValue(w.graphqlResponse, label) }
	body := func() string { return mainthread.Wait2(func() string { return w.graphqlResponse.body.ToPlainText() }) }

	mainthread.Wait(func() {
		w = NewWindow(files.Files)
		w.Show()

		// A GraphQL request is one of the types to add.
		defer func(prev collection.Type) { testRequestType = prev }(testRequestType)
		testRequestType = collection.TypeGraphQL
		w.requests.addRequest()
		if w.open == nil || w.open.Type != collection.TypeGraphQL || w.stack.CurrentIndex() != pageGraphQL {
			t.Fatalf("open %+v on page %d", w.open, w.stack.CurrentIndex())
		}

		w.graphql.url.SetText("{{host}}/graphql")
		w.graphql.headers.add("X-Test", "{{who}}")
		w.graphql.query.SetPlainText("query($id: Int) { user(id: $id) { id } }")
		w.graphql.variables.SetPlainText(`{"id": {{id}}}`)
		setCaptureRules(w.graphql.captures, []capture.Rule{{Variable: "user", Path: "data.user.id", Enabled: true}})
		w.graphql.send.Click()
	})

	// It is sent with the environment applied, and what it captures is
	// kept in the environment.
	waitFor(t, "the response", func() bool { return strings.Contains(body(), `"header": "world"`) })
	mainthread.Wait(func() {
		if field(fieldCode) != "200 OK" || field(fieldError) != "" || !strings.Contains(body(), `"id": 7`) {
			t.Errorf("status %q, error %q, body %s", field(fieldCode), field(fieldError), body())
		}
		if value := w.expand("{{user}}"); value != "7" {
			t.Errorf("captured %q", value)
		}

		// The errors of a response are shown as its error.
		w.graphql.query.SetPlainText("{ fail }")
		w.graphql.send.Click()
	})
	waitFor(t, "the errors", func() bool { return mainthread.Wait2(func() bool { return field(fieldError) != "" }) })
	mainthread.Wait(func() {
		if field(fieldError) != "no such field" || field(fieldCode) != "200 OK" {
			t.Errorf("status %q, error %q", field(fieldCode), field(fieldError))
		}

		// Variables that are not JSON are an error, and nothing is sent.
		w.graphql.variables.SetPlainText("{nope")
		w.graphql.send.Click()
	})
	waitFor(t, "the variables error", func() bool {
		return mainthread.Wait2(func() bool { return strings.Contains(field(fieldError), "JSON object") })
	})

	// The schema is asked for when its tab is shown.
	mainthread.Wait(func() {
		if w.graphql.schema.ToPlainText() != "" {
			t.Error("the schema was asked for before its tab was shown")
		}
		w.graphql.tabs.SetCurrentIndex(tabGraphQLSchema)
	})
	waitFor(t, "the schema", func() bool {
		return mainthread.Wait2(func() bool { return strings.Contains(w.graphql.schema.ToPlainText(), "type Query {") })
	})
	mainthread.Wait(func() {
		// A server that cannot be asked is reported.
		w.graphql.url.SetText("http://127.0.0.1:1/graphql")
		w.graphql.reload.Click()
	})
	waitFor(t, "the schema failure", func() bool {
		return mainthread.Wait2(func() bool { return strings.Contains(w.graphql.status.Text(), "could not be loaded") })
	})
	mainthread.Wait(func() { w.Close() })

	saved, err := loadRequests(files.Requests)
	if err != nil {
		t.Fatal(err)
	}
	r := saved.Folders[0].Requests[0]
	want := graphql.Request{
		URL:       "http://127.0.0.1:1/graphql",
		Headers:   []rest.KeyValue{{Key: "X-Test", Value: "{{who}}", Enabled: true}},
		Auth:      rest.Auth{Type: rest.AuthNone},
		Query:     "{ fail }",
		Variables: "{nope",
		Captures:  []capture.Rule{{Variable: "user", Path: "data.user.id", Enabled: true}},
	}
	if r.Type != collection.TypeGraphQL || r.GraphQL == nil || !reflect.DeepEqual(*r.GraphQL, want) {
		t.Errorf("saved %+v %+v", r, r.GraphQL)
	}
}

func TestGraphQLGroupAndDraft(t *testing.T) {
	files := testFiles(t)
	store := &collection.Store{}
	store.AddFolder("Pages").Add(collection.NewREST("Get", rest.Request{Method: "GET"}))
	store.AddFolder("Sockets").Add(collection.NewWebSocket("Chat", ws.Request{}))
	store.AddFolder("Graphs").Add(collection.NewGraphQL("Me", graphql.Request{URL: "api.test/graphql", Query: "{ me }"}))
	if err := store.Save(files.Requests); err != nil {
		t.Fatal(err)
	}

	mainthread.Wait(func() {
		w := NewWindow(files.Files)
		w.Show()
		tree := w.requests.tree

		// GraphQL requests have a heading of their own, first.
		var headings []string
		for i := range tree.TopLevelItemCount() {
			headings = append(headings, tree.TopLevelItem(i).Text(colName))
		}
		if want := []string{"GraphQL", "REST", "WebSocket"}; !reflect.DeepEqual(headings, want) {
			t.Errorf("headings %q, want %q", headings, want)
		}
		me := treeItem(tree, "Graphs", "Me")
		if me == nil || me.Icon(colName).CacheKey() != requestTypeIcon(collection.TypeGraphQL).CacheKey() {
			t.Fatal("the request has not the icon of its type")
		}

		// Opening one shows it in its own UI.
		tree.SetCurrentItem(me)
		if w.stack.CurrentIndex() != pageGraphQL || w.graphql.url.Text() != "api.test/graphql" || w.graphql.query.ToPlainText() != "{ me }" || !w.graphql.save.IsHidden() {
			t.Errorf("page %d, url %q", w.stack.CurrentIndex(), w.graphql.url.Text())
		}

		// One made from the New tab is not saved until Save is pressed.
		defer func(prev collection.Type) { testRequestType = prev }(testRequestType)
		testRequestType = collection.TypeGraphQL
		w.newRequest()
		if w.open != nil || w.stack.CurrentIndex() != pageGraphQL || w.graphql.save.IsHidden() || w.graphql.url.Text() != "" {
			t.Fatalf("draft: open %+v, page %d", w.open, w.stack.CurrentIndex())
		}
		w.graphql.url.SetText("new.test/graphql")
		w.graphql.save.Click()
		if w.open == nil || w.open.Type != collection.TypeGraphQL || w.open.GraphQL.URL != "new.test/graphql" {
			t.Errorf("saved draft %+v", w.open)
		}
	})
}

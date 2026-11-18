package ui

import (
	"github.com/nomad-software/qute"

	"maps"
	"testing"

	"github.com/nomad-software/triggerapi/internal/collection"
	"github.com/nomad-software/triggerapi/internal/env"
	"github.com/nomad-software/triggerapi/internal/rest"
)

func TestTabEnvironments(t *testing.T) {
	files := testFiles(t)
	envs := env.Store{Environments: []env.Environment{{Name: "Dev"}, {Name: "Live"}, {Name: "Stage"}}}
	if err := envs.Save(files.Environments); err != nil {
		t.Fatal(err)
	}
	store := &collection.Store{}
	folder := store.AddFolder("One")
	a := folder.Add(collection.NewREST("A", rest.Request{Method: "GET", URL: "a.test"}))
	b := folder.Add(collection.NewREST("B", rest.Request{Method: "GET", URL: "b.test"}))
	folder.Add(collection.NewREST("C", rest.Request{Method: "GET", URL: "c.test"}))
	// A has no environment, as in a set from before they were kept.
	store.AddSet("Work", []*collection.Request{a, b}).Environments = map[*collection.Request]string{b: "Stage"}
	if err := store.Save(files.Requests); err != nil {
		t.Fatal(err)
	}

	var w *Window
	active := func(what, want string) {
		t.Helper()
		if got := w.environments.activeName(); got != want {
			t.Errorf("%s: environment %q, want %q", what, got, want)
		}
	}
	showTab := func(i int) { w.tabs.bar.SetCurrentIndex(i) }
	qute.PostSync(func() {
		w = NewWindow(files.Files)
		w.Show()
		w.environments.list.SetCurrentRow(0)
		sets := w.requests.sets
		sets.list.SetCurrentItem(sets.list.Item(0))

		// A tab of a set with no environment takes the one in use.
		active("A shown", "Dev")
		if got := w.tabs.tabs[0].environment; got != "Dev" {
			t.Errorf("A remembers %q", got)
		}
		showTab(1)
		active("B shown", "Stage")
		showTab(0)
		active("A again", "Dev")

		// Choosing another while a pinned tab is shown is remembered,
		// and kept in the set.
		w.environments.list.SetCurrentRow(1)
		showTab(1)
		active("B after A changed", "Stage")
		showTab(0)
		active("A after it changed", "Live")
		if got, want := setEnvironments(sets.active), map[string]string{"A": "Live", "B": "Stage"}; !maps.Equal(got, want) {
			t.Errorf("set remembers %v", got)
		}

		// Renaming an environment renames it in the tabs and the sets.
		w.environments.update("Prod", nil)
		if got := w.tabs.tabs[0].environment; got != "Prod" || setEnvironments(sets.active)["A"] != "Prod" {
			t.Errorf("after renaming: tab %q, set %v", got, setEnvironments(sets.active))
		}

		// Pinned tabs that are not in a set remember theirs too.
		sets.list.ClearSelection()
		if sets.active != nil {
			t.Error("a set is still chosen")
			return
		}
		w.environments.list.SetCurrentRow(2)
		w.requests.tree.SetCurrentItem(treeItem(w.requests.tree, "One", "C"))
		w.tabs.current().button.Click()
		w.requests.tree.SetCurrentItem(treeItem(w.requests.tree, "One", "A"))
		w.tabs.current().button.Click()
		w.environments.list.SetCurrentRow(0)
		showTab(0)
		active("C shown", "Stage")
		showTab(1)
		active("A shown out of a set", "Dev")

		// The tab that is not pinned changes nothing.
		w.requests.tree.SetCurrentItem(treeItem(w.requests.tree, "One", "B"))
		active("B unpinned", "Dev")
	})

	saved, err := loadRequests(files.Requests)
	if err != nil || len(saved.Sets) != 1 {
		t.Fatalf("saved %+v, %v", saved, err)
	}
	if got, want := setEnvironments(saved.Sets[0]), map[string]string{"A": "Prod", "B": "Stage"}; !maps.Equal(got, want) {
		t.Errorf("saved set remembers %v", got)
	}
}

// setEnvironments returns the environments of the requests of a set by the
// names of the requests.
func setEnvironments(set *collection.Set) map[string]string {
	named := map[string]string{}
	for _, r := range set.Requests {
		named[r.Name] = set.Environment(r)
	}
	return named
}

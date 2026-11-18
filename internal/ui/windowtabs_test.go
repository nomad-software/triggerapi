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
		// Renaming an environment renames it in the tabs and the sets,
		// once the editing of its name is finished.
		w.environments.update("Pro", nil)
		w.environments.update("Prod", nil)
		w.environments.renamed()
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

// setTabsFixture saves environments of the names given and a set, Work, of
// requests A and B, which remember the environments given, and returns the
// window's files.
func setTabsFixture(t *testing.T, names []string, remembered map[string]string) testPaths {
	t.Helper()
	files := testFiles(t)
	envs := env.Store{}
	for _, name := range names {
		envs.Environments = append(envs.Environments, env.Environment{Name: name})
	}
	if err := envs.Save(files.Environments); err != nil {
		t.Fatal(err)
	}
	store := &collection.Store{}
	folder := store.AddFolder("One")
	set := store.AddSet("Work", nil)
	set.Environments = map[*collection.Request]string{}
	for _, name := range []string{"A", "B"} {
		r := folder.Add(collection.NewREST(name, rest.Request{Method: "GET", URL: name + ".test"}))
		set.Requests = append(set.Requests, r)
		if e := remembered[name]; e != "" {
			set.Environments[r] = e
		}
	}
	if err := store.Save(files.Requests); err != nil {
		t.Fatal(err)
	}
	return files
}

// A rename typed a key at a time, through the name of another environment,
// is followed from the name it had to the name it ends with.
func TestRenameEnvironmentByKeys(t *testing.T) {
	files := setTabsFixture(t, []string{"Dev", "Dev2"}, map[string]string{"A": "Dev2", "B": "Dev"})
	qute.PostSync(func() {
		w := NewWindow(files.Files)
		w.requests.sets.list.SetCurrentItem(w.requests.sets.list.Item(0))
		if got := w.environments.activeName(); got != "Dev2" {
			t.Errorf("environment %q", got)
		}
		w.environments.update("Dev", nil)
		w.environments.update("Dev3", nil)
		w.environments.renamed()
		if got := setEnvironments(w.requests.sets.active); got["A"] != "Dev3" || got["B"] != "Dev" {
			t.Errorf("set remembers %v", got)
		}
	})
}

// Closing the tab on show, by removing its request, leaves the environment
// of the tab shown in its place as it was.
func TestRemovePinnedKeepsEnvironment(t *testing.T) {
	files := setTabsFixture(t, []string{"Dev", "Live"}, map[string]string{"A": "Dev", "B": "Live"})
	qute.PostSync(func() {
		w := NewWindow(files.Files)
		w.requests.sets.list.SetCurrentItem(w.requests.sets.list.Item(0))
		a := w.tabs.tabs[0].request
		w.requests.store.Folders[0].Remove(0)
		w.requests.onRemove(a)
		if got := w.environments.activeName(); got != "Live" || w.tabs.current().environment != "Live" {
			t.Errorf("environment %q, tab remembers %q", got, w.tabs.current().environment)
		}
	})
}

// The play button follows the tabs of a set chosen, even where its first
// tab's environment is already the one in use.
func TestPlayFollowsSet(t *testing.T) {
	files := setTabsFixture(t, []string{"Dev"}, map[string]string{"A": "Dev"})
	qute.PostSync(func() {
		w := NewWindow(files.Files)
		w.environments.list.SetCurrentRow(0)
		if w.environment.play.IsEnabled() {
			t.Error("play enabled with no pinned tabs")
		}
		w.requests.sets.list.SetCurrentItem(w.requests.sets.list.Item(0))
		if !w.environment.play.IsEnabled() {
			t.Error("play disabled with the tabs of a set")
		}
	})
}

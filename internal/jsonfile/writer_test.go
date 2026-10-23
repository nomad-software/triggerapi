package jsonfile

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nomad-software/triggerapi/internal/crypt"
)

// held is a write function that waits for each write to be let through,
// recording the paths written in order.
type held struct {
	mu      sync.Mutex
	written []string
	started chan string
	release chan struct{}
}

func newHeld() *held {
	return &held{started: make(chan string, 100), release: make(chan struct{})}
}

func (h *held) write(path string, data []byte) error {
	h.started <- path
	<-h.release
	h.mu.Lock()
	h.written = append(h.written, path)
	h.mu.Unlock()
	return write(path, data)
}

func (h *held) paths() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.written)
}

func readDocument(t *testing.T, path string, keys *crypt.Keys) document {
	t.Helper()
	var d document
	if err := LoadWith(path, &d, keys); err != nil {
		t.Fatal(err)
	}
	return d
}

func TestWriterSaves(t *testing.T) {
	keys := openKeys(t, "id")
	path := filepath.Join(t.TempDir(), "doc.json")
	w := NewWriter()

	done := make(chan error, 1)
	if err := w.Save(path, document{Name: "secret"}, keys, func(err error) { done <- err }); err != nil {
		t.Fatal(err)
	}
	w.Flush()

	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	default:
		t.Fatal("done was not called before Flush returned")
	}
	if data, _ := os.ReadFile(path); !crypt.Encrypted(data) {
		t.Errorf("file = %s", data)
	}
	if got := readDocument(t, path, keys); got.Name != "secret" {
		t.Errorf("read %+v", got)
	}
	// It is written as SaveWith writes it.
	other := filepath.Join(t.TempDir(), "doc.json")
	if err := Save(other, document{Name: "plain"}); err != nil {
		t.Fatal(err)
	}
	if err := w.Save(path, document{Name: "plain"}, nil, nil); err != nil {
		t.Fatal(err)
	}
	w.Flush()
	a, _ := os.ReadFile(path)
	b, _ := os.ReadFile(other)
	if string(a) != string(b) {
		t.Errorf("written %q, SaveWith %q", a, b)
	}
}

// What is written is the value as it was when it was saved.
func TestWriterSavesSnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "doc.json")
	w := NewWriter()
	h := newHeld()
	w.write = h.write

	d := &document{Name: "before"}
	if err := w.Save(path, d, nil, nil); err != nil {
		t.Fatal(err)
	}
	<-h.started
	d.Name = "after"
	close(h.release)
	w.Flush()

	if got := readDocument(t, path, nil); got.Name != "before" {
		t.Errorf("read %+v", got)
	}
}

// A path saved again while it waits is written once, as last saved, and the
// done of what was overtaken is not called.
func TestWriterCoalesces(t *testing.T) {
	dir := t.TempDir()
	first, second := filepath.Join(dir, "first.json"), filepath.Join(dir, "second.json")
	w := NewWriter()
	h := newHeld()
	w.write = h.write

	// The first is being written while the second is saved three times.
	if err := w.Save(first, document{Name: "1"}, nil, nil); err != nil {
		t.Fatal(err)
	}
	if got := <-h.started; got != first {
		t.Fatalf("started %s", got)
	}
	var mu sync.Mutex
	var called []string
	for _, name := range []string{"a", "b", "c"} {
		if err := w.Save(second, document{Name: name}, nil, func(error) {
			mu.Lock()
			called = append(called, name)
			mu.Unlock()
		}); err != nil {
			t.Fatal(err)
		}
	}
	close(h.release)
	w.Flush()

	if got := h.paths(); !slices.Equal(got, []string{first, second}) {
		t.Errorf("written %q", got)
	}
	if got := readDocument(t, second, nil); got.Name != "c" {
		t.Errorf("read %+v", got)
	}
	if !slices.Equal(called, []string{"c"}) {
		t.Errorf("done called for %q", called)
	}
}

// Paths are written in the order they were first saved to.
func TestWriterOrder(t *testing.T) {
	dir := t.TempDir()
	w := NewWriter()
	h := newHeld()
	w.write = h.write

	var want []string
	for i := range 5 {
		path := filepath.Join(dir, fmt.Sprintf("%d.json", i))
		want = append(want, path)
		if err := w.Save(path, document{Name: "x"}, nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	// Saving the first again, while it is being written, queues it after
	// the others.
	<-h.started
	if err := w.Save(want[0], document{Name: "again"}, nil, nil); err != nil {
		t.Fatal(err)
	}
	want = append(want, want[0])
	close(h.release)
	w.Flush()

	if got := h.paths(); !slices.Equal(got, want) {
		t.Errorf("written %q, want %q", got, want)
	}
	if got := readDocument(t, want[0], nil); got.Name != "again" {
		t.Errorf("read %+v", got)
	}
}

// Flush waits for what is being written, and what is queued behind it.
func TestWriterFlushWaits(t *testing.T) {
	dir := t.TempDir()
	w := NewWriter()
	h := newHeld()
	w.write = h.write

	if err := w.Save(filepath.Join(dir, "a.json"), document{}, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := w.Save(filepath.Join(dir, "b.json"), document{}, nil, nil); err != nil {
		t.Fatal(err)
	}
	<-h.started

	flushed := make(chan struct{})
	go func() {
		w.Flush()
		close(flushed)
	}()
	select {
	case <-flushed:
		t.Fatal("Flush returned while a file was being written")
	case <-time.After(50 * time.Millisecond):
	}
	close(h.release)
	select {
	case <-flushed:
	case <-time.After(5 * time.Second):
		t.Fatal("Flush did not return")
	}
	if got := h.paths(); len(got) != 2 {
		t.Errorf("written %q", got)
	}

	// With nothing to write it returns at once, and the goroutine has
	// ended.
	w.Flush()
	w.mu.Lock()
	busy := w.busy
	w.mu.Unlock()
	if busy {
		t.Error("the goroutine is still running")
	}
}

// Failures are given to done; a value that is not JSON is reported by Save
// and nothing is written.
func TestWriterErrors(t *testing.T) {
	w := NewWriter()
	missing := filepath.Join(t.TempDir(), "gone", "doc.json")

	done := make(chan error, 1)
	if err := w.Save(missing, document{}, nil, func(err error) { done <- err }); err != nil {
		t.Fatal(err)
	}
	flushed := w.Flush()
	if err := <-done; err == nil {
		t.Error("writing into a missing folder did not fail")
	}
	// Flush says which file is not as it was last saved, until it is.
	if flushed == nil || !strings.Contains(flushed.Error(), missing) {
		t.Errorf("Flush = %v", flushed)
	}
	if err := w.Flush(); err == nil {
		t.Error("the failure was forgotten")
	}
	if err := os.Mkdir(filepath.Dir(missing), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := w.Save(missing, document{}, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := w.Flush(); err != nil {
		t.Errorf("Flush after a write that worked = %v", err)
	}

	path := filepath.Join(t.TempDir(), "doc.json")
	if err := w.Save(path, func() {}, nil, nil); err == nil {
		t.Error("a function was saved")
	}
	w.Flush()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("file written: %v", err)
	}

	// Keys that cannot be used fail the write, leaving the file as it was.
	if err := Save(path, document{Name: "kept"}); err != nil {
		t.Fatal(err)
	}
	if err := w.Save(path, document{Name: "lost"}, crypt.Unusable(fmt.Errorf("no keys")), func(err error) { done <- err }); err != nil {
		t.Fatal(err)
	}
	if err := w.Flush(); err == nil || !strings.Contains(err.Error(), path) {
		t.Errorf("Flush = %v", err)
	}
	if err := <-done; err == nil {
		t.Error("unusable keys did not fail")
	}
	if got := readDocument(t, path, nil); got.Name != "kept" {
		t.Errorf("read %+v", got)
	}
}

// Many goroutines saving and flushing at once leave each file as it was
// last saved. Run with -race.
func TestWriterConcurrent(t *testing.T) {
	keys := openKeys(t, "id")
	dir := t.TempDir()
	w := NewWriter()

	const writers, saves = 8, 50
	var wg sync.WaitGroup
	for i := range writers {
		path := filepath.Join(dir, fmt.Sprintf("%d.json", i))
		wg.Go(func() {
			for n := range saves {
				d := document{Name: fmt.Sprint(n)}
				if err := w.Save(path, &d, keys, func(err error) {
					if err != nil {
						t.Error(err)
					}
				}); err != nil {
					t.Error(err)
				}
				// The value is the saver's again at once.
				d.Name = "changed"
				if n%10 == 0 {
					w.Flush()
				}
			}
		})
	}
	wg.Wait()
	if err := w.Flush(); err != nil {
		t.Error(err)
	}

	for i := range writers {
		path := filepath.Join(dir, fmt.Sprintf("%d.json", i))
		if got := readDocument(t, path, keys); got.Name != fmt.Sprint(saves-1) {
			t.Errorf("%s: read %+v", path, got)
		}
	}
	// No temporary file is left behind.
	entries, _ := os.ReadDir(dir)
	if len(entries) != writers {
		t.Errorf("%d files in the folder", len(entries))
	}
}

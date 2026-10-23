package jsonfile

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sync"

	"github.com/nomad-software/triggerapi/internal/crypt"
)

// Writer saves files in the background, so that whoever saves, the GUI
// thread above all, does not wait for the indenting, the encryption and the
// disk. What is saved is turned into compact JSON at once, which is the only
// part that reads it, so it is the value as it was when it was saved, and can
// be changed straight away. One goroutine, started when there is something to
// write and ending when there is nothing left, writes the files one at a
// time, each exactly as SaveWith does. A file saved again before it has been
// written is written once, as it was saved the last time.
//
// A Writer is safe for use by several goroutines. The zero value is not
// usable: make one with NewWriter.
type Writer struct {
	mu sync.Mutex
	// queued holds what is waiting to be written to each path, and order
	// the paths in the order they were first saved to.
	queued map[string]job
	order  []string
	// busy is set while the goroutine that writes is running.
	busy bool
	// idle is signalled when the goroutine has nothing left to write.
	idle *sync.Cond
	// failed holds why the last write of each path failed, for as long as
	// it is the last.
	failed map[string]error

	// write replaces a file, which tests replace to hold it up.
	write func(path string, data []byte) error
}

// job is what is waiting to be written to one path.
type job struct {
	data []byte
	keys *crypt.Keys
	done func(error)
}

// NewWriter returns a Writer with nothing to write.
func NewWriter() *Writer {
	w := &Writer{queued: map[string]job{}, failed: map[string]error{}, write: write}
	w.idle = sync.NewCond(&w.mu)
	return w
}

// Save has v written to the JSON file at path, encrypted with keys unless
// they are nil, replacing it atomically. v is turned into JSON before Save
// returns, which is the only error it reports; the rest is done in the
// background, and done, if not nil, is called with its outcome from the
// goroutine that writes. A save that is overtaken by another of the same
// path before it is written is never written, and its done is not called.
func (w *Writer) Save(path string, v any, keys *crypt.Keys, done func(error)) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}

	w.mu.Lock()
	defer w.mu.Unlock()
	if _, waiting := w.queued[path]; !waiting {
		w.order = append(w.order, path)
	}
	w.queued[path] = job{data: data, keys: keys, done: done}
	if !w.busy {
		w.busy = true
		go w.run()
	}
	return nil
}

// Flush waits until everything saved before it was called has been written,
// or has failed to be. It is called before files are read, copied, moved or
// removed other than by the Writer. It returns why each file whose last save
// failed is not as it was last saved, which is nil when every one is, so
// that what is not on disk can be told where done can no longer report it,
// as when the program ends.
func (w *Writer) Flush() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	for w.busy {
		w.idle.Wait()
	}
	var failed []error
	for _, path := range slices.Sorted(maps.Keys(w.failed)) {
		failed = append(failed, fmt.Errorf("%s: %w", path, w.failed[path]))
	}
	return errors.Join(failed...)
}

// run writes what is queued, oldest first, until nothing is.
func (w *Writer) run() {
	for {
		w.mu.Lock()
		if len(w.order) == 0 {
			w.busy = false
			w.idle.Broadcast()
			w.mu.Unlock()
			return
		}
		path := w.order[0]
		w.order = w.order[1:]
		j := w.queued[path]
		delete(w.queued, path)
		w.mu.Unlock()

		err := seal(path, j.data, j.keys, w.write)

		w.mu.Lock()
		if err != nil {
			w.failed[path] = err
		} else {
			delete(w.failed, path)
		}
		w.mu.Unlock()
		if j.done != nil {
			j.done(err)
		}
	}
}

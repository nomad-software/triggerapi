package atomicfile

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
)

func TestWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file")

	if err := Write(path, []byte("first"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Write(path, []byte("second"), 0o600); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(path); string(data) != "second" {
		t.Errorf("file = %q", data)
	}
	if runtime.GOOS != "windows" {
		if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
			t.Errorf("mode = %v", info.Mode())
		}
		other := filepath.Join(dir, "other")
		if err := Write(other, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		if info, _ := os.Stat(other); info.Mode().Perm() != 0o644 {
			t.Errorf("mode = %v", info.Mode())
		}
	}
	// No temporary file is left behind.
	if entries, _ := os.ReadDir(dir); len(entries) != 2 {
		t.Errorf("%d files in the folder", len(entries))
	}
}

// A write that fails leaves what was there as it was, and nothing else.
func TestWriteFails(t *testing.T) {
	dir := t.TempDir()

	// A folder cannot be replaced by a file.
	folder := filepath.Join(dir, "folder")
	if err := os.Mkdir(folder, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folder, "inside"), []byte("kept"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Write(folder, []byte("lost"), 0o600); err == nil {
		t.Error("a folder was replaced")
	}
	if data, _ := os.ReadFile(filepath.Join(folder, "inside")); string(data) != "kept" {
		t.Errorf("inside = %q", data)
	}

	// Nor can a file go in a folder that is not there.
	if err := Write(filepath.Join(dir, "gone", "file"), []byte("lost"), 0o600); err == nil {
		t.Error("written into a missing folder")
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Errorf("%d files in the folder", len(entries))
	}
}

// Readers never see a part of a file while it is replaced, however many
// goroutines write it at once. Run with -race.
func TestWriteConcurrent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "file")
	contents := make([][]byte, 8)
	for i := range contents {
		contents[i] = bytes.Repeat([]byte(fmt.Sprint(i)), 64*1024)
	}
	if err := Write(path, contents[0], 0o600); err != nil {
		t.Fatal(err)
	}

	var writers, readers sync.WaitGroup
	done := make(chan struct{})
	for _, data := range contents {
		writers.Go(func() {
			for range 20 {
				if err := Write(path, data, 0o600); err != nil {
					t.Error(err)
				}
			}
		})
	}
	for range 4 {
		readers.Go(func() {
			for {
				select {
				case <-done:
					return
				default:
				}
				data, err := os.ReadFile(path)
				if err != nil {
					t.Error(err)
					return
				}
				whole := false
				for _, c := range contents {
					whole = whole || bytes.Equal(data, c)
				}
				if !whole {
					t.Errorf("read %d bytes that are not one of the writes", len(data))
					return
				}
			}
		})
	}
	writers.Wait()
	close(done)
	readers.Wait()
}

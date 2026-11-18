package ui

import (
	"github.com/nomad-software/qute"

	"context"
	"testing"
	"time"
)

// An answer to a load made before the cache was cleared, as when the network
// settings change, is neither kept nor used.
func TestServerCacheClear(t *testing.T) {
	cache := &serverCache[int]{}
	release := make(chan struct{})
	fetched := make(chan struct{})
	used := false
	qute.PostSync(func() {
		cache.load("server", false, func(context.Context) (int, error) {
			<-release
			defer close(fetched)
			return 1, nil
		}, func(int, error) { used = true })
		cache.clear()
	})
	close(release)
	<-fetched
	// The answer is handed to the main thread just after fetch returns.
	time.Sleep(100 * time.Millisecond)
	qute.PostSync(func() {
		if _, ok := cache.get("server"); ok || used {
			t.Errorf("kept %v, used %v", ok, used)
		}
	})

	// One made after is kept.
	qute.PostSync(func() {
		cache.load("server", false, func(context.Context) (int, error) { return 2, nil }, func(int, error) {})
	})
	waitFor(t, "the second answer", func() bool { value, ok := cache.get("server"); return ok && value == 2 })
}

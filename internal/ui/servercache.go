package ui

import (
	"github.com/nomad-software/qute"

	"context"

	"github.com/nomad-software/triggerapi/internal/network"
)

// serverCache holds what has been loaded about servers in the background, by
// server: the service definitions of gRPC servers, the schemas of GraphQL
// ones and what MCP servers have. Only the answer to the latest load is
// used; one that a later load has replaced is kept but not shown. It belongs
// to the main thread.
type serverCache[T any] struct {
	known map[string]T
	// asked counts the loads, so that an answer can be told from that of
	// a later one.
	asked int
	// cleared counts the times what is known has been forgotten, so that
	// the answer of a load made before is not kept.
	cleared int
}

// get returns what is known about a server, if it has been loaded.
func (c *serverCache[T]) get(server string) (T, bool) {
	value, ok := c.known[server]
	return value, ok
}

// drop has the answer of any load under way not used, as when there is no
// longer a server to ask.
func (c *serverCache[T]) drop() {
	c.asked++
}

// clear forgets what is known about every server, and has the answer of any
// load under way not used, as when the network settings change what a
// server can be reached with.
func (c *serverCache[T]) clear() {
	c.known = nil
	c.asked++
	c.cleared++
}

// load gives done what is known about a server: at once if it has been
// loaded before and force is not set, or else once fetch has loaded it in
// the background, with reflectTimeout to do it in. done is called on the
// main thread, unless a later load has been asked for meanwhile.
func (c *serverCache[T]) load(server string, force bool, fetch func(context.Context) (T, error), done func(T, error)) {
	c.asked++
	asked, cleared := c.asked, c.cleared

	if value, ok := c.known[server]; ok && !force {
		done(value, nil)
		return
	}

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), reflectTimeout)
		defer cancel()

		value, err := fetch(ctx)
		err = network.Explain(err)

		qute.Post(func() {
			// An answer got with what has since been forgotten, such as
			// the network settings before, is not kept.
			if err == nil && cleared == c.cleared {
				if c.known == nil {
					c.known = map[string]T{}
				}
				c.known[server] = value
			}
			if asked == c.asked {
				done(value, err)
			}
		})
	}()
}

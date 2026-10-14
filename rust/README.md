# TriggerAPI in Rust

A port of TriggerAPI to Rust, with an [egui](https://github.com/emilk/egui)
user interface, kept beside the Go program so that the two can be compared.
It has the same features, and reads and writes the same files in the same
places, so both can be pointed at one set of workspaces.

## Building and running

It needs a Rust toolchain (1.85 or later). There is no Qt or other system
library to install: egui draws the window with OpenGL, through eframe.

```sh
cd rust
cargo run --release            # build and start it
cargo build --release          # the program is target/release/triggerapi
cargo test                     # the tests that need no display
```

`cargo test` runs the tests of both crates, including calls to a gRPC
server started in the test. Pictures of the window can be made without
showing it, where there is a graphics adapter:

```sh
SHOT_DIR=/tmp/shots cargo test -p triggerapi shots -- --ignored
```

To try it on data that is not your own, point the configuration and data
folders somewhere else:

```sh
XDG_CONFIG_HOME=/tmp/t/config XDG_DATA_HOME=/tmp/t/data cargo run --release
```

## Layout

- `core` (`triggerapi-core`) is everything but the UI, as the `internal`
  packages of the Go program are: `rest` and `curl` (the HTTP engine), `rpc`
  (gRPC through server reflection, with tonic and prost-reflect), `env`,
  `capture`, `collection`, `openapi`, `postman`, `crypt` (age with SSH keys),
  `jsonfile`, `settings` and `workspace`. `json` keeps a document as it was
  written where Go's encoding/json does: indenting without reordering, and
  numbers as they are written.
- `app` (`triggerapi`) is the egui program: `app.rs` holds the state and wires
  the parts together, with a module for each part of the window.

## Compatibility with the Go program

The files are the same: `settings.json` in the system's configuration folder,
and the workspaces, each with its `environments.json` and `requests.json`,
encrypted as armoured age files for the SSH keys of the settings. Fields
that are not set are left out of the files as Go leaves them out, `null` is
read as Go reads it, and a request saved before there were types is a REST
request. Folders and requests are given ids while the program runs, so the UI
can refer to them, which are never saved.

## Differences from the Go program

- JSON in the editors and the response is coloured, but cannot be folded yet.
- egui draws its own widgets, so the window follows the system's light or dark
  setting but not the colours of the desktop's theme.
- The code font is found among the installed fonts by its family name, as Qt
  finds it; where it is not installed egui's own fixed width font is used.
- Something dragged in the Requests tree is dropped into the folder it is let
  go on (or the folder of the request it is let go on), or at the top level
  if it is let go on a heading or the space beneath the tree.
- The address in a curl command is written as the `url` crate writes it, which
  adds a `/` after a host with no path.

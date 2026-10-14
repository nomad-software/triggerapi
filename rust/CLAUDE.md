# CLAUDE.md

Guidance for working on the Rust port of TriggerAPI, in this folder. The Go
program in the rest of the repository is the reference: its `CLAUDE.md`
describes every feature, and this port follows it. Do not change the Go code
when working here.

## Compatibility of saved files

The rule of the Go program's `CLAUDE.md` applies here too, and to both
programs at once: they share `settings.json` and the workspaces, so anything
either writes must be read by the other without loss. New fields are optional
(`skip_serializing_if`, read with `nullable::de` so that `null` is the
default), and a change to the format is made in both programs. Test reading
files in the old format.

## Layout

- `core` has no UI. Keep request building, file formats and importers here,
  with unit tests beside them. `core/tests/grpc.rs` runs a tonic Echo server
  with reflection, as Go's `rpctest` does, and calls it.
- `app` is the egui program. `app.rs` holds the state (`App`) and turns what
  each part reports into changes: the parts (`tree`, `lists`, `request`,
  `grpc`, `response`, `tabs`, `editor`, `settings_dialog`, `dialogs`) draw
  themselves and return what the user did, rather than changing the window.
  Background work (requests, reflection) runs on the tokio runtime of `App`
  and reports through a channel read at the start of each frame.
- Folders and requests are referred to by their `id`, given when they are made
  or read and never saved. Dialogs that need an answer before acting hold a
  closure run with `&mut App` once answered.

## Commands

```sh
cargo build                         # debug
cargo build --release -p triggerapi # target/release/triggerapi
cargo test                          # all tests; give it a timeout
SHOT_DIR=/tmp/shots cargo test -p triggerapi shots -- --ignored   # pictures of the window
```

Look at the pictures from `shots` to check a change to the UI, rather than
opening a window on the user's display.

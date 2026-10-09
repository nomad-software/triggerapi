# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

Postie is a desktop client for making REST API and gRPC calls, inspired by Postman. It is written in Go with a Qt 6 GUI via the [miqt](https://github.com/mappu/miqt) bindings.

REST support, environments and saved requests in folders are implemented; gRPC and history are not started yet.

## Architecture

Packages under `internal/`; only `ui` depends on Qt.

- `rest` is the HTTP engine. `rest.Request` holds everything in the request UI (method, URL, params, headers, auth, body type, raw body, form fields), including fields the chosen auth/body type does not use, so it round-trips through JSON without loss. `Request.Build` turns it into an `*http.Request` and `rest.Send` returns a `rest.Response`. Put request-building rules here (default scheme, param ordering, header precedence) so they stay unit-testable.
- `env` holds environments (named sets of variables). `Environment.Expand` substitutes `{{name}}` placeholders, leaving unknown ones untouched.
- `collection` holds saved requests: a `Store` of `Folder`s, each with `Request`s (a name plus an embedded `rest.Request`). Folders are one level deep. Folders and requests are held by pointer so they keep their identity when others are removed.
- `settings` holds state kept between runs (window size, maximised state and the response body font), loaded in `NewWindow` and saved from the window's close event.
- `jsonfile` does the atomic JSON load/save used by all of these. Data lives in `environments.json`, `requests.json` and `settings.json` in the working directory; the paths are passed from `main.go` as `ui.Files`.
- `ui` is the Qt layer, wired together in `NewWindow`:
  - The sidebar is a tab widget with an Environments tab (`envList`) and a Requests tab (`requestTree`). Each owns its store and saves on every change; there are no Save buttons. If a file cannot be parsed at startup, saving to it is disabled so it is never overwritten.
  - `Window.stack` shows either the request UI (`requestPanel` + `responsePanel`) or `envEditor` (`pageRequest` / `pageEditor`).
  - The selected environment is the active one. Edit opens it in `envEditor`, which follows the selection while open and reports edits through `envList.update`.
  - Clicking a request in the tree, or pressing Edit, opens it: `Window.open` points at it, the panel shows its name as a title, `requestPanel.load` shows it, and every edit (`requestPanel.onChange`) is written back with `requestPanel.request()` and saved. With `Window.open == nil` the request UI holds an unsaved request. `requestTree` maps tree items to the store by position, so the two must be kept in the same order.
  - The URL field shows the enabled params as its query string and the two are kept in step both ways (`urlChanged` / `paramsChanged`, using `rest.SplitURL`, `JoinURL` and `MergeParams` in `rest/query.go`). The stored `rest.Request.URL` has no query string; the params hold it.
  - `requestPanel.request()` and `load()` are the only places widgets are converted to and from a `rest.Request`; a new field must be added to both, to an `onChange` hook, and to `rest.Request.Expand` so placeholders work in it.
  - Widgets report changes through callback fields (`onChange`, `onOpen`, ...) and use an `updating` flag to suppress them while being filled programmatically.
  - `Window.send` applies the active environment with `rest.Request.Expand`, runs `rest.Send` in a goroutine and returns to the GUI thread with `mainthread.Start`. While a request is in flight the Send button cancels it via the context.
  - `placeholderHighlighter` (`placeholders.go`) colours `{{name}}` placeholders green if the active environment defines them and orange if not. Text documents use a `QSyntaxHighlighter`; line edits have no formatting API, so the colours are delivered with an empty `QInputMethodEvent`, re-sent whenever the text or cursor moves. `Rehighlight` makes a `QPlainTextEdit` emit `textChanged`, so it is only called through `requestPanel.refreshPlaceholders`, which suppresses `onChange`. Table cells are not coloured.
  - The menu bar (`Window.newMenuBar`) has File > Quit, which closes the window so the close event still saves the settings, and Help > About.
  - `keyValueTable` is the shared editable key/value grid with per-row enable checkboxes and a trailing empty row.

## UI tests

`internal/ui/ui_test.go` has a `TestMain` that creates the `QApplication` offscreen and runs the Qt event loop on the main thread while tests run on another goroutine. Tests must pass `NewWindow` the paths from `testFiles(t)`, replace `requestTree.confirm` rather than let a modal dialog open, create and touch widgets inside `mainthread.Wait` / `mainthread.Wait2`, and poll for asynchronous results rather than blocking the main thread.

## Commands

```sh
make build                # go build -ldflags "-s -w" -o postie . (binary is gitignored)
make run                  # build, then ./postie
make vet                  # go vet ./...
make test                 # go test ./...
go test ./path/to/pkg -run TestName   # single test
```

Always build the binary without debug symbols (`-ldflags "-s -w"`), either with `make build` or by passing the flags to `go build`.

Smoke-test the GUI without a display:

```sh
QT_QPA_PLATFORM=offscreen timeout 3 ./postie   # exit code 124 means it started and kept running
```

## Building with miqt

- miqt is cgo: building needs a C++ compiler and the Qt 6 development headers (found via `pkg-config`, e.g. `Qt6Widgets`).
- The first build of `github.com/mappu/miqt/qt6` compiles the whole binding and takes about 5 minutes; later builds use the Go build cache and are fast. Use a long timeout for the first build, and avoid `go clean -cache` or changing cgo flags, which forces the full rebuild.
- That build prints a large volume of `-Wsfinae-incomplete` warnings from the Qt headers. They are noise; search the output for `error` instead of reading it.
- Linux is the only supported platform for now. A macOS release is planned, so avoid adding Linux-only assumptions (paths, shell-outs, X11/Wayland specifics) where a portable Qt or Go API exists.
- `README.md` lists the packages needed to build on each distribution; keep it in step if the Qt modules used change (currently only `Qt6Widgets`).

## miqt conventions

- Import as `qt "github.com/mappu/miqt/qt6"`. Use the `qt6` package, not `qt` (which is Qt 5).
- C++ overloaded constructors and methods become numbered Go functions (`NewQMainWindow2()`, `NewQLabel3("text")`). Check the miqt package source or docs for the right overload rather than guessing the number.
- Static methods are `Class_Method` (`qt.QApplication_Exec()`); signals are connected with `OnSignalName(func(...))`.
- Qt widgets must only be touched from the main (GUI) thread. Network calls run in goroutines and hand results back with `mainthread.Start` from `github.com/mappu/miqt/qt6/mainthread`.
- Memory: objects from `NewQ...` constructors are never freed by Go, so short-lived ones need `Delete()`. Value types returned by methods and static functions (`ToQVariant()`, `QFontDatabase_SystemFont()`, ...) already have a finalizer (`GoGC()` in the miqt source); calling `Delete()` on those is a double free that crashes at a later garbage collection. Check the generated function before deleting anything.
- Pass a widget to a layout or container through its embedded base (`button.QWidget`, `layout.QLayout`, `window.QObject`).

# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

Postie is a desktop client for making REST API and gRPC calls, inspired by Postman. It is written in Go with a Qt 6 GUI via the [miqt](https://github.com/mappu/miqt) bindings.

REST and gRPC requests, environments and saved requests in folders are implemented; history is not started yet.

## Architecture

Packages under `internal/`; only `ui` depends on Qt.

- `rest` is the HTTP engine. `rest.Request` holds everything in the request UI (method, URL, params, headers, auth, body type, raw body, form fields), including fields the chosen auth/body type does not use, so it round-trips through JSON without loss. `Request.Build` turns it into an `*http.Request` and `rest.Send` returns a `rest.Response`. Put request-building rules here (default scheme, param ordering, header precedence) so they stay unit-testable.
- `env` holds environments (named sets of variables). `Environment.Expand` substitutes `{{name}}` placeholders, leaving unknown ones untouched.
- `rpc` is the gRPC engine, built on the connectrpc libraries (`connectrpc.com/connect` and `connectrpc.com/grpcreflect`) with `google.golang.org/protobuf` for dynamic messages. Do not add `google.golang.org/grpc`. `rpc.Reflect` asks a server to describe itself through server reflection and returns a `Schema` (there is no support for loading `.proto` files); `rpc.Invoke` sends one JSON message over a bidirectional stream and collects every response, which covers unary and server-streaming methods. A status other than OK, including a cancelled call, is returned as a `Response`, not an error. `rpc/rpctest` is a connect-based test server with an Echo service, used by the `rpc` and `ui` tests.
- `collection` holds saved requests: a `Store` of `Folder`s, each with `Request`s. A `Request` has a name, a `Type` (`rest` or `grpc`) and exactly one of an embedded `*rest.Request` (stored flat beside the name, which is also the format from before there were types) or `GRPC *rpc.Request`; `Load` normalises this. Check `Type` before touching either. Folders are one level deep. Folders and requests are held by pointer so they keep their identity when others are removed.
- `settings` holds state kept between runs: window size, maximised state, sidebar width, response height, the response body font, and optional locations for the environments and requests files. It is loaded in `NewWindow` and saved from the window's close event and whenever the settings dialog is accepted.
- `jsonfile` does the atomic JSON load/save used by all of these. Data lives in `environments.json`, `requests.json` and `settings.json` in the working directory by default; those paths are passed from `main.go` as `ui.Files`. `settings.json` always stays there, while the settings can point the other two elsewhere (`Window.environmentsFile` / `requestsFile` give the paths in effect).
- `ui` is the Qt layer, wired together in `NewWindow`:
  - The sidebar is a tab widget with an Environments tab (`envList`) and a Requests tab (`requestTree`), which is the one selected at startup. Each owns its store and saves on every change; there are no Save buttons. If a file cannot be parsed at startup, saving to it is disabled so it is never overwritten.
  - `Window.stack` shows the REST request UI (`requestPanel` + `responsePanel`), the gRPC request UI (`grpcPanel` + its own `responsePanel`) or `envEditor` (`pageRequest` / `pageGRPC` / `pageEditor`). `Window.requestPage` picks the request page from the type of the open request.
  - The selected environment is the active one. Edit opens it in `envEditor`, which follows the selection while open and reports edits through `envList.update`.
  - Clicking a request in the tree, or pressing Edit, opens it: `Window.open` points at it, the `header` above the panel shows its name at the left and, at the right, a labelled drop-down that selects the active environment by selecting it in the sidebar list (`Window.updateHeaders` keeps both headers in step), `requestPanel.load` shows it, and every edit (`requestPanel.onChange`) is written back with `requestPanel.request()` and saved. With `Window.open == nil` the REST request UI holds an unsaved request. Add Request asks for the type through `askRequestType`, a package variable that tests replace; by default it shows `requestTypeDialog` (`requesttype.go`), which offers each type as an icon. Icons, including the program's own (`appIcon`, used as the window icon and so shown in the about box), are drawn with `QPainter` by `drawIcon` in `icons.go`; there are no image files. The same icons are shown before each request's name in the Requests tree. A new request type needs an entry in `requestTypes` and `typeLabels`. `requestTree` maps tree items to the store by position, so the two must be kept in the same order.
  - The URL field shows the enabled params as its query string and the two are kept in step both ways (`urlChanged` / `paramsChanged`, using `rest.SplitURL`, `JoinURL` and `MergeParams` in `rest/query.go`). The stored `rest.Request.URL` has no query string; the params hold it.
  - `requestPanel.request()` and `load()` are the only places widgets are converted to and from a `rest.Request` (likewise `grpcPanel` and `rpc.Request`); a new field must be added to both, to an `onChange` hook, and to the request's `Expand` so placeholders work in it.
  - Widgets report changes through callback fields (`onChange`, `onOpen`, ...) and use an `updating` flag to suppress them while being filled programmatically.
  - `Window.send` and `Window.invoke` apply the active environment with `Expand` and hand the work to `Window.run`, which runs it in a goroutine, returns to the GUI thread with `mainthread.Start`, and turns the button into Cancel while in flight. Results that arrive after another request has been shown are dropped (`Window.shown`). The status bar at the bottom of the window is not used for requests or responses; `Window.status` is only for problems loading or saving the data files. The Response tab of a response panel holds only real bodies (the HTTP body, or the gRPC messages); no hints or progress text go in it. Everything else about a response goes in the bar beneath it as labelled fields (`responsePanel.setFields`): Code (the HTTP status, or the gRPC code such as `5 NOT_FOUND`), Error (the gRPC status message, or the error there was instead of a response), Time, and Size and Protocol or Messages. Code and Error are at the left of the bar and the rest right-aligned. Values that are failures are red: an HTTP status of 400 or more, a gRPC status other than OK, and any error.
  - `Window.reflect` loads the service definition for the gRPC panel in the background and caches it per target in `Window.schemas`. It runs when a gRPC request is opened, when the server address or TLS setting changes, and on Reload (which bypasses the cache). `grpcPanel.setSchema` fills the method drop-down and keeps the saved method selected. Loading is silent; only a failure is reported, in the panel's status line. The drop-down is a `methodCombo`, which lists short method names under unselectable service headings and shows the full name when closed; read and set it through `current()` / `selectMethod()`, not the combo's own text and index.
  - `authEditor` is the Authorization tab shared by both request panels.
  - `placeholderHighlighter` (`placeholders.go`) colours `{{name}}` placeholders green if the active environment defines them and orange if not. Text documents use a `QSyntaxHighlighter`; line edits have no formatting API, so the colours are delivered with an empty `QInputMethodEvent`, re-sent whenever the text or cursor moves. `Rehighlight` makes a `QPlainTextEdit` emit `textChanged`, so it is only called through `requestPanel.refreshPlaceholders`, which suppresses `onChange`. Table cells are not coloured.
  - The sidebar width and response height are saved on close and restored by `Window.restoreLayout` the first time the window is shown, deferred with `Window.later` because splitter sizes are not known until the window has been laid out. The REST and gRPC pages share one response height (`Window.responseHeight`), applied to a page when it is shown.
  - The menu bar (`Window.newMenuBar`) has File > Settings (Ctrl+,), File > Quit (Ctrl+Q), which closes the window so the close event still saves the settings, and Help > About (Ctrl+I).
  - `settingsDialog` edits the settings and `Window.applySettings` puts them into effect at once. Moving the environments or requests to a file that exists loads it through `envList.load` / `requestTree.load` (so the stores can be replaced while running: use `Window.expand` rather than holding on to a store); moving them to a file that does not exist saves the current data there. A location that cannot be used is refused and the setting left as it was.
  - `keyValueTable` is the shared editable key/value grid with per-row enable checkboxes and a trailing empty row.

## UI tests

`internal/ui/ui_test.go` has a `TestMain` that creates the `QApplication` offscreen and runs the Qt event loop on the main thread while tests run on another goroutine. Tests must pass `NewWindow` the paths from `testFiles(t)`, replace `requestTree.confirm` rather than let a modal dialog open, set `testRequestType` (or use `addGRPCRequest`) to choose the type of an added request, use `waitFor` for asynchronous results, create and touch widgets inside `mainthread.Wait` / `mainthread.Wait2`, and poll for asynchronous results rather than blocking the main thread.

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

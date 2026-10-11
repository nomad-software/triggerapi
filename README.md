# TriggerAPI

A desktop client for making REST API and gRPC calls. It is written
in Go with a Qt 6 interface using the [miqt](https://github.com/mappu/miqt)
bindings.

![TriggerAPI showing a REST request with a JSON body, its JSON response and a value captured from it](docs/screenshot.png)

## Why TriggerAPI

- **No account, no cloud.** There is nothing to sign in to and nothing is
  synchronised anywhere. TriggerAPI works the same with no network at all.
- **No telemetry.** The only requests it makes are the ones you send.
- **Your data is yours.** Requests, environments and settings are plain JSON
  files in ordinary folders, which you can read, back up, copy to another
  machine or keep in version control. See [Running](#running) for where they
  are.
- **Native.** It is a single compiled program with a Qt interface, not a web
  browser in a window.
- **REST and gRPC together.** Both kinds of request live in the same folders,
  use the same environments and capture values for each other.
- **Easy to move to.** Postman collections and environments, and OpenAPI and
  Swagger documents, are imported as folders of requests.
- **Free software.** It is licensed under the GPL, so it can be used, studied,
  changed and shared.

## Features

**REST**

- Send requests with any common HTTP method, query params, headers, basic or
  bearer authorization, and a JSON, text, XML or form body.
- Params are shown in the URL and kept in step with it both ways. Params,
  headers and form fields can each be switched off without being removed.
- A long URL is wrapped, with the field growing to show all of it.
- Opening a request, or changing its method, shows the tab it is likely to
  need: the body for POST, PUT and PATCH, and the params for the rest.
- A Format button beside the type of a JSON body sets the JSON out, one value
  to a line, leaving any `{{placeholders}}` in it as they are.
- Preview the URL with the environment's variables filled in, in place of the
  URL field. Each value is coloured, and a placeholder with no value is
  marked.
- Show any request as a `curl` command, with the environment's variables
  filled in, to copy and run in a terminal, or run it from TriggerAPI with the
  installed `curl` and see the response as usual.

**gRPC**

- Call unary and server-streaming methods, sending the message as JSON with
  metadata and basic or bearer authorization.
- The methods of a server are discovered using gRPC server reflection, so no
  protobuf files are needed, and an example message can be filled in for the
  method chosen.
- A Format button sets out the JSON of the message, as it does a REST body.

**Responses**

- The status, time, size and protocol of a response are shown beneath it,
  with failures in red, and its headers in a tab of their own.
- Capture values from a response into variables of the selected environment,
  such as a token from a login to use in later requests. The Capture tab of
  the response lists the variables to set, each with the path of its value in
  the JSON of the response, such as `data.items[0].id`, and optionally a
  regex to keep only part of that value: what the regex matches, or what its
  first group matches if it has one. A tick or a cross on the tab and against
  each variable shows whether it was captured.
- Right-click a value in a JSON response and choose Capture to add the
  variable for it, with its path filled in.
- JSON bodies, messages and responses are syntax highlighted, and their
  objects and arrays can be folded.
- Find text in a response with Ctrl+F, or the button beside its tabs: every
  match is marked and counted, and Enter and Shift+Enter go from one to the
  next.
- Save the body of a response to a file. JSON is saved formatted.

**Organising requests**

- Keep separate sets of environments and requests in workspaces, and switch
  between them.
- Save requests in folders, nested to any depth, and move them between
  folders by dragging. Folders and requests are listed alphabetically, and
  can be searched by name.
- Open a request without the mouse: Ctrl+P goes to the search, Return goes
  on to what was found, the arrow keys move through the folders and requests
  (left closes a folder and right opens one), and Return opens the request.
- Pin requests to keep them open in tabs, which are reopened the next time
  TriggerAPI is run.
- Duplicate a workspace, an environment, a request, or a folder with
  everything in it.
- Import an OpenAPI 3 or Swagger 2 document, as JSON or YAML, or a Postman
  collection, to create a folder of requests.

**Environments**

- Define environments of variables and use them anywhere in a request as
  `{{name}}` placeholders. Environments exported from Postman can be imported.
- Give each variable a description. Several variables can share a name, to
  keep the values it is switched between: switching one on switches the
  other off.
- Placeholders are coloured where they are used, differently if the selected
  environment does not define them.
- Give a variable a function as its value to have the value generated for
  each request. See [Functions](#functions).

**Settings**

- Choose the font used for request and response bodies and the folder the
  workspaces are kept in.
- Set how long a request can take before it is given up on, and whether a
  REST request follows redirects or shows the response that redirects.
- Choose the colours of the interface: the icons of the sidebar and of each
  type of request, the buttons, placeholders, errors, the capture marks, the
  matches of a search and JSON highlighting.
- The size and layout of the window are remembered.
- Escape closes the environment editor, the search of a response, the curl
  command and the URL preview.

## Functions

A variable whose value is one of these functions, and nothing else, is given a
freshly generated value each time a request is sent. Within one request the
value is the same everywhere the variable is used.

| Value | Generates | Example |
|---|---|---|
| `random_uuid()` | A random UUID | `3f2b8c1e-9a4d-4e7b-b1c6-5d8e2f0a7c94` |
| `iso_date()` | Today's date | `2026-10-11` |
| `iso_date(1)` | The date that many days from today | `2026-10-12` |
| `iso_timestamp()` | The time now, in UTC | `2026-10-11T15:04:05Z` |
| `iso_timestamp(2)` | The same time that many days from now | `2026-10-13T15:04:05Z` |
| `unix_timestamp()` | The time now, in seconds since 1970 | `1791731045` |
| `unix_timestamp(2)` | The same time that many days from now | `1791903845` |

The number of days can be negative, so `iso_date(-1)` is yesterday. Dates are
in the local time zone and timestamps in UTC.

For example, with a variable `request_id` set to `random_uuid()`, a header of
`X-Request-Id: {{request_id}}` carries a new UUID on every request.

A function is only recognised when it is the whole value: `id-random_uuid()`
is used as it is written. To put a generated value inside other text, do it
where the variable is used, as in `id-{{request_id}}`.

## Keyboard shortcuts

**Anywhere**

| Keys | Does |
|---|---|
| Ctrl+Return | Send the REST request or invoke the gRPC method. While one is in flight, cancel it. |
| Ctrl+P | Go to the search of the Requests tab, to find a request and open it. |
| Ctrl+F | Find text in the response. |
| Escape | Close the environment editor, or else the search of the response, the curl command or the URL preview. |
| Ctrl+, | Open the settings. |
| Ctrl+I | Show the about box. |
| Ctrl+Q | Quit. |

**Sending**

| Keys | Where | Does |
|---|---|---|
| Return | The URL of a REST request | Send the request, or cancel the one in flight. |
| Return | The server address of a gRPC request | Invoke the method, or cancel the call in flight. |

**Finding and opening a request**

| Keys | Where | Does |
|---|---|---|
| Return | The search box | Go to the tree, at the first request found. |
| Up, Down | The tree | Move to the item above or below, without opening it. |
| Left | The tree | Close the folder, or go to the folder an item is in. |
| Right | The tree | Open the folder, or go to the first item in it. |
| Return | The tree | Open the request, in the tab it is pinned in or else the tab that is not pinned. On a folder, open or close it. |

**Finding text in a response**

| Keys | Does |
|---|---|
| Return | Go to the next match. |
| Shift+Return | Go to the previous match. |
| Escape | Close the search. |

Editing text works as it does elsewhere on the system. Ctrl+Z, for one,
undoes the formatting of a JSON body. On macOS the Command key takes the
place of Ctrl.

## Requirements

TriggerAPI is developed and tested on Linux. It has not been tested on macOS yet,
and there is no macOS release, but the instructions below should build it
there.

Building needs:

- Go 1.27 or later
- A C++ compiler (GCC or Clang)
- `pkg-config`
- The Qt 6 base development files (Qt 6 Widgets)

### Linux

Install them with your distribution's package manager:

```sh
# Arch, CachyOS, Manjaro
sudo pacman -S go gcc pkgconf qt6-base

# Debian, Ubuntu
sudo apt install golang-go build-essential pkg-config qt6-base-dev

# Fedora
sudo dnf install golang gcc-c++ pkgconf-pkg-config qt6-qtbase-devel

# openSUSE
sudo zypper install go gcc-c++ pkgconf-pkg-config qt6-base-devel
```

If your distribution's Go is older than 1.27, install Go from
[go.dev/dl](https://go.dev/dl/) instead.

Running a built binary on another machine only needs the Qt 6 runtime
libraries (`qt6-base` on Arch, `libqt6widgets6` on Debian and Ubuntu,
`qt6-qtbase` on Fedora, `libQt6Widgets6` on openSUSE).

### macOS

Install the Xcode command line tools, which provide the Clang compiler, and
then the rest with [Homebrew](https://brew.sh):

```sh
xcode-select --install
brew install go pkgconf qt
```

The `qt` formula is Qt 6. If `pkg-config` cannot find it afterwards, tell it
where Homebrew put Qt:

```sh
export PKG_CONFIG_PATH="$(brew --prefix qt)/lib/pkgconfig:$PKG_CONFIG_PATH"
```

### Checking

On either system, check that Qt can be found with:

```sh
pkg-config --modversion Qt6Widgets
```

This should print a version starting with 6.

## Building

```sh
make build
```

This is the same as:

```sh
go build -ldflags "-s -w" -o triggerapi .
```

The `-s -w` flags build without the symbol table and debug information, which
roughly halves the size of the binary, to about 53 MB.

The first build compiles the whole Qt binding and takes around five minutes,
printing a lot of compiler warnings from the Qt headers which can be ignored.
Later builds use the Go build cache and take a few seconds.

## gRPC

A gRPC request needs a server with
[server reflection](https://grpc.io/docs/guides/reflection/) turned on. Enter
its address as `host:port` and the methods it has are loaded into the method
list. Tick TLS, or use a `grpcs://` or `https://` address, for a server that
uses TLS.

Unary and server-streaming methods are supported. The responses of a streaming
method are shown together once the call ends or is cancelled. Loading service
definitions from `.proto` files is not supported yet.

## Running

```sh
./triggerapi
```

TriggerAPI keeps its data as JSON files.

The settings are kept where the system keeps the configuration of programs:

| System | Settings file |
| --- | --- |
| Linux | `~/.config/triggerapi/settings.json` (or under `$XDG_CONFIG_HOME`) |
| macOS | `~/Library/Application Support/triggerapi/settings.json` |

They hold the window size and layout, the code font, colours, the
workspaces folder, the workspace in use and the pinned requests.

The workspaces are kept, by default, where the system keeps the data of
programs:

| System | Workspaces folder |
| --- | --- |
| Linux | `~/.local/share/triggerapi/workspaces` (or under `$XDG_DATA_HOME`) |
| macOS | `~/Library/Application Support/triggerapi/workspaces` |

| Path in it | Contents |
| --- | --- |
| `<name>/environments.json` | The environments of a workspace and their variables |
| `<name>/requests.json` | The saved requests of a workspace and their folders |

Each workspace has its own environments and requests, and is a folder named
after it inside the workspaces folder. Workspaces are added, renamed and
removed in the Workspaces tab of the sidebar; the selected one is the one in
use. The workspaces folder can be kept elsewhere, such as a folder that is
synced or shared, by choosing its location in File > Settings.

Earlier versions kept the workspaces in a `workspaces` folder in the directory
TriggerAPI was run from. The first time this version is run from that
directory, with no workspaces yet in the new place, they are copied there and
left where they were.

Changes are saved as they are made. Passwords and tokens entered in requests
and environments are stored in these files as plain text.

## Testing

```sh
make test
```

The interface tests run Qt offscreen, so they do not need a display.

## License

TriggerAPI is free software, licensed under the
[GNU General Public License, version 3](LICENSE).

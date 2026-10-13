# TriggerAPI

A desktop client for making REST API and gRPC calls. It is written
in Go with a Qt 6 interface using the [miqt](https://github.com/mappu/miqt)
bindings.

![TriggerAPI showing a REST request with a JSON body, its JSON response and a value captured from it](docs/screenshot.png)

## Why TriggerAPI

- **No account, no cloud.** There is nothing to sign in to and nothing is
  synchronised anywhere. TriggerAPI works the same with no network at all.
- **No telemetry.** The only requests it makes are the ones you send.
- **Your data is yours.** Requests, environments and settings are files in
  ordinary folders, which you can back up, copy to another machine or keep in
  a folder that is synced. See [Running](#running) for where they are.
- **Encrypted on disk.** Requests and environments hold tokens and passwords,
  so they are encrypted with your own SSH keys, and a workspace is shared by
  encrypting it for the other person's public key. There is no key of
  TriggerAPI's and no service in the middle. See [Encryption](#encryption).
- **Native.** It is a single compiled program with a Qt interface, not a web
  browser in a window.
- **REST and gRPC together.** Both kinds of request live in the same folders,
  use the same environments and capture values for each other.
- **A whole gRPC server as requests, in two clicks.** TriggerAPI asks the
  server what it has, lists every endpoint, and makes a saved request of each
  one you tick, in a folder for each service. There are no protobuf files to
  find and nothing to type. When the server gains endpoints, open one of
  those requests and add the new ones to the same folders. See
  [Creating requests from a server](#creating-requests-from-a-server).
- **Nothing left unfilled by accident.** A `{{placeholder}}` the environment
  does not define, or defines as nothing, is red wherever it is used, before
  the request is sent.
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
- The Service Definitions tab lists the endpoints of the server under their
  services, each with a checkbox. Create Requests makes a folder of saved
  requests from those that are ticked, and Update Requests adds more to a
  folder made that way. See
  [Creating requests from a server](#creating-requests-from-a-server).

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
- Save the body of a response to a file. JSON is saved formatted. The name
  offered for the file is that of the request and the time, such as
  `book_room_20261011_173005_resp.json`, in lower case with underscores for
  anything that is not a letter or a digit.

**Organising requests**

- Keep separate sets of environments and requests in workspaces, and switch
  between them.
- The workspace and the environment in use are shown above the request, in
  drop-downs which switch to another, with a button beside the environment
  to edit it.
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
- Placeholders are coloured wherever they are used, the password of basic
  authorization included, which is shown as it is typed: green if the
  selected environment gives them a value, and red if it does not define
  them or defines them as nothing.
- Give a variable a function as its value to have the value generated for
  each request. See [Functions](#functions).

**Settings**

- Choose the font used for request and response bodies and the folder the
  workspaces are kept in.
- Set how long a request can take before it is given up on, and whether a
  REST request follows redirects or shows the response that redirects.
- Choose the colours of the interface, which are listed in sections: the
  icons of the sidebar and of each type of request, the buttons, placeholders,
  errors, the capture marks, the matches of a search and JSON highlighting.
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
| Ctrl+1, Ctrl+2, Ctrl+3 | Go to the Workspaces, Environments or Requests tab of the sidebar, with the keyboard in its list. |
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

**The sidebar**

| Keys | Where | Does |
|---|---|---|
| F2 | A workspace, a folder or a request | Rename it, as the rename button does. |
| F2 | An environment | Edit it, as the edit button does, with the keyboard in its name. |

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

## AppImage

Releases for Linux are an [AppImage](https://appimage.org): one file with
TriggerAPI and the Qt libraries it needs inside it, which runs without
installing anything.

```sh
chmod +x TriggerAPI-*-x86_64.AppImage
./TriggerAPI-*-x86_64.AppImage
```

It is built by a GitHub workflow (`.github/workflows/appimage.yml`), which runs
`packaging/appimage.sh` on Ubuntu 22.04 with Qt's own build of Qt 6.9:

- Pushing a tag that starts with `v` builds it and attaches it to a release of
  that name: `git tag v1.0.0 && git push origin v1.0.0`.
- It can also be run by hand from the Actions tab of the repository (AppImage >
  Run workflow), which leaves the AppImage as an artifact of the run to
  download.

It runs under both X11 and Wayland, using Wayland where the session is one.
To use the other, set `QT_QPA_PLATFORM` to `xcb` or `wayland` when starting
it.

On GNOME it takes its colours, light or dark, from the desktop, through Qt's
GTK theme plugin, which is in the AppImage. GTK itself is not, and is the
desktop's own.

The C library of the system (glibc) is not put in the AppImage, as it cannot
be reliably: it has to match the system's graphics drivers. It is built on an
old distribution instead, and runs on any with glibc 2.35 or later (Ubuntu
22.04, Debian 12, Fedora 36 and anything newer, or rolling). `curl`, which the
Execute button of the curl command uses, is not in it either.

`make appimage` runs the same script locally, but an AppImage built on a newer
system only runs on systems at least as new.

## gRPC

A gRPC request needs a server with
[server reflection](https://grpc.io/docs/guides/reflection/) turned on. Enter
its address as `host:port` and the methods it has are loaded into the method
list. Tick TLS, or use a `grpcs://` or `https://` address, for a server that
uses TLS.

Unary and server-streaming methods are supported. The responses of a streaming
method are shown together once the call ends or is cancelled. Loading service
definitions from `.proto` files is not supported yet.

### Creating requests from a server

The Service Definitions tab of a gRPC request lists every endpoint of the
server, by name, under a heading for each service.

1. Tick the endpoints you want, or use Select All.
2. Click Create Requests.

A folder named after the server address is added to the Requests tab, with a
folder inside it for each service and a request in that for each endpoint
ticked. Each request has the server address and its method, and no message:
open it and use Use Example Message to fill one in.

To add endpoints later, open any request in those folders. The endpoints
there are requests for already are ticked and cannot be unticked. Tick
others and click Update Requests, and they are added to the same folders.
Only the folders the open request is in are changed, and requests are only
removed in the sidebar.

Which endpoints there are requests for is worked out from the names of the
service folders and the method each request calls, so the outer folder and
the requests can be renamed freely.

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
workspaces folder, the encryption keys, the workspace in use and the pinned
requests.

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

Changes are saved as they are made. The files of the workspaces are encrypted,
and they and the settings file can be read and written only by the user who
runs TriggerAPI.

### Encryption

The environments and requests of every workspace are encrypted with a pair
of SSH keys, using [age](https://github.com/FiloSottile/age). Give the files of
the public and the private key (Ed25519 or RSA, such as
`~/.ssh/id_ed25519.pub` and `~/.ssh/id_ed25519`) under Encryption in the
Workspaces tab of File > Settings. The settings hold the paths of the keys, not
the keys.

- TriggerAPI needs the keys to work. Without them, as on the first run, it
  says so, shows every workspace empty and saves nothing.
- Naming the keys encrypts the files of every workspace, including any from
  before there was encryption, and everything saved from then on.
- Naming another pair encrypts every file again with those.
- Taking the keys away leaves the files encrypted as they are, and nothing is
  loaded or saved until the keys are put back.
- If the private key has a passphrase it is asked for when TriggerAPI starts.
  The SSH agent is not used.
- If the keys cannot be opened, or are not the ones a file was encrypted
  with, the file is not read and is never written over.

The settings file itself is not encrypted. Keep a copy of the private key:
without it the files cannot be recovered.

### Sharing workspaces

Workspaces are given to someone else, and taken from them, under Import and
export in the Workspaces tab of File > Settings.

- **Export** asks for the file of the other person's public SSH key and for
  where to save, and writes every workspace to a zip file with its
  environments and requests encrypted for that key. Only their private key can
  read it, so not even the keys it was exported with can. Your own files are
  left as they are.
- **Import** takes such a zip file, which has to have been exported for your
  public key, and adds its workspaces to the workspaces folder. A workspace
  whose name is taken is given a number after it, so nothing is replaced. A
  file that is not an export, or cannot be read with your keys, adds nothing.

In the zip file each workspace is a folder named after it, holding its
`environments.json` and `requests.json`.

## Testing

```sh
make test
```

The interface tests run Qt offscreen, so they do not need a display.

## License

TriggerAPI is free software, licensed under the
[GNU General Public License, version 3](LICENSE).

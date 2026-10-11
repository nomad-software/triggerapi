# TriggerAPI

A desktop client for making REST API and gRPC calls. It is written
in Go with a Qt 6 interface using the [miqt](https://github.com/mappu/miqt)
bindings.

![TriggerAPI showing a REST request, its JSON response and a value captured from it](docs/screenshot.png)

## Features

**REST**

- Send requests with any common HTTP method, query params, headers, basic or
  bearer authorization, and a JSON, text, XML or form body.
- Params are shown in the URL and kept in step with it both ways. Params,
  headers and form fields can each be switched off without being removed.
- A long URL is wrapped, with the field growing to show all of it.
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

**Responses**

- The status, time, size and protocol of a response are shown beneath it,
  with failures in red, and its headers in a tab of their own.
- Capture values from a response into variables of the selected environment,
  such as a token from a login to use in later requests. The Capture tab of
  the response lists the variables to set, each with the path of its value in
  the JSON of the response, such as `data.items[0].id`. A tick or a cross on
  the tab and against each variable shows whether it was captured.
- JSON bodies, messages and responses are syntax highlighted, and their
  objects and arrays can be folded.
- Save the body of a response to a file. JSON is saved formatted.

**Organising requests**

- Keep separate sets of environments and requests in workspaces, and switch
  between them.
- Save requests in folders, nested to any depth, and move them between
  folders by dragging. Folders and requests are listed alphabetically, and
  can be searched by name.
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

**Settings**

- Choose the font used for request and response bodies and the folder the
  workspaces are kept in.
- Choose the colours of the interface: the icons of the sidebar and of each
  type of request, the buttons, placeholders, errors, the capture marks and
  JSON highlighting.
- The size and layout of the window are remembered.
- Escape closes the environment editor, the curl command and the URL
  preview.

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

They hold the window size and layout, the response font, colours, the
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

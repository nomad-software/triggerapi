# TriggerAPI

A desktop client for making REST API and gRPC calls, inspired by Postman. It is written
in Go with a Qt 6 interface using the [miqt](https://github.com/mappu/miqt)
bindings.

- Send requests with any common HTTP method, query params, headers, basic or
  bearer authorization, and a JSON, text, XML or form body.
- Show any REST request as a `curl` command, with the environment's variables
  filled in, to copy and run in a terminal, or run it from TriggerAPI with the
  installed `curl` and see the response as usual.
- Call gRPC methods, sending the message as JSON with metadata and basic or
  bearer authorization. The methods of a server are discovered using gRPC
  server reflection, so no protobuf files are needed.
- JSON bodies, messages and responses are syntax highlighted, and their
  objects and arrays can be folded.
- Save requests in folders, nested to any depth, and move them between
  folders by dragging. Folders and requests are listed alphabetically, and
  can be searched by name.
- Import an OpenAPI 3 or Swagger 2 document, as JSON or YAML, or a Postman
  collection, to create a folder of requests.
- Define environments of variables and use them anywhere in a request as
  `{{name}}` placeholders. Environments exported from Postman can be imported.

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

TriggerAPI keeps its data as JSON files, by default in the directory it is run
from:

| File | Contents |
| --- | --- |
| `environments.json` | Environments and their variables |
| `requests.json` | Saved requests and their folders |
| `settings.json` | Window size and layout, the response font and file locations |

The environments and requests files can be kept elsewhere, such as a folder
that is synced or shared, by choosing their locations in File > Settings.
`settings.json` always stays in the directory TriggerAPI is run from.

Changes are saved as they are made. Passwords and tokens entered in requests
and environments are stored in these files as plain text.

## Testing

```sh
make test
```

The interface tests run Qt offscreen, so they do not need a display.

# Postie

A desktop client for making REST API and gRPC calls, inspired by Postman. It is written
in Go with a Qt 6 interface using the [miqt](https://github.com/mappu/miqt)
bindings.

- Send requests with any common HTTP method, query params, headers, basic or
  bearer authorization, and a JSON, text, XML or form body.
- Call gRPC methods, sending the message as JSON with metadata and basic or
  bearer authorization. The methods of a server are discovered using gRPC
  server reflection, so no protobuf files are needed.
- JSON bodies, messages and responses are syntax highlighted, and their
  objects and arrays can be folded.
- Save requests in folders, nested to any depth, and rearrange them by
  dragging.
- Define environments of variables and use them anywhere in a request as
  `{{name}}` placeholders.

## Requirements

Postie currently builds and runs on Linux. A macOS release is planned but not
yet supported.

Building needs:

- Go 1.27 or later
- A C++ compiler (GCC or Clang)
- `pkg-config`
- The Qt 6 base development files (Qt 6 Widgets)

Install them with your distribution's package manager:

```sh
# Arch, CachyOS, Manjaro
sudo pacman -S go gcc pkgconf qt6-base

# Debian, Ubuntu
sudo apt install golang-go build-essential pkg-config qt6-base-dev

# Fedora
sudo dnf install golang gcc-c++ pkgconf-pkg-config qt6-qtbase-devel
```

Check that Qt can be found with:

```sh
pkg-config --modversion Qt6Widgets
```

Running a built binary on another machine only needs the Qt 6 runtime
libraries (`qt6-base` on Arch, `libqt6widgets6` on Debian and Ubuntu,
`qt6-qtbase` on Fedora).

## Building

```sh
make build
```

This is the same as:

```sh
go build -ldflags "-s -w" -o postie .
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
./postie
```

Postie keeps its data as JSON files, by default in the directory it is run
from:

| File | Contents |
| --- | --- |
| `environments.json` | Environments and their variables |
| `requests.json` | Saved requests and their folders |
| `settings.json` | Window size and layout, the response font and file locations |

The environments and requests files can be kept elsewhere, such as a folder
that is synced or shared, by choosing their locations in File > Settings.
`settings.json` always stays in the directory Postie is run from.

Changes are saved as they are made. Passwords and tokens entered in requests
and environments are stored in these files as plain text.

## Testing

```sh
make test
```

The interface tests run Qt offscreen, so they do not need a display.

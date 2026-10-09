# Postie

A desktop client for making REST API calls, inspired by Postman. It is written
in Go with a Qt 6 interface using the [miqt](https://github.com/mappu/miqt)
bindings.

- Send requests with any common HTTP method, query params, headers, basic or
  bearer authorization, and a JSON, text, XML or form body.
- Save requests in folders.
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
reduces the binary from about 108 MB to about 34 MB.

The first build compiles the whole Qt binding and takes around five minutes,
printing a lot of compiler warnings from the Qt headers which can be ignored.
Later builds use the Go build cache and take a few seconds.

## Running

```sh
./postie
```

Postie keeps its data as JSON files in the directory it is run from:

| File | Contents |
| --- | --- |
| `environments.json` | Environments and their variables |
| `requests.json` | Saved requests and their folders |
| `settings.json` | Window size and the response font |

Changes are saved as they are made. Passwords and tokens entered in requests
and environments are stored in these files as plain text.

## Testing

```sh
make test
```

The interface tests run Qt offscreen, so they do not need a display.

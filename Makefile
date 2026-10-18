BINARY := triggerapi

# -s -w leaves out the symbol table and debug information, which roughly
# halves the size of the binary.
LDFLAGS := -s -w

.PHONY: build run test vet screenshot icons mcp-server rest-server grpc-server appimage macos clean

build:
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) .

run: build
	./$(BINARY)

test:
	go test ./...

vet:
	go vet ./...

# Draws the window with sample requests into the screenshot of the README.
# Run it again whenever what the window looks like changes.
screenshot:
	TRIGGERAPI_SCREENSHOT=$(CURDIR)/docs/screenshot.png go test -timeout 90s -run '^TestScreenshot$$' -count=1 ./internal/ui/

# Draws the program's icon into the files the packages are made from:
# packaging/triggerapi.png and packaging/macos/triggerapi.png. Run it again
# whenever the icon changes.
icons:
	TRIGGERAPI_ICONS=$(CURDIR)/packaging go test -timeout 90s -run '^TestWriteIcons$$' -count=1 ./internal/ui/

# Starts the sample MCP server at http://localhost:8000/mcp, to try MCP
# requests against by hand. Options go in ARGS, as in
# make mcp-server ARGS="-token secret" or ARGS="-legacy".
mcp-server:
	go run ./test/mcpserver $(ARGS)

# Starts the sample REST server at http://localhost:8001, which lists its
# endpoints at /, to try streams of server-sent events against by hand.
# Options go in ARGS, as in make rest-server ARGS="-addr localhost:9001".
rest-server:
	go run ./test/restserver $(ARGS)

# Starts the sample gRPC server at localhost:50051, without TLS, which has a
# method of every kind, to try gRPC requests against by hand. Options go in
# ARGS, as in make grpc-server ARGS="-addr localhost:9090".
grpc-server:
	go run ./test/grpcserver $(ARGS)

# Packs the program and the Qt libraries it uses into one file that runs
# without installing anything. Releases are made by the GitHub workflow, on
# an old distribution so that they run on more systems.
appimage:
	packaging/appimage.sh

# Builds the macOS app for Apple Silicon, with Qt inside it, in a disk image.
# It runs on a Mac with Homebrew's Qt; releases are made by the GitHub
# workflow.
macos:
	packaging/macos.sh

clean:
	rm -f $(BINARY)
	rm -rf build

BINARY := triggerapi

# -s -w leaves out the symbol table and debug information, which roughly
# halves the size of the binary.
LDFLAGS := -s -w

.PHONY: build run test bench vet screenshot icons mcp-server rest-server grpc-server appimage clean

build:
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) .

run: build
	./$(BINARY)

test:
	go test -timeout 120s ./...

# Measures the work of the user interface that costs the most: making the
# window, drawing the request tree and the history, searching the tree,
# showing a large JSON response, switching requests and drawing icons.
# Compare runs with benchstat (golang.org/x/perf/cmd/benchstat).
bench:
	go test -timeout 600s -run '^$$' -bench . -benchmem -count 5 ./internal/ui

vet:
	go vet ./...

# Draws the window with sample requests into the screenshot of the README.
# Run it again whenever what the window looks like changes.
screenshot:
	TRIGGERAPI_SCREENSHOT=$(CURDIR)/docs/screenshot.png go test -timeout 90s -run '^TestScreenshot$$' -count=1 ./internal/ui/

# Draws the program's icon into internal/appicon/appicon.png, which is built
# into the program. Run it again whenever the icon changes.
icons:
	TRIGGERAPI_ICONS=$(CURDIR) go test -timeout 90s -run '^TestWriteIcons$$' -count=1 ./internal/ui/

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
# without installing anything. It is built in /tmp, and only the AppImage is
# left here. Releases are made by the GitHub workflow, on an old
# distribution so that they run on more systems.
appimage:
	packaging/appimage.sh

clean:
	rm -f $(BINARY) *.AppImage

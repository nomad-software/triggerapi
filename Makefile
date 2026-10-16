BINARY := triggerapi

# -s -w leaves out the symbol table and debug information, which roughly
# halves the size of the binary.
LDFLAGS := -s -w

.PHONY: build run test vet screenshot mcp-server appimage clean

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

# Starts the sample MCP server at http://localhost:8000/mcp, to try MCP
# requests against by hand. Options go in ARGS, as in
# make mcp-server ARGS="-token secret" or ARGS="-legacy".
mcp-server:
	go run ./test/mcpserver $(ARGS)

# Packs the program and the Qt libraries it uses into one file that runs
# without installing anything. Releases are made by the GitHub workflow, on
# an old distribution so that they run on more systems.
appimage:
	packaging/appimage.sh

clean:
	rm -f $(BINARY)
	rm -rf build

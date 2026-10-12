BINARY := triggerapi

# -s -w leaves out the symbol table and debug information, which roughly
# halves the size of the binary.
LDFLAGS := -s -w

.PHONY: build run test vet appimage clean

build:
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) .

run: build
	./$(BINARY)

test:
	go test ./...

vet:
	go vet ./...

# Packs the program and the Qt libraries it uses into one file that runs
# without installing anything. Releases are made by the GitHub workflow, on
# an old distribution so that they run on more systems.
appimage:
	packaging/appimage.sh

clean:
	rm -f $(BINARY)
	rm -rf build

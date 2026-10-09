BINARY := postie

# -s -w leaves out the symbol table and debug information, which takes the
# binary from about 108 MB to about 34 MB.
LDFLAGS := -s -w

.PHONY: build run test vet clean

build:
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) .

run: build
	./$(BINARY)

test:
	go test ./...

vet:
	go vet ./...

clean:
	rm -f $(BINARY)

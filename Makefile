BIN := dist/xuz
DIST := dist

# Cross-compilation targets: make <target> (or `make all` for both)
#   arm64-darwin  -> macOS on Apple Silicon (aarch64)
#   x86_64-linux  -> Linux on x86-64 (Intel/AMD)
TARGETS := arm64-darwin x86_64-linux

.PHONY: build test smoke install clean all $(TARGETS)

build:
	go build -o $(BIN) ./cmd/xuz

test: build
	go test ./...
	python3 scripts/pty_smoke.py $(BIN)

smoke: build
	python3 scripts/pty_smoke.py $(BIN)

install:
	sh install.sh

# Build a release binary for a specific target into dist/.
all: $(TARGETS)

arm64-darwin:
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags "-s -w" \
		-o $(DIST)/xuz-darwin-arm64 ./cmd/xuz

x86_64-linux:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "-s -w" \
		-o $(DIST)/xuz-linux-amd64 ./cmd/xuz

clean:
	rm -rf dist

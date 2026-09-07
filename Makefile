BIN := dist/xuz

.PHONY: build test smoke install clean

build:
	go build -o $(BIN) ./cmd/xuz

test: build
	go test ./...
	python3 scripts/pty_smoke.py $(BIN)

smoke: build
	python3 scripts/pty_smoke.py $(BIN)

install:
	sh install.sh

clean:
	rm -rf dist

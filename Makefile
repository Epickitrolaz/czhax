BINARY := czhax
PKG := ./cmd/czhax
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo 0.1.0-phase1)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: all build build-arm64 build-armv7 build-amd64 build-all install test run run-cz run-cz45 clean

all: test build

test:
	go test -v ./...

build:
	mkdir -p dist
	go build -ldflags="$(LDFLAGS)" -o dist/$(BINARY) $(PKG)

build-arm64:
	mkdir -p dist
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -ldflags="$(LDFLAGS)" -o dist/$(BINARY)-linux-arm64 $(PKG)

build-armv7:
	mkdir -p dist
	GOOS=linux GOARCH=arm GOARM=7 CGO_ENABLED=0 go build -ldflags="$(LDFLAGS)" -o dist/$(BINARY)-linux-armv7 $(PKG)

build-amd64:
	mkdir -p dist
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -ldflags="$(LDFLAGS)" -o dist/$(BINARY)-linux-amd64 $(PKG)

build-all: build-arm64 build-armv7 build-amd64
	cd dist && sha256sum $(BINARY)-linux-* > checksums.txt

install: build
	install -m 0755 dist/$(BINARY) /usr/local/bin/$(BINARY)

run:
	go run $(PKG)

run-cz:
	go run $(PKG) --cz

run-cz45:
	go run $(PKG) --cz45

clean:
	rm -rf dist

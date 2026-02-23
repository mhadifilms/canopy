BINARY_NAME := canopyd
MODULE := github.com/canopy-dev/canopyd
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -ldflags "-X $(MODULE)/internal/config.Version=$(VERSION)"

.PHONY: all build build-arm64 build-amd64 test lint clean

all: build

build:
	go build $(LDFLAGS) -o bin/$(BINARY_NAME) ./cmd/canopyd

build-arm64:
	GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -o bin/$(BINARY_NAME)-darwin-arm64 ./cmd/canopyd

build-amd64:
	GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -o bin/$(BINARY_NAME)-darwin-amd64 ./cmd/canopyd

build-all: build-arm64 build-amd64

test:
	go test -race -cover ./...

lint:
	go vet ./...

clean:
	rm -rf bin/
	go clean -testcache

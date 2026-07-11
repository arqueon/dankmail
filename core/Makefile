# Go core build for dankmail (dmail).
BINARY_NAME=dmail
BUILD_DIR=bin

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
BUILD_TIME ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS=-s -w -X main.Version=$(VERSION) -X main.Commit=$(COMMIT) -X main.BuildTime=$(BUILD_TIME)

.PHONY: all build dev clean test cover fmt vet check-i18n deps generate

all: build

build:
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/dmail

dev:
	go build -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/dmail

clean:
	rm -rf $(BUILD_DIR)

test: check-i18n
	go test ./...

# English is the source language; Spanish belongs only in the i18n layers.
check-i18n:
	./scripts/check-i18n.sh

cover:
	go test -coverprofile=coverage.out ./... && go tool cover -func=coverage.out

fmt:
	gofmt -w .

vet:
	go vet ./...

deps:
	go mod tidy

generate:
	go generate ./ent/...

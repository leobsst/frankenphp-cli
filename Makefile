# Go build of FrankenManager. Every target builds with CGO_ENABLED=0, so the
# 7 release binaries cross-compile from any machine.

BINARY  := frankenmanager
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null | sed 's/^v//')
OUT     ?= bin
# The golangci-lint of the CI, run without installing it
GOLANGCI_LINT := go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0
PKG     := github.com/leobsst/frankenphp-cli
LDFLAGS := -s -w -X $(PKG)/internal/version.Version=$(VERSION)
GOFLAGS := -trimpath -ldflags '$(LDFLAGS)'

# os/arch[/arm-version]
TARGETS := darwin/amd64 darwin/arm64 \
           linux/amd64 linux/arm64 linux/arm/7 \
           windows/amd64 windows/arm64

export CGO_ENABLED := 0

.PHONY: build build-all test lint hooks clean

## build: native binary in $(OUT)/frankenmanager
build:
	go build $(GOFLAGS) -o $(OUT)/$(BINARY) ./cmd/frankenmanager

## build-all: the 7 release targets in $(OUT)/frankenmanager-<os>-<arch>[.exe]
build-all:
	@set -e; for t in $(TARGETS); do \
		os=$$(echo $$t | cut -d/ -f1); arch=$$(echo $$t | cut -d/ -f2); arm=$$(echo $$t | cut -d/ -f3); \
		name=$(BINARY)-$$os-$$arch$${arm:+v$$arm}; [ $$os = windows ] && name=$$name.exe; \
		echo "-> $$name"; \
		GOOS=$$os GOARCH=$$arch GOARM=$$arm go build $(GOFLAGS) -o $(OUT)/$$name ./cmd/frankenmanager; \
	done

## test: Go tests (no network, no Docker, no real data directory)
test:
	go test ./...

## lint: golangci-lint (configuration in .golangci.yml)
lint:
	$(GOLANGCI_LINT) run

## hooks: enable the pre-commit hook of .githooks (lint before each commit)
hooks:
	git config core.hooksPath .githooks

## clean: move $(OUT) to the trash when `trash` exists, delete it otherwise
clean:
	@if command -v trash >/dev/null; then trash $(OUT); else rm -rf $(OUT); fi

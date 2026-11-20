# goto — a keyboard launcher for the terminal.
#
# Single command surface for building, testing and installing gt.
# Run `make help` for targets.

SHELL := /bin/bash
.SHELLFLAGS := -eu -o pipefail -c
.DEFAULT_GOAL := help

BIN     := bin/gt
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

# Where `make install` puts gt. Defaults to Go's own install directory;
# override with e.g. `make install BINDIR=/usr/local/bin`.
BINDIR ?= $(or $(shell go env GOBIN),$(shell go env GOPATH)/bin)

.PHONY: help build run install uninstall test lint docs fmt tidy clean

help:
	@printf 'goto — a keyboard launcher for the terminal\n\n'
	@printf 'Usage: make <target>\n\n'
	@printf '  help        Show this help (default).\n'
	@printf '  build       Build %s.\n' '$(BIN)'
	@printf '  run         Build and open goto.\n'
	@printf '  install     Build and copy gt to BINDIR (now: %s).\n' '$(BINDIR)'
	@printf '  uninstall   Remove gt from BINDIR.\n'
	@printf '  test        Run all tests with the race detector.\n'
	@printf '  lint        Check formatting (gofmt) and run go vet.\n'
	@printf '  docs        Regenerate docs/configuration.md from the setting declarations.\n'
	@printf '  fmt         Format all Go code.\n'
	@printf '  tidy        Tidy go.mod and go.sum.\n'
	@printf '  clean       Remove build output.\n'

build:
	@go build -ldflags '$(LDFLAGS)' -o '$(BIN)' ./cmd/gt
	@printf '→ built %s (%s)\n' '$(BIN)' '$(VERSION)'

run: build
	@./$(BIN)

install: build
	@mkdir -p '$(BINDIR)'
	@install -m 0755 '$(BIN)' '$(BINDIR)/gt'
	@printf '→ installed %s/gt\n' '$(BINDIR)'
	@case ":$$PATH:" in *":$(BINDIR):"*) ;; *) printf '  note: %s is not on your PATH yet\n' '$(BINDIR)' ;; esac

uninstall:
	@rm -f '$(BINDIR)/gt'
	@printf '→ removed %s/gt\n' '$(BINDIR)'

test:
	@go test -race ./...

lint:
	@unformatted=$$(gofmt -l .); \
	if [[ -n "$$unformatted" ]]; then printf 'needs gofmt:\n%s\n' "$$unformatted"; exit 1; fi
	@go vet ./...

docs:
	@go test ./cmd/gt -run '^TestConfigDocsUpToDate$$' -count=1 -args -update
	@printf '→ wrote docs/configuration.md\n'

fmt:
	@gofmt -w .

tidy:
	@go mod tidy

clean:
	@rm -rf bin

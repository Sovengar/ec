# Dev shortcuts. After any change: `make install` (the binary lives in $(PREFIX)/bin).

SHELL := /bin/bash

PREFIX ?= $(HOME)/.local
BINDIR := $(PREFIX)/bin
BIN    := bin/ec
PKG    := .
# This machine's /usr/bin/go is a custom build that clashes with the mise GOROOT.
GO_BIN ?= $(shell mise which go 2>/dev/null || command -v go)
GO     ?= env -u GOROOT $(GO_BIN)

# Pinned so local and CI run the same linter (needs Go 1.27 export data support).
GOLANGCI_LINT_VERSION := v2.15.0

.DEFAULT_GOAL := help
.PHONY: help build install uninstall fmt fmt-check vet lint test check all run fixtures smoke tidy clean

help: ## Shows this help
	@grep -hE '^[a-zA-Z0-9_-]+:.*?## ' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-11s\033[0m %s\n", $$1, $$2}'

build: ## Builds the binary at bin/ec
	$(GO) build -o $(BIN) $(PKG)

install: build ## Installs bin/ec into PREFIX/bin (default ~/.local/bin; mandatory after changes)
	install -d $(DESTDIR)$(BINDIR)
	install -m 0755 $(BIN) $(DESTDIR)$(BINDIR)/ec

uninstall: ## Deletes the installed binary from PREFIX/bin
	rm -f $(DESTDIR)$(BINDIR)/ec

fmt: ## Runs gofmt over the tree
	gofmt -w $$(find . -name '*.go' -not -path './bin/*')

fmt-check: ## Checks gofmt without writing
	@out="$$(gofmt -l $$(find . -name '*.go' -not -path './bin/*'))"; \
	if [ -n "$$out" ]; then echo "gofmt pending in:"; echo "$$out"; exit 1; fi

vet: ## go vet
	$(GO) vet ./...

lint: fmt-check ## gofmt + golangci-lint (pinned, always via go run)
	$(GO) run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION) run

test: ## Runs the suite with -race
	$(GO) test -race -count=1 ./...

check: build lint test ## build + lint + test

all: check vet ## check + vet

run: ## Starts the resolver against the current repo
	$(GO) run .

fixtures: ## Regenerates testdata/playground (a repo with many conflicts)
	./scripts/gen-fixtures.sh

smoke: build ## TUI smoke test in tmux against testdata/playground
	@test -d testdata/playground/.git || { echo "testdata/playground is missing - run 'make fixtures'"; exit 1; }
	@tmux kill-session -t ec-smoke 2>/dev/null || true; \
	tmux new-session -d -s ec-smoke -x 200 -y 50 "cd $(CURDIR)/testdata/playground && $(CURDIR)/$(BIN)"; \
	sleep 2; tmux capture-pane -t ec-smoke -p | head -20; \
	tmux send-keys -t ec-smoke Enter; sleep 1; \
	tmux capture-pane -t ec-smoke -p | head -20; \
	tmux kill-session -t ec-smoke 2>/dev/null || true

tidy: ## go mod tidy
	$(GO) mod tidy

clean: ## Deletes the build artefacts in bin/
	rm -rf bin

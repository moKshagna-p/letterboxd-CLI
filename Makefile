.PHONY: build test run-cli run-tui run-app ui install-local

GOCACHE ?= $(PWD)/.gocache
GOMODCACHE ?= $(PWD)/.gomodcache

build:
	GOCACHE=$(GOCACHE) GOMODCACHE=$(GOMODCACHE) go build ./...

test:
	GOCACHE=$(GOCACHE) GOMODCACHE=$(GOMODCACHE) go test ./...

run-cli:
	GOCACHE=$(GOCACHE) GOMODCACHE=$(GOMODCACHE) go run ./cmd/cli

run-tui:
	GOCACHE=$(GOCACHE) GOMODCACHE=$(GOMODCACHE) go run ./cmd/tui

run-app:
	GOCACHE=$(GOCACHE) GOMODCACHE=$(GOMODCACHE) go run ./cmd/letterboxd-tui

ui:
	./letterboxd-tui ui

install-local:
	mkdir -p ./bin
	GOCACHE=$(GOCACHE) GOMODCACHE=$(GOMODCACHE) go build -o ./bin/letterboxd-tui ./cmd/letterboxd-tui
	GOCACHE=$(GOCACHE) GOMODCACHE=$(GOMODCACHE) go build -o ./bin/film-heatmap ./cmd/film-heatmap
	GOCACHE=$(GOCACHE) GOMODCACHE=$(GOMODCACHE) go build -o ./bin/heatmap ./cmd/heatmap
	GOCACHE=$(GOCACHE) GOMODCACHE=$(GOMODCACHE) go build -o ./bin/tui ./cmd/tui
	@echo "Built ./bin/letterboxd-tui, ./bin/film-heatmap, ./bin/heatmap, and ./bin/tui"
	@echo "Add to PATH for this shell: export PATH=\"$$PWD/bin:$$PATH\""

.PHONY: build test run-cli run-tui run-app install-local

build:
	go build ./...

test:
	go test ./...

run-cli:
	go run ./cmd/cli

run-tui:
	go run ./cmd/tui

run-app:
	go run ./cmd/film-heatmap

install-local:
	mkdir -p ./bin
	go build -o ./bin/film-heatmap ./cmd/film-heatmap
	go build -o ./bin/heatmap ./cmd/heatmap
	@echo "Built ./bin/film-heatmap and ./bin/heatmap"
	@echo "Add to PATH for this shell: export PATH=\"$$PWD/bin:$$PATH\""

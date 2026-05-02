.PHONY: build run test lint cover tidy

BIN ?= bin/ripjira
PKG := ./...

build:
	@mkdir -p $(dir $(BIN))
	go build -o $(BIN) ./cmd/ripjira

run: build
	$(BIN)

test:
	go test $(PKG)

lint:
	golangci-lint run

cover:
	go test -coverprofile=coverage.out $(PKG)
	go tool cover -func=coverage.out | tail -n 1

tidy:
	go mod tidy

MODULE  := github.com/HudoGriz/cephilis
BINARY  := bin/cephilis
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X $(MODULE)/internal/cli.Version=$(VERSION)

.PHONY: build test lint clean vet fmt tidy install

build:
	CGO_ENABLED=0 go build -trimpath -ldflags='$(LDFLAGS)' -o $(BINARY) ./cmd/cephilis

test:
	go test -race -count=1 ./...

cover:
	go test -race -count=1 -coverprofile=cover.out ./...
	go tool cover -html=cover.out -o cover.html
	@echo "Coverage report written to cover.html"

lint:
	golangci-lint run ./...

vet:
	go vet ./...

fmt:
	gofmt -l -w .

tidy:
	go mod tidy

install: build
	sudo install -D -m 0755 $(BINARY) /usr/local/bin/cephilis

clean:
	rm -rf bin/ dist/ cover.out cover.html

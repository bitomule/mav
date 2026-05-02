.PHONY: build test fmt check

GOCACHE ?= $(CURDIR)/.build/go-cache

build:
	GOCACHE=$(GOCACHE) go build -o .build/mav ./cmd/mav

test:
	GOCACHE=$(GOCACHE) go test ./...

fmt:
	gofmt -w cmd internal

check: fmt test build

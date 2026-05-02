.PHONY: build test fmt check

GOCACHE ?= $(CURDIR)/.build/go-cache
GOMODCACHE ?= $(CURDIR)/.build/go-mod-cache

build:
	GOCACHE=$(GOCACHE) GOMODCACHE=$(GOMODCACHE) go build -o .build/mav ./cmd/mav

test:
	GOCACHE=$(GOCACHE) GOMODCACHE=$(GOMODCACHE) go test ./...

fmt:
	gofmt -w cmd internal

check: fmt test build

.PHONY: build test test-one fmt check

GOCACHE ?= $(CURDIR)/.build/go-cache
GOMODCACHE ?= $(CURDIR)/.build/go-mod-cache

build:
	GOCACHE=$(GOCACHE) GOMODCACHE=$(GOMODCACHE) go build -o .build/mav ./cmd/mav

test:
	GOCACHE=$(GOCACHE) GOMODCACHE=$(GOMODCACHE) go test ./...

# One test, or a -run pattern, without retyping the cache variables:
#   make test-one RUN=TestEnvUDIDWithoutKindBeatsTargetCommand
# The full suite takes about a minute, which is long enough that an
# ablation -- revert the fix, watch the test go red, restore it, watch it
# go green -- stops being run at all if each half costs that.
test-one:
	GOCACHE=$(GOCACHE) GOMODCACHE=$(GOMODCACHE) go test ./internal/mav -run '$(RUN)' -v

fmt:
	gofmt -w cmd internal

check: fmt test build

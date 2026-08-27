BINARY := canopy
PREFIX ?= $(HOME)/.local
BINDIR ?= $(PREFIX)/bin

.PHONY: build install test fmt check clean

fmt:
	gofmt -w $$(find . -name '*.go' -not -path './vendor/*')

check:
	@test -z "$$(gofmt -l $$(find . -name '*.go' -not -path './vendor/*'))" || (echo "Go files need formatting; run 'make fmt'"; exit 1)
	go test ./...

build:
	go build -trimpath -o $(BINARY) ./cmd/canopy

install:
	go build -trimpath -o $(BINDIR)/$(BINARY) ./cmd/canopy

test:
	go test ./...

clean:
	rm -f $(BINARY)

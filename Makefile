BINARY := canopy
PREFIX ?= $(HOME)/.local
BINDIR ?= $(PREFIX)/bin

.PHONY: build install test clean

build:
	go build -trimpath -o $(BINARY) ./cmd/canopy

install:
	go build -trimpath -o $(BINDIR)/$(BINARY) ./cmd/canopy

test:
	go test ./...

clean:
	rm -f $(BINARY)

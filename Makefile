BINARY := canopy
PREFIX ?= $(HOME)/.local
BINDIR ?= $(PREFIX)/bin

.PHONY: build install dev test bench fmt check clean

fmt:
	gofmt -w $$(find . -name '*.go' -not -path './vendor/*')

check:
	@test -z "$$(gofmt -l $$(find . -name '*.go' -not -path './vendor/*'))" || (echo "Go files need formatting; run 'make fmt'"; exit 1)
	go test ./...

build:
	go build -trimpath -o $(BINARY) ./cmd/canopy

install:
	go install -trimpath ./cmd/canopy

dev: install
	canopy daemon stop 2>/dev/null || true
	canopy daemon start

test:
	go test ./...

bench:
	@go test -bench=. -benchmem -count=1 ./internal/... 2>&1 | awk '\
	function commas(n,  s,r){s=sprintf("%d",n+0);r="";while(length(s)>3){r=","substr(s,length(s)-2)r;s=substr(s,1,length(s)-3)};return s r}\
	/^pkg:/{split($$2,a,"/");pkg=a[length(a)];next}\
	/^Benchmark/{\
	  name=$$1;sub(/^Benchmark/,"",name);sub(/-[0-9]+$$/,"",name);\
	  if(!had){printf "%s  %s\n  %-32s  %8s  %10s  %10s  %10s\n",(seen ? "\n" : ""),pkg,"name","iters","ns/op","B/op","allocs/op";had=1;seen=1}\
	  printf "  %-32s  %8s  %10s  %10s  %10s\n",name,commas($$2),commas($$3),commas($$5),commas($$7);next}\
	/^ok /{had=0;next}{next}'

clean:
	rm -f $(BINARY)

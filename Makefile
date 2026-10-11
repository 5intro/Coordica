GO ?= $(if $(wildcard .tools/go/bin/go),$(CURDIR)/.tools/go/bin/go,go)
export GOCACHE ?= $(CURDIR)/.cache/go-build
export GOMODCACHE ?= $(CURDIR)/.cache/mod
export GOPATH ?= $(CURDIR)/.cache/gopath

.PHONY: build web test generate check package clean
build: web
	$(GO) build -trimpath -o bin/coordica ./cmd/coordica
web:
	npm --prefix web ci
	npm --prefix web run build
generate:
	$(GO) run ./cmd/contractgen
test:
	$(GO) test -race ./...
	npm --prefix web test
check:
	$(GO) vet ./...
	npm --prefix web run typecheck
# Package the three existing bin/ binaries; this does not build or publish them.
package:
	python3 scripts/package-release.py
clean:
	rm -rf bin

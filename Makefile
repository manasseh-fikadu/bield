# Bield build and development tasks. Run `make help` for the list.

GO      ?= go
BUN     ?= bun
# The port `make dev` runs Vite on. The Go server proxies the web UI to it.
VITE_PORT ?= 5173
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

.DEFAULT_GOAL := help
.PHONY: help build web dev dev-api dev-web demo demo-api site site-demo test e2e screenshots bench load lint types clean

## build: build ./bield with the web UI embedded
build: web
	$(GO) build -tags ui -trimpath -ldflags '$(LDFLAGS)' -o bield ./cmd/bield

## web: build the web UI into web/build
web: web/node_modules
	cd web && $(BUN) run build

## dev: run the Go server and the Vite dev server together
dev: web/node_modules
	@$(MAKE) --no-print-directory -j2 dev-api dev-web

dev-api:
	$(GO) run ./cmd/bield serve --dev-ui http://127.0.0.1:$(VITE_PORT)

dev-web:
	cd web && $(BUN) run dev --port $(VITE_PORT)

## demo: run Bield against a made-up tailnet, with the web UI from Vite
demo: web/node_modules
	@$(MAKE) --no-print-directory -j2 demo-api dev-web

demo-api:
	$(GO) run ./cmd/bield serve --demo --dev-ui http://127.0.0.1:$(VITE_PORT)

## site: run the landing page, on http://127.0.0.1:4701
site: site/node_modules
	cd site && $(BUN) run dev

## site-demo: build the clickable demo into the landing page
site-demo: web/node_modules site/node_modules
	$(GO) build -o site/.bield-demo ./cmd/bield
	cd site && BIELD_BIN=.bield-demo $(BUN) run demo

## test: run the Go and web tests, and type-check the web app and the landing page
test: web/node_modules site/node_modules
	$(GO) test -race -shuffle=on ./...
	cd web && $(BUN) run test
	cd web && $(BUN) run check
	cd site && $(BUN) run test
	cd site && $(BUN) run check

## e2e: click through the demo in a browser (run make build first)
e2e: web/node_modules
	cd web && $(BUN) run e2e

## screenshots: retake the screenshots in docs/images from the demo (run make build first)
screenshots: web/node_modules
	cd web && $(BUN) run screenshots

## bench: benchmark the store and the usage recorder with 200 devices and 90 days of history
bench:
	$(GO) test ./internal/store ./internal/usage -run '^$$' -bench . -benchmem

## load: run Bield against a demo of 200 devices and say what it costs (takes minutes)
load:
	BIELD_LOAD_DAYS=$${BIELD_LOAD_DAYS:-7} $(GO) test -tags load -run TestLoad -v -count=1 -timeout 40m ./cmd/bield

## lint: run golangci-lint, shellcheck and prettier
lint: web/node_modules site/node_modules
	golangci-lint run ./...
	shellcheck agent/*.sh install.sh .github/scripts/*.sh
	cd web && $(BUN) run lint
	cd site && $(BUN) run lint

## types: regenerate web/src/lib/api/types.ts from internal/api/types.go
types:
	$(GO) tool tygo generate

## clean: remove build output
clean:
	rm -rf bield web/build web/.svelte-kit site/build site/public/demo site/.bield-demo

web/node_modules: web/package.json web/bun.lock
	cd web && $(BUN) install --frozen-lockfile
	@touch $@

site/node_modules: site/package.json site/bun.lock
	cd site && $(BUN) install --frozen-lockfile
	@touch $@

## help: list these targets
help:
	@awk '/^## / { sub(/^## /, ""); split($$0, a, ": "); printf "  %-7s %s\n", a[1], a[2] }' $(MAKEFILE_LIST)

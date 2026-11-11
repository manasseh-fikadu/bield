# Bield build and development tasks. Run `make help` for the list.

GO      ?= go
BUN     ?= bun
# The port `make dev` runs Vite on. The Go server proxies the web UI to it.
VITE_PORT ?= 5173
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

.DEFAULT_GOAL := help
.PHONY: help build web dev dev-api dev-web demo demo-api site docs site-demo social test e2e screenshots recording bench load lint types prices code-server clean

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

## docs: run the documentation site, on http://127.0.0.1:4702/docs/ (the landing page's Docs link needs make site too)
docs: site/docs-site/node_modules
	cd site/docs-site && $(BUN) run dev

## site-demo: build the clickable demo into the landing page
site-demo: web/node_modules site/node_modules
	$(GO) build -o site/.bield-demo ./cmd/bield
	cd site && BIELD_BIN=.bield-demo $(BUN) run demo

## social: redraw site/public/social.png, the picture shown when a link to the site is shared
social: site/node_modules web/node_modules
	cd web && $(BUN) x playwright screenshot --viewport-size 1200,630 "file://$(CURDIR)/site/social/card.html" "$(CURDIR)/site/public/social.png"

## recording: film the demo into docs/images/demo.webp (README) and demo.mp4 (to upload), which needs make build first and ffmpeg
# The first three seconds are the page loading and settling, so they're cut.
recording: web/node_modules
	cd web && $(BUN) run recording
	ffmpeg -v error -y -ss 3 -i web/test-results/recording.webm -vf "fps=15,scale=960:-1:flags=lanczos" -c:v libwebp_anim -quality 85 -compression_level 6 -preset text -loop 0 -an docs/images/demo.webp
	ffmpeg -v error -y -ss 3 -i web/test-results/recording.webm -vf "fps=25,scale=1280:-2" -c:v libx264 -crf 27 -preset slow -pix_fmt yuv420p -movflags +faststart -an docs/images/demo.mp4

## test: run the Go and web tests, and test and type-check the web app, the landing page and the docs site
test: web/node_modules site/node_modules site/docs-site/node_modules
	$(GO) test -race -shuffle=on ./...
	cd web && $(BUN) run test
	cd web && $(BUN) run check
	cd site && $(BUN) run test
	cd site && $(BUN) run check
	cd site/docs-site && $(BUN) run test
	cd site/docs-site && $(BUN) run check

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
lint: web/node_modules site/node_modules site/docs-site/node_modules
	golangci-lint run ./...
	shellcheck agent/*.sh install.sh .github/scripts/*.sh
	cd web && $(BUN) run lint
	cd site && $(BUN) run lint
	cd site/docs-site && $(BUN) run lint

## types: regenerate web/src/lib/api/types.ts from internal/api/types.go
types:
	$(GO) tool tygo generate

## prices: refresh the model prices in internal/pricing/prices.json from LiteLLM's list
prices:
	$(GO) run ./internal/pricing/gen

## code-server: pin the latest code-server release, and its SHA-256s, in internal/editor/release.json
code-server:
	$(GO) run ./internal/editor/gen

## clean: remove build output
clean:
	rm -rf bield web/build web/.svelte-kit site/build site/public/demo site/.bield-demo site/docs-site/.astro

web/node_modules: web/package.json web/bun.lock
	cd web && $(BUN) install --frozen-lockfile
	@touch $@

site/node_modules: site/package.json site/bun.lock
	cd site && $(BUN) install --frozen-lockfile
	@touch $@

site/docs-site/node_modules: site/docs-site/package.json site/docs-site/bun.lock
	cd site/docs-site && $(BUN) install --frozen-lockfile
	@touch $@

## help: list these targets
help:
	@awk '/^## / { sub(/^## /, ""); split($$0, a, ": "); printf "  %-7s %s\n", a[1], a[2] }' $(MAKEFILE_LIST)

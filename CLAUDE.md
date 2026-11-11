# Bield

The order of work is the checklist in issue #1. Issue #2 has the stack and layout.

## Commands

- `make dev` runs Vite and the Go server, which serves Bield only on the tailnet and proxies the web UI to Vite (`--dev-ui`). Open the URL in the `bield is running` log line. `BIELD_TAILSCALE=host make dev` uses the machine's Tailscale instead of an embedded node, which needs a login the first time; in host mode, requests from the machine itself are anonymous, so open it from another device. Only admins get the UI through `--dev-ui`. The Go side doesn't reload, so restart after Go changes.
- `make build` builds `./bield` with the UI embedded, using the `ui` build tag. Plain `go build`, `go test` and `go vet` leave the UI out, so they work without a web build.
- `make demo` runs the Go server in demo mode (`--demo`) with Vite: a made-up tailnet with 12 machines and a week of history, so it needs no Tailscale and the visitor is an admin. `--demo-devices 200 --demo-days 90` makes it big. Terminals, files and Docker actions answer with a notice there. `make build && make e2e` runs the Playwright smoke test against it, and `make bench` and `make load` measure Bield with 200 devices.
- `make test` runs `go test -race -shuffle=on ./...`, `bun test` for the web app's `.test.ts` files, and `svelte-check`.
- `make dev` serves every UI component and chart at `/design`, a page production builds answer with a 404.
- `make lint` runs golangci-lint (configured in `.golangci.yml`), shellcheck and prettier. Run it before committing Go, shell or web changes.
- `make prices` refreshes `internal/pricing/prices.json`, the model prices behind the coding cost estimates, from LiteLLM's list. Commit the result.
- `make code-server` pins the latest code-server release in `internal/editor/release.json`, with the SHA-256 GitHub keeps for each build, for VS Code in the browser (`internal/editor`, `docs/vscode.md`). Commit the result; `-version` in `go run ./internal/editor/gen` pins another.
- `make types` regenerates `web/src/lib/api/types.ts` from `internal/api/types.go` with tygo. Never edit the generated file by hand.
- The web app uses Bun as its package manager (`cd web && bun add ...`).
- `make site` runs the landing page on <http://127.0.0.1:4701>. It's a static page in `site/`, separate from the dashboard, built with Vite and Tailwind (`cd site && bun run build`, into `site/build`). The dashboards in it are illustrations in `site/src/mock`, generated once from the Paper file, so nothing in them is live. Their `data-part` attributes are hooks for `site/src/motion.ts`, which plays each section from the scroll, so regenerating a mockup means putting them back. `site/src/stage.ts` works out where each scene falls. `make site-demo` builds the clickable demo that replaces the first one (see `docs/demo.md`): the real web app with a recording of `bield serve --demo`, in `site/public/demo`, which isn't committed. `.github/workflows/pages.yml` publishes it all to GitHub Pages from `main`. The app has to keep working under a path (`BIELD_BASE`): link with `resolve()`, and go by `page.route.id` rather than `page.url.pathname`.
- `site/docs-site/` is the documentation site: Astro 7 with Starlight (the new `markdown.processor` API, not `remarkPlugins`), reading `docs/` in place without changing it, under `/docs/` of the same site. `make docs` runs it on <http://127.0.0.1:4702/docs/>, and `make site` proxies `/docs` to it. Titles come from each file's first heading, links and `images/` paths are rewritten in `src/plugins/repo-links.ts` (a link to a missing file fails the build), `> [!NOTE]` becomes a Starlight aside, and `docs/README.md` is the sidebar. Starlight only transforms Markdown under `src/content/docs` unless `markdown.processedDirs` says otherwise, and Astro's content cache is keyed by file contents, so delete `site/docs-site/.astro` and `node_modules/.astro` after changing a plugin. `cd site && bun run build` builds the landing page and then the docs into `site/build/docs`, with `BIELD_SITE_URL` as the address the site is served from. `bun run links` checks the links in the build. The landing page's "What's new" section is the latest three releases in `CHANGELOG.md` at the repo root, put into `site/index.html` at the `<!-- @changelog -->` marker by `changelog()` in `site/vite.config.ts` (`site/scripts/changelog.ts` reads the file, and a changelog it can't read fails the build). Add a line under Unreleased in `CHANGELOG.md` for changes users would notice, and rename Unreleased when a release is cut. `make social` redraws `site/public/social.png`.

## The command line

`bield alerts ack 5`, `bield devices` and the rest of the commands that talk to a running Bield are rows in the table in `internal/cli` (`commands`), and the help, the shell completion and `docs/cli.md` come from it: `TestDocsNameEveryCommand` fails if the doc leaves a command out. They call `internal/client`, a Go client of the API that uses the types in `internal/api`, so a new route gets a method there and a command here. A command that changes something sets `Admin`, and one that stops, deletes or overwrites sets `Destructive`, which makes it need `--yes` (nothing ever prompts). Tests run the real handlers on a fake tailnet with `internal/api/apitest`, and use a stub server for what needs a machine, such as Docker and files. The CLI marks the changes it makes through its `bield-cli/<version>` User-Agent, which Bield puts in the activity log as `via: cli`.

`bield mcp` (`internal/cli/mcp.go`) makes its MCP tools from the same table, with the official Go SDK, and marks its changes `via: mcp`. A new row becomes a tool by itself: its arguments come from `Args` and `Flags`, and an argument with a new name needs a line in `argDescriptions`. Set `NoTool` with a reason if it shouldn't be a tool, `ReadOnly` if it needs an admin but changes nothing, `CLIOnly` on a flag a tool can't use, and `Stdin` if it reads its data from standard input. `TestToolsCoverCommands` checks all of it. Tools that need an admin only exist with `--allow-changes`.

## Tests and CI

Follow the test conventions in CONTRIBUTING.md: table tests next to the code, `testing/synctest` instead of a clock interface, and fakes for Tailscale and SSH in `…test` packages (such as `internal/tailnet/tailnettest`), so unit tests never need a tailnet.

CI is `.github/workflows/ci.yml`. Its `CI passed` job fails if any other job fails, so branch protection needs to require only that one check, and jobs can be added without touching the rule. Actions are pinned to commit SHAs with a version comment, and Dependabot updates them. When you bump the `go` directive in `go.mod`, bump `GO_VERSION` in the workflow too. `GOLANGCI_LINT_VERSION` is pinned there and updated by hand.

## Commits

Bield uses DCO sign-off (see CONTRIBUTING.md). Commit through the `commit-conventional` skill as usual, but with `git commit -s`. In this repo, the `Signed-off-by` trailer is the one exception to that skill's subject-only rule.

## Front end: Svelte 5 and SvelteKit 3

`web/` uses **Svelte 5** (runes) and **SvelteKit 3**. SvelteKit 3 came out on 2026-10-01 and has many breaking changes, and most examples online (and most of what you remember) are SvelteKit 2. Don't write SvelteKit APIs from memory: look them up with the Svelte MCP server (`list-sections`, then `get-documentation`), and run `svelte-autofixer` on components you write. Never use Svelte 4 syntax (`export let`, `$:`, `on:click`, `<slot>`, stores for component state).

The app is built with `adapter-static` as a single-page app and embedded in the Go binary, which serves `/api`. SvelteKit's server features (`+page.server.ts`, `hooks.server.ts`, form actions, remote functions, cookies, CSRF) aren't used, so their v3 changes are left out below. If that changes, read the full guide: `get-documentation "kit/migrating-to-sveltekit-3"`.

### SvelteKit 3: write this, not that

| | Not this (v2) | This (v3) |
|---|---|---|
| Config | `svelte.config.js` with a `kit: {…}` block | Pass options to `sveltekit({…})` in `vite.config.ts`. Former `kit.*` options are top-level, next to `compilerOptions`. `svelte.config.js` is no longer supported. |
| `src/lib` alias | `$lib/foo` | `#lib/foo.js` (also for `foo.ts`) and `#lib/Thing.svelte`: always with an extension. Declared in `package.json` `imports`: `"#lib": "./src/lib/index.js"`, `"#lib/*": "./src/lib/*"`. |
| Page state | `$app/stores` (`$page`, `$navigating`, `$updated`) | `$app/state` (`page`, `navigating`, `updated`), no `$` prefix. `$app/stores` is removed. |
| `page.url` | Mutate it in place | It's readonly. Copy it first: `new URL(page.url.href)`. |
| Environment | `$app/environment`, `$env/static/public` | `$app/env`, `$app/env/public` (`$env/*` is deprecated) |
| Paths | `base`, `assets`, `resolveRoute` from `$app/paths` | `resolve('/blog/[slug]', { slug })`, `resolve('blog/hello')`, `asset('logo.png')`. Only route IDs start with `/`. |
| Reloading data | `invalidateAll()` | `refreshAll()`. Clicking a link to the current page now calls it too. |
| `goto` options | `invalidateAll`, `noScroll`/`keepFocus`, `replaceState` | `refreshAll`, `reset: false`, `replace` |
| Shallow routing | `pushState(url, state)`, `replaceState(url, state)` | `goto(url, { shallow: true, state })`, adding `replace: true` to replace |
| External URLs | `goto('https://…')` | `window.location.href = url`. `goto` rejects URLs outside the app. |
| Link attributes | `data-sveltekit-preload-data="off"`, `data-sveltekit-noscroll`, `data-sveltekit-keepfocus` | `="false"`, `data-sveltekit-reset="false"` |
| `preloadData` | Assume `loaded` or `redirect` | Also handle `{ type: 'error', status, error }`. |
| Navigation events | `delta` on every navigation | `delta` only on `popstate` (back/forward) |
| `error()` | `error(404, { message, code })` | `error(404, 'Not found', { code })`. `App.Error` always has `status`. |
| `handleError` | Only called for unexpected errors | Called for every error, including `error(...)` and rendering errors. It can return `status`. |
| Types | `Page` and `Handle` from `@sveltejs/kit` | `Page` from `$app/state`, hook types from `@sveltejs/kit/hooks`, `defineParams` from `@sveltejs/kit/params` |
| Param matchers | One file per matcher in `src/params/` | One `src/params.ts` using `defineParams` (functions or Standard Schema) |
| tsconfig | `extends: "./.svelte-kit/tsconfig.json"` | `extends: "$app/tsconfig"` with explicit `include` and `exclude` |
| Service worker | `$service-worker` | `version` from `$app/env`, `assets`/`immutable`/`prerendered` from `$app/manifest`, plus `$app/paths`. Needs its own `src/service-worker/tsconfig.json`. |

Watch out for server-only modules. Any file with a `server` segment in its name (`server.ts`, `x.server.ts`) and any `server/` directory outside `src/routes` is server-only, and a client-only SPA can't import it. Don't name front-end modules or folders `server`; use something like `#lib/hub/`.

Minimum versions: Node 22.17, TypeScript 6, Svelte 5.57.1, Vite 8.0.12, `@sveltejs/vite-plugin-svelte` 7.

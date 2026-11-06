import { readFileSync } from 'node:fs';
import { extname, resolve } from 'node:path';
import tailwindcss from '@tailwindcss/vite';
import { defineConfig, type Plugin } from 'vite';
import { latestReleases, renderReleases } from './scripts/changelog';

/**
 * Puts the mockups into the page, where it says <!-- @mock name -->. They're long and generated
 * from the design, so the page stays readable without them.
 */
function mockups(): Plugin {
	const dir = resolve(import.meta.dirname, 'src/mock');
	return {
		name: 'bield-mockups',
		transformIndexHtml: {
			order: 'pre',
			handler: (html) =>
				html.replace(/<!-- @mock (\w+) -->/g, (_, name: string) =>
					readFileSync(resolve(dir, `${name}.html`), 'utf8')
				)
		},
		handleHotUpdate({ file, server }) {
			if (file.startsWith(dir)) {
				server.ws.send({ type: 'full-reload' });
				return [];
			}
		}
	};
}

/** The repository, which a release's notes are linked from. */
const repo = 'manasseh-fikadu/bield';

/** How many releases the landing page lists. The rest are in the file and on GitHub. */
const releasesShown = 3;

/**
 * Puts the latest releases into the page, where it says <!-- @changelog -->. They come from
 * CHANGELOG.md, so a release is written down once. A changelog that can't be read fails the build,
 * and not the page that's already out.
 */
function changelog(): Plugin {
	const file = resolve(import.meta.dirname, '../CHANGELOG.md');
	return {
		name: 'bield-changelog',
		transformIndexHtml: {
			order: 'pre',
			handler: (html) =>
				html.replace('<!-- @changelog -->', () =>
					renderReleases(latestReleases(readFileSync(file, 'utf8'), releasesShown), repo)
				)
		},
		configureServer(server) {
			// It's outside the folder that Vite watches.
			server.watcher.add(file);
			server.watcher.on('change', (changed) => {
				if (changed === file) {
					server.ws.send({ type: 'full-reload' });
				}
			});
		}
	};
}

/**
 * Serves the demo's pages in development as GitHub Pages does: a folder is its index.html. Vite
 * would answer with the landing page, which would then show itself in its own frame.
 */
function demoPages(): Plugin {
	return {
		name: 'bield-demo-pages',
		configureServer(server) {
			server.middlewares.use((req, _res, next) => {
				const [path, query] = (req.url ?? '').split('?');
				if (/^\/demo(\/|$)/.test(path) && !extname(path)) {
					const index = `${path.replace(/\/$/, '')}/index.html`;
					req.url = query ? `${index}?${query}` : index;
				}
				next();
			});
		}
	};
}

/**
 * Where the page is served from, which the few addresses that have to be absolute need: the
 * canonical link, and the pictures shown when the page is shared. The build gets it from the
 * environment (BIELD_SITE_URL, in the Pages workflow), and development from its own port.
 */
const siteUrl = (process.env.BIELD_SITE_URL ?? 'http://127.0.0.1:4701/').replace(/\/?$/, '/');

/** Puts the site's address into the page, where it says @site. */
function site(): Plugin {
	return {
		name: 'bield-site',
		transformIndexHtml: {
			order: 'pre',
			handler: (html) => html.replaceAll('@site/', siteUrl)
		}
	};
}

// A static page, with relative links so it works from any path, such as on GitHub Pages.
export default defineConfig({
	base: './',
	plugins: [tailwindcss(), mockups(), changelog(), demoPages(), site()],
	build: { outDir: 'build' },
	server: {
		host: '127.0.0.1',
		port: 4701,
		strictPort: true,
		// The documentation is its own site (docs-site/, `make docs`), which this page links to.
		proxy: { '/docs': 'http://127.0.0.1:4702' }
	}
});

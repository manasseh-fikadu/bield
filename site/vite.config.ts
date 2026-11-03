import { readFileSync } from 'node:fs';
import { extname, resolve } from 'node:path';
import tailwindcss from '@tailwindcss/vite';
import { defineConfig, type Plugin } from 'vite';

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

// A static page, with relative links so it works from any path, such as on GitHub Pages.
export default defineConfig({
	base: './',
	plugins: [tailwindcss(), mockups(), demoPages()],
	build: { outDir: 'build' },
	server: { host: '127.0.0.1', port: 4701, strictPort: true }
});

import { readdirSync } from 'node:fs';
import { join, relative, sep } from 'node:path';

/**
 * Where the finished site will be served from, as in vite.config.ts and docs-site/astro.config.ts:
 * the Pages workflow sets BIELD_SITE_URL, and without it the site is on its development port.
 */
export const siteUrl = (process.env.BIELD_SITE_URL ?? 'http://127.0.0.1:4701/').replace(
	/\/?$/,
	'/'
);

/**
 * What's in the build that isn't the site's own pages: the demo is a copy of the dashboard, which
 * has its own links, and Pagefind's files are the search index.
 */
const apart = ['demo', 'docs/pagefind'];

/** The HTML files of a finished build, as paths inside it. */
export function findPages(dir: string): string[] {
	const found: string[] = [];
	const walk = (folder: string) => {
		for (const entry of readdirSync(folder, { withFileTypes: true })) {
			const path = join(folder, entry.name);
			const rel = relative(dir, path).split(sep).join('/');
			if (entry.isDirectory()) {
				if (!apart.includes(rel)) walk(path);
			} else if (entry.name.endsWith('.html')) {
				found.push(rel);
			}
		}
	};
	walk(dir);
	return found.sort();
}

/** The address a page is served at, inside the site: "docs/install/index.html" is "docs/install/". */
export function addressOf(page: string): string {
	return page === 'index.html'
		? ''
		: page.replace(/(^|\/)index\.html$/, '$1').replace(/\.html$/, '');
}

/** The page a server answers with for what it doesn't have. It's served at any address, so it has no place of its own. */
export function isNotFound(page: string): boolean {
	return page === '404.html' || page.endsWith('/404.html');
}

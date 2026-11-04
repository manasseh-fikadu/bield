// Writes sitemap.xml and robots.txt into the finished build: bun scripts/sitemap.ts [build dir]
import { writeFileSync } from 'node:fs';
import { join, resolve } from 'node:path';
import { addressOf, findPages, isNotFound, siteUrl } from './pages';

/** The sitemap for a list of pages, which are paths inside the build. The 404 page isn't one. */
export function sitemap(pages: string[], base = siteUrl): string {
	const urls = pages
		.filter((page) => !isNotFound(page))
		.map((page) => new URL(addressOf(page), base).href)
		.sort()
		.map((url) => `\t<url><loc>${escape(url)}</loc></url>`);
	return `<?xml version="1.0" encoding="UTF-8"?>\n<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">\n${urls.join('\n')}\n</urlset>\n`;
}

export function robots(base = siteUrl): string {
	return `User-agent: *\nAllow: /\n\nSitemap: ${new URL('sitemap.xml', base).href}\n`;
}

function escape(text: string): string {
	return text.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
}

if (import.meta.main) {
	const dir = resolve(process.argv[2] ?? 'build');
	const pages = findPages(dir);
	writeFileSync(join(dir, 'sitemap.xml'), sitemap(pages));
	writeFileSync(join(dir, 'robots.txt'), robots());
	console.log(`sitemap.xml lists ${pages.filter((page) => !isNotFound(page)).length} pages`);
}

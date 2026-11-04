import { expect, test } from 'bun:test';
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { addressOf, findPages } from './pages';
import { robots, sitemap } from './sitemap';

test.each([
	['index.html', ''],
	['docs/index.html', 'docs/'],
	['docs/install/index.html', 'docs/install/'],
	['404.html', '404'],
	['about.html', 'about']
])('the address of %s', (page, want) => {
	expect(addressOf(page)).toBe(want);
});

test('a sitemap of the pages, without the 404 page', () => {
	const xml = sitemap(
		['docs/faq/index.html', 'index.html', '404.html', 'docs/404.html'],
		'https://example.test/bield/'
	);
	expect(xml).toContain('<loc>https://example.test/bield/</loc>');
	expect(xml).toContain('<loc>https://example.test/bield/docs/faq/</loc>');
	expect(xml).not.toContain('404');
});

test('robots.txt points at the sitemap', () => {
	expect(robots('https://example.test/')).toContain('Sitemap: https://example.test/sitemap.xml');
});

test('finds pages, but not the demo or the search index', () => {
	const dir = mkdtempSync(join(tmpdir(), 'bield-pages-'));
	try {
		for (const file of [
			'index.html',
			'docs/index.html',
			'docs/pagefind/x.html',
			'demo/index.html',
			'demo/a/index.html'
		]) {
			mkdirSync(join(dir, file, '..'), { recursive: true });
			writeFileSync(join(dir, file), '');
		}
		writeFileSync(join(dir, 'docs/app.js'), '');
		expect(findPages(dir)).toEqual(['docs/index.html', 'index.html']);
	} finally {
		rmSync(dir, { recursive: true, force: true });
	}
});

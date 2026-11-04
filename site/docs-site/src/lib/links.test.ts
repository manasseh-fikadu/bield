import { afterAll, beforeAll, describe, expect, test } from 'bun:test';
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { entryId, pageId, rewriteLink, type LinkContext } from './links';

let root: string;
let ctx: LinkContext;
let docs: string;

beforeAll(() => {
	root = mkdtempSync(join(tmpdir(), 'bield-links-'));
	docs = join(root, 'docs');
	mkdirSync(join(docs, 'images'), { recursive: true });
	mkdirSync(join(root, 'agent'));
	for (const file of [
		'docs/README.md',
		'docs/install.md',
		'docs/images/overview.png',
		'SECURITY.md',
		'agent/agent.sh'
	]) {
		writeFileSync(join(root, file), '');
	}
	ctx = { repoRoot: root, base: '/bield/docs', repoUrl: 'https://example.test/r', branch: 'main' };
});

afterAll(() => rmSync(root, { recursive: true, force: true }));

describe('rewriteLink', () => {
	test.each([
		['a page', 'install.md', '/bield/docs/install/'],
		['a page and a heading', 'install.md#running-it', '/bield/docs/install/#running-it'],
		['the index', 'README.md', '/bield/docs/'],
		['a picture', 'images/overview.png', '/bield/docs/images/overview.png'],
		['a file in the repository', '../SECURITY.md', 'https://example.test/r/blob/main/SECURITY.md'],
		[
			'a heading in one',
			'../SECURITY.md#threat-model',
			'https://example.test/r/blob/main/SECURITY.md#threat-model'
		],
		['a folder', '../agent', 'https://example.test/r/tree/main/agent'],
		['a file in a folder', '../agent/agent.sh', 'https://example.test/r/blob/main/agent/agent.sh'],
		['this page', '#section', '#section'],
		['another site', 'https://example.com/a.md', 'https://example.com/a.md'],
		['mail', 'mailto:a@example.com', 'mailto:a@example.com'],
		['the same scheme', '//example.com/a', '//example.com/a']
	])('%s', (_name, href, want) => {
		expect(rewriteLink(href, docs, ctx)).toBe(want);
	});

	test('at the root of a domain', () => {
		const at = { ...ctx, base: '' };
		expect(rewriteLink('install.md', docs, at)).toBe('/install/');
		expect(rewriteLink('README.md', docs, at)).toBe('/');
	});

	test.each([
		['a missing page', 'nope.md'],
		['a missing file in the repository', '../nope.md'],
		['something outside the repository', '../../outside.md']
	])('%s is an error', (_name, href) => {
		expect(() => rewriteLink(href, docs, ctx)).toThrow();
	});
});

test.each([
	['install.md', 'install', 'install'],
	['README.md', '', 'index'],
	['guides/README.md', 'guides', 'guides'],
	['guides/a.md', 'guides/a', 'guides/a']
])('ids of %s', (file, page, entry) => {
	expect(pageId(file)).toBe(page);
	expect(entryId(file)).toBe(entry);
});

import { afterAll, beforeAll, describe, expect, test } from 'bun:test';
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { check, checkOutside, fileFor, readPage, targetOf } from './check-links';

const base = 'https://example.test/bield/';

describe('targetOf', () => {
	test.each([
		['a relative page', 'install/', 'docs/', { kind: 'inside', path: 'docs/install/', hash: '' }],
		[
			'up a folder',
			'../faq/#cost',
			'docs/install/',
			{ kind: 'inside', path: 'docs/faq/', hash: 'cost' }
		],
		['the site root', '/bield/', 'docs/', { kind: 'inside', path: '', hash: '' }],
		['this page', '#top', 'docs/faq/', { kind: 'inside', path: 'docs/faq/', hash: 'top' }],
		[
			'the site by its address',
			'https://example.test/bield/docs/',
			'',
			{ kind: 'inside', path: 'docs/', hash: '' }
		],
		[
			'another site',
			'https://github.com/a/b',
			'',
			{ kind: 'outside', url: 'https://github.com/a/b' }
		],
		[
			'the host, outside the site',
			'https://example.test/other/',
			'',
			{ kind: 'outside', url: 'https://example.test/other/' }
		],
		['mail', 'mailto:a@example.test', '', { kind: 'skip' }],
		["the reader's own machine", 'http://127.0.0.1:4700/', '', { kind: 'skip' }]
	])('%s', (_name, link, from, want) => {
		expect(targetOf(link, from, base)).toEqual(want as never);
	});

	test('a bad % escape is a link to nothing, not a crash', () => {
		expect(targetOf('docs/foo%', '', base)).toEqual({
			kind: 'inside',
			path: 'docs/foo%',
			hash: ''
		});
	});

	test('a root-relative link without the base path', () => {
		expect(targetOf('/favicon.svg', '', base).kind).toBe('wrong');
	});

	test('at the root of a domain', () => {
		expect(targetOf('/favicon.svg', 'docs/', 'https://example.test/')).toEqual({
			kind: 'inside',
			path: 'favicon.svg',
			hash: ''
		});
	});
});

test('readPage finds links and anchors', async () => {
	const page = await readPage(
		'index.html',
		'<a href="a/">A</a><img src="x.png"><h2 id="one">One</h2><a name="two"></a><link rel="icon" href="i.svg">'
	);
	expect(page.links).toEqual(['a/', 'x.png', 'i.svg']);
	expect([...page.ids]).toEqual(['one', 'two']);
});

describe('check', () => {
	let dir: string;
	beforeAll(() => {
		dir = mkdtempSync(join(tmpdir(), 'bield-check-'));
		const files: Record<string, string> = {
			'index.html':
				'<a href="docs/">Docs</a><a href="docs/faq/#cost">F</a><a href="demo/">Demo</a><a href="https://github.com/a/b#x">G</a>',
			'docs/index.html': '<a href="faq/">FAQ</a><a href="../favicon.svg">i</a>',
			'docs/faq/index.html':
				'<h2 id="cost">Cost</h2><a href="../missing/">gone</a><a href="#nope">x</a><a href="/favicon.svg">abs</a>',
			'favicon.svg': ''
		};
		for (const [file, body] of Object.entries(files)) {
			mkdirSync(join(dir, file, '..'), { recursive: true });
			writeFileSync(join(dir, file), body);
		}
	});
	afterAll(() => rmSync(dir, { recursive: true, force: true }));

	test('finds what is broken, and only that', async () => {
		const { problems, outside } = await check(dir, base);
		expect(problems.map((p) => `${p.file} ${p.link}`).sort()).toEqual([
			'docs/faq/index.html #nope',
			'docs/faq/index.html ../missing/',
			'docs/faq/index.html /favicon.svg'
		]);
		expect([...outside.keys()]).toEqual(['https://github.com/a/b']);
	});

	test('serves files like Pages does', () => {
		expect(fileFor(dir, 'docs/')).toBe('docs/index.html');
		expect(fileFor(dir, 'docs')).toBe('docs/index.html');
		expect(fileFor(dir, 'favicon.svg')).toBe('favicon.svg');
		expect(fileFor(dir, 'nope')).toBeUndefined();
	});
});

describe('checkOutside', () => {
	const answers: Record<string, number> = {
		'https://ok.test/': 200,
		'https://gone.test/': 404,
		'https://walled.test/': 429
	};
	const fake = (async (url: string | URL | Request) => {
		const status = answers[String(url)];
		return new Response(null, { status });
	}) as typeof fetch;

	test('reports a page that is gone, and not one that is walled', async () => {
		const failed = await checkOutside(Object.keys(answers), fake);
		expect(failed.map((f) => f.link)).toEqual(['https://gone.test/']);
	});
});

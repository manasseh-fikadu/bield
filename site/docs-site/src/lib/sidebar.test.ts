import { describe, expect, test } from 'bun:test';
import { sidebarFrom } from './sidebar';

const index = `# Docs

Intro [link](x.md).

## Set up

| Page | What |
| --- | --- |
| [Installing](install.md) | The script, with [a link](docker.md) |

## Run it

| Page | What |
| --- | --- |
| [FAQ](faq.md#top) | Questions |
`;

describe('sidebarFrom', () => {
	test('groups the tables', () => {
		expect(sidebarFrom(index, ['README.md', 'install.md', 'faq.md', 'images'])).toEqual([
			{ label: 'Set up', items: [{ label: 'Installing', slug: 'install' }] },
			{ label: 'Run it', items: [{ label: 'FAQ', slug: 'faq' }] }
		]);
	});

	test('a page that is not listed', () => {
		expect(() => sidebarFrom(index, ['install.md', 'faq.md', 'new.md'])).toThrow('new.md');
	});

	test('a listed page that is missing', () => {
		expect(() => sidebarFrom(index, ['install.md'])).toThrow('faq.md');
	});
});

import { expect, test } from 'bun:test';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { inline, latestReleases, longDate, parseChangelog, renderReleases } from './changelog';

const sample = `# Changelog

An introduction, which is no release's.

## Unreleased

### Added

- Something that isn't out yet.

## [0.2.0] - 2026-12-01

The summary of the release,
over two lines.

### Added

- A new page. It has a long description
  that goes on to the next line.
- A \`bield\` command, which is #12.

### Fixed

- A [link](https://example.test/x) and an <angle> & more.

## 0.1.0 - 2026-11-04

### Added

* The first thing.

[0.2.0]: https://example.test/compare
`;

test('the releases of a changelog, newest first, with their changes by kind', () => {
	const releases = parseChangelog(sample);
	expect(releases.map((r) => r.version)).toEqual(['Unreleased', '0.2.0', '0.1.0']);
	expect(releases[0].date).toBeNull();
	const [, second, third] = releases;
	expect(second.date).toBe('2026-12-01');
	expect(second.summary).toBe('The summary of the release, over two lines.');
	expect(second.groups.map((g) => g.name)).toEqual(['Added', 'Fixed']);
	expect(second.groups[0].items).toEqual([
		'A new page. It has a long description that goes on to the next line.',
		'A `bield` command, which is #12.'
	]);
	expect(third.summary).toBe('');
	expect(third.groups[0].items).toEqual(['The first thing.']);
});

test('only what has been released, and the newest of it', () => {
	expect(latestReleases(sample, 5).map((r) => r.version)).toEqual(['0.2.0', '0.1.0']);
	expect(latestReleases(sample, 1).map((r) => r.version)).toEqual(['0.2.0']);
});

test.each([
	['a release with no date', '## 1.0.0\n\n### Added\n\n- x\n', 'has no date'],
	['a version that is there twice', '## 1.0.0 - 2026-01-01\n## 1.0.0 - 2026-01-02\n', 'twice'],
	['a change under no heading', '## 1.0.0 - 2026-01-01\n\n- x\n', "isn't under a ###"]
])('%s is an error that says where', (_, markdown, message) => {
	expect(() => parseChangelog(markdown)).toThrow(message);
});

test('a changelog with nothing released is an error, so the page never goes out empty', () => {
	expect(() => latestReleases('# Changelog\n\n## Unreleased\n', 3)).toThrow('no release');
	expect(() => latestReleases('## 1.0.0 - 2026-01-01\n\n### Added\n', 3)).toThrow('no release');
});

test('inline code, links and issue numbers, and nothing else, become HTML', () => {
	const html = inline('A `<b>` and [text](https://example.test/a?b=1&c=2) in #7 and <i>.', 'o/r');
	expect(html).toContain('<code class="font-mono text-[13px] text-fg">&lt;b&gt;</code>');
	expect(html).toContain('href="https://example.test/a?b=1&amp;c=2"');
	expect(html).toContain('href="https://github.com/o/r/issues/7"');
	expect(html).toContain('&lt;i&gt;');
	expect(html).not.toContain('<i>');
});

test('a link that is not to the web is shown as text', () => {
	const html = inline('[click](javascript:alert(1)) and [x](../README.md)', 'o/r');
	expect(html).not.toContain('href="javascript');
	expect(html).not.toContain('<a ');
});

test('a day as words, whatever time zone the build runs in', () => {
	expect(longDate('2026-11-05')).toBe('Nov 5, 2026');
	expect(longDate('2027-01-01')).toBe('Jan 1, 2027');
});

test('the releases as HTML', () => {
	const html = renderReleases(latestReleases(sample, 2), 'o/r');
	expect(html.match(/<article/g)).toHaveLength(2);
	expect(html).toContain('>v0.2.0</h3>');
	expect(html).toContain('<time datetime="2026-12-01"');
	expect(html).toContain('href="https://github.com/o/r/releases/tag/v0.2.0"');
	expect(html).toContain('>Added</h4>');
	expect(html).toContain('>Fixed</h4>');
	// A release with no summary has no paragraph for it.
	const [first, second] = html.split('<article').slice(1);
	expect(first).toContain('The summary of the release, over two lines.');
	expect(second).not.toContain('<p ');
});

test('the changelog in this repository can be shown', () => {
	const markdown = readFileSync(resolve(import.meta.dirname, '../../CHANGELOG.md'), 'utf8');
	const releases = latestReleases(markdown, 3);
	expect(releases.length).toBeGreaterThan(0);
	for (const r of releases) {
		expect(r.version).toMatch(/^\d+\.\d+\.\d+/);
		expect(r.date).toMatch(/^\d{4}-\d{2}-\d{2}$/);
	}
	// Every release has been looked at in order, newest first.
	const dates = releases.map((r) => r.date!);
	expect(dates).toEqual([...dates].sort().reverse());
	expect(renderReleases(releases, 'manasseh-fikadu/bield')).not.toContain('undefined');
});

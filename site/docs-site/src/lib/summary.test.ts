import { expect, test } from 'bun:test';
import { firstParagraph } from './summary';

test.each([
	['the first paragraph', '# T\n\nOne two.\nThree.\n\nNext.', 'One two. Three.'],
	[
		'links and code',
		'# T\n\nSee [the docs](a.md) for `bield serve`, **now**.',
		'See the docs for bield serve, now.'
	],
	['skips a picture', '# T\n\n![alt](x.png)\n\nText.', 'Text.'],
	['skips a table before it', '# T\n\n| a | b |\n| - | - |\n\nText.', 'Text.'],
	[
		'skips a wrapped list item',
		'# T\n\n- An item that wraps\n  onto the next line\n\nText.',
		'Text.'
	],
	['skips a wrapped HTML block', '# T\n\n<img src="a.png"\n  width="280">\n\nText.', 'Text.'],
	['nothing but a heading', '# T\n\n## Sub\n\nText.', undefined],
	['no paragraph', '# T\n', undefined],
	['a title only after a fence', '```\ncode\n```\n# T\n\nBody.', 'Body.']
])('%s', (_name, markdown, want) => {
	expect(firstParagraph(markdown)).toBe(want);
});

test('cuts a long one at a word', () => {
	const text = firstParagraph(`# T\n\n${'word '.repeat(60)}`) ?? '';
	expect(text.length).toBeLessThanOrEqual(160);
	expect(text.endsWith('word…')).toBe(true);
});

import { expect, test } from 'bun:test';
import { firstHeading } from './title';

test.each([
	['a title', '# Installing Bield\n\nText.', 'Installing Bield'],
	['not the second one', 'Intro\n\n# First\n\n# Second', 'First'],
	['skips lower levels', '## Sub\n\n# Top', 'Top'],
	['skips fences', '```sh\n# a comment\n```\n\n# Real', 'Real'],
	['skips longer fences', '````md\n```\n# no\n```\n````\n# Real', 'Real'],
	['skips tilde fences', '~~~\n# no\n~~~\n# Real', 'Real'],
	['closing hashes', '# Settings ##', 'Settings'],
	['code and links', '# The `bield` [command](cli.md)', 'The bield command'],
	['emphasis', '# A *new* __look__ and _more_', 'A new look and more'],
	[
		'underscores in a name',
		'# Setting BIELD_SETUP_LISTEN and my_var_name',
		'Setting BIELD_SETUP_LISTEN and my_var_name'
	],
	['no heading', 'Just text.', undefined],
	['a hash with no space', '#tag', undefined],
	['an indented code line', '    # not a heading\n# Yes', 'Yes']
])('%s', (_name, markdown, want) => {
	expect(firstHeading(markdown)).toBe(want);
});

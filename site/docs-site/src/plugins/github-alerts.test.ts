import { expect, test } from 'bun:test';
import type { Root } from 'mdast';
import remarkParse from 'remark-parse';
import { unified } from 'unified';
import { githubAlerts } from './github-alerts';

function transform(markdown: string): Root {
	const processor = unified().use(remarkParse).use(githubAlerts);
	return processor.runSync(processor.parse(markdown)) as Root;
}

/** What each top-level node became, as `type` or `directive:name:title`. */
function shape(tree: Root): string[] {
	return tree.children.map((node) => {
		if ((node.type as string) !== 'containerDirective') return node.type;
		const directive = node as unknown as {
			name: string;
			children: { children: { value: string }[] }[];
		};
		return `directive:${directive.name}:${directive.children[0].children[0].value}`;
	});
}

test.each([
	['note', '> [!NOTE]\n> Careful.', 'directive:note:Note'],
	['tip', '> [!TIP]\n> Try this.', 'directive:tip:Tip'],
	['important', '> [!IMPORTANT]\n> Read this.', 'directive:tip:Important'],
	['warning', '> [!WARNING]\n> Mind this.', 'directive:caution:Warning'],
	['caution', '> [!CAUTION]\n> Dangerous.', 'directive:danger:Caution'],
	['lower case', '> [!note]\n> Careful.', 'directive:note:Note'],
	['a plain quote', '> Just a quote.', 'blockquote'],
	['a marker mid-line', '> Text [!NOTE] here', 'blockquote'],
	['an unknown kind', '> [!DANGER]\n> Nope.', 'blockquote']
])('%s', (_name, markdown, want) => {
	expect(shape(transform(markdown))).toEqual([want]);
});

test('keeps the body, and drops the marker line', () => {
	const tree = transform('> [!NOTE]\n> First line.\n>\n> Second paragraph.');
	const directive = tree.children[0] as unknown as {
		children: { type: string; children?: { value: string }[] }[];
	};
	expect(directive.children.map((child) => child.type)).toEqual([
		'paragraph',
		'paragraph',
		'paragraph'
	]);
	expect(directive.children[1].children?.[0].value).toBe('First line.');
});

test('finds one inside a list', () => {
	const tree = transform('- item\n\n  > [!TIP]\n  > Inside.');
	expect(JSON.stringify(tree)).toContain('"containerDirective"');
});

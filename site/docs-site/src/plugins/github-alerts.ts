import type { Blockquote, Paragraph, Parent, Root, RootContent } from 'mdast';
import { visit } from 'unist-util-visit';

/**
 * GitHub writes a callout as a quote that starts with `[!NOTE]`. Starlight writes one as a
 * `:::note` directive. This turns the first into the second, so the pages keep GitHub's syntax and
 * the site shows what Starlight shows. Starlight's own plugin, which runs after this one, then
 * builds the box.
 */
const kinds = {
	note: { variant: 'note', title: 'Note' },
	tip: { variant: 'tip', title: 'Tip' },
	important: { variant: 'tip', title: 'Important' },
	warning: { variant: 'caution', title: 'Warning' },
	caution: { variant: 'danger', title: 'Caution' }
} as const;

type Kind = keyof typeof kinds;

// The directive nodes come from remark-directive, which mdast's own types don't know.
interface ContainerDirective extends Parent {
	type: 'containerDirective';
	name: string;
	attributes: Record<string, string>;
	children: RootContent[];
}

export function githubAlerts() {
	return (tree: Root) => {
		visit(tree, 'blockquote', (node: Blockquote, index, parent) => {
			if (index === undefined || !parent) return;
			const found = alertOf(node);
			if (!found) return;
			const { kind, rest } = found;
			const { variant, title } = kinds[kind];
			const label: Paragraph = {
				type: 'paragraph',
				data: { directiveLabel: true } as Paragraph['data'],
				children: [{ type: 'text', value: title }]
			};
			const directive: ContainerDirective = {
				type: 'containerDirective',
				name: variant,
				attributes: {},
				children: [label, ...rest]
			};
			parent.children[index] = directive as unknown as RootContent;
		});
	};
}

function alertOf(node: Blockquote): { kind: Kind; rest: RootContent[] } | undefined {
	const [first, ...others] = node.children;
	if (first?.type !== 'paragraph') return undefined;
	const [text, ...inline] = first.children;
	if (text?.type !== 'text') return undefined;
	const marker = /^\[!(note|tip|important|warning|caution)\][ \t]*(?:\n|$)/i.exec(text.value);
	if (!marker) return undefined;
	const kind = marker[1].toLowerCase() as Kind;

	const remaining = text.value.slice(marker[0].length);
	const children = remaining === '' ? inline : [{ ...text, value: remaining }, ...inline];
	const rest: RootContent[] = children.length > 0 ? [{ ...first, children }] : [];
	return { kind, rest: [...rest, ...others] };
}

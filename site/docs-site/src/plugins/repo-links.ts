import { dirname } from 'node:path';
import type { Definition, Html, Image, Link, Root } from 'mdast';
import { visit } from 'unist-util-visit';
import { rewriteLink, type LinkContext } from '../lib/links';

/**
 * Rewrites the links and pictures in a page from the way GitHub resolves them to the way the site
 * does (see rewriteLink), and drops the page's first heading, since the page's title shows as one.
 */
export function repoLinks(ctx: LinkContext) {
	return (tree: Root, file: { path: string }) => {
		const fromDir = dirname(file.path);

		let dropped = false;
		tree.children = tree.children.filter((node) => {
			if (dropped || node.type !== 'heading' || node.depth !== 1) return true;
			dropped = true;
			return false;
		});

		visit(tree, (node) => {
			if (node.type === 'link' || node.type === 'image' || node.type === 'definition') {
				const target = node as Link | Image | Definition;
				target.url = rewriteLink(target.url, fromDir, ctx);
			} else if (node.type === 'html') {
				// A picture written as HTML, for its width: <img src="images/phone.png" width="280">
				const html = node as Html;
				html.value = html.value.replace(
					/(\b(?:src|href)=)(["'])([^"']*)\2/g,
					(_all, attr: string, quote: string, url: string) =>
						`${attr}${quote}${rewriteLink(url, fromDir, ctx)}${quote}`
				);
			}
		});
	};
}

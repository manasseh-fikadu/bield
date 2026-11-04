import { readFile } from 'node:fs/promises';
import { relative, sep } from 'node:path';
import { glob, type Loader } from 'astro/loaders';
import { branch, docsDir, repoUrl } from './paths';
import { entryId } from './links';
import { firstParagraph } from './summary';
import { firstHeading } from './title';

/**
 * Loads the pages from docs/, where GitHub shows them, without changing them. They have no front
 * matter, so a page's title is its first heading, its description is its first paragraph, and its
 * "edit this page" link goes to its file on GitHub.
 */
export function docsLoader(): Loader {
	const files = glob({
		base: docsDir,
		pattern: '**/*.md',
		generateId: ({ entry }) => entryId(entry)
	});
	return {
		name: 'bield-docs',
		load: (context) =>
			files.load({
				...context,
				parseData: async (props) => {
					const data: Record<string, unknown> = { ...props.data };
					if (props.filePath) {
						const markdown = await readFile(props.filePath, 'utf8');
						data.title ??= firstHeading(markdown);
						data.description ??= firstParagraph(markdown);
						const file = relative(docsDir, props.filePath).split(sep).join('/');
						data.editUrl ??= `${repoUrl}/edit/${branch}/docs/${file}`;
					}
					return context.parseData({ ...props, data: data as typeof props.data });
				}
			})
	};
}

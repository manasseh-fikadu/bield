import { readdirSync, readFileSync } from 'node:fs';

export interface SidebarGroup {
	label: string;
	items: { label: string; slug: string }[];
}

/**
 * The sidebar is the index in docs/README.md: each `##` heading is a group, and the first link in
 * each row of the table beneath it is an item. The index is what GitHub shows for the folder, so
 * it stays the one list of pages. A page that's missing from it, or listed without a file, is an
 * error.
 */
export function sidebarFrom(index: string, files: string[]): SidebarGroup[] {
	const groups: SidebarGroup[] = [];
	for (const line of index.split('\n')) {
		const heading = /^##\s+(.+?)\s*$/.exec(line)?.[1];
		if (heading) {
			groups.push({ label: heading, items: [] });
			continue;
		}
		const link = /^\|\s*\[([^\]]+)\]\(([^)#]+)(?:#[^)]*)?\)/.exec(line);
		if (link && groups.length > 0) {
			groups.at(-1)!.items.push({ label: link[1], slug: link[2].replace(/\.md$/, '') });
		}
	}

	const listed = new Set(groups.flatMap((group) => group.items.map((item) => `${item.slug}.md`)));
	const pages = files.filter((file) => file.endsWith('.md') && file !== 'README.md');
	const missing = pages.filter((file) => !listed.has(file));
	if (missing.length > 0) {
		throw new Error(
			`docs/README.md doesn't list ${missing.join(', ')}, so they'd be missing from the sidebar`
		);
	}
	const unknown = [...listed].filter((file) => !pages.includes(file));
	if (unknown.length > 0) {
		throw new Error(`docs/README.md lists ${unknown.join(', ')}, which aren't in docs/`);
	}
	return groups.filter((group) => group.items.length > 0);
}

export function sidebarOf(docsDir: string): SidebarGroup[] {
	return sidebarFrom(readFileSync(`${docsDir}/README.md`, 'utf8'), readdirSync(docsDir));
}

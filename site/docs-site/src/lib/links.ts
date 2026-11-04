import { existsSync, statSync } from 'node:fs';
import { relative, resolve, sep } from 'node:path';

export interface LinkContext {
	/** The repository's root on disk. */
	repoRoot: string;
	/** Where the site is served from, with no trailing slash: "/bield/docs", or "" at a domain's root. */
	base: string;
	/** The repository on GitHub, for files the site doesn't carry. */
	repoUrl: string;
	branch: string;
}

/**
 * Turns a link written for GitHub into one for the site. The pages link to each other as files
 * (`tailscale.md#https`), to pictures beside them (`images/overview.png`) and to the rest of the
 * repository (`../SECURITY.md`), which the site sends to GitHub. A link to something that isn't
 * there is an error, so the build fails instead of the reader.
 */
export function rewriteLink(href: string, fromDir: string, ctx: LinkContext): string {
	if (
		href === '' ||
		href.startsWith('#') ||
		href.startsWith('//') ||
		/^[a-z][a-z0-9+.-]*:/i.test(href)
	) {
		return href;
	}
	const match = /^([^#?]*)(\?[^#]*)?(#.*)?$/.exec(href);
	let path: string;
	try {
		path = decodeURI(match?.[1] ?? href);
	} catch {
		throw new Error(`The link "${href}" in ${fromDir} isn't valid: it has a bad % escape`);
	}
	const query = match?.[2] ?? '';
	const hash = match?.[3] ?? '';

	const absolute = resolve(fromDir, path);
	const rel = relative(ctx.repoRoot, absolute).split(sep).join('/');
	if (rel === '..' || rel.startsWith('../')) {
		throw new Error(`The link "${href}" in ${fromDir} leaves the repository`);
	}
	if (!existsSync(absolute)) {
		throw new Error(`The link "${href}" in ${fromDir} points at ${rel}, which doesn't exist`);
	}

	// The docs folder is the site's index page.
	if (rel === 'docs') return `${ctx.base}/${query}${hash}`;

	// Pages and the files beside them are served by the site.
	if (rel.startsWith('docs/')) {
		const inDocs = rel.slice('docs/'.length);
		if (inDocs.endsWith('.md')) {
			const id = pageId(inDocs);
			return `${ctx.base}/${id === '' ? '' : `${id}/`}${query}${hash}`;
		}
		if (statSync(absolute).isFile()) return `${ctx.base}/${encodeURI(inDocs)}${query}${hash}`;
	}

	const kind = statSync(absolute).isDirectory() ? 'tree' : 'blob';
	return `${ctx.repoUrl}/${kind}/${ctx.branch}/${encodeURI(rel)}${hash}`;
}

/**
 * A page's path on the site, from its file in docs/: install.md is "install", and a README.md
 * is the index of its folder, so docs/README.md is "".
 */
export function pageId(fileInDocs: string): string {
	const id = fileInDocs.replace(/\.md$/, '');
	if (id === 'README') return '';
	return id.endsWith('/README') ? id.slice(0, -'/README'.length) : id;
}

/** The id a page has in the content collection, where the index is "index". */
export function entryId(fileInDocs: string): string {
	return pageId(fileInDocs) || 'index';
}

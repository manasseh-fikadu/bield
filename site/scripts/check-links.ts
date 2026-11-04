// Checks the links in a finished build: bun scripts/check-links.ts [build dir] [--external]
//
// Every link to a page, a file or a heading inside the site has to land on something, and a link
// that starts with "/" has to carry the site's base path, or it works on a domain's root and
// breaks on GitHub Pages. With --external, links to other sites are requested too, which is slow
// and sometimes the other site's fault, so it's not part of the build.
import { existsSync, readFileSync, statSync } from 'node:fs';
import { join, posix, resolve } from 'node:path';
import { addressOf, findPages, isNotFound, siteUrl } from './pages';

export interface Page {
	/** Its path inside the build, such as "docs/install/index.html". */
	file: string;
	links: string[];
	ids: Set<string>;
}

export interface Problem {
	file: string;
	link: string;
	problem: string;
}

/** The links a page makes, and the places in it a link can point at. */
export async function readPage(file: string, html: string): Promise<Page> {
	const links: string[] = [];
	const ids = new Set<string>();
	const rewriter = new HTMLRewriter()
		.on('a[href], link[href]', {
			element(el) {
				// A canonical or alternate link names this page, or the site's own address.
				const href = el.getAttribute('href');
				if (href) links.push(href);
			}
		})
		.on('img[src], script[src], source[src], video[src], audio[src], iframe[src]', {
			element(el) {
				const src = el.getAttribute('src');
				if (src) links.push(src);
			}
		})
		.on('meta[property="og:image"], meta[name="twitter:image"]', {
			element(el) {
				const content = el.getAttribute('content');
				if (content) links.push(content);
			}
		})
		.on('[id], a[name]', {
			element(el) {
				const id = el.getAttribute('id') ?? el.getAttribute('name');
				if (id) ids.add(id);
			}
		});
	await rewriter.transform(new Response(html)).text();
	return { file, links, ids };
}

type Target =
	| { kind: 'inside'; path: string; hash: string }
	| { kind: 'outside'; url: string }
	| { kind: 'skip' };

/**
 * Where a link goes: a path inside the site, a URL elsewhere, or nowhere to check. `from` is the
 * address the page is served at, inside the site. `base` is the site's own address.
 */
export function targetOf(
	link: string,
	from: string,
	base: string
): Target | { kind: 'wrong'; why: string } {
	if (/^(mailto|tel|javascript|data|blob):/i.test(link)) return { kind: 'skip' };
	const baseUrl = new URL(base);
	const here = new URL(from, baseUrl);
	let url: URL;
	try {
		url = new URL(link, here);
	} catch {
		return { kind: 'wrong', why: "isn't a valid address" };
	}
	if (url.protocol !== 'http:' && url.protocol !== 'https:') return { kind: 'skip' };

	const absolute = /^[a-z][a-z0-9+.-]*:|^\/\//i.test(link);
	if (url.origin !== baseUrl.origin) {
		// The pages tell people to open Bield on their own machine, which isn't a page to request.
		const local = ['localhost', '127.0.0.1', '[::1]', '0.0.0.0'].includes(url.hostname);
		return local ? { kind: 'skip' } : { kind: 'outside', url: url.href };
	}
	if (!url.pathname.startsWith(baseUrl.pathname)) {
		// On the site's host, but outside the part of it that's this site's.
		if (absolute) return { kind: 'outside', url: url.href };
		return {
			kind: 'wrong',
			why: `is outside the site's base path ${baseUrl.pathname}, so it breaks when served from it`
		};
	}
	return {
		kind: 'inside',
		path: decodeURIComponent(url.pathname.slice(baseUrl.pathname.length)),
		hash: decodeURIComponent(url.hash.slice(1))
	};
}

/** The file in the build that serves a path, as GitHub Pages would. */
export function fileFor(dir: string, path: string): string | undefined {
	const candidates =
		path === '' || path.endsWith('/')
			? [`${path}index.html`]
			: [path, `${path}/index.html`, `${path}.html`];
	return candidates.find(
		(candidate) => existsSync(join(dir, candidate)) && statSync(join(dir, candidate)).isFile()
	);
}

export async function check(
	dir: string,
	base = siteUrl
): Promise<{ problems: Problem[]; outside: Map<string, string[]> }> {
	const pages = new Map<string, Page>();
	for (const file of findPages(dir))
		pages.set(file, await readPage(file, readFileSync(join(dir, file), 'utf8')));

	// The demo is added by its own build, so a build without it has nothing to link to.
	const hasDemo = existsSync(join(dir, 'demo'));
	const problems: Problem[] = [];
	const outside = new Map<string, string[]>();

	for (const page of pages.values()) {
		if (isNotFound(page.file)) continue;
		const from = addressOf(page.file);
		for (const link of page.links) {
			const target = targetOf(link, from, base);
			if (target.kind === 'skip') continue;
			if (target.kind === 'wrong') {
				problems.push({ file: page.file, link, problem: target.why });
				continue;
			}
			if (target.kind === 'outside') {
				const url = target.url.replace(/#.*$/, '');
				outside.set(url, [...(outside.get(url) ?? []), page.file]);
				continue;
			}
			if (!hasDemo && /^demo(\/|$)/.test(target.path)) continue;
			const file = fileFor(dir, target.path);
			if (!file) {
				problems.push({
					file: page.file,
					link,
					problem: `points at ${target.path || '/'}, which isn't in the build`
				});
				continue;
			}
			if (target.hash === '' || target.hash === 'top' || !file.endsWith('.html')) continue;
			const ids = (pages.get(file) ?? (await readPage(file, readFileSync(join(dir, file), 'utf8'))))
				.ids;
			if (!ids.has(target.hash)) {
				problems.push({
					file: page.file,
					link,
					problem: `points at #${target.hash}, which isn't on ${file}`
				});
			}
		}
	}
	return { problems, outside };
}

/** Requests each link, a few at a time, and says which ones fail. A status that means "go away", not "gone", is let through. */
export async function checkOutside(
	urls: string[],
	fetchImpl: typeof fetch = fetch
): Promise<Problem[]> {
	const failures: Problem[] = [];
	let next = 0;
	const worker = async () => {
		while (next < urls.length) {
			const url = urls[next++];
			const status = await request(url, fetchImpl);
			if (typeof status === 'string') failures.push({ file: '', link: url, problem: status });
		}
	};
	await Promise.all(Array.from({ length: 6 }, worker));
	return failures;
}

async function request(url: string, fetchImpl: typeof fetch): Promise<string | number> {
	let last = 'no answer';
	for (let attempt = 0; attempt < 3; attempt++) {
		try {
			let res = await fetchImpl(url, {
				method: 'HEAD',
				redirect: 'follow',
				signal: AbortSignal.timeout(20_000)
			});
			// Some servers don't answer HEAD.
			if (res.status === 405 || res.status === 501 || res.status === 403) {
				res = await fetchImpl(url, {
					method: 'GET',
					redirect: 'follow',
					signal: AbortSignal.timeout(20_000)
				});
			}
			if (res.ok) return res.status;
			// Rate limits and bot walls say nothing about whether the page is there.
			if ([401, 403, 429, 999].includes(res.status)) return res.status;
			last = `answered ${res.status}`;
			if (res.status < 500) break;
		} catch (error) {
			last = error instanceof Error ? error.message : String(error);
		}
		await new Promise((done) => setTimeout(done, 1000 * (attempt + 1)));
	}
	return last;
}

if (import.meta.main) {
	const args = process.argv.slice(2);
	const dir = resolve(args.find((arg) => !arg.startsWith('--')) ?? 'build');
	const { problems, outside } = await check(dir);
	const pagesChecked = findPages(dir).length;

	if (args.includes('--external')) {
		const failed = await checkOutside([...outside.keys()].sort());
		for (const failure of failed) {
			problems.push({
				file: (outside.get(failure.link) ?? []).join(', '),
				link: failure.link,
				problem: failure.problem
			});
		}
	}

	for (const { file, link, problem } of problems)
		console.error(`${posix.normalize(file)}: ${link} ${problem}`);
	const note = args.includes('--external')
		? `${outside.size} outside links requested`
		: `${outside.size} outside links not requested`;
	console.log(`${pagesChecked} pages checked, ${problems.length} broken links, ${note}`);
	process.exit(problems.length > 0 ? 1 : 0);
}

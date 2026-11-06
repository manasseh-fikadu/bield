/**
 * Reads CHANGELOG.md, which is the one place a release's changes are written down, and makes the
 * landing page's list of releases from it. It's in this folder, and not in vite.config.ts, so it
 * can be tested.
 */

/** The changes of one kind in a release, such as the ones that were fixed. */
export interface Group {
	name: string;
	items: string[];
}

export interface Release {
	/** Such as 0.1.1, or Unreleased. */
	version: string;
	/** As 2026-11-05, or null for what hasn't been released. */
	date: string | null;
	/** What it's about, in a sentence or two, or "". */
	summary: string;
	groups: Group[];
}

/** A release's heading: "## 0.1.1 - 2026-11-05", or "## [0.1.1] - 2026-11-05". */
const heading = /^##\s+\[?([^\]\s]+)\]?(?:\s+[-–—]\s+(\d{4}-\d{2}-\d{2}))?\s*$/;

/** The releases in a changelog, in the order it has them, which is newest first. */
export function parseChangelog(markdown: string): Release[] {
	const releases: Release[] = [];
	let release: Release | undefined;
	let group: Group | undefined;
	let summary: string[] = [];
	// Whether the line before was a change, which a line that is indented goes on with.
	let inChange = false;

	const finish = () => {
		if (release) {
			release.summary = summary.join(' ');
		}
		summary = [];
	};

	for (const [index, raw] of markdown.split('\n').entries()) {
		const line = raw.trimEnd();
		const at = `CHANGELOG.md line ${index + 1}`;
		const found = heading.exec(line);
		if (found) {
			finish();
			const [, name, date] = found;
			const unreleased = name.toLowerCase() === 'unreleased';
			const version = unreleased ? 'Unreleased' : name;
			if (!unreleased && !date) {
				throw new Error(`${at}: ${version} has no date`);
			}
			if (releases.some((r) => r.version === version)) {
				throw new Error(`${at}: ${version} is there twice`);
			}
			release = { version, date: date ?? null, summary: '', groups: [] };
			releases.push(release);
			group = undefined;
			inChange = false;
			continue;
		}
		if (!release) {
			// The title and the introduction.
			continue;
		}
		const kind = /^###\s+(.+?)\s*$/.exec(line);
		if (kind) {
			group = { name: kind[1], items: [] };
			release.groups.push(group);
			inChange = false;
			continue;
		}
		const bullet = /^[-*]\s+(.*)$/.exec(line);
		if (bullet) {
			if (!group) {
				throw new Error(`${at}: a change that isn't under a ### heading`);
			}
			group.items.push(bullet[1]);
			inChange = true;
			continue;
		}
		if (line.trim() === '' || /^\[[^\]]+\]:\s/.test(line)) {
			inChange = false;
		} else if (inChange && /^\s/.test(raw) && group) {
			group.items[group.items.length - 1] += ` ${line.trim()}`;
		} else if (!group) {
			summary.push(line.trim());
		}
	}
	finish();
	return releases;
}

/** The newest `count` releases, leaving out what hasn't been released and what has nothing in it. */
export function latestReleases(markdown: string, count: number): Release[] {
	const released = parseChangelog(markdown).filter(
		(r) => r.date !== null && r.groups.some((g) => g.items.length > 0)
	);
	if (released.length === 0) {
		throw new Error('CHANGELOG.md has no release with changes in it');
	}
	return released.slice(0, count);
}

const escapes: Record<string, string> = { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' };
const escape = (text: string) => text.replace(/[&<>"]/g, (c) => escapes[c]);

const link = 'underline decoration-track underline-offset-2 transition-colors hover:text-fg-muted';

/**
 * Text as HTML, with the little Markdown a change uses: `code`, [a link](https://example.test) and
 * #123, which is an issue or a pull request of the repository. Anything else shows as it was typed.
 */
export function inline(text: string, repo: string): string {
	return text
		.split(/(`[^`]+`)/)
		.map((part) => {
			if (part.startsWith('`') && part.endsWith('`') && part.length > 2) {
				return `<code class="font-mono text-[13px] text-fg">${escape(part.slice(1, -1))}</code>`;
			}
			return escape(part)
				.replace(
					/\[([^\]]+)\]\((https?:\/\/[^\s)]+)\)/g,
					(_, label: string, url: string) => `<a href="${url}" class="${link}">${label}</a>`
				)
				.replace(
					/(^|[\s(])#(\d+)\b/g,
					(_, before: string, n: string) =>
						`${before}<a href="https://github.com/${repo}/issues/${n}" class="${link}">#${n}</a>`
				);
		})
		.join('');
}

/** 2026-11-05 as "Nov 5, 2026". The date is a day, so no time zone moves it. */
export function longDate(date: string): string {
	return new Intl.DateTimeFormat('en-US', {
		month: 'short',
		day: 'numeric',
		year: 'numeric',
		timeZone: 'UTC'
	}).format(new Date(`${date}T00:00:00Z`));
}

/**
 * The releases as the landing page's HTML: an article for each, with its changes by kind. `repo` is
 * the repository as owner/name, which a release's notes are linked from.
 */
export function renderReleases(releases: Release[], repo: string): string {
	return releases
		.map((r) => {
			const tag = `v${r.version}`;
			const groups = r.groups
				.filter((g) => g.items.length > 0)
				.map(
					(g) => `
						<div class="flex flex-col gap-1.5 sm:flex-row sm:gap-6">
							<h4 class="shrink-0 font-mono text-[13px] leading-6.25 text-fg-faint sm:w-24">${escape(g.name)}</h4>
							<ul class="flex min-w-0 flex-col gap-2 text-[15px] leading-6 text-fg">${g.items
								.map(
									(item) =>
										`
								<li class="relative pl-4 before:absolute before:top-3 before:left-0 before:h-px before:w-2 before:bg-track">${inline(item, repo)}</li>`
								)
								.join('')}
							</ul>
						</div>`
				)
				.join('');
			return `
					<article class="flex flex-col gap-4 border-t border-line py-7 first:border-t-0 first:pt-0 xl:first:pt-3.5">
						<div class="flex flex-wrap items-baseline justify-between gap-x-4 gap-y-1">
							<h3 class="font-mono text-[15px] leading-5.5 font-medium text-fg">${escape(tag)}</h3>
							<time datetime="${r.date}" class="font-mono text-[13px] leading-5 text-fg-faint">${longDate(r.date!)}</time>
						</div>${
							r.summary
								? `
						<p class="text-[16px] leading-6.5 text-fg-muted">${inline(r.summary, repo)}</p>`
								: ''
						}
						<div class="flex flex-col gap-4">${groups}
						</div>
						<a href="https://github.com/${repo}/releases/tag/${encodeURIComponent(tag)}" class="w-fit text-[14px] leading-5.5 text-fg-faint ${link}">Release notes</a>
					</article>`;
		})
		.join('');
}

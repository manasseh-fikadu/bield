const limit = 160;

/**
 * A page's description for search results and link previews: its first paragraph, as plain text,
 * cut at a word. The pages have no front matter to say it in.
 */
export function firstParagraph(markdown: string): string | undefined {
	const lines = markdown.split('\n');
	const start = lines.findIndex((line) => /^ {0,3}#[ \t]+\S/.test(line));
	let paragraph: string[] = [];
	for (const line of lines.slice(start + 1)) {
		if (line.trim() === '') {
			if (paragraph.length > 0) break;
			continue;
		}
		// Only prose will do: not a heading, list, table, quote, code, picture or HTML.
		if (paragraph.length === 0 && /^\s*(#|[-*+]\s|\d+[.)]\s|\||>|`{3}|~{3}|!|<)/.test(line)) {
			if (/^\s*#/.test(line) || /^\s*(`{3}|~{3})/.test(line)) break;
			continue;
		}
		paragraph.push(line.trim());
	}
	const text = plain(paragraph.join(' '));
	if (text === '') return undefined;
	if (text.length <= limit) return text;
	const cut = text.slice(0, limit - 1);
	return `${cut.slice(0, Math.max(cut.lastIndexOf(' '), 80)).replace(/[\s,;:.]+$/, '')}…`;
}

function plain(text: string): string {
	return text
		.replace(/!\[[^\]]*\]\([^)]*\)/g, '')
		.replace(/\[([^\]]*)\]\([^)]*\)/g, '$1')
		.replace(/`([^`]*)`/g, '$1')
		.replace(/(\*\*|__)(.+?)\1/g, '$2')
		.replace(/(\*|_)(.+?)\1/g, '$2')
		.replace(/\s+/g, ' ')
		.trim();
}

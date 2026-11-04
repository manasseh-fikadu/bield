/**
 * The page title: the text of the first top-level heading, as GitHub shows the file. Code and
 * links in it are reduced to their text, since a title is plain.
 */
export function firstHeading(markdown: string): string | undefined {
	let fence: string | undefined;
	for (const line of markdown.split('\n')) {
		const marker = /^ {0,3}(`{3,}|~{3,})/.exec(line)?.[1];
		if (marker) {
			if (!fence) fence = marker;
			else if (marker.startsWith(fence[0]) && marker.length >= fence.length) fence = undefined;
			continue;
		}
		if (fence) continue;
		const heading = /^ {0,3}#[ \t]+(.+?)(?:[ \t]+#+)?[ \t]*$/.exec(line)?.[1];
		if (heading) return plain(heading);
	}
	return undefined;
}

function plain(text: string): string {
	return text
		.replace(/\[([^\]]*)\]\([^)]*\)/g, '$1')
		.replace(/`([^`]*)`/g, '$1')
		.replace(/\*\*(.+?)\*\*/g, '$1')
		.replace(/\*(.+?)\*/g, '$1')
		.replace(/(^|\W)_{1,2}([^_]+?)_{1,2}(?!\w)/g, '$1$2')
		.trim();
}

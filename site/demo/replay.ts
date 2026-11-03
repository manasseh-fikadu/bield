// What the demo's stand-in for the hub works out from what it recorded. Pure, so the tests can
// reach it without a browser.

const times = /"(20\d\d-\d\d-\d\dT\d\d:\d\d:\d\d(?:\.\d+)?(?:Z|[+-]\d\d:\d\d))"/g;
const stamps = /"ts":(\d{13})/g;

/** Moves every time in a recorded answer on by `by` milliseconds, as if it had just been said. */
export function shiftTimes(json: string, by: number): string {
	return json
		.replace(times, (_, t: string) => JSON.stringify(new Date(Date.parse(t) + by).toISOString()))
		.replace(stamps, (_, ts: string) => `"ts":${Number(ts) + by}`);
}

/** What identifies a request to /api: its path and its query, in a set order. */
export function routeKey(url: string): string {
	const u = new URL(url, 'http://demo');
	const path = decodeURIComponent(u.pathname).replace(/^\/api\//, '');
	const query = [...u.searchParams].map(([k, v]) => `${k}=${v}`).sort();
	return query.length ? `${path}?${query.join('&')}` : path;
}

/** A key without its query, for what the demo recorded only once, such as a usage sample. */
export function withoutQuery(key: string): string {
	return key.split('?')[0];
}

interface Numbered {
	id: number;
}

/** A page of a list that's newest first, as /api/events and /api/alerts answer with. */
export function pageOf<T extends Numbered>(
	items: T[],
	q: URLSearchParams,
	size = 50
): { items: T[]; more: boolean } {
	const before = Number(q.get('before') ?? Infinity);
	const after = Number(q.get('after') ?? -Infinity);
	const limit = Number(q.get('limit') ?? size);
	const shown = items.filter((i) => i.id < before && i.id > after);
	return { items: shown.slice(0, limit), more: shown.length > limit };
}

/** A recorded alert, as far as the demo cares. */
export interface AlertLike extends Numbered {
	state: string;
	ackedAt?: string;
	ackedBy?: string;
	snoozedUntil?: string;
	snoozedBy?: string;
	notes: { at: string; by?: string; text: string }[];
}

/** Who you are in the demo. */
const login = 'alex@example.com';

/**
 * What a request to change an alert does to it, or null if it's no such request. The demo keeps
 * these in the browser, so acknowledging one works, and a reload puts it back.
 */
export function changeAlert<T extends AlertLike>(
	alert: T,
	method: string,
	action: string,
	body: { minutes?: number; text?: string },
	now: number
): T | null {
	const at = new Date(now).toISOString();
	if (action === 'ack' && method === 'PUT') {
		return { ...alert, state: 'acknowledged', ackedAt: at, ackedBy: login };
	}
	if (action === 'ack' && method === 'DELETE') {
		return { ...alert, state: 'open', ackedAt: undefined, ackedBy: undefined };
	}
	if (action === 'snooze' && method === 'PUT') {
		const until = new Date(now + (body.minutes ?? 60) * 60_000).toISOString();
		return { ...alert, state: 'snoozed', snoozedUntil: until, snoozedBy: login };
	}
	if (action === 'snooze' && method === 'DELETE') {
		const state = alert.ackedAt ? 'acknowledged' : 'open';
		return { ...alert, state, snoozedUntil: undefined, snoozedBy: undefined };
	}
	if (action === 'notes' && method === 'POST') {
		return { ...alert, notes: [...alert.notes, { at, by: login, text: body.text ?? '' }] };
	}
	return null;
}

/** One message of a stream, as the recorder kept it. */
export interface Frame {
	event: string;
	data: string;
}

/** A container's recorded log lines, which `q` asks for some of. */
export function logLines<T extends { s: number }>(frames: Frame[], q: URLSearchParams): T[] {
	const lines = frames
		.filter((f) => f.event === 'lines')
		.flatMap((f) => JSON.parse(f.data) as T[])
		.filter((l) => (l.s === 2 ? q.get('stderr') !== '0' : q.get('stdout') !== '0'));
	const tail = Number(q.get('tail') ?? 500);
	return tail > 0 ? lines.slice(-tail) : [];
}

// A stand-in for the hub, in the browser, which lets the real dashboard run on a static page. The
// page loads it before the app. It answers the app's requests to /api and its event streams with
// what demo/record.ts recorded from `bield serve --demo`, with the times moved on to now.

import {
	changeAlert,
	logLines,
	pageOf,
	routeKey,
	shiftTimes,
	withoutQuery,
	type AlertLike,
	type Frame
} from './replay.js';

interface Manifest {
	recordedAt: number;
	routes: Record<string, string>;
	snapshots: string[];
	streams: Record<string, string>;
}

const root = new URL('.', (document.currentScript as HTMLScriptElement).src);
const realFetch = window.fetch.bind(window);
const RealEventSource = window.EventSource;

const manifest: Promise<Manifest> = realFetch(new URL('manifest.json', root)).then((r) => r.json());
const files = new Map<string, Promise<string>>();

function file(name: string): Promise<string> {
	let text = files.get(name);
	if (!text) {
		text = realFetch(new URL(`api/${name}.json`, root)).then((r) => r.text());
		files.set(name, text);
	}
	return text;
}

function json(body: unknown, status = 200): Response {
	return new Response(JSON.stringify(body), {
		status,
		headers: { 'content-type': 'application/json' }
	});
}

/** How the demo says no, which the app shows as a toast. */
const readOnly =
	"This demo runs in your browser, so it can't change that. Run bield serve --demo to try it.";

/** What has been done to alerts in this tab. */
const changed = new Map<number, AlertLike>();

function current<T extends AlertLike>(alert: T): T {
	return (changed.get(alert.id) as T | undefined) ?? alert;
}

/** Recorded, with the times moved on by as long as it's been. */
async function recorded(name: string): Promise<string> {
	const { recordedAt } = await manifest;
	return shiftTimes(await file(name), Date.now() - recordedAt);
}

let latest: { alerts: AlertLike[] } | null = null;

async function resolvedAlerts(): Promise<AlertLike[]> {
	const { routes } = await manifest;
	const list = JSON.parse(await recorded(routes.alerts)) as { alerts: AlertLike[] };
	return list.alerts;
}

async function changeAnAlert(id: number, method: string, action: string, init?: RequestInit) {
	const alert = changed.get(id) ?? latest?.alerts.find((a) => a.id === id);
	const body = init?.body ? JSON.parse(String(init.body)) : {};
	const next = alert && changeAlert(alert, method, action, body, Date.now());
	if (!next) {
		return json({ error: readOnly }, 403);
	}
	changed.set(id, next);
	// The app hears about it the way it would from the hub: in the next snapshot.
	for (const source of sources) {
		source.resend?.();
	}
	return action === 'notes' ? json(next.notes.at(-1)) : json(next);
}

async function answer(url: string, init?: RequestInit): Promise<Response> {
	const method = (init?.method ?? 'GET').toUpperCase();
	const key = routeKey(url);
	const q = new URL(url, location.href).searchParams;

	const alertAction = /^alerts\/(\d+)\/(ack|snooze|notes)$/.exec(key);
	if (alertAction) {
		return changeAnAlert(Number(alertAction[1]), method, alertAction[2], init);
	}
	if (method !== 'GET' && method !== 'HEAD') {
		return json({ error: readOnly }, 403);
	}

	const { routes } = await manifest;
	const path = withoutQuery(key);
	if (path === 'events') {
		const { events } = JSON.parse(await recorded(routes.events)) as {
			events: (AlertLike & { kind: string; deviceId?: string })[];
		};
		const shown = events.filter(
			(e) =>
				(!q.get('kind') || e.kind === q.get('kind')) &&
				(!q.get('device') || e.deviceId === q.get('device'))
		);
		const page = pageOf(shown, q);
		return json({ events: page.items, more: page.more });
	}
	if (path === 'alerts') {
		const page = pageOf(await resolvedAlerts(), q);
		return json({ alerts: page.items.map(current), more: page.more });
	}
	const alert = /^alerts\/(\d+)$/.exec(path);
	if (alert) {
		const id = Number(alert[1]);
		const found =
			changed.get(id) ??
			latest?.alerts.find((a) => a.id === id) ??
			(await resolvedAlerts()).find((a) => a.id === id);
		return found ? json(found) : json({ error: 'No alert here' }, 404);
	}

	const name = routes[key] ?? routes[path];
	if (!name) {
		return json({ error: "This demo didn't record that" }, 404);
	}
	return new Response(await recorded(name), {
		headers: { 'content-type': 'application/json' }
	});
}

window.fetch = ((input: RequestInfo | URL, init?: RequestInit) => {
	const url = input instanceof Request ? input.url : String(input);
	const path = new URL(url, location.href).pathname;
	if (!path.startsWith('/api/')) {
		return realFetch(input, init);
	}
	return answer(url, init ?? (input instanceof Request ? { method: input.method } : undefined));
}) as typeof fetch;

/** A recorded stream, which looks like an EventSource to the app. */
class Source extends EventTarget {
	static readonly CONNECTING = 0;
	static readonly OPEN = 1;
	static readonly CLOSED = 2;
	readonly CONNECTING = 0;
	readonly OPEN = 1;
	readonly CLOSED = 2;
	readyState = 1;
	withCredentials = false;
	onerror: ((e: Event) => void) | null = null;
	onmessage: ((e: MessageEvent) => void) | null = null;
	onopen: ((e: Event) => void) | null = null;
	readonly url: string;
	/** Sends the latest snapshot again, after something changed. */
	resend?: () => void;
	#timers: ReturnType<typeof setInterval>[] = [];

	constructor(url: string) {
		super();
		this.url = url;
		sources.add(this);
		void this.#start(url).catch(() => {
			this.readyState = 2;
			this.onerror?.(new Event('error'));
		});
	}

	close() {
		this.readyState = 2;
		this.#timers.forEach(clearInterval);
		sources.delete(this);
	}

	#send(event: string, data: unknown) {
		if (this.readyState !== 2) {
			this.dispatchEvent(new MessageEvent(event, { data: JSON.stringify(data) }));
		}
	}

	#every(ms: number, fn: () => void) {
		this.#timers.push(setInterval(fn, ms));
	}

	async #start(url: string) {
		const { snapshots, streams } = await manifest;
		const key = withoutQuery(routeKey(url));
		if (key === 'stream') {
			return this.#snapshots(snapshots);
		}
		const name = streams[key];
		if (!name) {
			throw new Error(`No recording of ${key}`);
		}
		const frames = JSON.parse(await file(name)) as Frame[];
		const q = new URL(url, location.href).searchParams;
		return key.endsWith('/stats') ? this.#stats(frames) : this.#logs(frames, q);
	}

	/** The hub's snapshots, one every 10 seconds, round and round. */
	async #snapshots(names: string[]) {
		let at = 0;
		const send = async (name: string) => {
			const text = await file(name);
			const generatedAt = Date.parse(JSON.parse(text).generatedAt);
			const snapshot = JSON.parse(shiftTimes(text, Date.now() - generatedAt));
			snapshot.alerts = snapshot.alerts.map(current);
			latest = snapshot;
			this.#send('snapshot', snapshot);
		};
		this.resend = () => void send(names[(at + names.length - 1) % names.length]);
		await send(names[at++]);
		this.#every(10_000, () => void send(names[at++ % names.length]));
	}

	/** What the container used, with the last of it as now, and more of the same every second. */
	#stats(frames: Frame[]) {
		const end = frames.find((f) => f.event === 'end');
		if (end) {
			return this.#send('end', JSON.parse(end.data));
		}
		const samples = frames
			.filter((f) => f.event === 'sample')
			.map((f) => JSON.parse(f.data) as { ts: number });
		const now = Date.now();
		this.#send(
			'backlog',
			samples.map((s, i) => ({ ...s, ts: now - (samples.length - i) * 1000 }))
		);
		let at = 0;
		this.#every(1000, () => {
			if (samples.length) {
				this.#send('sample', { ...samples[at++ % samples.length], ts: Date.now() });
			}
		});
	}

	/** A container's log, and then, if it's being followed, a line now and then. */
	#logs(frames: Frame[], q: URLSearchParams) {
		const start = frames.find((f) => f.event === 'start');
		this.#send('start', start ? JSON.parse(start.data) : {});
		const lines = logLines<{ s: number; t: string }>(frames, q);
		if (q.get('tail') !== '0' && lines.length) {
			const by = Date.now() - Date.parse(lines[lines.length - 1].t);
			this.#send(
				'lines',
				lines.map((l) => ({ ...l, t: new Date(Date.parse(l.t) + by).toISOString() }))
			);
		}
		if (q.get('follow') === '0') {
			return this.#send('end', { reason: 'The log ended' });
		}
		let at = 0;
		this.#every(4000, () => {
			if (lines.length) {
				this.#send('lines', [{ ...lines[at++ % lines.length], t: new Date().toISOString() }]);
			}
		});
	}
}

const sources = new Set<Source>();

window.EventSource = function (url: string | URL, init?: EventSourceInit) {
	const u = new URL(url, location.href);
	return u.pathname.startsWith('/api/') ? new Source(u.href) : new RealEventSource(url, init);
} as unknown as typeof EventSource;

// Links straight to /api, such as a log's download, have nothing behind them on a static page.
document.addEventListener(
	'click',
	(e) => {
		const link = (e.target as Element).closest?.('a[href^="/api/"]');
		if (link) {
			e.preventDefault();
		}
	},
	true
);

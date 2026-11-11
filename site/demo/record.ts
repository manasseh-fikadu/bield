// Records what `bield serve --demo` answers, for demo/shim.ts to answer with on a static page: every
// request the dashboard can make in a demo, and a while of its event streams. It fails if a
// request the demo needs fails, so a change to the API can't quietly break the page.

import { createHash } from 'node:crypto';
import { mkdir, writeFile } from 'node:fs/promises';
import { join } from 'node:path';
import { routeKey, type Frame } from './replay.js';

const ranges = ['1h', '6h', '24h', '7d', '30d', '90d'];
/** The ranges of what the hub doesn't sum up into buckets, as in history.ts. */
const shortRanges = ['1h', '6h', '24h', '7d'];
const snapshotCount = 6;
const atOnce = 8;

/** As much of a snapshot as this needs. */
interface Snapshot {
	devices: { id: string; docker: boolean }[];
	checks: { id: string }[];
	collectors: { state: string }[];
}

export interface Recording {
	deviceIds: string[];
	containerNames: string[];
}

export async function record(hub: string, out: string): Promise<Recording> {
	await mkdir(join(out, 'api'), { recursive: true });
	const routes: Record<string, string> = {};
	const streams: Record<string, string> = {};
	const recordedAt = Date.now();

	/** Keeps a file under what it says, so answers that are the same are kept once. */
	async function save(text: string): Promise<string> {
		const name = createHash('sha256').update(text).digest('hex').slice(0, 16);
		await writeFile(join(out, 'api', `${name}.json`), text);
		return name;
	}

	async function get(path: string): Promise<string | null> {
		const res = await fetch(`${hub}/api/${path}`, { headers: { accept: 'application/json' } });
		return res.ok ? res.text() : null;
	}

	/** Records a request. `also` are other paths that get the same answer, such as by ID. */
	async function keep(path: string, { optional = false, also = [] as string[] } = {}) {
		const text = await get(path);
		if (text === null) {
			if (optional) {
				return null;
			}
			throw new Error(`The demo didn't answer /api/${path}`);
		}
		const name = await save(text);
		for (const p of [path, ...also]) {
			routes[routeKey(`/api/${p}`)] = name;
		}
		return text;
	}

	/** The messages of an event stream, for `ms` milliseconds or until `enough` of them. */
	async function listen(path: string, ms: number, enough = Infinity): Promise<Frame[]> {
		const stop = new AbortController();
		const timer = setTimeout(() => stop.abort(), ms);
		const frames: Frame[] = [];
		try {
			const res = await fetch(`${hub}/api/${path}`, { signal: stop.signal });
			if (!res.ok || !res.body) {
				throw new Error(`The demo didn't answer /api/${path}`);
			}
			let buffer = '';
			const decoder = new TextDecoder();
			for await (const chunk of res.body) {
				buffer += decoder.decode(chunk, { stream: true });
				for (let i = buffer.indexOf('\n\n'); i >= 0; i = buffer.indexOf('\n\n')) {
					const block = buffer.slice(0, i);
					buffer = buffer.slice(i + 2);
					const event = /^event: (.*)$/m.exec(block)?.[1] ?? 'message';
					const data = [...block.matchAll(/^data: (.*)$/gm)].map((m) => m[1]).join('\n');
					frames.push({ event, data });
				}
				if (frames.filter((f) => f.event === 'snapshot').length >= enough) {
					stop.abort();
				}
			}
		} catch (err) {
			if (!stop.signal.aborted) {
				throw err;
			}
		} finally {
			clearTimeout(timer);
		}
		return frames;
	}

	/**
	 * A snapshot from once the hub has found what it's going to. It takes a few seconds after it
	 * starts to run its collectors and look for Docker on each machine, and a recording from before
	 * that would quietly have less in it.
	 */
	async function settled(): Promise<Snapshot> {
		let steady = 0;
		let last = '';
		for (const giveUp = Date.now() + 90_000; Date.now() < giveUp; await Bun.sleep(2000)) {
			const [frame] = await listen('stream', 15_000, 1);
			const snapshot = JSON.parse(frame.data) as Snapshot;
			const docker = snapshot.devices.filter((d) => d.docker).map((d) => d.id);
			const waiting = snapshot.collectors.some((c) => c.state === 'waiting');
			const now = docker.join();
			steady = !waiting && now === last ? steady + 1 : 0;
			last = now;
			if (steady >= 4) {
				return snapshot;
			}
		}
		throw new Error("The demo's hub didn't settle");
	}

	/** Runs the jobs, a few at a time. */
	async function pool(jobs: (() => Promise<unknown>)[]) {
		const queue = [...jobs];
		await Promise.all(
			Array.from({ length: atOnce }, async () => {
				for (let job = queue.shift(); job; job = queue.shift()) {
					await job();
				}
			})
		);
	}

	const snapshot = await settled();
	const jobs: (() => Promise<unknown>)[] = [];
	const add = (path: string, options?: { optional?: boolean; also?: string[] }) =>
		jobs.push(() => keep(path, options));

	for (const path of ['me', 'health', 'agent', 'settings', 'settings/units', 'access', 'tokens']) {
		add(path);
	}
	add('notifications/channels');
	add('checks/recent');
	add('alerts');
	// The hub gives 100 at a time, and the stand-in answers with pages of its own.
	jobs.push(async () => {
		const events: { id: number }[] = [];
		for (let before = ''; ; before = `&before=${events[events.length - 1].id}`) {
			const text = await get(`events?limit=100${before}`);
			if (text === null) {
				throw new Error("The demo didn't answer /api/events");
			}
			const page = JSON.parse(text) as { events: { id: number }[]; more: boolean };
			events.push(...page.events);
			if (!page.more || !page.events.length) {
				break;
			}
		}
		routes.events = await save(JSON.stringify({ events, more: false }));
	});
	for (const r of ranges) {
		add(`health/history?range=${r}`);
		for (const c of snapshot.checks) {
			add(`checks/${encodeURIComponent(c.id)}/history?range=${r}`, { optional: true });
		}
	}
	const containerNames = new Set<string>();
	for (const d of snapshot.devices) {
		const device = `devices/${encodeURIComponent(d.id)}`;
		add(`${device}/usage`, { optional: true });
		add(`${device}/ssh`, { optional: true });
		add(`${device}/editor`, { optional: true });
		for (const r of ranges) {
			add(`${device}/history?range=${r}`);
			add(`${device}/usage/history?range=${r}`, { optional: true });
		}
		if (d.docker) {
			jobs.push(async () => {
				const docker = `${device}/docker`;
				for (const what of ['overview', 'volumes', 'images', 'networks']) {
					await keep(`${docker}/${what}`);
				}
				const list = await keep(`${docker}/containers`);
				const { containers } = JSON.parse(list!) as {
					containers: { id: string; name: string }[];
				};
				await pool(
					containers.map((c) => async () => {
						containerNames.add(c.name);
						const named = `${docker}/containers/${encodeURIComponent(c.name)}`;
						const byId = `${docker}/containers/${encodeURIComponent(c.id)}`;
						await keep(named, { also: [byId] });
						await keep(`${named}/inspect`, { optional: true, also: [`${byId}/inspect`] });
						await keep(`${named}/top`, { optional: true, also: [`${byId}/top`] });
						for (const r of shortRanges) {
							await keep(`${named}/history?range=${r}`, {
								optional: true,
								also: [`${byId}/history?range=${r}`]
							});
						}
						const [logs, stats] = await Promise.all([
							listen(`${named}/logs?follow=0&tail=500`, 3000),
							listen(`${named}/stats`, 6000)
						]);
						const logsAt = await save(JSON.stringify(logs));
						const statsAt = await save(JSON.stringify(stats));
						streams[routeKey(`/api/${named}/logs`)] = logsAt;
						streams[routeKey(`/api/${byId}/logs`)] = logsAt;
						streams[routeKey(`/api/${named}/stats`)] = statsAt;
						streams[routeKey(`/api/${byId}/stats`)] = statsAt;
					})
				);
			});
		}
	}
	await pool(jobs);

	// Last, since it takes a minute: long enough for the numbers on the page to move.
	const frames = await listen('stream', 90_000, snapshotCount);
	const snapshots = await Promise.all(
		frames.filter((f) => f.event === 'snapshot').map((f) => save(f.data))
	);
	await writeFile(
		join(out, 'manifest.json'),
		JSON.stringify({ recordedAt, routes, snapshots, streams })
	);
	return { deviceIds: snapshot.devices.map((d) => d.id), containerNames: [...containerNames] };
}

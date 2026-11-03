import { describe, expect, test } from 'bun:test';
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

describe('shiftTimes', () => {
	test('moves times on, wherever they are', () => {
		const got = JSON.parse(
			shiftTimes(
				JSON.stringify({
					at: '2026-11-03T12:00:00Z',
					nested: [{ from: '2026-11-03T15:00:00.5+03:00', ts: 1793700000000 }]
				}),
				60_000
			)
		);
		expect(got.at).toBe('2026-11-03T12:01:00.000Z');
		expect(got.nested[0].from).toBe('2026-11-03T12:01:00.500Z');
		expect(got.nested[0].ts).toBe(1793700060000);
	});

	test('keeps a history and its steps lined up', () => {
		const step = 60_000;
		const from = Date.parse('2026-11-03T12:00:00Z');
		const got = JSON.parse(
			shiftTimes(
				JSON.stringify({ from: new Date(from).toISOString(), ts: from + 3 * step }),
				90_000
			)
		);
		expect(got.ts).toBe(Date.parse(got.from) + 3 * step);
	});

	test('leaves everything else alone', () => {
		const json = JSON.stringify({
			total: 1800000000000,
			name: '2026 was a good year',
			zero: '0001-01-01T00:00:00Z'
		});
		expect(shiftTimes(json, 1000)).toBe(json);
	});
});

describe('routeKey', () => {
	test.each([
		['/api/me', 'me'],
		['/api/devices/n1/history?range=1h', 'devices/n1/history?range=1h'],
		['/api/checks/ping%3An1/history?range=7d', 'checks/ping:n1/history?range=7d'],
		['/api/events?limit=5&kind=ssh', 'events?kind=ssh&limit=5'],
		['http://demo/api/events?kind=ssh&limit=5', 'events?kind=ssh&limit=5']
	])('%s is %s', (url, want) => {
		expect(routeKey(url)).toBe(want);
	});

	test('a query comes off', () => {
		expect(withoutQuery('devices/n1/usage?at=5')).toBe('devices/n1/usage');
	});
});

describe('pageOf', () => {
	const items = [5, 4, 3, 2, 1].map((id) => ({ id }));
	const ids = (q: string, size?: number) => {
		const { items: got, more } = pageOf(items, new URLSearchParams(q), size);
		return [got.map((i) => i.id), more];
	};

	test.each<[string, string, unknown]>([
		['everything', '', [[5, 4, 3, 2, 1], false]],
		['a limit, and that there is more', 'limit=2', [[5, 4], true]],
		['what comes before', 'before=4', [[3, 2, 1], false]],
		['what came after', 'after=3', [[5, 4], false]]
	])('%s', (_name, q, want) => {
		expect(ids(q)).toEqual(want as never);
	});
});

describe('changeAlert', () => {
	const now = Date.parse('2026-11-03T12:00:00Z');
	const alert: AlertLike = { id: 1, state: 'open', notes: [] };

	test('acknowledges, and takes it back', () => {
		const acked = changeAlert(alert, 'PUT', 'ack', {}, now)!;
		expect(acked).toMatchObject({ state: 'acknowledged', ackedBy: 'alex@example.com' });
		expect(changeAlert(acked, 'DELETE', 'ack', {}, now)).toMatchObject({ state: 'open' });
	});

	test('snoozes for as long as it was asked, and wakes', () => {
		const snoozed = changeAlert(alert, 'PUT', 'snooze', { minutes: 60 }, now)!;
		expect(snoozed.state).toBe('snoozed');
		expect(snoozed.snoozedUntil).toBe('2026-11-03T13:00:00.000Z');
		expect(changeAlert(snoozed, 'DELETE', 'snooze', {}, now)?.state).toBe('open');
	});

	test('waking one that was acknowledged leaves it acknowledged', () => {
		const acked = changeAlert(alert, 'PUT', 'ack', {}, now)!;
		const snoozed = changeAlert(acked, 'PUT', 'snooze', {}, now)!;
		expect(changeAlert(snoozed, 'DELETE', 'snooze', {}, now)?.state).toBe('acknowledged');
	});

	test('keeps a note', () => {
		const got = changeAlert(alert, 'POST', 'notes', { text: 'Swapped the disk' }, now)!;
		expect(got.notes).toEqual([
			{ at: '2026-11-03T12:00:00.000Z', by: 'alex@example.com', text: 'Swapped the disk' }
		]);
	});

	test('is no answer to anything else', () => {
		expect(changeAlert(alert, 'POST', 'ack', {}, now)).toBeNull();
	});
});

describe('logLines', () => {
	const frames: Frame[] = [
		{ event: 'start', data: '{}' },
		{ event: 'lines', data: JSON.stringify([{ s: 1, m: 'a' }]) },
		{
			event: 'lines',
			data: JSON.stringify([
				{ s: 2, m: 'b' },
				{ s: 1, m: 'c' }
			])
		}
	];
	const got = (q: string) =>
		logLines<{ s: number; m: string }>(frames, new URLSearchParams(q)).map((l) => l.m);

	test.each([
		['all of them', '', ['a', 'b', 'c']],
		['the end of them', 'tail=1', ['c']],
		['none', 'tail=0', []],
		['only errors', 'stdout=0', ['b']],
		['no errors', 'stderr=0', ['a', 'c']]
	])('%s', (_name, q, want) => {
		expect(got(q)).toEqual(want);
	});
});

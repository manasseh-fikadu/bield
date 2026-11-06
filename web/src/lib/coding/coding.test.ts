import { describe, expect, test } from 'bun:test';
import type { CodingLimit, CodingSource, CodingSummary } from '#lib/api/types.js';
import {
	apiEstimateHelp,
	axisText,
	bucketText,
	buckets,
	chartSeries,
	compactTokens,
	costParts,
	dollars,
	dollarsShort,
	limitGroups,
	limitText,
	modelsText,
	share,
	sourceHint,
	speedParts,
	tickFormat,
	toolColor,
	toolName,
	versionBefore,
	windowName
} from './coding.js';

describe('toolName and toolColor', () => {
	test('known tools have names and colors', () => {
		expect(toolName('claude')).toBe('Claude Code');
		expect(toolName('codex')).toBe('Codex');
		expect(toolName('opencode')).toBe('OpenCode');
		expect(toolName('pi')).toBe('Pi');
		expect(toolColor('claude')).not.toBe(toolColor('codex'));
	});

	test('a tool this version does not know is capitalised, and keeps its color', () => {
		expect(toolName('gemini')).toBe('Gemini');
		expect(toolName('')).toBe('Unknown tool');
		expect(toolColor('gemini')).toBe(toolColor('gemini'));
		expect(toolColor('gemini')).toMatch(/^#[0-9a-f]{6}$/);
	});
});

describe('compactTokens', () => {
	test.each([
		[0, '0'],
		[842, '842'],
		[1000, '1K'],
		[20_700, '20.7K'],
		[1_234_567, '1.23M'],
		[20_700_000, '20.7M'],
		[36_300_000, '36.3M'],
		[101_000_000, '101M'],
		[898_000_000, '898M'],
		[7_990_000_000, '7.99B'],
		[8_990_000_000, '8.99B'],
		[2_500_000_000_000, '2.5T'],
		[999_600, '1M'],
		[999_999_999, '1B'],
		[-1500, '-1.5K']
	])('%d is %s', (n, want) => {
		expect(compactTokens(n)).toBe(want);
	});

	test('what is not a number has no count', () => {
		expect(compactTokens(null)).toBe('—');
		expect(compactTokens(undefined)).toBe('—');
		expect(compactTokens(NaN)).toBe('—');
	});
});

describe('dollars', () => {
	test('to the cent, with separators', () => {
		expect(dollars(3246.37)).toBe('$3,246.37');
		expect(dollars(0)).toBe('$0.00');
		expect(dollars(2.88)).toBe('$2.88');
	});

	test('less than a cent is said so', () => {
		expect(dollars(0.001)).toBe('<$0.01');
	});

	test('nothing is a dash', () => {
		expect(dollars(null)).toBe('—');
	});

	test('dollarsShort for an axis', () => {
		expect(dollarsShort(0)).toBe('$0');
		expect(dollarsShort(0.25)).toBe('$0.25');
		expect(dollarsShort(1.5)).toBe('$1.5');
		expect(dollarsShort(12.5)).toBe('$12.5');
		expect(dollarsShort(300)).toBe('$300');
		expect(dollarsShort(1500)).toBe('$1.5k');
		expect(dollarsShort(25_000)).toBe('$25k');
	});
});

describe('share', () => {
	test.each([
		[18.8, 100, '18.8%'],
		[100, 100, '100%'],
		[99.99, 100, '100%'],
		[0.01, 100, '<0.1%'],
		[0, 100, '0%'],
		[5, 0, '0%']
	])('%d of %d is %s', (part, whole, want) => {
		expect(share(part, whole)).toBe(want);
	});
});

describe('parts', () => {
	test('costParts lists the four kinds', () => {
		const parts = costParts({ total: 10, input: 1, cacheRead: 2, cacheWrite: 3, output: 4 });
		expect(parts.map((p) => [p.key, p.value])).toEqual([
			['input', 1],
			['cacheRead', 2],
			['cacheWrite', 3],
			['output', 4]
		]);
	});

	test('speedParts splits the fast cost off the standard', () => {
		expect(speedParts(10, 4).map((p) => p.value)).toEqual([6, 4]);
		expect(speedParts(1, 4).map((p) => p.value)).toEqual([0, 4]);
	});
});

describe('windowName', () => {
	test.each([
		[300, '', '5-hour'],
		[60, '', 'hourly'],
		[1440, '', 'daily'],
		[10_080, '', 'weekly'],
		[43_200, '', 'monthly'],
		[2880, '', '2-day'],
		[90, '', '90-minute'],
		[0, 'five_hour', '5-hour'],
		[0, 'seven_day', '7-day'],
		[0, 'whatever', '']
	])('%d minutes, id %s: %s', (minutes, id, want) => {
		expect(windowName(minutes, id)).toBe(want);
	});
});

describe('limitText', () => {
	const now = Date.parse('2026-11-06T18:00:00Z');
	const limit = (over: Partial<CodingLimit>): CodingLimit => ({
		deviceId: 'd1',
		tool: 'codex',
		id: 'codex:primary',
		windowMinutes: 10_080,
		status: 'ok',
		plan: 'plus',
		seen: '2026-11-06T17:50:00Z',
		expired: false,
		...over
	});

	test('a reading that holds', () => {
		const t = limitText(
			limit({ used: 63.2, resetsAt: new Date(now + (2 * 24 + 5) * 3600_000).toISOString() }),
			now
		);
		expect(t).toEqual({
			title: 'Weekly limit',
			text: '63% of the weekly limit used, resets in 2d 5h',
			used: 63.2,
			state: 'ok'
		});
	});

	test('a reading whose window has started over', () => {
		const t = limitText(limit({ used: 63, expired: true, resetsAt: '2026-11-06T10:00:00Z' }), now);
		expect(t.state).toBe('expired');
		expect(t.used).toBeNull();
		expect(t.text).toContain('63% then');
	});

	test('a limit that was hit, and when it resets', () => {
		const t = limitText(
			limit({
				tool: 'claude',
				id: 'five_hour',
				windowMinutes: 300,
				status: 'rejected',
				resetsAt: new Date(now + 80 * 60_000).toISOString()
			}),
			now
		);
		expect(t.state).toBe('rejected');
		expect(t.text).toBe('Limit reached, resets in 1h 20m');
		expect(t.used).toBe(100);
		expect(t.title).toBe('5-hour limit');
	});

	test('a limit that was hit and has reset since', () => {
		const t = limitText(
			limit({
				tool: 'claude',
				id: 'five_hour',
				windowMinutes: 300,
				status: 'rejected',
				expired: true,
				seen: '2026-11-06T09:00:00Z'
			}),
			now
		);
		expect(t.state).toBe('expired');
		expect(t.text).toContain('Hit the 5-hour limit 9h ago');
	});

	test('a limit with nothing in it', () => {
		expect(limitText(limit({ windowMinutes: 0, id: 'x' }), now)).toMatchObject({
			title: 'Limit',
			state: 'unknown',
			used: null
		});
	});
});

describe('sourceHint', () => {
	const src = (status: string, tool = 'opencode'): CodingSource => ({
		deviceId: 'd1',
		tool,
		status,
		at: '2026-11-06T18:00:00Z'
	});

	test('what is read says nothing', () => {
		expect(sourceHint(src('ok'), 'this machine')).toBeUndefined();
	});

	test('OpenCode without sqlite3 or Python', () => {
		expect(sourceHint(src('needs-sqlite'), 'this machine')).toBe(
			'Install sqlite3 or Python 3 on this machine to read OpenCode.'
		);
	});

	test('files that cannot be read', () => {
		expect(sourceHint(src('unreadable', 'claude'), 'vps-fra')).toBe(
			"The agent on vps-fra couldn't read Claude Code's files."
		);
	});
});

test('versionBefore', () => {
	expect(versionBefore('1.3.1', '1.4.0')).toBe(true);
	expect(versionBefore('1.4.0', '1.4.0')).toBe(false);
	expect(versionBefore('1.10.0', '1.4.0')).toBe(false);
	expect(versionBefore('1.4', '1.4.1')).toBe(true);
	expect(versionBefore('2.0.0', '1.4.0')).toBe(false);
});

test('modelsText', () => {
	expect(modelsText([])).toBe('');
	expect(modelsText(['a'])).toBe('a');
	expect(modelsText(['a', 'b', 'c'])).toBe('a +2');
});

describe('series', () => {
	const summary = {
		step: 'day',
		series: [
			{
				tool: 'claude',
				points: [
					{ ts: 1000, cost: 2, tokens: 20 },
					{ ts: 2000, cost: 0, tokens: 0 }
				]
			},
			{
				tool: 'codex',
				points: [
					{ ts: 1000, cost: 1, tokens: 10 },
					{ ts: 2000, cost: 4, tokens: 40 }
				]
			}
		]
	} as unknown as CodingSummary;

	test('chartSeries plots the metric, a line for each tool', () => {
		const cost = chartSeries(summary, 'cost');
		expect(cost.map((s) => s.key)).toEqual(['claude', 'codex']);
		expect(cost[1].points).toEqual([
			{ ts: 1000, v: 1 },
			{ ts: 2000, v: 4 }
		]);
		expect(chartSeries(summary, 'tokens')[0].points[0].v).toBe(20);
		expect(chartSeries(summary, 'cost')[0].label).toBe('Claude Code');
	});

	test('buckets adds the tools up, latest first', () => {
		expect(buckets(summary)).toEqual([
			{ ts: 2000, cost: 4, tokens: 40 },
			{ ts: 1000, cost: 3, tokens: 30 }
		]);
	});

	test('axis and bucket text', () => {
		expect(axisText('cost', 300)).toBe('$300');
		expect(axisText('tokens', 2_000_000)).toBe('2M');
		expect(tickFormat('day')).toEqual({ month: 'short', day: 'numeric' });
		expect(tickFormat('hour')).toEqual({ hour: 'numeric' });
		// The day is in the machine's own time zone, so only check what it holds.
		const day = bucketText(Date.parse('2026-11-06T12:00:00Z'), 'day');
		expect(day).toContain('Nov');
		expect(bucketText(Date.parse('2026-11-06T12:00:00Z'), 'hour').length).toBeGreaterThan(
			day.length
		);
	});
});

test('the estimate is explained', () => {
	expect(apiEstimateHelp).toContain('Subscriptions');
});

describe('limitGroups', () => {
	const now = Date.parse('2026-11-06T18:00:00Z');
	const names = (id: string) => ({ d1: 'macbook', d2: 'work-mac', d3: 'desktop' })[id] ?? id;
	const summary = {
		limits: [
			{
				deviceId: 'd1',
				tool: 'codex',
				id: 'codex:primary',
				windowMinutes: 10_080,
				used: 63,
				resetsAt: '2026-11-08T23:00:00Z',
				status: 'ok',
				plan: 'plus',
				seen: '2026-11-06T17:50:00Z',
				expired: false
			}
		],
		sources: [
			{ deviceId: 'd1', tool: 'claude', status: 'ok', at: '' },
			{ deviceId: 'd1', tool: 'codex', status: 'ok', at: '' },
			{ deviceId: 'd2', tool: 'claude', status: 'ok', at: '' },
			{ deviceId: 'd3', tool: 'opencode', status: 'ok', at: '' },
			{ deviceId: 'd3', tool: 'codex', status: 'unreadable', at: '' }
		]
	} as unknown as CodingSummary;

	test('lists a machine with a limit, and the ones whose tools say none, by name', () => {
		const groups = limitGroups(summary, now, names);
		expect(groups.map((g) => g.name)).toEqual(['macbook', 'work-mac']);
		expect(groups[0].rows.map((r) => [r.tool, r.state])).toEqual([
			['claude', 'none'],
			['codex', 'ok']
		]);
		expect(groups[0].rows[1].text).toContain('63% of the weekly limit used');
		expect(groups[0].rows[1].title).toBe('Codex · weekly limit');
		expect(groups[1].rows[0].text).toContain('No limit hit recently');
	});

	test('a tool with no limits to say and an unreadable one add nothing', () => {
		const groups = limitGroups(summary, now, names);
		expect(groups.some((g) => g.name === 'desktop')).toBe(false);
	});
});

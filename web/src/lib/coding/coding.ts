// How the UI shows what the AI tools (Claude Code, Codex, OpenCode and more) on the
// tailnet's machines used: tokens, what they'd cost on the model makers'
// APIs, and where the tools' limits stand. All of it is worked out by the hub;
// this is words, numbers and colors.

import type {
	CodingCost,
	CodingLimit,
	CodingSource,
	CodingSummary,
	CodingTokens
} from '#lib/api/types.js';
import type { ChartSeries } from '#lib/charts/AreaChart.svelte';
import { ago } from '#lib/format.js';

/** How far back a summary goes. */
export type CodingRange = '24h' | '7d' | '30d' | '90d' | '1y';

/** The ranges, for a picker. */
export const codingRanges: { value: CodingRange; label: string }[] = [
	{ value: '24h', label: '24h' },
	{ value: '7d', label: '7 days' },
	{ value: '30d', label: '30 days' },
	{ value: '90d', label: '90 days' },
	{ value: '1y', label: '1 year' }
];

/** A range for "in the last …", such as "24 hours" or "30 days". */
export function codingRangeName(range: CodingRange): string {
	return { '24h': '24 hours', '7d': '7 days', '30d': '30 days', '90d': '90 days', '1y': 'year' }[
		range
	];
}

/** What a device's page says about its AI usage card, at the top, where it is out of sight. */
export interface Glance {
	/** Whether an admin turned watching on. */
	enabled: boolean;
	/** Whether there is anything to show. */
	hasData: boolean;
	/** The estimated cost over the card's range. */
	cost: number;
	/** How many sessions are going now. */
	live: number;
	/** The card's range, such as "7 days". */
	range: string;
}

/** What a chart plots. */
export type Metric = 'cost' | 'tokens';

/** The agent version that reports AI tools' usage. */
export const minAgentVersion = '1.4.0';

const toolNames: Record<string, string> = {
	claude: 'Claude Code',
	codex: 'Codex',
	opencode: 'OpenCode',
	pi: 'Pi',
	gemini: 'Gemini CLI',
	grok: 'Grok',
	muse: 'Muse',
	antigravity: 'Antigravity'
};

/** A tool's name, such as "Claude Code". A tool this version doesn't know gets its id, capitalised. */
export function toolName(tool: string): string {
	return toolNames[tool] ?? (tool ? tool[0].toUpperCase() + tool.slice(1) : 'Unknown tool');
}

const toolColors: Record<string, string> = {
	claude: '#d95926',
	// Not the near-white of a lit lamp, which a tool's color would be taken for.
	codex: '#199e70',
	opencode: '#3987e5',
	pi: '#9085e9',
	gemini: '#14a3b8',
	grok: '#c98500',
	muse: '#d55181',
	antigravity: '#e66767'
};

/** Colors for the tools this version doesn't know, which stay the same for a tool. */
const spare = ['#8f9a2c', '#b8743d', '#6f86d6', '#a85c9e'];

/** A tool's color in charts, which stays the same for the tool whichever page it's on. */
export function toolColor(tool: string): string {
	if (toolColors[tool]) {
		return toolColors[tool];
	}
	let h = 0;
	for (const c of tool) {
		h = (h * 31 + c.charCodeAt(0)) >>> 0;
	}
	return spare[h % spare.length];
}

/** Trims a trailing ".00" or ".50" of a number written to some digits. */
function trim(s: string): string {
	return s.includes('.') ? s.replace(/\.?0+$/, '') : s;
}

/**
 * A count of tokens to three digits, such as 842, 20.7K, 8.99M or 101M, and
 * B for billions and T for trillions. It's rounded, so 999,600 is 1M.
 */
export function compactTokens(n: number | null | undefined): string {
	if (n == null || !Number.isFinite(n)) {
		return '—';
	}
	const sign = n < 0 ? '-' : '';
	let v = Math.abs(n);
	if (v < 1000) {
		return `${sign}${Math.round(v)}`;
	}
	const units = ['K', 'M', 'B', 'T'];
	const digits = (x: number) => (x < 10 ? 2 : x < 100 ? 1 : 0);
	let i = 0;
	v /= 1000;
	// 999.6K is 1M, not 1000K.
	while (Number(v.toFixed(digits(v))) >= 1000 && i < units.length - 1) {
		v /= 1000;
		i++;
	}
	return `${sign}${trim(v.toFixed(digits(v)))}${units[i]}`;
}

/** Dollars to the cent, such as $3,246.37, or <$0.01 for less than that. */
export function dollars(v: number | null | undefined): string {
	if (v == null || !Number.isFinite(v)) {
		return '—';
	}
	if (v > 0 && v < 0.005) {
		return '<$0.01';
	}
	return v.toLocaleString('en-US', { style: 'currency', currency: 'USD' });
}

/** Dollars for the axis of a chart and for a tooltip that has room for little, such as $300, $1.5k or $0.25. */
export function dollarsShort(v: number): string {
	if (!Number.isFinite(v) || v === 0) {
		return '$0';
	}
	if (v >= 10_000) {
		return `$${Math.round(v / 1000)}k`;
	}
	if (v >= 1000) {
		return `$${trim((v / 1000).toFixed(1))}k`;
	}
	if (v >= 100) {
		return `$${Math.round(v)}`;
	}
	return `$${trim(v.toFixed(v >= 10 ? 1 : 2))}`;
}

/** A part of a whole as a percent, such as 18.8%, <0.1% or 100%. */
export function share(part: number, whole: number): string {
	if (!(whole > 0) || !(part > 0)) {
		return '0%';
	}
	const p = (part / whole) * 100;
	if (p < 0.1) {
		return '<0.1%';
	}
	return p >= 99.95 ? '100%' : `${p.toFixed(1)}%`;
}

/** What a cost is, for a line under a number and for a tooltip. */
export const apiEstimate = 'API estimate';

/** Why the cost is only an estimate, for a tooltip. */
export const apiEstimateHelp =
	"What the same tokens would cost on the model maker's API, at the list price. Subscriptions such as Claude Max or ChatGPT Plus don't bill per token, so this isn't what anyone paid. Where a tool wrote down what a request cost, that figure is used.";

/** The kinds of token, as the cost and token bars show them. */
export interface Part {
	key: string;
	label: string;
	value: number;
	color: string;
}

const partColors = {
	input: '#3987e5',
	cacheRead: '#9085e9',
	cacheWrite: '#c98500',
	output: '#d55181'
};

/** What each kind of token cost, for a bar. */
export function costParts(c: CodingCost): Part[] {
	return [
		{ key: 'input', label: 'Input', value: c.input, color: partColors.input },
		{ key: 'cacheRead', label: 'Cache read', value: c.cacheRead, color: partColors.cacheRead },
		{ key: 'cacheWrite', label: 'Cache write', value: c.cacheWrite, color: partColors.cacheWrite },
		{ key: 'output', label: 'Output', value: c.output, color: partColors.output }
	];
}

/** How many tokens of each kind, for a bar. */
export function tokenParts(t: CodingTokens): Part[] {
	return [
		{ key: 'input', label: 'Uncached input', value: t.input, color: partColors.input },
		{ key: 'cacheRead', label: 'Cache read', value: t.cacheRead, color: partColors.cacheRead },
		{ key: 'cacheWrite', label: 'Cache write', value: t.cacheWrite, color: partColors.cacheWrite },
		{ key: 'output', label: 'Output', value: t.output, color: partColors.output }
	];
}

/** The cost at the standard tier and at the fast one, for a bar. */
export function speedParts(total: number, fast: number): Part[] {
	return [
		{ key: 'standard', label: 'Standard', value: Math.max(0, total - fast), color: '#7d8085' },
		{ key: 'fast', label: 'Fast', value: fast, color: '#f5b544' }
	];
}

/** The value of a series' point as the chart and its tooltip show it. */
export function metricText(metric: Metric, v: number): string {
	return metric === 'cost' ? dollars(v) : compactTokens(v);
}

/** An axis label for a value of a metric. */
export function axisText(metric: Metric, v: number): string {
	return metric === 'cost' ? dollarsShort(v) : compactTokens(v);
}

/** The lines of a chart: a tool each, in the summary's order, which is biggest first. */
export function chartSeries(summary: CodingSummary, metric: Metric): ChartSeries[] {
	return summary.series.map((s) => ({
		key: s.tool,
		label: toolName(s.tool),
		color: toolColor(s.tool),
		points: s.points.map((p) => ({ ts: p.ts, v: metric === 'cost' ? p.cost : p.tokens }))
	}));
}

/** What every tool used in each hour or day, the latest first, for a table. */
export function buckets(summary: CodingSummary): { ts: number; cost: number; tokens: number }[] {
	const sums = new Map<number, { ts: number; cost: number; tokens: number }>();
	for (const s of summary.series) {
		for (const p of s.points) {
			const b = sums.get(p.ts) ?? { ts: p.ts, cost: 0, tokens: 0 };
			b.cost += p.cost;
			b.tokens += p.tokens;
			sums.set(p.ts, b);
		}
	}
	return [...sums.values()].sort((a, b) => b.ts - a.ts);
}

/** A bucket's time as the table and tooltip show it: a day such as Nov 6, or an hour such as Nov 6, 3 PM. */
export function bucketText(ts: number, step: string): string {
	return new Date(ts).toLocaleString(
		undefined,
		step === 'day'
			? { month: 'short', day: 'numeric' }
			: { month: 'short', day: 'numeric', hour: 'numeric' }
	);
}

/** How a chart's axis writes a time, for a step of an hour or a day. */
export function tickFormat(step: string): Intl.DateTimeFormatOptions {
	return step === 'day' ? { month: 'short', day: 'numeric' } : { hour: 'numeric' };
}

/** The time zone the browser is in, which the hub makes days of. */
export function browserTimeZone(): string {
	try {
		return Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC';
	} catch {
		return 'UTC';
	}
}

/** The name of a limit's window, such as "weekly" or "5-hour", from its length in minutes or else its id. */
export function windowName(minutes: number, id = ''): string {
	if (minutes > 0) {
		if (minutes === 60) {
			return 'hourly';
		}
		if (minutes === 1440) {
			return 'daily';
		}
		if (minutes === 10_080) {
			return 'weekly';
		}
		if (minutes >= 40_000 && minutes <= 44_640) {
			return 'monthly';
		}
		if (minutes % 1440 === 0) {
			return `${minutes / 1440}-day`;
		}
		if (minutes % 60 === 0) {
			return `${minutes / 60}-hour`;
		}
		return `${minutes}-minute`;
	}
	const named: Record<string, string> = {
		five_hour: '5-hour',
		seven_day: '7-day',
		seven_day_opus: '7-day Opus',
		seven_day_sonnet: '7-day Sonnet'
	};
	return named[id] ?? '';
}

/** A limit's state, which decides how it's drawn. */
export type LimitState = 'ok' | 'rejected' | 'expired' | 'unknown';

export interface LimitText {
	/** Such as "Weekly limit". */
	title: string;
	/** Such as "63% of the weekly limit used, resets in 2d 5h". */
	text: string;
	/** The percent to draw as a meter, if the reading still holds. */
	used: number | null;
	state: LimitState;
}

/**
 * What a limit says, at `now`. A reading is from when the tool wrote it, so one
 * whose window has started over since says that and not the old percent. Claude
 * Code writes a limit only when it refuses a request for being over one.
 */
export function limitText(l: CodingLimit, now: number): LimitText {
	const name = windowName(l.windowMinutes, l.id);
	const title = name ? `${name[0].toUpperCase()}${name.slice(1)} limit` : 'Limit';
	const noun = name ? `the ${name} limit` : 'the limit';
	const resets = l.resetsAt ? ago(l.resetsAt, now) : '';
	if (l.status === 'rejected') {
		if (l.expired) {
			return {
				title,
				text: `Hit ${noun} ${ago(l.seen, now)}. The window has reset since`,
				used: null,
				state: 'expired'
			};
		}
		return {
			title,
			text: `Limit reached${resets ? `, resets ${resets}` : ''}`,
			used: 100,
			state: 'rejected'
		};
	}
	if (l.used != null) {
		const pct = Math.round(l.used);
		if (l.expired) {
			return {
				title,
				text: `The window has reset since ${l.tool === 'codex' ? 'Codex' : 'the tool'} last said (${pct}% then)`,
				used: null,
				state: 'expired'
			};
		}
		return {
			title,
			text: `${pct}% of ${noun} used${resets ? `, resets ${resets}` : ''}`,
			used: l.used,
			state: 'ok'
		};
	}
	return { title, text: 'Not reported', used: null, state: 'unknown' };
}

/** What to say about a tool the agent has trouble reading, or undefined if it reads it. `where` is the machine's name, or "this machine". */
export function sourceHint(s: CodingSource, where: string): string | undefined {
	switch (s.status) {
		case 'ok':
			return undefined;
		case 'needs-sqlite':
			return `Install sqlite3 or Python 3 on ${where} to read ${toolName(s.tool)}.`;
		case 'needs-python':
			return `Install Python 3 on ${where} to read ${toolName(s.tool)}.`;
		default:
			return `The agent on ${where} couldn't read ${toolName(s.tool)}'s files.`;
	}
}

/** Whether version `a` of the agent comes before `b`, such as 1.3.1 before 1.4.0. */
export function versionBefore(a: string, b: string): boolean {
	const pa = a.split('.').map((n) => parseInt(n, 10) || 0);
	const pb = b.split('.').map((n) => parseInt(n, 10) || 0);
	for (let i = 0; i < Math.max(pa.length, pb.length); i++) {
		const d = (pa[i] ?? 0) - (pb[i] ?? 0);
		if (d !== 0) {
			return d < 0;
		}
	}
	return false;
}

/** The models a session used, such as "claude-opus-5-5 +1". */
export function modelsText(models: string[]): string {
	if (models.length === 0) {
		return '';
	}
	return models.length === 1 ? models[0] : `${models[0]} +${models.length - 1}`;
}

/** One line of the limits page: a limit, or the lack of one. */
export interface LimitRow {
	key: string;
	tool: string;
	title: string;
	text: string;
	/** The percent to draw as a meter, if there is one. */
	used: number | null;
	state: LimitState | 'none';
	plan: string;
}

/** The limits of one machine. */
export interface LimitGroup {
	deviceId: string;
	name: string;
	rows: LimitRow[];
}

/** The tools that say anything about their limits, in the order the page lists them. */
const limitTools = ['claude', 'codex'];

/** What a tool with no limit on record says, if it's one that can. */
function noLimit(tool: string): string {
	return tool === 'claude'
		? 'No limit hit recently'
		: `${toolName(tool)} hasn't said how much of its limit is used.`;
}

/**
 * The limits of every machine whose tools say anything about them, in order of
 * the machine's name. A tool that is read but has no limit on record gets a line that
 * says so, which for Claude Code is the usual state.
 */
export function limitGroups(
	summary: CodingSummary,
	now: number,
	nameOf: (deviceId: string) => string
): LimitGroup[] {
	const wanted = new Map<string, Set<string>>();
	const want = (device: string, tool: string) => {
		if (!wanted.has(device)) {
			wanted.set(device, new Set());
		}
		wanted.get(device)?.add(tool);
	};
	for (const l of summary.limits) {
		want(l.deviceId, l.tool);
	}
	for (const s of summary.sources) {
		if (s.status === 'ok' && limitTools.includes(s.tool)) {
			want(s.deviceId, s.tool);
		}
	}
	const order = (tool: string) => {
		const i = limitTools.indexOf(tool);
		return i < 0 ? limitTools.length : i;
	};
	return [...wanted]
		.map(([deviceId, tools]) => ({
			deviceId,
			name: nameOf(deviceId),
			rows: [...tools]
				.sort((a, b) => order(a) - order(b) || a.localeCompare(b))
				.flatMap((tool): LimitRow[] => {
					const own = summary.limits.filter((l) => l.deviceId === deviceId && l.tool === tool);
					if (own.length === 0) {
						return [
							{
								key: `${deviceId}:${tool}`,
								tool,
								title: toolName(tool),
								text: noLimit(tool),
								used: null,
								state: 'none',
								plan: ''
							}
						];
					}
					return own.map((l) => {
						const t = limitText(l, now);
						return {
							key: `${deviceId}:${tool}:${l.id}`,
							tool,
							title: `${toolName(tool)} · ${t.title.toLowerCase()}`,
							text: t.text,
							used: t.used,
							state: t.state,
							plan: l.plan
						};
					});
				})
		}))
		.sort((a, b) => a.name.localeCompare(b.name));
}

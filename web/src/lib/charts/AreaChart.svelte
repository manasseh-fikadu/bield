<!--
	A time-series chart of one or more lines, each with a faint area under it.
	Pointing at it, or focusing it and pressing the arrow keys, shows every
	series' value at that time. The lines draw in from the left once.
-->
<script lang="ts" module>
	import type { Point } from './path.js';

	export interface ChartSeries {
		key: string;
		label: string;
		/** A CSS color. */
		color: string;
		/** The series' points, oldest first. */
		points: Point[];
	}
</script>

<script lang="ts">
	import type { Snippet } from 'svelte';
	import {
		areaPath,
		linePath,
		nearest,
		niceMax,
		runs,
		timeTickFormat,
		typicalStep
	} from './path.js';

	interface Props {
		series: ChartSeries[];
		/** What the chart shows, for screen readers, such as "CPU over the last hour". */
		label: string;
		height?: number;
		/** The value at the top of the axis. A round number above the largest value if it's not given. */
		yMax?: number;
		/** Formats a value for the axis and the tooltip. */
		format?: (v: number) => string;
		/** The time at the left edge, in Unix milliseconds. The first point's if it's not given. */
		from?: number;
		/** The time at the right edge. The last point's if it's not given. */
		to?: number;
		/** Makes points selectable, with a click or Enter. */
		onSelect?: (ts: number) => void;
		/** Areas under the lines. Turn them off when several series overlap. */
		fill?: boolean;
		/** Leaves out the axes, for a small chart next to another. */
		compact?: boolean;
		/** Draws the tooltip's body for a time, in place of each series' value. */
		tooltip?: Snippet<[number]>;
		/** Called with the time being pointed at, or null when it stops. */
		onHover?: (ts: number | null) => void;
		/** How the axis writes a time. It depends on how long the chart is if it's not given. */
		tickFormat?: Intl.DateTimeFormatOptions;
	}

	let {
		series,
		label,
		height = 160,
		yMax,
		format = (v: number) => v.toFixed(0),
		from,
		to,
		onSelect,
		fill = true,
		compact = false,
		tooltip,
		onHover,
		tickFormat: tickFormatProp
	}: Props = $props();

	const pad = $derived(
		compact ? { left: 0, right: 0, top: 4, bottom: 1 } : { left: 40, right: 0, top: 6, bottom: 22 }
	);

	let width = $state(0);
	/** The time being pointed at, or null. */
	let hoverAt: number | null = $state(null);
	const hover = $derived(hoverAt);

	function setHover(ts: number | null) {
		if (ts !== hoverAt) {
			hoverAt = ts;
			onHover?.(ts);
		}
	}

	const all = $derived(series.flatMap((s) => s.points));
	const t0 = $derived(
		from ?? (all.length ? Math.min(...all.map((p) => p.ts)) : Date.now() - 3_600_000)
	);
	const t1 = $derived(to ?? (all.length ? Math.max(...all.map((p) => p.ts)) : Date.now()));
	const top = $derived(
		yMax !== undefined && yMax > 0 ? yMax : niceMax(Math.max(0, ...all.map((p) => p.v ?? 0)) * 1.1)
	);
	const innerW = $derived(Math.max(0, width - pad.left - pad.right));
	const innerH = $derived(height - pad.top - pad.bottom);
	const baseline = $derived(pad.top + innerH);
	const step = $derived(typicalStep(series[0]?.points ?? []));
	const times = $derived([...new Set(all.map((p) => p.ts))].sort((a, b) => a - b));

	function x(ts: number): number {
		return pad.left + (t1 > t0 ? ((ts - t0) / (t1 - t0)) * innerW : innerW / 2);
	}

	function y(v: number): number {
		return pad.top + innerH - (Math.min(v, top) / top) * innerH;
	}

	const yTicks = $derived([0, 0.5, 1].map((f) => f * top));
	const xTicks = $derived.by(() => {
		if (width <= 0) {
			return [];
		}
		// All the points at one time: one label in the middle, not several on top of each other.
		if (t1 <= t0) {
			return [t0];
		}
		const count = width > 500 ? 5 : 3;
		return Array.from({ length: count }, (_, i) => t0 + ((t1 - t0) * i) / (count - 1));
	});
	const tickFormat = $derived(tickFormatProp ?? timeTickFormat(t1 - t0));

	function valueAt(s: ChartSeries, ts: number): number | null {
		return s.points.find((p) => p.ts === ts)?.v ?? null;
	}

	function when(ts: number): string {
		return new Date(ts).toLocaleString(undefined, {
			month: 'short',
			day: 'numeric',
			hour: 'numeric',
			minute: '2-digit'
		});
	}

	/** The time of the point nearest the pointer. */
	function timeAt(e: MouseEvent): number | null {
		if (!times.length || innerW <= 0) {
			return null;
		}
		const rect = (e.currentTarget as Element).getBoundingClientRect();
		const ts = t0 + ((e.clientX - rect.left - pad.left) / innerW) * (t1 - t0);
		return times[nearest(times, ts)];
	}

	function onkeydown(e: KeyboardEvent) {
		if (!times.length) {
			return;
		}
		const i = hover === null ? times.length - 1 : nearest(times, hover);
		switch (e.key) {
			case 'ArrowLeft':
				setHover(times[Math.max(0, i - 1)]);
				break;
			case 'ArrowRight':
				setHover(times[Math.min(times.length - 1, i + 1)]);
				break;
			case 'Home':
				setHover(times[0]);
				break;
			case 'End':
				setHover(times[times.length - 1]);
				break;
			case 'Enter':
			case ' ':
				if (hover !== null && onSelect) {
					onSelect(hover);
				}
				break;
			default:
				return;
		}
		e.preventDefault();
	}

	/** The tooltip's text, for screen readers. */
	const valueText = $derived(
		hover === null
			? undefined
			: `${when(hover)}: ${series
					.map((s) => {
						const v = valueAt(s, hover!);
						return `${s.label} ${v === null ? 'no data' : format(v)}`;
					})
					.join(', ')}`
	);
</script>

<div bind:clientWidth={width} class="relative w-full select-none" style:height="{height}px">
	{#if width > 0}
		<svg
			{width}
			{height}
			class={['block touch-pan-y rounded-sm', onSelect && 'cursor-pointer']}
			role="slider"
			tabindex="0"
			aria-label={label}
			aria-valuemin={times[0]}
			aria-valuemax={times[times.length - 1]}
			aria-valuenow={hover ?? undefined}
			aria-valuetext={valueText}
			onpointermove={(e) => setHover(timeAt(e))}
			onpointerleave={() => setHover(null)}
			onfocus={() => setHover(hoverAt ?? times[times.length - 1] ?? null)}
			onblur={() => setHover(null)}
			{onkeydown}
			onclick={(e) => {
				// From the click itself: on touch screens, pointerleave clears the
				// hover before the click arrives.
				const ts = timeAt(e);
				if (ts !== null && onSelect) {
					onSelect(ts);
				}
			}}
		>
			{#if !compact}
				{#each yTicks as t, i (i)}
					<line
						x1={pad.left}
						x2={width - pad.right}
						y1={y(t)}
						y2={y(t)}
						stroke="var(--color-line)"
					/>
					<text x={0} y={y(t) + 4} class="fill-fg-faint font-mono text-[11px]">
						{format(t)}
					</text>
				{/each}
				{#each xTicks as t, i (i)}
					<text
						x={x(t)}
						y={height - 6}
						text-anchor={xTicks.length === 1
							? 'middle'
							: i === 0
								? 'start'
								: i === xTicks.length - 1
									? 'end'
									: 'middle'}
						class="fill-fg-faint font-mono text-[11px]"
					>
						{new Date(t).toLocaleString(undefined, tickFormat)}
					</text>
				{/each}
			{:else}
				<line x1={0} x2={width} y1={baseline} y2={baseline} stroke="var(--color-line)" />
			{/if}
			<g class="animate-wipe">
				{#each series as s (s.key)}
					{#each runs(s.points, x, y, step * 3) as run, j (j)}
						{#if run.length === 1}
							<!-- A point between two gaps has no line, so it gets a dot. -->
							<circle cx={run[0][0]} cy={run[0][1]} r="2" fill={s.color} />
						{:else}
							{#if fill}
								<path d={areaPath(run, baseline)} fill={s.color} fill-opacity="0.06" />
							{/if}
							<path
								d={linePath(run)}
								fill="none"
								stroke={s.color}
								stroke-width="1.5"
								stroke-linejoin="round"
							/>
						{/if}
					{/each}
				{/each}
			</g>
			{#if hover !== null}
				<line
					x1={x(hover)}
					x2={x(hover)}
					y1={pad.top}
					y2={baseline}
					stroke="var(--color-fg-faint)"
				/>
				{#each series as s (s.key)}
					{@const v = valueAt(s, hover)}
					{#if v !== null}
						<circle
							cx={x(hover)}
							cy={y(v)}
							r="3"
							fill={s.color}
							stroke="var(--color-surface)"
							stroke-width="2"
						/>
					{/if}
				{/each}
			{/if}
		</svg>
		{#if hover !== null}
			<div
				class="pointer-events-none absolute -top-0.5 z-10 flex min-w-36 flex-col gap-1 rounded-lg border border-line-strong bg-raised p-3 shadow-popover"
				style:left={x(hover) <= width / 2 ? `${x(hover) + 12}px` : undefined}
				style:right={x(hover) > width / 2 ? `${width - x(hover) + 12}px` : undefined}
				aria-hidden="true"
			>
				{#if tooltip}
					{@render tooltip(hover)}
				{:else}
					<div class="font-mono text-[11px] leading-4 text-fg-faint">{when(hover)}</div>
					{#each series as s (s.key)}
						{@const v = valueAt(s, hover)}
						<div class="flex items-center justify-between gap-4 text-[12px] leading-4.25">
							<span class="flex items-center gap-1.5 text-fg-muted">
								<span class="h-0.5 w-3" style:background={s.color}></span>
								{s.label}
							</span>
							<span class="font-mono text-fg">{v === null ? '—' : format(v)}</span>
						</div>
					{/each}
				{/if}
			</div>
		{/if}
	{/if}
</div>

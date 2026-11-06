<!--
	What each tool used over a range, a line for each, by the hour for a day
	and by the day for anything longer. Pointing at it says what each used then.
-->
<script lang="ts">
	import type { CodingSummary } from '#lib/api/types.js';
	import AreaChart from '#lib/charts/AreaChart.svelte';
	import {
		axisText,
		bucketText,
		chartSeries,
		metricText,
		tickFormat,
		toolColor,
		toolName,
		type Metric
	} from './coding.js';

	interface Props {
		summary: CodingSummary;
		metric: Metric;
		height?: number;
	}

	let { summary, metric, height = 220 }: Props = $props();

	const series = $derived(chartSeries(summary, metric));
	const hasData = $derived(series.some((s) => s.points.some((p) => (p.v ?? 0) > 0)));
	const label = $derived(
		`${metric === 'cost' ? 'Cost' : 'Tokens'} by ${toolListText(summary)}, by the ${summary.step}`
	);

	function toolListText(s: CodingSummary): string {
		return s.series.length > 1 ? 'tool' : s.series[0] ? toolName(s.series[0].tool) : 'tool';
	}

	function valueAt(tool: string, ts: number): number {
		return series.find((s) => s.key === tool)?.points.find((p) => p.ts === ts)?.v ?? 0;
	}
</script>

{#snippet tip(ts: number)}
	<div class="font-mono text-[11px] leading-4 text-fg-faint">{bucketText(ts, summary.step)}</div>
	{#each series as s (s.key)}
		<div class="flex items-center justify-between gap-4 text-[12px] leading-4.25">
			<span class="flex items-center gap-1.5 text-fg-muted">
				<span class="h-0.5 w-3" style:background={toolColor(s.key)}></span>
				{toolName(s.key)}
			</span>
			<span class="font-mono text-fg">{metricText(metric, valueAt(s.key, ts))}</span>
		</div>
	{/each}
{/snippet}

<div>
	{#if series.length > 1}
		<ul class="mb-2 flex flex-wrap gap-x-4 gap-y-1">
			{#each series as s (s.key)}
				<li class="flex items-center gap-1.5 text-[12px] text-fg-muted">
					<span class="size-2 shrink-0 rounded-sm" style:background={s.color}></span>
					{s.label}
				</li>
			{/each}
		</ul>
	{/if}
	{#if hasData}
		<AreaChart
			{series}
			{label}
			{height}
			fill={false}
			format={(v) => axisText(metric, v)}
			tickFormat={tickFormat(summary.step)}
			tooltip={tip}
		/>
	{:else}
		<p
			class="flex items-center justify-center rounded-lg border border-line bg-canvas text-[13px] text-fg-subtle"
			style:height="{height}px"
		>
			Nothing was used in this range.
		</p>
	{/if}
</div>

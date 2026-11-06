<!--
	What was used, split by model, by day or hour, or by device, as a table of
	cost, its share and tokens (or tokens first, when the tokens are on show).
-->
<script lang="ts">
	import type { CodingSummary } from '#lib/api/types.js';
	import Badge from '#lib/ui/Badge.svelte';
	import Segmented from '#lib/ui/Segmented.svelte';
	import {
		bucketText,
		buckets,
		compactTokens,
		dollars,
		share,
		toolColor,
		toolName,
		type Metric
	} from './coding.js';

	interface Props {
		summary: CodingSummary;
		metric: Metric;
		/** A device's name, from its ID. */
		nameOf: (deviceId: string) => string;
		/** Whether the summary is of every device, so it can be split by device. */
		fleet?: boolean;
	}

	let { summary, metric, nameOf, fleet = false }: Props = $props();

	type By = 'model' | 'time' | 'device';
	let by = $state<By>('model');

	const options = $derived([
		{ value: 'model' as By, label: 'Model' },
		{ value: 'time' as By, label: summary.step === 'day' ? 'Day' : 'Hour' },
		...(fleet ? [{ value: 'device' as By, label: 'Device' }] : [])
	]);
	// A summary of one device has no Device to stay on.
	const shown = $derived(by === 'device' && !fleet ? 'model' : by);

	const heading = $derived(options.find((o) => o.value === shown)?.label ?? 'Model');

	interface Row {
		key: string;
		name: string;
		hint?: string;
		color?: string;
		unpriced?: boolean;
		off?: boolean;
		cost: number;
		tokens: number;
	}

	const rows: Row[] = $derived.by(() => {
		let out: Row[];
		if (shown === 'model') {
			out = summary.models.map((m) => ({
				key: `${m.tool}:${m.model}`,
				name: m.model,
				hint: toolName(m.tool),
				color: toolColor(m.tool),
				unpriced: !m.priced,
				cost: m.cost,
				tokens: m.tokens.total
			}));
		} else if (shown === 'device') {
			out = summary.devices
				.filter((d) => d.totals.tokens.total > 0)
				.map((d) => ({
					key: d.deviceId,
					name: nameOf(d.deviceId),
					off: !d.enabled,
					cost: d.totals.cost.total,
					tokens: d.totals.tokens.total
				}));
		} else {
			return buckets(summary)
				.filter((b) => b.tokens > 0 || b.cost > 0)
				.map((b) => ({
					key: String(b.ts),
					name: bucketText(b.ts, summary.step),
					cost: b.cost,
					tokens: b.tokens
				}));
		}
		const size = metric === 'cost' ? (r: Row) => r.cost : (r: Row) => r.tokens;
		return out.sort((a, b) => size(b) - size(a) || a.name.localeCompare(b.name));
	});

	const whole = $derived(
		metric === 'cost' ? summary.totals.cost.total : summary.totals.tokens.total
	);
	const th = 'px-3 py-2.5 font-normal first:pl-4 last:pr-4';
	const td = 'px-3 py-2.5 align-top first:pl-4 last:pr-4';
</script>

<div>
	<div class="mb-3 flex flex-wrap items-center justify-between gap-2">
		<h3 class="text-[14px] font-medium text-fg">Breakdown</h3>
		<Segmented bind:value={by} {options} label="Split by" />
	</div>
	{#if rows.length === 0}
		<p class="rounded-lg border border-line px-4 py-6 text-center text-[13px] text-fg-subtle">
			Nothing to split in this range.
		</p>
	{:else}
		<div class="overflow-x-auto rounded-lg border border-line">
			<table class="w-full text-left text-[13px]">
				<thead>
					<tr class="border-b border-line text-[12px] text-fg-subtle">
						<th scope="col" class={th}>{heading}</th>
						{#if metric === 'cost'}
							<th scope="col" class="{th} text-right">Cost</th>
							<th scope="col" class="{th} text-right">Share</th>
							<th scope="col" class="{th} hidden text-right sm:table-cell">Tokens</th>
						{:else}
							<th scope="col" class="{th} text-right">Tokens</th>
							<th scope="col" class="{th} text-right">Share</th>
							<th scope="col" class="{th} hidden text-right sm:table-cell">Cost</th>
						{/if}
					</tr>
				</thead>
				<tbody>
					{#each rows as r (r.key)}
						{@const part = metric === 'cost' ? r.cost : r.tokens}
						<tr class="border-b border-line last:border-0">
							<td class="{td} min-w-36">
								<span class="flex flex-wrap items-center gap-x-2 gap-y-0.5">
									<span
										class={[
											'min-w-0 break-words text-fg',
											shown === 'model' && 'font-mono text-[12.5px]'
										]}>{r.name}</span
									>
									{#if r.hint}
										<span class="shrink-0 text-[12px] text-fg-faint">{r.hint}</span>
									{/if}
									{#if r.unpriced}
										<Badge tone="amber">no price</Badge>
									{/if}
									{#if r.off}
										<Badge>watching off</Badge>
									{/if}
								</span>
								{#if r.color && whole > 0}
									<span
										class="mt-1.5 block h-0.5 rounded-[1px]"
										style:width="{Math.max(2, (part / whole) * 100)}%"
										style:background={r.color}
									></span>
								{/if}
							</td>
							{#if metric === 'cost'}
								<td class="{td} text-right font-mono text-fg">{dollars(r.cost)}</td>
								<td class="{td} text-right font-mono text-fg-muted">{share(r.cost, whole)}</td>
								<td class="{td} hidden text-right font-mono text-fg-muted sm:table-cell"
									>{compactTokens(r.tokens)}</td
								>
							{:else}
								<td class="{td} text-right font-mono text-fg">{compactTokens(r.tokens)}</td>
								<td class="{td} text-right font-mono text-fg-muted">{share(r.tokens, whole)}</td>
								<td class="{td} hidden text-right font-mono text-fg-muted sm:table-cell"
									>{dollars(r.cost)}</td
								>
							{/if}
						</tr>
					{/each}
				</tbody>
			</table>
		</div>
	{/if}
</div>

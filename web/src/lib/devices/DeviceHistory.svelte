<!--
	A device's history over a range: its CPU and memory, and under them its
	network and its round trip from the hub. Pointing at the first says what
	was running then, if Bield measures the machine. It loads again every
	minute.
-->
<script lang="ts">
	import { getDeviceHistory, getDeviceUsage } from '#lib/api/client.js';
	import type { Device, DeviceHistory, UsageSample } from '#lib/api/types.js';
	import AreaChart from '#lib/charts/AreaChart.svelte';
	import type { Point } from '#lib/charts/path.js';
	import { percent, rateShort } from '#lib/format.js';
	import { historyRanges, rangeName, type HistoryRange } from '#lib/history.js';
	import { cpuText } from '#lib/usage.js';
	import Segmented from '#lib/ui/Segmented.svelte';
	import Skeleton from '#lib/ui/Skeleton.svelte';

	interface Props {
		device: Device;
		range: HistoryRange;
		/**
		 * Makes the CPU and memory chart clickable. It's called with the
		 * middle of the step that was clicked, in Unix milliseconds.
		 */
		onSelect?: (ts: number) => void;
	}

	let { device, range = $bindable(), onSelect }: Props = $props();

	let history = $state.raw<DeviceHistory | null>(null);
	let failed = $state(false);
	// Every snapshot passes in a new `device`, so the effect reads only its
	// ID, and loads again only for another device or range.
	const deviceId = $derived(device.id);
	const measured = $derived(!!device.measuredBy);

	$effect(() => {
		const id = deviceId;
		const r = range;
		let current = true;
		history = null;
		failed = false;
		async function load() {
			try {
				const h = await getDeviceHistory(id, r);
				if (current) {
					history = h;
					failed = false;
				}
			} catch {
				if (current) {
					failed = true;
				}
			}
		}
		load();
		const timer = setInterval(load, 60_000);
		return () => {
			current = false;
			clearInterval(timer);
		};
	});

	function hasData(...series: Point[][]): boolean {
		return series.some((points) => points.some((p) => p.v !== null));
	}

	const cpuColor = 'var(--color-lamp)';
	const memoryColor = 'var(--color-blue)';

	const from = $derived(history ? Date.parse(history.from) : undefined);
	const to = $derived(history ? Date.parse(history.to) : undefined);
	const nothing = $derived(`Nothing measured in the last ${rangeName(range)}.`);

	/** What was running at the time being pointed at, once it has loaded. */
	let running = $state.raw<{ ts: number; sample: UsageSample | null } | null>(null);
	const loaded = new Map<string, UsageSample | null>();
	let waiting: ReturnType<typeof setTimeout> | undefined;

	function pointAt(ts: number | null) {
		clearTimeout(waiting);
		if (ts === null || !measured || !history) {
			return;
		}
		const at = ts + history.step / 2;
		const key = `${deviceId}:${ts}`;
		if (loaded.has(key)) {
			running = { ts, sample: loaded.get(key) ?? null };
			return;
		}
		// Sweeping across the chart shouldn't ask about every minute it passes.
		waiting = setTimeout(async () => {
			try {
				const { sample } = await getDeviceUsage(deviceId, at);
				loaded.set(key, sample ?? null);
				running = { ts, sample: sample ?? null };
			} catch {
				// The tooltip keeps to the chart's own values.
			}
		}, 150);
	}

	function moment(ts: number): string {
		return new Date(ts).toLocaleString(undefined, {
			weekday: 'short',
			hour: 'numeric',
			minute: '2-digit'
		});
	}

	function valueAt(points: Point[], ts: number): number | null {
		return points.find((p) => p.ts === ts)?.v ?? null;
	}
</script>

{#snippet tooltip(ts: number)}
	{@const h = history}
	{@const sample = running?.ts === ts ? running.sample : null}
	{@const cpu = h ? valueAt(h.cpu, ts) : null}
	{@const memory = h ? valueAt(h.memory, ts) : null}
	<div class="flex w-56 flex-col gap-2">
		<div class="flex justify-between gap-3">
			<span class="text-[12px] leading-4 font-medium text-fg">
				{measured ? 'What was running' : 'At this time'}
			</span>
			<span class="shrink-0 font-mono text-[12px] leading-4 text-fg-faint">{moment(ts)}</span>
		</div>
		<div class="flex flex-col gap-0.75">
			{#each (sample?.groups ?? []).toSorted((a, b) => b.cpu - a.cpu).slice(0, 3) as g, i (g.key)}
				<div class="flex justify-between gap-3">
					<span class={['truncate text-[12px] leading-4.25', i ? 'text-fg-muted' : 'text-fg']}>
						{g.name}
					</span>
					<span class={['font-mono text-[12px] leading-4.25', i ? 'text-fg-muted' : 'text-fg']}>
						{cpuText(g.cpu)}
					</span>
				</div>
			{/each}
			<div class={['flex justify-between gap-3', sample && 'border-t border-line-strong pt-1.25']}>
				<span class="text-[12px] leading-4.25 text-fg-faint">Total CPU</span>
				<span class="font-mono text-[12px] leading-4.25 text-fg">{percent(cpu)}</span>
			</div>
			<div class="flex justify-between gap-3">
				<span class="text-[12px] leading-4.25 text-fg-faint">Memory</span>
				<span class="font-mono text-[12px] leading-4.25 text-fg">{percent(memory)}</span>
			</div>
		</div>
	</div>
{/snippet}

<div class="flex flex-col gap-3.5">
	<div class="flex flex-wrap items-center justify-between gap-x-4 gap-y-2">
		<div class="flex items-center gap-4">
			<h2 class="text-[13px] leading-5 font-medium text-fg">History</h2>
			<span class="flex items-center gap-1.5 text-[12px] leading-4 text-fg-faint">
				<span class="h-0.5 w-3 shrink-0" style:background={cpuColor}></span>CPU
			</span>
			<span class="flex items-center gap-1.5 text-[12px] leading-4 text-fg-faint">
				<span class="h-0.5 w-3 shrink-0" style:background={memoryColor}></span>Memory
			</span>
		</div>
		<Segmented bind:value={range} options={historyRanges} label="Time range" />
	</div>

	{#if history}
		{@const h = history}
		{#if measured || hasData(h.cpu, h.memory, h.netRx, h.netTx)}
			<div class="relative">
				<AreaChart
					label="CPU and memory over the last {rangeName(range)}"
					series={[
						{ key: 'mem', label: 'Memory', color: memoryColor, points: h.memory },
						{ key: 'cpu', label: 'CPU', color: cpuColor, points: h.cpu }
					]}
					{from}
					{to}
					height={154}
					yMax={100}
					format={(v) => `${v.toFixed(0)}%`}
					onSelect={onSelect && ((ts) => onSelect(ts + h.step / 2))}
					onHover={pointAt}
					{tooltip}
				/>
				{#if !hasData(h.cpu, h.memory)}
					<p class="absolute inset-0 grid place-items-center pb-6 pl-10 text-[13px] text-fg-faint">
						{nothing}
					</p>
				{/if}
			</div>
		{/if}
		<div class="grid gap-6 md:grid-cols-2">
			{#if measured || hasData(h.netRx, h.netTx)}
				<div class="flex min-w-0 flex-col gap-2">
					<div class="flex justify-between gap-3">
						<h3 class="text-[12px] leading-4 font-normal text-fg-muted">Network</h3>
						<span class="flex items-center gap-3 text-[12px] leading-4 text-fg-faint">
							<span class="flex items-center gap-1.5">
								<span class="h-0.5 w-3 shrink-0 bg-lamp"></span>Down
							</span>
							<span class="flex items-center gap-1.5">
								<span class="h-0.5 w-3 shrink-0 bg-fg-faint"></span>Up
							</span>
						</span>
					</div>
					<div class="relative">
						<AreaChart
							label="Network traffic over the last {rangeName(range)}"
							series={[
								{ key: 'tx', label: 'Up', color: 'var(--color-fg-faint)', points: h.netTx },
								{ key: 'rx', label: 'Down', color: 'var(--color-lamp)', points: h.netRx }
							]}
							{from}
							{to}
							height={64}
							fill={false}
							compact
							format={(v) => `${rateShort(v)}/s`}
						/>
						{#if !hasData(h.netRx, h.netTx)}
							<p class="absolute inset-0 grid place-items-center text-[12px] text-fg-faint">
								{nothing}
							</p>
						{/if}
					</div>
				</div>
			{/if}
			{#if !device.self}
				<div class="flex min-w-0 flex-col gap-2">
					<div class="flex justify-between gap-3">
						<h3 class="text-[12px] leading-4 font-normal text-fg-muted">Round trip from the hub</h3>
					</div>
					<div class="relative">
						<AreaChart
							label="Round-trip time from the hub over the last {rangeName(range)}"
							series={[{ key: 'rtt', label: 'RTT', color: 'var(--color-lamp)', points: h.rtt }]}
							{from}
							{to}
							height={64}
							fill={false}
							compact
							format={(v) => `${v.toFixed(0)} ms`}
						/>
						{#if !hasData(h.rtt)}
							<p class="absolute inset-0 grid place-items-center text-[12px] text-fg-faint">
								{h.rtt.length ? 'No ping got a reply' : 'No pings yet'} in the last {rangeName(
									range
								)}.
							</p>
						{/if}
					</div>
				</div>
			{/if}
		</div>
	{:else if failed}
		<p class="grid h-40 place-items-center text-[13px] text-fg-faint">
			Couldn't load the history. Bield tries again in a minute.
		</p>
	{:else}
		<Skeleton class="h-40" />
	{/if}
</div>

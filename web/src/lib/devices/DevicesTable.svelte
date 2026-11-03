<!--
	A table of devices: each one's lamp, its route from the hub and round trip,
	what it uses, and how long it has been up, with a link to its page. Phones
	get a list instead.
-->
<script lang="ts">
	import { resolve } from '$app/paths';
	import { alertedDevices, quietlyOffline } from '#lib/alerts.js';
	import type { Alert, Device, Thresholds } from '#lib/api/types.js';
	import { getDeviceHistory } from '#lib/api/client.js';
	import LazyTrend from '#lib/charts/LazyTrend.svelte';
	import { levelText, limitsOf } from '#lib/device.js';
	import type { ContainerCount } from '#lib/docker/fleet.svelte.js';
	import { latency, osLabel, percent, ratio, shortDuration, since } from '#lib/format.js';
	import { stepValues } from '#lib/history.js';
	import { routeLabel, routeName, slowLink, tailnetIP } from '#lib/tailnet.js';
	import StatusDot from '#lib/ui/StatusDot.svelte';

	interface Props {
		devices: Device[];
		/** When "offline for" is measured from, in Unix milliseconds: the snapshot's time. */
		now: number;
		thresholds: Thresholds;
		/** The open alerts, which say whose lamp is red and whose is a quiet ring. */
		alerts: Alert[];
		/** How many containers run on each machine, if the table has a column for them. */
		containers?: Map<string, ContainerCount>;
		/** Adds each device's tailnet IP. */
		detailed?: boolean;
	}

	let { devices, now, thresholds, alerts, containers, detailed = false }: Props = $props();

	const alerted = $derived(alertedDevices(alerts));
	const unseen = $derived(alertedDevices(alerts.filter((a) => a.state === 'open')));

	function href(d: Device) {
		return resolve('/devices/[id]', { id: d.id });
	}

	/** A device's CPU over the last hour. */
	async function cpuTrend(id: string) {
		const h = await getDeviceHistory(id, '1h');
		return stepValues(h, h.cpu);
	}

	const th = 'h-7.5 px-0 text-[12px] leading-4 font-normal text-fg-faint';
	const num = 'text-right font-mono text-[12px] leading-5';
</script>

{#snippet lamp(d: Device)}
	<StatusDot
		status={d.status}
		quiet={quietlyOffline(d, alerted)}
		breathe={unseen.has(d.id)}
		class="block"
	/>
{/snippet}

{#snippet offline(d: Device)}
	<span class={quietlyOffline(d, alerted) ? 'text-fg-faint' : 'text-red-fg'}>
		Offline{d.lastSeen ? ` ${since(d.lastSeen, now)}` : ''}
	</span>
{/snippet}

<ul class="md:hidden">
	{#each devices as d (d.id)}
		<li class="border-t border-line first:border-0">
			<a
				href={href(d)}
				class="flex items-center gap-3 px-4 py-2.5 transition-colors hover:bg-hover/50 focus-visible:rounded-none"
			>
				<span class="w-2 shrink-0">{@render lamp(d)}</span>
				<span class="min-w-0 flex-1">
					<span class="block truncate text-[13px] leading-5 font-medium text-fg">{d.name}</span>
					<span class="block truncate text-[12px] leading-4 text-fg-faint">
						{osLabel(d.os)} · <span class="font-mono text-[11px]">{routeLabel(d)}</span>
					</span>
				</span>
				<span class="shrink-0 text-[12px] leading-4 text-fg-muted">
					{#if !d.online}
						{@render offline(d)}
					{:else if d.status === 'attention'}
						<span class="text-amber">{d.statusReason}</span>
					{/if}
				</span>
			</a>
		</li>
	{/each}
</ul>

<div class="hidden overflow-x-auto md:block">
	<table class="w-full min-w-3xl table-fixed border-collapse text-left">
		<thead>
			<tr>
				<th class={[th, 'pr-3 pl-11']}>Name</th>
				<th class={[th, 'w-30']}>Route</th>
				{#if detailed}
					<th class={[th, 'w-32 max-xl:hidden']}>Tailnet IP</th>
				{/if}
				<th class={[th, 'w-20 text-right max-xl:hidden']}>RTT</th>
				<th class={[th, 'w-39 text-right']}>CPU</th>
				<th class={[th, 'w-20 text-right xl:w-30']}>Memory</th>
				<th class={[th, 'w-20 text-right xl:w-30']}>Disk</th>
				{#if containers}
					<th class={[th, 'w-22.5 text-right max-xl:hidden']}>Containers</th>
				{/if}
				<th class={[th, 'w-26 pr-6 text-right']}>Up</th>
			</tr>
		</thead>
		<tbody>
			{#each devices as d (d.id)}
				{@const m = d.metrics}
				{@const limits = limitsOf(d, thresholds)}
				{@const memory = ratio(m?.memory?.used, m?.memory?.total)}
				{@const disk = ratio(m?.disk?.used, m?.disk?.total)}
				{@const count = containers?.get(d.id)}
				<!-- The link stretches over the whole row, so any of it opens the device. -->
				<tr class="relative h-10.5 border-t border-line transition-colors hover:bg-hover/50">
					<td class="py-0 pr-3 pl-6">
						<span class="flex min-w-0 items-center">
							<span class="w-5 shrink-0">{@render lamp(d)}</span>
							<span class="flex min-w-0 items-baseline gap-2.5">
								<a
									href={href(d)}
									class="truncate text-[13px] leading-5 font-medium text-fg after:absolute after:inset-0 focus-visible:rounded-sm"
								>
									{d.name}
								</a>
								<span class="truncate text-[12px] leading-5 text-fg-faint">
									{d.info?.model || osLabel(d.os)}
								</span>
							</span>
						</span>
					</td>
					<td class="text-[13px] leading-5 whitespace-nowrap text-fg-muted">{routeName(d)}</td>
					{#if detailed}
						<td class="font-mono text-[12px] leading-5 text-fg-muted max-xl:hidden">
							{tailnetIP(d) ?? '—'}
						</td>
					{/if}
					<td
						class={[
							num,
							'whitespace-nowrap max-xl:hidden',
							slowLink(d)
								? 'text-amber'
								: d.path.rttMs === undefined
									? 'text-fg-faint'
									: 'text-fg-muted'
						]}
					>
						{d.path.rttMs === undefined ? '—' : latency(d.path.rttMs)}
					</td>
					<td class={[num, !d.online || m?.cpu === undefined ? 'text-fg-faint' : 'text-fg']}>
						{#if d.online && m?.cpu !== undefined}
							<div class="flex items-center justify-end gap-2.5">
								<LazyTrend id={d.id} load={cpuTrend} />
								<span class={['w-9 shrink-0', levelText(m.cpu, limits.cpu)]}>{percent(m.cpu)}</span>
							</div>
						{:else}
							—
						{/if}
					</td>
					<td class={[num, d.online ? levelText(memory, limits.memory) : 'text-fg-faint']}>
						{d.online && memory !== null ? percent(memory) : '—'}
					</td>
					<td
						class={[
							num,
							d.online
								? levelText(disk, limits.disk, limits.diskCritical)
								: disk === null
									? 'text-fg-faint'
									: 'text-fg-muted'
						]}
					>
						{disk === null ? '—' : percent(disk)}
					</td>
					{#if containers}
						<td class={[num, 'max-xl:hidden', count ? 'text-fg' : 'text-fg-faint']}>
							{count ? `${count.running}/${count.total}` : '—'}
						</td>
					{/if}
					<td class="pr-6 text-right whitespace-nowrap">
						{#if d.online}
							<span class={[num, m?.uptime ? 'text-fg-muted' : 'text-fg-faint']}>
								{m?.uptime ? shortDuration(m.uptime) : '—'}
							</span>
						{:else}
							<span class="text-[12px] leading-5">{@render offline(d)}</span>
						{/if}
					</td>
				</tr>
			{/each}
		</tbody>
	</table>
</div>

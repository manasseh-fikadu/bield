<!--
	A machine on the network map: its lamp, its name, what sort of machine it
	is, what's wrong with it or else its OS and address, and its CPU and disk.
	Healthy machines stay quiet: only trouble gets color.
-->
<script lang="ts">
	import { resolve } from '$app/paths';
	import { agentRuns } from '#lib/agent.js';
	import type { Device, Thresholds } from '#lib/api/types.js';
	import { deviceIcon, limitsOf } from '#lib/device.js';
	import { osLabel, ratio, since } from '#lib/format.js';
	import { tailnetIP } from '#lib/tailnet.js';
	import StatusDot from '#lib/ui/StatusDot.svelte';
	import MiniMeter from './MiniMeter.svelte';

	interface Props {
		device: Device;
		thresholds: Thresholds;
		/** It's off and nobody needs to know. */
		quiet: boolean;
		/** It has an alert nobody has acknowledged, so its lamp breathes. */
		breathe: boolean;
		/** When "offline for" is measured from, in Unix milliseconds. */
		now: number;
		/** The line to it is lit, since it's being pointed at. */
		lit: boolean;
		/** How long its lamp waits to light as the map draws in, in milliseconds, if it does. */
		lightAfter?: number;
		/** Called when pointing at it starts and stops. */
		onlit: (lit: boolean) => void;
	}

	let { device: d, thresholds, quiet, breathe, now, lit, lightAfter, onlit }: Props = $props();

	const Icon = $derived(deviceIcon(d));
	const m = $derived(d.metrics);
	const limits = $derived(limitsOf(d, thresholds));
	const disk = $derived(ratio(m?.disk?.used, m?.disk?.total));
</script>

<a
	href={resolve('/devices/[id]', { id: d.id })}
	class={[
		'flex h-full flex-col rounded-lg border px-3.5 py-2.75 transition-colors duration-[220ms] focus-visible:rounded-lg',
		lit
			? 'border-track-dim bg-hover'
			: d.online
				? 'border-line bg-raised'
				: 'border-line bg-surface'
	]}
	onmouseenter={() => onlit(true)}
	onmouseleave={() => onlit(false)}
	onfocus={() => onlit(true)}
	onblur={() => onlit(false)}
>
	<span class="flex h-4.5 shrink-0 items-center gap-2">
		<span
			class={['flex', lightAfter !== undefined && 'animate-light']}
			style:animation-delay={lightAfter !== undefined ? `${lightAfter}ms` : undefined}
		>
			<StatusDot status={d.status} {quiet} {breathe} small />
		</span>
		<span
			class={[
				'min-w-0 flex-1 truncate text-[13.5px] leading-4.5 font-medium',
				quiet ? 'text-fg-muted' : 'text-fg'
			]}
		>
			{d.name}
		</span>
		<Icon
			class={['size-3.5 shrink-0', lit ? 'text-fg-muted' : 'text-fg-faint']}
			strokeWidth={1.6}
		/>
	</span>
	<span class="flex h-4.75 shrink-0 gap-2 pt-0.75">
		{#if !d.online}
			<span class={['truncate text-[12px] leading-4', quiet ? 'text-fg-faint' : 'text-red-fg']}>
				Offline{d.lastSeen ? ` ${since(d.lastSeen, now)}` : ''}
			</span>
		{:else if d.status === 'attention'}
			<span class="truncate text-[12px] leading-4 text-amber" title={d.statusReason}>
				{d.statusReason}
			</span>
		{:else}
			<span class="shrink-0 text-[12px] leading-4 text-fg-faint">{osLabel(d.os)}</span>
			<span class="truncate font-mono text-[11px] leading-4 text-fg-faint">{tailnetIP(d)}</span>
		{/if}
	</span>
	{#if m}
		<span class="flex gap-3.5 pt-2.5">
			<MiniMeter
				label="CPU"
				value={d.online ? (m.cpu ?? null) : null}
				warn={limits.cpu}
				dim={!d.online}
			/>
			<MiniMeter
				label="Disk"
				value={disk}
				warn={limits.disk}
				crit={limits.diskCritical}
				dim={!d.online}
			/>
		</span>
	{:else}
		<span class="truncate pt-2.5 text-[11px] leading-3.5 text-fg-faint">
			{agentRuns(d.os) ? 'No agent on this machine' : `No metrics from ${osLabel(d.os)}`}
		</span>
	{/if}
</a>

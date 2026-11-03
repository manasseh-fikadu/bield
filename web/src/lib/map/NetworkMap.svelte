<!--
	The network map: the internet and the home relay above the hub, and every
	other machine under it, nearest first, with one trunk down the middle. A
	solid line is a direct connection, a dashed one goes through a relay, and a
	dotted one isn't observed. Lines are grey unless something's wrong. Pointing
	at a card lights the line from the hub to it. On a phone, the map is a list.
-->
<script lang="ts" module>
	/** The map draws itself in once each time the app loads, not each time it's opened. */
	let drawn = false;
</script>

<script lang="ts">
	import Globe from '@lucide/svelte/icons/globe';
	import Radio from '@lucide/svelte/icons/radio';
	import { resolve } from '$app/paths';
	import { alertedDevices, quietlyOffline } from '#lib/alerts.js';
	import type { Device, Snapshot, Status } from '#lib/api/types.js';
	import { limitsOf } from '#lib/device.js';
	import { duration, latency, ratio, rateShort } from '#lib/format.js';
	import Logo from '#lib/shell/Logo.svelte';
	import {
		derpStatus,
		nearestFirst,
		routeLabel,
		slowLink,
		tailnetIP,
		uplinkStatus
	} from '#lib/tailnet.js';
	import StatusDot from '#lib/ui/StatusDot.svelte';
	import MachineCard from './MachineCard.svelte';
	import MiniMeter from './MiniMeter.svelte';
	import { layoutMap } from './layout.js';

	interface Props {
		snapshot: Snapshot;
	}

	let { snapshot }: Props = $props();

	let width = $state(0);
	/** The machine being pointed at, whose line is lit. */
	let lit = $state<string | null>(null);

	const hub = $derived(snapshot.devices.find((d) => d.self));
	const machines = $derived(nearestFirst(snapshot.devices.filter((d) => !d.self)));
	const byId = $derived(new Map(machines.map((d) => [d.id, d])));
	const ids = $derived(machines.map((d) => d.id));
	const layout = $derived(width > 0 ? layoutMap(width, ids) : null);
	const alerted = $derived(alertedDevices(snapshot.alerts));
	/** The machines with an alert nobody has acknowledged. */
	const unseen = $derived(alertedDevices(snapshot.alerts.filter((a) => a.state === 'open')));
	const now = $derived(Date.parse(snapshot.generatedAt));
	const uplink = $derived(snapshot.uplink);

	const firstDraw = !drawn;
	$effect(() => {
		if (layout) {
			drawn = true;
		}
	});

	/** How long a card's lamp waits for its line, as the map draws in from the top. */
	function lightAfter(y: number, height: number): number | undefined {
		return firstDraw ? Math.round((y / height) * 700) : undefined;
	}

	// Now and then, a short pulse travels along the line to a live machine.
	let signal = $state<{ id: string; n: number } | null>(null);
	const liveIds = $derived(machines.filter((d) => d.online).map((d) => d.id));
	$effect(() => {
		if (matchMedia('(prefers-reduced-motion: reduce)').matches) {
			return;
		}
		let n = 0;
		const timer = setInterval(() => {
			const list = liveIds;
			if (list.length && document.visibilityState === 'visible') {
				signal = { id: list[n % list.length], n };
				n++;
			}
		}, 6000);
		return () => clearInterval(timer);
	});

	type LineStyle = 'direct' | 'relay' | 'none';

	/** What a line looks like: solid, dashed or dotted. */
	const dash: Record<LineStyle, string | undefined> = {
		direct: undefined,
		relay: '4 3',
		none: '1.5 3.5'
	};

	function lineStyle(d: Device): LineStyle {
		if (!d.online) {
			return 'none';
		}
		return d.path.kind === 'derp' || d.path.kind === 'peer-relay' ? 'relay' : 'direct';
	}

	function lineColor(d: Device): string {
		if (!d.online) {
			return quietlyOffline(d, alerted) ? 'var(--color-fg-faint)' : 'var(--color-red)';
		}
		return d.status === 'attention' || slowLink(d) ? 'var(--color-amber)' : 'var(--color-track)';
	}

	/** The line from the hub up to the internet or the relay. */
	function upstreamColor(s: Status): string {
		return s === 'attention'
			? 'var(--color-amber)'
			: s === 'offline'
				? 'var(--color-red)'
				: s === 'unknown'
					? 'var(--color-fg-faint)'
					: 'var(--color-track)';
	}

	const internetStatus = $derived(uplinkStatus(uplink));
	const relayStatus = $derived(derpStatus(uplink));
	const relayName = $derived(
		uplink.derp?.name ?? (uplink.checkedAt ? 'No relay answered' : 'Waiting for netcheck')
	);
	const relayLabel = $derived(
		uplink.derp ? `${uplink.derp.code} · ${latency(uplink.derp.latencyMs)}` : 'not observed'
	);
	const families = $derived(
		uplink.ipv4 && uplink.ipv6 ? 'IPv4, IPv6' : uplink.ipv4 ? 'IPv4' : uplink.ipv6 ? 'IPv6' : 'None'
	);
</script>

{#snippet upstream(status: Status, title: string, Icon: typeof Globe, line: string, note: string)}
	<div class="flex h-full flex-col gap-1 rounded-lg border border-line bg-raised px-3.5 py-2.75">
		<div class="flex h-4.5 shrink-0 items-center gap-2">
			<StatusDot {status} small />
			<span class="min-w-0 flex-1 truncate text-[13.5px] leading-4.5 font-medium text-fg">
				{title}
			</span>
			<Icon class="size-3.5 shrink-0 text-fg-faint" strokeWidth={1.6} />
		</div>
		<div class="flex h-4 shrink-0 justify-between gap-2">
			<span class="truncate font-mono text-[11px] leading-4 text-fg-muted">{line}</span>
			<span
				class={[
					'shrink-0 text-[12px] leading-4',
					status === 'attention' ? 'text-amber' : 'text-fg-faint'
				]}
			>
				{note}
			</span>
		</div>
	</div>
{/snippet}

{#snippet internetCard()}
	{@render upstream(
		internetStatus,
		'Internet',
		Globe,
		uplink.publicIp ?? (uplink.checkedAt ? 'The hub’s uplink' : 'Waiting for netcheck'),
		uplink.checkedAt ? families : ''
	)}
{/snippet}

{#snippet relayCard()}
	{@render upstream(
		relayStatus,
		'Home relay',
		Radio,
		relayName,
		uplink.checkedAt ? (uplink.udp ? 'UDP' : 'UDP blocked') : ''
	)}
{/snippet}

{#snippet hubCard(d: Device, stacked = false)}
	{@const m = d.metrics}
	<a
		href={resolve('/devices/[id]', { id: d.id })}
		class={[
			'flex h-full gap-x-5 gap-y-3 rounded-[10px] border border-line-strong bg-hover px-4 py-3.5 shadow-[0_0_0_3px_rgb(221_231_243/0.03)] transition-colors duration-[220ms] hover:border-line-heavy focus-visible:rounded-[10px]',
			stacked ? 'flex-col' : 'items-center'
		]}
	>
		<span class="flex min-w-0 flex-1 flex-col gap-0.5">
			<span class="flex h-5 shrink-0 items-center gap-2">
				<Logo class="size-4.5 shrink-0 text-fg" />
				<span class="truncate text-[15px] leading-5 font-medium text-fg">{d.name}</span>
			</span>
			<span class="truncate text-[12px] leading-4 text-fg-muted">
				This hub{m?.uptime ? `, up ${duration(m.uptime).split(' ')[0]}` : ''}
			</span>
			<span class="truncate font-mono text-[11px] leading-4 text-fg-faint">{tailnetIP(d)}</span>
		</span>
		{#if m}
			{@const limits = limitsOf(d, snapshot.hub.thresholds)}
			<span class={['flex min-w-0 shrink flex-col gap-2.5', !stacked && 'w-61']}>
				<span class="flex gap-3.5">
					<MiniMeter label="CPU" value={m.cpu ?? null} warn={limits.cpu} bright />
					<MiniMeter
						label="Memory"
						value={ratio(m.memory?.used, m.memory?.total)}
						warn={limits.memory}
						bright
					/>
					<MiniMeter
						label="Disk"
						value={ratio(m.disk?.used, m.disk?.total)}
						warn={limits.disk}
						crit={limits.diskCritical}
						bright
					/>
				</span>
				<span class="flex h-3.5 shrink-0 justify-between gap-2">
					<span class="truncate font-mono text-[11px] leading-3.5 text-fg-muted">
						↓ {rateShort(m.netRx)}/s ↑ {rateShort(m.netTx)}/s
					</span>
					{#if m.load}
						<span class="shrink-0 font-mono text-[11px] leading-3.5 text-fg-muted">
							load {m.load[0].toFixed(2)}
						</span>
					{/if}
				</span>
			</span>
		{/if}
	</a>
{/snippet}

{#snippet machineCard(d: Device, after?: number)}
	<MachineCard
		device={d}
		thresholds={snapshot.hub.thresholds}
		quiet={quietlyOffline(d, alerted)}
		breathe={unseen.has(d.id)}
		{now}
		lit={lit === d.id}
		lightAfter={after}
		onlit={(on) => (lit = on ? d.id : lit === d.id ? null : lit)}
	/>
{/snippet}

{#snippet title()}
	<h2 class="text-[13.5px] leading-5 font-medium text-fg">Network</h2>
	<p class="text-[12px] leading-4 text-fg-faint">Nearest machines first</p>
{/snippet}

{#snippet allMachines()}
	<a
		href={resolve('/devices')}
		class="text-[12px] leading-5 text-fg-faint transition-colors hover:text-fg-muted pointer-coarse:-my-2.5 pointer-coarse:py-2.5"
	>
		All machines →
	</a>
{/snippet}

{#snippet legendLine(style: LineStyle, text: string)}
	<span class="flex h-4 shrink-0 items-center gap-2.5">
		<svg width="22" height="6" viewBox="0 0 22 6" class="shrink-0" aria-hidden="true">
			<path
				d="M1 3H22"
				fill="none"
				stroke={style === 'none' ? 'var(--color-fg-faint)' : 'var(--color-track)'}
				stroke-width="1.5"
				stroke-linecap={style === 'none' ? 'round' : undefined}
				stroke-dasharray={dash[style]}
			/>
		</svg>
		<span class="text-[12px] leading-4 text-fg-faint">{text}</span>
	</span>
{/snippet}

<div bind:clientWidth={width} class="w-full">
	{#if layout}
		<div class="relative" style:height="{layout.height}px">
			<div class="absolute top-0 left-0">{@render title()}</div>
			<div class="absolute top-0 right-0">{@render allMachines()}</div>

			<svg
				width={layout.width}
				height={layout.height}
				class={['pointer-events-none absolute inset-0', firstDraw && 'animate-reveal']}
				aria-hidden="true"
			>
				<g fill="none" stroke-width="1.5">
					<path d={layout.shared} stroke="var(--color-track)" />
					<path
						d={layout.internetLine}
						stroke={upstreamColor(internetStatus)}
						stroke-dasharray={internetStatus === 'online' ? undefined : dash.none}
						stroke-linecap={internetStatus === 'online' ? undefined : 'round'}
					/>
					<path
						d={layout.relayLine}
						stroke={upstreamColor(relayStatus)}
						stroke-dasharray={relayStatus === 'online' ? undefined : dash.none}
						stroke-linecap={relayStatus === 'online' ? undefined : 'round'}
					/>
					{#each layout.drops as drop (drop.id)}
						{@const d = byId.get(drop.id)}
						{#if d}
							{@const style = lineStyle(d)}
							<path
								d={drop.d}
								stroke={lineColor(d)}
								stroke-dasharray={dash[style]}
								stroke-linecap={style === 'none' ? 'round' : undefined}
							/>
						{/if}
					{/each}
					{#each layout.drops as drop (drop.id)}
						<path
							d={drop.lit}
							stroke="var(--color-lamp)"
							class={[
								'transition-opacity duration-[220ms]',
								lit === drop.id ? 'opacity-100' : 'opacity-0'
							]}
						/>
					{/each}
					{#if signal}
						{@const drop = layout.drops.find((x) => x.id === signal?.id)}
						{#if drop}
							{#key signal.n}
								<path
									d={drop.lit}
									pathLength="1"
									stroke="var(--color-lamp)"
									stroke-dasharray="0.06 1"
									stroke-linecap="round"
									class="animate-signal"
								/>
							{/key}
						{/if}
					{/if}
				</g>
			</svg>

			<span
				class="absolute font-mono text-[11px] leading-3.5 whitespace-nowrap text-fg-faint"
				style:left="{layout.internetLabel.x}px"
				style:top="{layout.internetLabel.y}px">uplink</span
			>
			<span
				class="absolute font-mono text-[11px] leading-3.5 whitespace-nowrap text-fg-faint"
				style:left="{layout.relayLabel.x}px"
				style:top="{layout.relayLabel.y}px">{relayLabel}</span
			>
			{#each layout.drops as drop (drop.id)}
				{@const d = byId.get(drop.id)}
				{#if d}
					<span
						class={[
							'absolute font-mono text-[11px] leading-3.5 whitespace-nowrap transition-colors duration-[220ms]',
							lit === drop.id ? 'text-fg-muted' : 'text-fg-faint'
						]}
						style:left="{drop.lx}px"
						style:top="{drop.ly}px"
					>
						{routeLabel(d)}
					</span>
				{/if}
			{/each}

			<div
				class="absolute"
				style:left="{layout.internet.x}px"
				style:top="{layout.internet.y}px"
				style:width="{layout.internet.w}px"
				style:height="{layout.internet.h}px"
			>
				{@render internetCard()}
			</div>
			<div
				class="absolute"
				style:left="{layout.relay.x}px"
				style:top="{layout.relay.y}px"
				style:width="{layout.relay.w}px"
				style:height="{layout.relay.h}px"
			>
				{@render relayCard()}
			</div>
			{#if hub}
				<div
					class="absolute"
					style:left="{layout.hub.x}px"
					style:top="{layout.hub.y}px"
					style:width="{layout.hub.w}px"
					style:height="{layout.hub.h}px"
				>
					{@render hubCard(hub)}
				</div>
			{/if}
			{#each machines as d (d.id)}
				{@const b = layout.cards.get(d.id)}
				{#if b}
					<div
						class="absolute"
						style:left="{b.x}px"
						style:top="{b.y}px"
						style:width="{b.w}px"
						style:height="{b.h}px"
					>
						{@render machineCard(d, lightAfter(b.y, layout.height))}
					</div>
				{/if}
			{/each}
			<div
				class="absolute flex flex-col justify-center gap-2 pl-3.5"
				style:left="{layout.legend.x}px"
				style:top="{layout.legend.y}px"
				style:width="{layout.legend.w}px"
				style:height="{layout.legend.h}px"
			>
				{@render legendLine('direct', 'Direct')}
				{@render legendLine('relay', 'Through a relay')}
				{@render legendLine('none', 'Not observed')}
			</div>
		</div>
	{:else if width > 0}
		<div class="flex flex-col gap-3">
			<div class="flex items-start justify-between gap-3">
				<div>{@render title()}</div>
				{@render allMachines()}
			</div>
			<div class="grid grid-cols-2 gap-2">
				<div class="min-w-0">{@render internetCard()}</div>
				<div class="min-w-0">{@render relayCard()}</div>
			</div>
			{#if hub}
				{@render hubCard(hub, true)}
			{/if}
			<ul class="relative flex flex-col gap-2 pl-3.5">
				<span class="absolute top-0 bottom-11 left-0 w-px bg-track" aria-hidden="true"></span>
				{#each machines as d (d.id)}
					<li class="relative">
						<span class="absolute top-2 -left-3.5 h-px w-2.5 bg-track" aria-hidden="true"></span>
						<p class="mb-1 font-mono text-[11px] leading-3.5 text-fg-faint">{routeLabel(d)}</p>
						<div class="h-23">{@render machineCard(d)}</div>
					</li>
				{/each}
			</ul>
		</div>
	{:else}
		<div class="h-130"></div>
	{/if}
</div>

<script lang="ts">
	import { goto } from '$app/navigation';
	import { resolve } from '$app/paths';
	import { page } from '$app/state';
	import { isAdmin } from '#lib/access.js';
	import { alertedDevices, onHub, openAlerts, troubleFirst } from '#lib/alerts.js';
	import NeedsYou from '#lib/alerts/NeedsYou.svelte';
	import { scoreClass } from '#lib/checks.js';
	import HealthPanel from '#lib/checks/HealthPanel.svelte';
	import DevicesTable from '#lib/devices/DevicesTable.svelte';
	import { ContainerCounts } from '#lib/docker/fleet.svelte.js';
	import { latency, plural } from '#lib/format.js';
	import { live } from '#lib/live.svelte.js';
	import NetworkMap from '#lib/map/NetworkMap.svelte';
	import { markSetupSent, setupSent, shouldOpenSetup } from '#lib/setup.js';
	import Skeleton from '#lib/ui/Skeleton.svelte';
	import StatusDot from '#lib/ui/StatusDot.svelte';

	/** How many machines the table shows until someone asks for the rest. */
	const firstRows = 6;

	// An admin of a Bield that nobody has set up lands in the setup, once per
	// browser tab, so leaving it doesn't bring them back every time.
	$effect(() => {
		if (shouldOpenSetup(isAdmin(page.data.me), live.snapshot?.hub, setupSent())) {
			markSetupSent();
			void goto(resolve('/setup'), { replace: true });
		}
	});

	const snapshot = $derived(live.snapshot);
	const devices = $derived(snapshot?.devices ?? []);
	const online = $derived(devices.filter((d) => d.online).length);
	const now = $derived(snapshot ? Date.parse(snapshot.generatedAt) : Date.now());
	const needs = $derived(snapshot ? openAlerts(snapshot.alerts).length : 0);
	const failing = $derived((snapshot?.collectors ?? []).filter((c) => c.state === 'failing'));
	const alerted = $derived(alertedDevices(snapshot?.alerts ?? []));
	const sorted = $derived(troubleFirst(devices, alerted));

	let showAll = $state(false);
	const rows = $derived(showAll ? sorted : sorted.slice(0, firstRows));

	// The hub's machine can be two nodes, which would count its containers twice.
	const dockerDevices = $derived(devices.filter((d) => d.docker && (d.self || !onHub(d))));
	const dockerIds = $derived(dockerDevices.map((d) => d.id).join(' '));
	const counts = new ContainerCounts();
	$effect(() => {
		const ids = dockerIds;
		return ids ? counts.start(ids.split(' ')) : undefined;
	});
	const containers = $derived(counts.total(dockerDevices));

	const health = $derived(snapshot?.health);
	const derp = $derived(snapshot?.uplink.derp);
</script>

{#snippet stat(label: string, value: string, tone = 'text-fg')}
	<div class="flex flex-col gap-0.5">
		<dt class="text-[12px] leading-4 whitespace-nowrap text-fg-faint">{label}</dt>
		<dd class={['font-mono text-[15px] leading-5 whitespace-nowrap', tone]}>{value}</dd>
	</div>
{/snippet}

<div class="flex flex-wrap items-end justify-between gap-x-8 gap-y-4 px-4 pt-6.5 pb-5.5 sm:px-6">
	<h1 class="text-[24px] leading-7.5 font-medium tracking-[-0.02em] text-fg">
		{#if snapshot}
			{online} of {plural(devices.length, 'machine')} online.
			{#if needs > 0}
				<span class="text-amber">{needs} {needs === 1 ? 'thing needs' : 'things need'} you.</span>
			{:else}
				<span class="text-fg-muted">Nothing needs you.</span>
			{/if}
		{:else}
			Connecting to the hub…
		{/if}
	</h1>
	{#if snapshot}
		<dl class="flex flex-wrap gap-x-8 gap-y-3">
			{@render stat('Health', String(health?.score ?? '—'), scoreClass(health?.score ?? null))}
			{#if health && health.total > 0}
				{@render stat('Checks passing', `${health.passing}/${health.passing + health.failing}`)}
			{/if}
			{#if containers}
				{@render stat('Containers up', `${containers.running}/${containers.total}`)}
			{/if}
			{#if derp}
				{@render stat('Home relay', `${derp.code} ${latency(derp.latencyMs)}`)}
			{/if}
		</dl>
	{/if}
</div>

{#each failing as c (c.name)}
	<a
		href={resolve('/activity')}
		class="flex items-center gap-2.5 border-t border-line px-4 py-2.5 transition-colors hover:bg-hover/50 focus-visible:rounded-none sm:px-6"
	>
		<StatusDot status="attention" />
		<span class="text-[13px] leading-5 font-medium text-fg">{c.name} is failing.</span>
		{#if c.error}
			<span class="min-w-0 truncate text-[13px] leading-5 text-fg-muted">{c.error}</span>
		{/if}
	</a>
{/each}

<div class="flex flex-col border-t border-line xl:flex-row">
	<div class="min-w-0 flex-1 px-4 pt-3.5 pb-5.5 sm:px-6">
		{#if snapshot}
			<NetworkMap {snapshot} />
		{:else}
			<Skeleton class="h-130 rounded-lg" />
		{/if}
	</div>
	<div class="flex shrink-0 flex-col border-t border-line xl:w-72 xl:border-t-0 xl:border-l">
		<NeedsYou alerts={snapshot?.alerts ?? null} {devices} {now} />
		<div class="grow"></div>
		<HealthPanel health={health ?? null} />
	</div>
</div>

<section class="border-t border-line" aria-label="Machines">
	<header class="flex h-12 shrink-0 items-center justify-between px-4 sm:px-6">
		<h2 class="text-[13px] leading-5 font-medium text-fg">Machines</h2>
		<span class="text-[12px] leading-4 text-fg-faint">Trouble first</span>
	</header>
	<div class="border-t border-line">
		{#if snapshot}
			<DevicesTable
				devices={rows}
				{now}
				thresholds={snapshot.hub.thresholds}
				alerts={snapshot.alerts}
				containers={counts.byDevice}
			/>
			{#if sorted.length > firstRows}
				<button
					type="button"
					class="flex h-10 w-full items-center border-t border-line px-4 text-left text-[12px] leading-4 text-fg-muted transition-colors hover:bg-hover/50 hover:text-fg focus-visible:rounded-none sm:px-6 md:pl-11"
					onclick={() => (showAll = !showAll)}
				>
					{showAll ? 'Show fewer' : `Show ${sorted.length - firstRows} more`}
				</button>
			{/if}
		{:else}
			<Skeleton class="m-4 h-24" />
		{/if}
	</div>
</section>

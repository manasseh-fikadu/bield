<!--
	A container beside the list: what's wrong with it, what an admin can do to
	it, and its logs, its stats and the main facts about it. Its own page has
	everything else. The parent keys it by device and container, so it starts
	over when either changes.
-->
<script lang="ts">
	import X from '@lucide/svelte/icons/x';
	import AdminOnly from '#lib/AdminOnly.svelte';
	import type { DockerContainerDetail, DockerContainerRow } from '#lib/api/types.js';
	import { dateTime, since } from '#lib/format.js';
	import { live } from '#lib/live.svelte.js';
	import Button from '#lib/ui/Button.svelte';
	import DataRow from '#lib/ui/DataRow.svelte';
	import IconButton from '#lib/ui/IconButton.svelte';
	import Skeleton from '#lib/ui/Skeleton.svelte';
	import TabPanel from '#lib/ui/TabPanel.svelte';
	import Tabs from '#lib/ui/Tabs.svelte';
	import { buttonClass } from '#lib/ui/button.js';
	import { actionCopy, type PendingAction } from './actions.js';
	import { getDockerContainer } from './api.js';
	import { containerHref } from './containerLink.js';
	import AdminNotice from './detail/AdminNotice.svelte';
	import { statsEnded } from './detail/helpers.js';
	import { actionsFor, portLabel, shortId } from './docker.js';
	import { rowNote } from './list.js';
	import { LiveStats } from './livestats.svelte.js';
	import LiveCharts from './LiveCharts.svelte';
	import LogViewer from './LogViewer.svelte';
	import { Poll } from './poll.svelte.js';

	interface Props {
		deviceId: string;
		container: DockerContainerRow;
		onclose: () => void;
		/** Called when an admin picks an action, to confirm it. */
		onaction: (pending: PendingAction) => void;
	}

	let { deviceId, container: c, onclose, onaction }: Props = $props();

	let tab = $state('logs');

	const detail = new Poll<DockerContainerDetail>(
		() => getDockerContainer(deviceId, c.name),
		10_000
	);
	$effect(() => detail.start());

	const d = $derived(detail.data);
	const running = $derived(d?.state.running ?? c.state === 'running');
	const streaming = $derived(running && !d?.state.paused);
	const containerId = $derived(d?.id ?? null);
	const startedAt = $derived(d?.state.startedAt);

	const stats = new LiveStats();
	$effect(() => {
		// A container that restarted is a new run, and its stream a new one.
		void startedAt;
		return stats.connect(deviceId, containerId, streaming);
	});

	const note = $derived(rowNote(c, Date.now()));
	const noteTone = { amber: 'text-amber', red: 'text-red-fg', faint: 'text-fg-faint' };
	const tabs = [
		{ value: 'logs', label: 'Logs' },
		{ value: 'stats', label: 'Stats' },
		{ value: 'details', label: 'Details' }
	];
</script>

<div class="flex h-full min-h-0 flex-col">
	<div class="flex flex-col gap-3 px-4 pt-4.5 pb-3.5">
		<div class="flex items-start justify-between gap-3">
			<div class="flex min-w-0 flex-col gap-0.5">
				<h2 class="truncate text-[15px] leading-5 font-semibold tracking-[-0.01em] text-fg">
					{c.name}
				</h2>
				<p class={['truncate text-[12px] leading-4', note ? noteTone[note.tone] : 'text-fg-faint']}>
					{#if c.state === 'running' && note}
						Health check failing, up {since(c.startedAt, Date.now())}
					{:else}
						{note?.text ?? c.status}
					{/if}
				</p>
			</div>
			<IconButton label="Close" size="sm" onclick={onclose} class="-mt-1 -mr-1">
				<X class="size-4" />
			</IconButton>
		</div>
		<AdminOnly>
			<div class="flex gap-1.5">
				{#each actionsFor(c.state) as action, i (action)}
					<Button
						size="xs"
						variant={i === 0 ? 'primary' : 'secondary'}
						disabled={live.demo}
						onclick={() => onaction({ id: c.id, name: c.name, state: c.state, action })}
					>
						{actionCopy[action].label}
					</Button>
				{/each}
			</div>
		</AdminOnly>
	</div>
	<Tabs
		bind:value={tab}
		{tabs}
		listClass="h-9.5 gap-4.5 border-t px-4"
		class="flex min-h-0 flex-1 flex-col"
	>
		{#snippet trailing()}
			<a
				href={containerHref(deviceId, c.name)}
				class="text-[12px] leading-4 text-fg-faint transition-colors hover:text-fg-muted"
			>
				Full page →
			</a>
		{/snippet}
		<TabPanel value="logs" class="flex min-h-0 flex-1 flex-col">
			{#if tab === 'logs'}
				<AdminOnly>
					{#if d}
						<LogViewer
							{deviceId}
							container={c.name}
							{running}
							startedAt={d.state.startedAt}
							tty={d.tty}
							embedded
						/>
					{:else}
						<div class="p-4"><Skeleton class="h-40" /></div>
					{/if}
					{#snippet fallback()}<div class="p-4">
							<AdminNotice what="read container logs" />
						</div>{/snippet}
				</AdminOnly>
			{/if}
		</TabPanel>
		<TabPanel value="stats" class="min-h-0 flex-1 overflow-y-auto p-4">
			{#if tab === 'stats' && d}
				<LiveCharts
					samples={stats.samples}
					ended={statsEnded(d.state, stats.ended)}
					memLimit={d.resources.memory ?? null}
				/>
			{/if}
		</TabPanel>
		<TabPanel value="details" class="min-h-0 flex-1 overflow-y-auto">
			{#if tab === 'details'}
				<dl class="flex flex-col gap-2.25 p-4">
					<DataRow flat label="Image" copy={c.image}>{c.image}</DataRow>
					<DataRow flat label="ID" copy={c.id}>{shortId(c.id)}</DataRow>
					{#if c.project}
						<DataRow flat label="Project">{c.project}</DataRow>
					{/if}
					<DataRow flat label="State">{c.status}</DataRow>
					{#if c.startedAt}
						<DataRow flat label="Started">{dateTime(c.startedAt)}</DataRow>
					{/if}
					<DataRow flat label="Restarts">{c.restartCount}</DataRow>
					{#if c.restartPolicy}
						<DataRow flat label="Restart policy">{c.restartPolicy}</DataRow>
					{/if}
					<DataRow flat label="Ports">
						{c.ports.length ? c.ports.map(portLabel).join(', ') : 'none'}
					</DataRow>
					<DataRow flat label="Networks">{c.networks.join(', ') || 'none'}</DataRow>
					<DataRow flat label="Mounts">{c.mounts}</DataRow>
				</dl>
				<div class="px-4 pb-4">
					<a href={containerHref(deviceId, c.name)} class={buttonClass('secondary', 'sm')}>
						Open its page
					</a>
				</div>
			{/if}
		</TabPanel>
	</Tabs>
</div>

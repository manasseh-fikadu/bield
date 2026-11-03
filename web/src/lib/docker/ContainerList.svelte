<!--
	The containers of one device, by compose project. It shows what `poll` has
	loaded: a placeholder while the first load is on its way, why it failed, or
	the list, with a note if a refresh failed since.
-->
<script lang="ts">
	import ContainerIcon from '@lucide/svelte/icons/container';
	import { resolve } from '$app/paths';
	import type { DockerContainerList } from '#lib/api/types.js';
	import { ago } from '#lib/format.js';
	import Card from '#lib/ui/Card.svelte';
	import CodeLine from '#lib/ui/CodeLine.svelte';
	import EmptyState from '#lib/ui/EmptyState.svelte';
	import ErrorState from '#lib/ui/ErrorState.svelte';
	import Skeleton from '#lib/ui/Skeleton.svelte';
	import { buttonClass } from '#lib/ui/button.js';
	import ContainerRow from './ContainerRow.svelte';
	import { groupRows } from './docker.js';
	import { visibleRows } from './list.js';
	import type { Poll } from './poll.svelte.js';

	interface Props {
		poll: Poll<DockerContainerList>;
		deviceId: string;
		deviceName: string;
		/** What to filter by. */
		query: string;
		showStopped: boolean;
		/** The name of the container open in the panel, if one is. */
		open: string | null;
		/** Opens a container in the panel. */
		onopen: (name: string) => void;
	}

	let { poll, deviceId, deviceName, query, showStopped, open, onopen }: Props = $props();

	// The time moves on for "up 3m" and "stopped 3h ago".
	let now = $state(Date.now());
	$effect(() => {
		const timer = setInterval(() => (now = Date.now()), 5000);
		return () => clearInterval(timer);
	});

	let refreshing = $state(false);
	async function refresh() {
		refreshing = true;
		try {
			await poll.reload();
		} finally {
			refreshing = false;
		}
	}

	const all = $derived(poll.data?.containers ?? []);
	const shown = $derived(visibleRows(all, query, showStopped));
	const groups = $derived(groupRows(shown));
</script>

{#if poll.error && !poll.data}
	<div class="p-4 sm:p-6">
		<Card>
			<ErrorState
				title="Couldn't reach Docker on {deviceName}"
				icon={ContainerIcon}
				message={poll.error}
				loading={refreshing}
				onretry={refresh}
			>
				{#snippet actions()}
					<a href={resolve('/devices/[id]', { id: deviceId })} class={buttonClass('ghost')}>
						Open the device
					</a>
				{/snippet}
			</ErrorState>
		</Card>
	</div>
{:else if !poll.data}
	<div class="space-y-2 p-4 sm:p-6">
		{#each { length: 4 }, i (i)}
			<Skeleton class="h-11" />
		{/each}
	</div>
{:else if all.length === 0}
	<div class="p-4 sm:p-6">
		<Card>
			<EmptyState title="No containers on {deviceName}" icon={ContainerIcon}>
				Start one on {deviceName}, with docker run or Docker Compose, and it shows up here within a
				few seconds. For example:
				<div class="mt-3 text-left"><CodeLine command="docker run -d --name hello nginx" /></div>
			</EmptyState>
		</Card>
	</div>
{:else}
	{#if poll.error}
		<p
			role="status"
			class="border-b border-line px-4 py-2 text-[12px] leading-4 text-amber sm:px-6"
		>
			Refresh failed, so this is from {ago(poll.at, Math.max(now, poll.at))}: {poll.error}
		</p>
	{/if}
	{#if groups.length === 0}
		<p class="py-10 text-center text-[13px] text-fg-faint">
			{#if query.trim()}
				No containers match “{query.trim()}”.
			{:else}
				No containers are up. Turn on “Show stopped” to see the rest.
			{/if}
		</p>
	{:else}
		<div class="overflow-x-auto">
			<table class="w-full table-fixed border-collapse text-left">
				<thead>
					<tr class="text-[12px] leading-4 text-fg-faint">
						<th class="h-7.5 pr-3 pl-4 font-normal sm:pl-11">Container</th>
						<th class="w-16 text-right font-normal sm:w-30">CPU</th>
						<th class="w-21 text-right font-normal max-sm:hidden">Memory</th>
						<th class="w-15 text-right font-normal max-sm:hidden">Port</th>
						<th class="w-16 pr-4 text-right font-normal sm:pr-6">Up</th>
					</tr>
				</thead>
				{#each groups as g (g.project ?? '')}
					<tbody>
						<tr class="h-7.5 border-t border-line bg-canvas">
							<th colspan="5" class="px-4 font-normal sm:px-6">
								<span class="flex items-center justify-between gap-3">
									{#if g.project}
										<span class="truncate font-mono text-[12px] leading-4 text-fg-muted">
											{g.project}
										</span>
										<span class="shrink-0 text-[12px] leading-4 text-fg-faint">Compose project</span
										>
									{:else}
										<span class="text-[12px] leading-4 text-fg-muted">Standalone</span>
										<span class="shrink-0 text-[12px] leading-4 text-fg-faint"
											>Not in a project</span
										>
									{/if}
								</span>
							</th>
						</tr>
						{#each g.containers as c (c.id)}
							<ContainerRow container={c} {deviceId} {now} open={open === c.name} {onopen} />
						{/each}
					</tbody>
				{/each}
			</table>
		</div>
	{/if}
{/if}

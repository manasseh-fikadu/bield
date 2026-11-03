<script lang="ts">
	import ContainerIcon from '@lucide/svelte/icons/container';
	import { goto } from '$app/navigation';
	import { resolve } from '$app/paths';
	import { page } from '$app/state';
	import AdminOnly from '#lib/AdminOnly.svelte';
	import type { Device } from '#lib/api/types.js';
	import SetUpAccess from '#lib/devices/SetUpAccess.svelte';
	import ActionDialog from '#lib/docker/ActionDialog.svelte';
	import type { PendingAction } from '#lib/docker/actions.js';
	import { getDockerContainers, getDockerOverview } from '#lib/docker/api.js';
	import ContainerList from '#lib/docker/ContainerList.svelte';
	import ContainerPanel from '#lib/docker/ContainerPanel.svelte';
	import { totals } from '#lib/docker/docker.js';
	import ImagesTab from '#lib/docker/ImagesTab.svelte';
	import { containerCounts, pacedOverview, parseTab } from '#lib/docker/list.js';
	import NetworksTab from '#lib/docker/NetworksTab.svelte';
	import { Poll } from '#lib/docker/poll.svelte.js';
	import VolumesTab from '#lib/docker/VolumesTab.svelte';
	import { Persisted } from '#lib/files/persisted.svelte.js';
	import { bytes, percent } from '#lib/format.js';
	import { live } from '#lib/live.svelte.js';
	import { crumb } from '#lib/shell/header.svelte.js';
	import Card from '#lib/ui/Card.svelte';
	import EmptyState from '#lib/ui/EmptyState.svelte';
	import Input from '#lib/ui/Input.svelte';
	import Select from '#lib/ui/Select.svelte';
	import Skeleton from '#lib/ui/Skeleton.svelte';
	import Switch from '#lib/ui/Switch.svelte';
	import TabPanel from '#lib/ui/TabPanel.svelte';
	import Tabs from '#lib/ui/Tabs.svelte';

	const uid = $props.id();

	/** The machines whose Docker Bield can read. */
	const devices = $derived((live.snapshot?.devices ?? []).filter((d) => d.docker));
	/** The device in ?device=, or else the first. */
	const device = $derived.by((): Device | undefined => {
		const wanted = page.url.searchParams.get('device');
		return devices.find((d) => d.id === wanted) ?? devices[0];
	});
	// The live data replaces every device's object with each snapshot, and anything that read the
	// object would start over: the polls would restart and the filter would be lost. The page uses
	// only what stays equal from one snapshot to the next.
	const deviceId = $derived(device?.id);
	const deviceName = $derived(device?.name);

	crumb(() => deviceName);

	// The tab and the open container are in the URL, so links can point at them.
	const tab = $derived(parseTab(page.url.searchParams.get('tab')));
	const openName = $derived(page.url.searchParams.get('container'));

	/** Another machine's containers aren't this one's, so the tab and anything else is left behind. */
	function setDevice(id: string) {
		const url = new URL(page.url.href);
		url.search = '';
		url.searchParams.set('device', id);
		void goto(url, { replace: true, reset: false });
	}

	function setTab(t: string) {
		const url = new URL(page.url.href);
		if (t === 'containers') {
			url.searchParams.delete('tab');
		} else {
			url.searchParams.set('tab', t);
		}
		void goto(url, { replace: true, reset: false });
	}

	function setOpen(name: string | null) {
		const url = new URL(page.url.href);
		if (name) {
			url.searchParams.set('container', name);
		} else {
			url.searchParams.delete('container');
		}
		void goto(url, { replace: true, reset: false });
	}

	// The containers feed the headline as well as the first tab, so they load whichever tab is open.
	// Each device gets polls of its own, which the effects start and the previous device's stop.
	const containers = $derived.by(() => {
		const id = deviceId;
		return id ? new Poll(() => getDockerContainers(id), 5000) : null;
	});
	// The disk takes the daemon a while to measure: ask every few seconds until it has, then rarely.
	const overview = $derived.by(() => {
		const id = deviceId;
		return id
			? new Poll(
					pacedOverview(() => getDockerOverview(id)),
					4000
				)
			: null;
	});
	$effect(() => containers?.start());
	$effect(() => overview?.start());

	const rows = $derived(containers?.data?.containers ?? null);
	const counts = $derived(rows ? containerCounts(rows) : null);
	const sum = $derived(rows ? totals(rows) : null);
	const openRow = $derived(
		tab === 'containers' && openName ? rows?.find((c) => c.name === openName) : undefined
	);

	/** The container an admin is about to start, stop or restart. */
	let pending = $state<PendingAction | null>(null);

	let query = $state('');
	// Kept in the browser: stopped containers are noise to some and the point to others.
	const showStopped = new Persisted('bield.docker.showStopped', true);

	const tabs = $derived([
		{ value: 'containers', label: 'Containers' },
		{ value: 'volumes', label: 'Volumes', count: overview?.data?.disk?.volumes.count },
		{ value: 'images', label: 'Images', count: overview?.data?.images },
		{ value: 'networks', label: 'Networks' }
	]);
</script>

{#if !live.snapshot}
	<div class="p-4 sm:p-6"><Skeleton class="h-96 rounded-lg" /></div>
{:else if !device}
	<div class="p-4 sm:p-6">
		<Card>
			<EmptyState title="No machine has Docker yet" icon={ContainerIcon}>
				Docker on the machine Bield runs on shows up here by itself. For another machine, an admin
				sets up SSH access on its device page, and Bield checks it for Docker.
				<AdminOnly>
					<SetUpAccess devices={live.snapshot?.devices ?? []} />
					{#snippet fallback()}
						<a
							href={resolve('/devices')}
							class="mt-2 block text-fg-muted underline decoration-line-heavy underline-offset-4 hover:text-fg"
							>See the devices</a
						>
					{/snippet}
				</AdminOnly>
			</EmptyState>
		</Card>
	</div>
{:else}
	<!-- Another device starts over: the filter, the tab's data and any action waiting to be confirmed. -->
	{#key deviceId}
		{#if containers && overview}
			<div class="flex">
				<div class="flex min-w-0 flex-1 flex-col">
					<div
						class="flex flex-wrap items-end justify-between gap-x-6 gap-y-3 px-4 pt-5.5 pb-4 sm:px-6"
					>
						<div class="flex min-w-0 flex-col gap-1">
							<h1 class="text-[24px] leading-7.5 font-medium tracking-[-0.02em] text-fg">
								{#if counts}
									{counts.running} of {counts.total} running.
									{#if counts.unhealthy > 0}
										<span class="text-amber">
											{counts.unhealthy} unhealthy{counts.crashed > 0 ? ',' : '.'}
										</span>
									{/if}
									{#if counts.crashed > 0}
										<span class="text-red-fg">{counts.crashed} crashed.</span>
									{/if}
								{:else}
									Docker on {deviceName}
								{/if}
							</h1>
							{#if sum}
								<p class="font-mono text-[12px] leading-4 text-fg-faint">
									CPU {percent(sum.cpu, 1)}, memory {bytes(sum.mem)}{overview.data?.disk
										? `, disk ${bytes(overview.data.disk.total)}`
										: ''}{overview.data ? `, engine ${overview.data.version}` : ''}
								</p>
							{/if}
						</div>
						{#if devices.length > 1}
							<Select
								value={device.id}
								onchange={(e) => setDevice(e.currentTarget.value)}
								class="h-7.5! w-auto min-w-28 text-[13px]!"
								aria-label="Device"
							>
								{#each devices as d (d.id)}
									<option value={d.id}>{d.name}{d.online || d.ssh?.hub ? '' : ' (offline)'}</option>
								{/each}
							</Select>
						{/if}
					</div>

					<Tabs bind:value={() => tab, setTab} {tabs} listClass="border-t border-b-0 px-4 sm:px-6">
						{#snippet trailing()}
							{#if tab === 'containers'}
								<span
									class="flex items-center gap-2 text-[12px] leading-4 text-fg-muted max-sm:hidden"
								>
									<Switch id="{uid}-stopped" bind:checked={showStopped.value} />
									<label for="{uid}-stopped">Stopped</label>
								</span>
								<Input
									bind:value={query}
									placeholder="Filter by name or port"
									class="h-6.5! w-28 rounded-[5px] px-2! text-[12px]! sm:w-45"
									aria-label="Filter containers"
									autocomplete="off"
									spellcheck={false}
								/>
							{/if}
						{/snippet}
						<TabPanel value="containers" class="border-t border-line outline-none">
							{#if tab === 'containers'}
								<ContainerList
									poll={containers}
									deviceId={deviceId!}
									deviceName={deviceName!}
									{query}
									showStopped={showStopped.value}
									open={openRow?.name ?? null}
									onopen={setOpen}
								/>
							{/if}
						</TabPanel>
						<TabPanel value="volumes" class="border-t border-line p-4 sm:p-6">
							{#if tab === 'volumes'}
								<VolumesTab deviceId={deviceId!} />
							{/if}
						</TabPanel>
						<TabPanel value="images" class="border-t border-line p-4 sm:p-6">
							{#if tab === 'images'}
								<ImagesTab deviceId={deviceId!} />
							{/if}
						</TabPanel>
						<TabPanel value="networks" class="border-t border-line p-4 sm:p-6">
							{#if tab === 'networks'}
								<NetworksTab deviceId={deviceId!} />
							{/if}
						</TabPanel>
					</Tabs>
				</div>
				{#if openRow}
					<aside
						class="sticky top-15 hidden h-[calc(100dvh-4.25rem)] w-110 shrink-0 border-l border-line xl:block"
						aria-label={openRow.name}
					>
						{#key openRow.name}
							<ContainerPanel
								deviceId={deviceId!}
								container={openRow}
								onclose={() => setOpen(null)}
								onaction={(p) => (pending = p)}
							/>
						{/key}
					</aside>
				{/if}
			</div>

			<AdminOnly>
				<ActionDialog
					deviceId={deviceId!}
					deviceName={deviceName!}
					bind:pending
					ondone={() => containers.reload()}
				/>
			</AdminOnly>
		{/if}
	{/key}
{/if}

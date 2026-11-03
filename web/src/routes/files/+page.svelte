<script lang="ts">
	import FolderOpen from '@lucide/svelte/icons/folder-open';
	import ShieldAlert from '@lucide/svelte/icons/shield-alert';
	import { goto } from '$app/navigation';
	import { resolve } from '$app/paths';
	import { page } from '$app/state';
	import AdminOnly from '#lib/AdminOnly.svelte';
	import type { Device } from '#lib/api/types.js';
	import SetUpAccess from '#lib/devices/SetUpAccess.svelte';
	import FileBrowser from '#lib/files/FileBrowser.svelte';
	import { live } from '#lib/live.svelte.js';
	import { crumb } from '#lib/shell/header.svelte.js';
	import { canOpen, listedDeviceId, terminalDevices } from '#lib/terminal.js';
	import Card from '#lib/ui/Card.svelte';
	import EmptyState from '#lib/ui/EmptyState.svelte';
	import Select from '#lib/ui/Select.svelte';
	import Skeleton from '#lib/ui/Skeleton.svelte';

	const all = $derived(live.snapshot?.devices ?? []);
	/** The machines Bield can log in to, which is also where it can open their files. */
	const devices = $derived(terminalDevices(all));
	/**
	 * The device in ?device=, or else the machine Bield runs on, or else the
	 * first. The machine Bield runs on can be two nodes that share one access,
	 * and the list has only one of them.
	 */
	const device = $derived.by((): Device | undefined => {
		const wanted = page.url.searchParams.get('device');
		const id = wanted && listedDeviceId(all, wanted);
		return devices.find((d) => d.id === id) ?? devices.find((d) => d.ssh?.hub) ?? devices[0];
	});
	const ready = $derived(!!device && canOpen(device));
	// The live data replaces every device's object with each snapshot, and anything that read the
	// object would start over: a folder would reload and an open editor would lose its text. The
	// browser gets only what it needs, which stays equal from one snapshot to the next.
	const deviceId = $derived(device?.id);
	const deviceOs = $derived(device?.os);
	const deviceDocker = $derived(device?.docker ?? false);

	crumb(() => device?.name);

	function setDevice(id: string) {
		// Another machine's folders aren't this one's, so the path and the file stay behind.
		const url = new URL(page.url.href);
		url.search = '';
		url.searchParams.set('device', id);
		void goto(url, { replace: true, reset: false });
	}
</script>

{#snippet picker()}
	{#if device && devices.length > 1}
		<Select
			value={device.id}
			onchange={(e) => setDevice(e.currentTarget.value)}
			class="mr-1 h-7! w-auto min-w-24 rounded-[5px]! text-[12px]!"
			aria-label="Device"
		>
			{#each devices as d (d.id)}
				<option value={d.id}>{d.name}{d.online || d.ssh?.hub ? '' : ' (offline)'}</option>
			{/each}
		</Select>
	{/if}
{/snippet}

<AdminOnly>
	{#if !live.snapshot}
		<div class="p-4 sm:p-6"><Skeleton class="h-96 rounded-lg" /></div>
	{:else if !device}
		<div class="p-4 sm:p-6">
			<Card>
				<EmptyState title="No machine has SSH access yet" icon={FolderOpen}>
					Bield needs SSH access to a machine first. On the machine it runs on, that takes one
					click.
					<SetUpAccess devices={all} />
				</EmptyState>
			</Card>
		</div>
	{:else if !ready}
		<div class="p-4 sm:p-6">
			{@render picker()}
			<Card class="mt-3">
				<EmptyState
					title={device.ssh?.state === 'host-key-changed'
						? `${device.name}'s host key changed`
						: `${device.name} is offline`}
					icon={ShieldAlert}
				>
					{#if device.ssh?.state === 'host-key-changed'}
						Bield won't log in until an admin trusts the new key, on <a
							href="{resolve('/devices/[id]', { id: device.id })}#ssh"
							class="text-fg underline underline-offset-4">its page</a
						>.
					{:else}
						Files open again once it's back on the tailnet.
					{/if}
				</EmptyState>
			</Card>
		</div>
	{:else}
		{#key deviceId}
			<FileBrowser deviceId={deviceId!} os={deviceOs!} docker={deviceDocker} leading={picker} />
		{/key}
	{/if}
</AdminOnly>

<!--
	What you can do on a device, one click away: its terminal, its files, its
	Docker and its agent. Without SSH access the terminal and files become a
	button that goes to the SSH card, which sets it up. Terminal, files and the
	agent are for admins, and so is the SSH state they depend on.
-->
<script lang="ts">
	import { resolve } from '$app/paths';
	import { page } from '$app/state';
	import { isAdmin } from '#lib/access.js';
	import { agentRuns } from '#lib/agent.js';
	import type { Device } from '#lib/api/types.js';
	import { canOpen } from '#lib/terminal.js';
	import { buttonClass } from '#lib/ui/button.js';
	import AddAgent from './AddAgent.svelte';

	interface Props {
		device: Device;
	}

	let { device }: Props = $props();

	const query = $derived(`?device=${encodeURIComponent(device.id)}`);
	const ssh = $derived(device.ssh);
	const open = $derived(canOpen(device));
	/** The machine could have SSH access, and needs it set up or fixed. */
	const needsSSH = $derived(
		!!ssh && !open && (ssh.state === 'not-set-up' || ssh.state === 'host-key-changed')
	);
	// An agent to install, or to update. A machine Bield runs on has its own measuring.
	const agent = $derived.by(() => {
		const a = device.agent;
		if (!isAdmin(page.data.me) || device.measuredBy === 'hub' || !agentRuns(device.os)) {
			return null;
		}
		if (!a) {
			return 'Install agent';
		}
		return a.outdated || !a.enrolledAt ? (a.enrolledAt ? 'Update agent' : 'Re-enroll agent') : null;
	});
	const any = $derived(open || needsSSH || device.docker || agent !== null);
</script>

{#if any}
	<div
		class="flex flex-wrap items-center gap-1.5"
		role="group"
		aria-label="Actions for {device.name}"
	>
		{#if open}
			<a href="{resolve('/terminal')}{query}" class={buttonClass('secondary', 'sm')}>Terminal</a>
			<a href="{resolve('/files')}{query}" class={buttonClass('secondary', 'sm')}>Files</a>
		{:else if needsSSH}
			<a href="#ssh" class={buttonClass('secondary', 'sm')}>
				{ssh?.state === 'host-key-changed' ? 'Fix SSH' : 'Set up SSH'}
			</a>
		{/if}
		{#if device.docker}
			<a href="{resolve('/docker')}{query}" class={buttonClass('secondary', 'sm')}>Docker</a>
		{/if}
		{#if agent}
			<AddAgent {device} label={agent} />
		{/if}
	</div>
{/if}

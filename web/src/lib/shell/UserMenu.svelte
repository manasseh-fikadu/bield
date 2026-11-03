<!-- Who's signed in, at the foot of the sidebar, as a row that opens a menu with their details. -->
<script lang="ts">
	import Bug from '@lucide/svelte/icons/bug';
	import Keyboard from '@lucide/svelte/icons/keyboard';
	import { page } from '$app/state';
	import { isAdmin, roleLabel } from '#lib/access.js';
	import { live } from '#lib/live.svelte.js';
	import Badge from '#lib/ui/Badge.svelte';
	import Menu from '#lib/ui/Menu.svelte';
	import MenuItem from '#lib/ui/MenuItem.svelte';
	import MenuLabel from '#lib/ui/MenuLabel.svelte';
	import MenuSeparator from '#lib/ui/MenuSeparator.svelte';

	interface Props {
		/** Shows the list of keyboard shortcuts. */
		onshortcuts: () => void;
	}

	let { onshortcuts }: Props = $props();

	const me = $derived(page.data.me);
	const name = $derived(me?.name || me?.login || 'Signed in');
	const role = $derived(me?.role ? roleLabel(me.role) : '');
</script>

<Menu class="w-52" side="top" align="start">
	{#snippet trigger({ props })}
		<button
			{...props}
			type="button"
			class="flex h-10 w-full shrink-0 items-center gap-2.5 rounded-md px-2.5 text-left transition-colors hover:bg-hover/60"
			aria-label="Signed in as {name}"
		>
			<span
				class="flex size-6 shrink-0 items-center justify-center rounded-full border border-line-strong bg-raised text-[11px] leading-3.5 font-semibold text-fg-muted"
				aria-hidden="true"
			>
				{name.slice(0, 1).toUpperCase()}
			</span>
			<span class="min-w-0 flex-1 truncate text-[13px] leading-5 text-fg-muted">{name}</span>
			<span class="shrink-0 text-[12px] leading-4 text-fg-faint">{role}</span>
		</button>
	{/snippet}
	<MenuLabel>
		<span class="flex items-center justify-between gap-2">
			<span class="truncate text-[13px] font-medium text-fg">{name}</span>
			{#if role}
				<Badge>{role}</Badge>
			{/if}
		</span>
		{#if me?.login && me.login !== name}
			<span class="mt-0.5 block truncate">{me.login}</span>
		{/if}
		{#if me?.device}
			<span class="mt-0.5 block truncate font-mono text-[11px] text-fg-faint">from {me.device}</span
			>
		{/if}
	</MenuLabel>
	<MenuSeparator />
	<MenuItem icon={Keyboard} onSelect={onshortcuts}>Keyboard shortcuts</MenuItem>
	{#if isAdmin(me)}
		<!-- An API URL, not an app route, so the browser loads it, not SvelteKit's router. -->
		<MenuItem href="/api/debug/tailnet" reload icon={Bug}>Tailnet debug info</MenuItem>
	{/if}
	{#if live.snapshot}
		<MenuSeparator />
		<MenuLabel>
			<span class="font-mono text-[11px] text-fg-faint">Bield {live.snapshot.hub.version}</span>
		</MenuLabel>
	{/if}
</Menu>

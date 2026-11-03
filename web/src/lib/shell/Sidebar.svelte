<!--
	The sidebar: Bield's name and tailnet, the pages the caller can open with
	how many devices and open alerts there are, and the signed-in user.
-->
<script lang="ts">
	import { resolve } from '$app/paths';
	import { page } from '$app/state';
	import { isAdmin } from '#lib/access.js';
	import { openAlerts } from '#lib/alerts.js';
	import { live } from '#lib/live.svelte.js';
	import Logo from './Logo.svelte';
	import UserMenu from './UserMenu.svelte';
	import { navFor, navItemFor, type NavItem } from './nav.js';

	interface Props {
		/** Shows the list of keyboard shortcuts. */
		onshortcuts: () => void;
	}

	let { onshortcuts }: Props = $props();

	const sections = $derived(navFor(isAdmin(page.data.me)));
	const current = $derived(navItemFor(page.route.id ?? ''));

	/** The tailnet's DNS suffix, such as tail-demo.ts.net, from the hub's name on it. */
	const domain = $derived(live.snapshot?.hub.dnsName.split('.').slice(1).join('.') ?? '');
	const alerts = $derived(live.snapshot ? openAlerts(live.snapshot.alerts).length : 0);

	/** The count after an item, and whether it needs someone. */
	function count(item: NavItem): { n: number; warn: boolean } | null {
		if (item.href === '/devices' && live.snapshot) {
			return { n: live.snapshot.devices.length, warn: false };
		}
		if (item.href === '/alerts' && alerts > 0) {
			return { n: alerts, warn: true };
		}
		return null;
	}
</script>

<div class="flex h-full flex-col px-3 py-4">
	<a href={resolve('/')} class="flex h-10 shrink-0 items-center gap-2.5 rounded-md px-2.5">
		<Logo class="size-5 shrink-0 text-fg" />
		<span class="min-w-0">
			<span class="block truncate text-[14px] leading-4.5 font-semibold tracking-[-0.01em] text-fg"
				>{live.name}</span
			>
			{#if domain}
				<span class="block truncate font-mono text-[11px] leading-3.5 text-fg-faint">{domain}</span>
			{/if}
		</span>
	</a>
	<nav class="flex flex-1 flex-col gap-4.5 overflow-y-auto pt-5" aria-label="Pages">
		{#each sections as section (section.label)}
			<div class="flex flex-col gap-0.5" role="group" aria-label={section.label}>
				{#each section.items as item (item.href)}
					{@const active = item.href === current?.href}
					{@const c = count(item)}
					<a
						href={resolve(item.href)}
						aria-current={active ? 'page' : undefined}
						class={[
							'group flex h-8 shrink-0 items-center gap-2.5 rounded-md px-2.5 text-[13px] leading-5 transition-colors',
							active
								? 'bg-hover font-medium text-fg'
								: 'text-fg-muted hover:bg-hover/60 hover:text-fg'
						]}
					>
						<item.icon
							class={[
								'size-4 shrink-0 transition-colors',
								active ? 'text-fg' : 'text-fg-faint group-hover:text-fg-muted'
							]}
							strokeWidth={1.75}
						/>
						<span class="flex-1 truncate">{item.label}</span>
						{#if c}
							<span
								class={[
									'num text-[12px] leading-4 font-normal',
									c.warn ? 'text-amber' : 'text-fg-faint'
								]}
							>
								{c.n}
								{#if c.warn}<span class="sr-only">open</span>{/if}
							</span>
						{/if}
					</a>
				{/each}
			</div>
		{/each}
	</nav>
	<UserMenu {onshortcuts} />
</div>

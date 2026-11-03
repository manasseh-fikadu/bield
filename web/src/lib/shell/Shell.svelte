<!--
	The app around every page: the sidebar (a drawer on small screens) and the
	sheet the page sits on, under a header with the breadcrumb, the live
	status and search, and the command palette and the toasts. It keeps the
	live stream connected while it's shown.
-->
<script lang="ts">
	import type { Snippet } from 'svelte';
	import { Dialog } from 'bits-ui';
	import LockKeyhole from '@lucide/svelte/icons/lock-keyhole';
	import MenuIcon from '@lucide/svelte/icons/menu';
	import Search from '@lucide/svelte/icons/search';
	import { afterNavigate, invalidate } from '$app/navigation';
	import { resolve } from '$app/paths';
	import { page } from '$app/state';
	import { isAdmin, lookupFailed } from '#lib/access.js';
	import { getMe } from '#lib/api/client.js';
	import { clock } from '#lib/format.js';
	import { connect, live } from '#lib/live.svelte.js';
	import EmptyState from '#lib/ui/EmptyState.svelte';
	import Toaster from '#lib/ui/Toaster.svelte';
	import { iconButtonClass } from '#lib/ui/button.js';
	import CommandPalette from './CommandPalette.svelte';
	import DemoBanner from './DemoBanner.svelte';
	import DemoOff from './DemoOff.svelte';
	import LiveIndicator from './LiveIndicator.svelte';
	import Shortcuts from './Shortcuts.svelte';
	import Sidebar from './Sidebar.svelte';
	import { header } from './header.svelte.js';
	import { navItemFor } from './nav.js';

	interface Props {
		children: Snippet;
	}

	let { children }: Props = $props();

	/** Macs say ⌘ for the palette's shortcut, and everything else Ctrl. */
	const modKey = /Mac|iPhone|iPad/.test(navigator.userAgent) ? '⌘' : 'Ctrl';

	let drawerOpen = $state(false);
	let paletteOpen = $state(false);
	let shortcutsOpen = $state(false);

	const item = $derived(navItemFor(page.route.id ?? ''));
	/** A device's page is named after the device, which only the live data knows. */
	const deviceName = $derived(
		page.route.id === '/devices/[id]'
			? live.snapshot?.devices.find((d) => d.id === page.params.id)?.name
			: undefined
	);
	const title = $derived(
		page.data.title ??
			deviceName ??
			(page.error ? (page.status === 404 ? 'Not found' : 'Error') : item?.label) ??
			live.name
	);
	/** What the breadcrumb says after the page's name, such as the device a page is about. */
	const detail = $derived(header.detail ?? (title !== item?.label ? title : undefined));

	/**
	 * The pages drawn edge to edge on the sheet, in sections split by lines,
	 * as the Paper redesign has them. The rest sit in cards with room around.
	 */
	const flushRoutes = new Set(['/', '/devices/[id]', '/docker', '/files']);
	const flush = $derived(page.route.id !== null && flushRoutes.has(page.route.id) && !page.error);

	/** The page is for admins, and the caller isn't one. */
	const forbidden = $derived(item?.adminOnly === true && !isAdmin(page.data.me));
	/** What a demo doesn't have, if this is the page for it. */
	const demoOff = $derived(
		live.demo
			? ({ '/terminal': 'Terminals', '/files': 'Files' } as const)[item?.href as never]
			: undefined
	);

	/**
	 * Whether the stream has been down long enough to say so. A restart of the
	 * hub drops it for a moment, and that isn't worth a notice.
	 */
	let unreachable = $state(false);
	$effect(() => {
		if (live.connection !== 'reconnecting') {
			unreachable = false;
			return;
		}
		const timer = setTimeout(() => (unreachable = true), 4000);
		return () => clearTimeout(timer);
	});

	afterNavigate(() => {
		drawerOpen = false;
	});

	$effect(() => connect({ onDrop: checkRole }));

	/**
	 * Asks the hub again who the caller is, and reloads `me` if their role
	 * changed, so the sidebar and pages follow. The hub ends the stream when
	 * a role changes. An answer that Bield couldn't find out their role isn't
	 * a change: the next drop asks again.
	 */
	async function checkRole() {
		try {
			const me = await getMe();
			if (me.role !== page.data.me?.role && !lookupFailed(me)) {
				await invalidate('bield:me');
			}
		} catch {
			// The hub is down or restarting. The stream keeps trying.
		}
	}
</script>

<svelte:head>
	<title>{title === live.name ? title : `${title} · ${live.name}`}</title>
</svelte:head>

<div class="min-h-dvh">
	<aside class="fixed inset-y-0 left-0 z-30 hidden w-55 bg-canvas lg:block">
		<Sidebar onshortcuts={() => (shortcutsOpen = true)} />
	</aside>

	<Dialog.Root bind:open={drawerOpen}>
		<Dialog.Portal>
			<Dialog.Overlay class="fixed inset-0 z-40 bg-black/70 fade lg:hidden" />
			<Dialog.Content
				class="fixed inset-y-0 left-0 z-40 w-64 border-r border-line bg-canvas transition-[translate] duration-200 ease-entrance outline-none data-[ending-style]:-translate-x-full data-[starting-style]:-translate-x-full lg:hidden"
			>
				<Dialog.Title class="sr-only">Pages</Dialog.Title>
				<Sidebar onshortcuts={() => (shortcutsOpen = true)} />
			</Dialog.Content>
		</Dialog.Portal>
	</Dialog.Root>

	<!--
		On a big screen the page is a sheet with rounded corners. The header
		sticks to the top of the window, over a strip of the canvas, so what
		scrolls under it never shows above the sheet's corners.
	-->
	<div class="lg:pr-2 lg:pb-2 lg:pl-55">
		<div class="sticky top-0 z-20 lg:bg-canvas lg:pt-2">
			<header
				class="flex h-13 items-center gap-4 border-b border-line bg-surface/90 px-4 backdrop-blur-md sm:px-5 lg:rounded-t-[10px] lg:border-x lg:border-t lg:bg-surface lg:backdrop-blur-none"
			>
				<button
					type="button"
					class={[iconButtonClass(), '-ml-1.5 lg:hidden']}
					onclick={() => (drawerOpen = true)}
					aria-label="Open the page list"
				>
					<MenuIcon class="size-4" />
				</button>
				<nav
					class="flex min-w-0 flex-1 items-center gap-2 text-[13px] leading-5"
					aria-label="Breadcrumb"
				>
					{#if detail && item}
						<a
							href={resolve(item.href)}
							class="hidden shrink-0 text-fg-faint transition-colors hover:text-fg-muted sm:inline"
							>{item.label}</a
						>
						<span class="hidden text-fg-faint sm:inline" aria-hidden="true">/</span>
						<span class="truncate font-medium text-fg" aria-current="page">{detail}</span>
					{:else}
						<span class="truncate font-medium text-fg" aria-current="page">{title}</span>
					{/if}
				</nav>
				<LiveIndicator />
				<button
					type="button"
					onclick={() => (paletteOpen = true)}
					class="flex h-7.5 shrink-0 items-center gap-2 rounded-md border border-line-strong pr-1.5 pl-2.5 text-[13px] leading-5 text-fg-faint transition-colors hover:border-line-heavy hover:text-fg-muted max-sm:w-7.5 max-sm:justify-center max-sm:px-0 sm:w-52"
					aria-label="Search pages and devices"
					aria-keyshortcuts="Meta+K Control+K"
				>
					<Search class="size-3.5 shrink-0" strokeWidth={2} />
					<span class="hidden flex-1 text-left sm:inline">Search</span>
					<span
						class="hidden rounded-[4px] bg-raised px-1.25 py-0.5 font-mono text-[11px] leading-3.5 sm:inline"
						aria-hidden="true">{modKey === '⌘' ? '⌘K' : 'Ctrl K'}</span
					>
				</button>
			</header>
		</div>
		{#if unreachable}
			<p
				role="status"
				class="border-b border-amber/25 bg-amber/5 px-4 py-2 text-[13px] text-amber sm:px-6 lg:border-x lg:border-x-line"
			>
				Can't reach Bield{live.receivedAt > 0
					? `, so this is what it sent at ${clock(live.receivedAt)}`
					: ''}. Trying again.
			</p>
		{/if}
		{#if live.demo}
			<DemoBanner />
		{/if}
		<main
			class={[
				'min-h-[calc(100dvh-3.25rem)] bg-surface max-lg:pb-[env(safe-area-inset-bottom)] lg:min-h-[calc(100dvh-4.25rem)] lg:rounded-b-[10px] lg:border-x lg:border-b lg:border-line',
				!flush && 'px-4 py-6 sm:px-6 lg:py-8'
			]}
		>
			<div class={[!flush && 'mx-auto max-w-[1400px]']}>
				{#if forbidden}
					<EmptyState title="Only admins can open {title}" icon={LockKeyhole}>
						An admin can make you one with <code class="font-mono text-fg-muted"
							>bield admin add</code
						>.
					</EmptyState>
				{:else if demoOff}
					<DemoOff what={demoOff} />
				{:else}
					{@render children()}
				{/if}
			</div>
		</main>
	</div>

	<CommandPalette bind:open={paletteOpen} onshortcuts={() => (shortcutsOpen = true)} />
	<Shortcuts bind:open={shortcutsOpen} mod={modKey} />
	<Toaster />
</div>

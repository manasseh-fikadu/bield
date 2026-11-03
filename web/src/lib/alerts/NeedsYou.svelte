<!--
	The alerts that need someone, for the side of Overview: the ones nobody has
	acknowledged, the most severe first, and then the acknowledged ones, which
	are quiet. One is open at a time, with what can be done about it.
-->
<script lang="ts">
	import ShieldCheck from '@lucide/svelte/icons/shield-check';
	import { resolve } from '$app/paths';
	import { activeAlerts } from '#lib/alerts.js';
	import type { Alert, Device } from '#lib/api/types.js';
	import { ago, plural } from '#lib/format.js';
	import type { Status } from '#lib/status.js';
	import EmptyState from '#lib/ui/EmptyState.svelte';
	import Skeleton from '#lib/ui/Skeleton.svelte';
	import StatusDot from '#lib/ui/StatusDot.svelte';
	import AlertActions from './AlertActions.svelte';

	interface Props {
		/** The alerts to show from, or null before the first snapshot. */
		alerts: Alert[] | null;
		devices: Device[];
		now: number;
		/** The most to show. */
		limit?: number;
	}

	let { alerts, devices, now, limit = 5 }: Props = $props();

	const active = $derived(alerts ? activeAlerts(alerts) : []);
	// Alerts that are only acknowledged come after the rest, and keep their order.
	const sorted = $derived(active.toSorted((a, b) => Number(!!a.ackedAt) - Number(!!b.ackedAt)));
	const shown = $derived(sorted.slice(0, limit));
	const snoozed = $derived(alerts ? alerts.filter((a) => a.state === 'snoozed').length : 0);

	/** The alert someone opened, if it's still here. Otherwise the first that needs someone. */
	let picked = $state<number | null>(null);
	const openId = $derived(
		shown.some((a) => a.id === picked) ? picked : (shown.find((a) => !a.ackedAt)?.id ?? null)
	);

	const lamps: Record<Alert['severity'], Status> = {
		critical: 'offline',
		warning: 'attention',
		info: 'online'
	};

	/** The line under an alert's title: what's wrong, or who has seen it. */
	function line(a: Alert): string {
		if (a.ackedAt) {
			return `Acknowledged${a.ackedBy ? ` by ${a.ackedBy}` : ''}, ${ago(a.ackedAt, now)}.`;
		}
		const detail = a.detail.replace(/\.?$/, '.');
		const device = devices.find((d) => d.id === a.deviceId);
		// An alert that names its machine doesn't need to say it again.
		return device && !a.title.includes(device.name)
			? `${detail} On ${device.name}, ${ago(a.openedAt, now)}.`
			: detail;
	}
</script>

<section class="flex flex-col" aria-label="What needs you">
	<header class="flex h-12 shrink-0 items-center justify-between px-4">
		<h2 class="text-[13px] leading-5 font-medium text-fg">Needs you</h2>
		<a
			href={resolve('/alerts')}
			class="text-[12px] leading-4 text-fg-faint transition-colors hover:text-fg-muted pointer-coarse:-my-2.5 pointer-coarse:py-2.5"
		>
			All alerts →
		</a>
	</header>
	{#if alerts === null}
		<div class="border-t border-line p-4"><Skeleton class="h-16" /></div>
	{:else if shown.length === 0}
		<div class="border-t border-line">
			<EmptyState title="Nothing needs you" icon={ShieldCheck}>
				Everything the hub can see looks fine.
			</EmptyState>
		</div>
	{:else}
		<ul>
			{#each shown as a (a.id)}
				{@const open = a.id === openId}
				{@const acked = !!a.ackedAt}
				<li class={['border-t border-line transition-colors', open && 'bg-raised']}>
					<button
						type="button"
						class={[
							'flex w-full gap-2.5 px-4 pt-3 text-left focus-visible:rounded-none',
							!open && 'pb-3'
						]}
						aria-expanded={open}
						onclick={() => (picked = a.id)}
					>
						<span class="flex h-5 w-2 shrink-0 items-center">
							{#if acked}
								<StatusDot status="unknown" />
							{:else}
								<StatusDot status={lamps[a.severity]} breathe={a.severity !== 'info'} filled />
							{/if}
						</span>
						<span class="flex min-w-0 flex-1 flex-col gap-0.5">
							<span
								class={['text-[13px] leading-5', acked ? 'text-fg-muted' : 'font-medium text-fg']}
							>
								{a.title}
							</span>
							<span class={['text-[12px] leading-4.25', acked ? 'text-fg-faint' : 'text-fg-muted']}>
								{line(a)}
							</span>
						</span>
					</button>
					{#if open}
						<AlertActions alert={a} primary={!acked} class="pt-2 pr-4 pb-3 pl-8.5" />
					{/if}
				</li>
			{/each}
		</ul>
	{/if}
	{#if active.length > shown.length || snoozed > 0}
		<p class="flex gap-2 border-t border-line px-4 py-2.5 text-[12px] leading-4 text-fg-faint">
			{#if active.length > shown.length}
				<a href={resolve('/alerts')} class="hover:text-fg-muted">
					{plural(active.length - shown.length, 'more alert')}
				</a>
			{/if}
			{#if snoozed > 0}
				<a href="{resolve('/alerts')}?tab=snoozed" class="hover:text-fg-muted">{snoozed} snoozed</a>
			{/if}
		</p>
	{/if}
</section>

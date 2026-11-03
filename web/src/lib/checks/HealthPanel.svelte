<!--
	The health score on Overview, over the last day. The history loads again
	every minute.
-->
<script lang="ts">
	import { resolve } from '$app/paths';
	import { getHealthHistory } from '#lib/api/client.js';
	import type { HealthHistory, HealthScore } from '#lib/api/types.js';
	import Sparkline from '#lib/charts/Sparkline.svelte';
	import { stepValues } from '#lib/history.js';
	import Skeleton from '#lib/ui/Skeleton.svelte';

	interface Props {
		/** The health score, or null before the first snapshot. */
		health: HealthScore | null;
	}

	let { health }: Props = $props();

	let history = $state.raw<HealthHistory | null>(null);

	$effect(() => {
		let current = true;
		async function load() {
			try {
				const h = await getHealthHistory('24h');
				if (current) {
					history = h;
				}
			} catch {
				// The panel keeps the history it has, and tries again.
			}
		}
		load();
		const timer = setInterval(load, 60_000);
		return () => {
			current = false;
			clearInterval(timer);
		};
	});

	const score = $derived(health?.score ?? null);
	const values = $derived(history ? stepValues(history, history.score) : []);
	const sampled = $derived(values.some((v) => v !== null));
</script>

<section class="flex shrink-0 flex-col gap-2.5 border-t border-line px-4 pt-3.5 pb-4">
	<header class="flex h-5 shrink-0 items-center justify-between">
		<h2 class="text-[13.5px] leading-5 font-medium text-fg">Health</h2>
		<a
			href={resolve('/checks')}
			class="text-[12px] leading-5 text-fg-faint transition-colors hover:text-fg-muted"
		>
			Checks →
		</a>
	</header>
	{#if !history}
		<Skeleton class="h-14" />
	{:else if sampled}
		<Sparkline {values} height={56} min={0} max={100} stroke={1.5} color="var(--color-lamp)" />
	{:else}
		<p class="flex h-14 items-center text-[12px] leading-4 text-fg-faint">
			{health && health.total > 0 ? 'No score yet today.' : 'Add a check to get a score.'}
		</p>
	{/if}
	<div class="flex h-3.5 shrink-0 justify-between">
		<span class="text-[11px] leading-3.5 text-fg-faint">24 hours ago</span>
		<span class="font-mono text-[11px] leading-3.5 text-fg-muted">{score ?? '—'} now</span>
	</div>
</section>

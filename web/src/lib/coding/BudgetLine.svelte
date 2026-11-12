<!--
	How much of the AI usage budget of every machine together is used over its
	window, in a line under the AI usage page's header. Its alerts see the same
	numbers. An admin sets the budget in Settings, under Alerts.
-->
<script lang="ts">
	import { resolve } from '$app/paths';
	import AdminOnly from '#lib/AdminOnly.svelte';
	import type { CodingBudget } from '#lib/api/types.js';
	import Meter from '#lib/ui/Meter.svelte';
	import { budgetUse, codingRangeName, type CodingRange } from './coding.js';

	interface Props {
		budget: CodingBudget;
	}

	let { budget }: Props = $props();

	const uses = $derived(budgetUse(budget));
</script>

<div class="flex flex-wrap items-center gap-x-6 gap-y-2 text-[12px] text-fg-subtle">
	<span>
		Budget for the last {codingRangeName(budget.window as CodingRange)}
	</span>
	{#each uses as u (u.metric)}
		<span class="flex items-center gap-2">
			<span class="font-mono whitespace-nowrap text-fg-muted">{u.text}</span>
			<span class="w-16 shrink-0"><Meter value={u.percent} warn={80} crit={100} /></span>
			<span class={u.percent >= 100 ? 'text-red-fg' : u.percent >= 80 ? 'text-amber-fg' : ''}>
				{Math.floor(u.percent)}%
			</span>
		</span>
	{/each}
	<AdminOnly>
		<a
			href="{resolve('/settings')}?tab=alerts"
			class="text-fg-faint underline decoration-line-heavy underline-offset-4 hover:text-fg"
			>Change</a
		>
	</AdminOnly>
</div>

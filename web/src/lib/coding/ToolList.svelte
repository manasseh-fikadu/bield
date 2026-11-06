<!--
	The tools that were used, biggest first, each with its sessions, and its
	share of the cost or of the tokens, whichever is on show.
-->
<script lang="ts">
	import type { CodingTool, CodingTotals } from '#lib/api/types.js';
	import { plural } from '#lib/format.js';
	import { compactTokens, dollars, share, toolColor, toolName, type Metric } from './coding.js';

	interface Props {
		tools: CodingTool[];
		/** The totals the shares are of. */
		totals: CodingTotals;
		metric: Metric;
		/** Smaller, for a card that has less room. */
		compact?: boolean;
	}

	let { tools, totals, metric, compact = false }: Props = $props();
</script>

<ul class={compact ? 'space-y-2.5' : 'space-y-4'}>
	{#each tools as t (t.tool)}
		{@const own = t.totals}
		<li>
			<div class="flex items-baseline justify-between gap-3">
				<span class="flex min-w-0 items-center gap-2">
					<span class="size-2 shrink-0 rounded-full" style:background={toolColor(t.tool)}></span>
					<span class={['truncate text-fg', compact ? 'text-[13px]' : 'text-[14px]']}>
						{toolName(t.tool)}
					</span>
					<span class="shrink-0 text-[12px] text-fg-faint">{plural(own.sessions, 'session')}</span>
				</span>
				<span class={['shrink-0 font-mono text-fg', compact ? 'text-[13px]' : 'text-[14px]']}>
					{metric === 'cost' ? dollars(own.cost.total) : compactTokens(own.tokens.total)}
				</span>
			</div>
			<p class="pl-4 text-[12px] leading-5 text-fg-faint">
				{#if metric === 'cost'}
					{share(own.cost.total, totals.cost.total)} of cost · {compactTokens(own.tokens.total)} tokens
				{:else}
					{share(own.tokens.total, totals.tokens.total)} of tokens · {dollars(own.cost.total)}
				{/if}
			</p>
		</li>
	{/each}
</ul>

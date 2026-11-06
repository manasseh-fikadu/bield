<!--
	The tokens that were processed, by kind, and what the cache saved. A token
	the cache served again costs a tenth or less of one that was sent fresh,
	which is why most of the cost of a long session is cache reads and writes.
-->
<script lang="ts">
	import type { CodingTotals } from '#lib/api/types.js';
	import { compactTokens, dollars } from './coding.js';

	interface Props {
		totals: CodingTotals;
	}

	let { totals }: Props = $props();

	const tiles = $derived([
		{ label: 'Processed tokens', value: compactTokens(totals.tokens.total) },
		{ label: 'Cache read', value: compactTokens(totals.tokens.cacheRead) },
		{ label: 'Cache write', value: compactTokens(totals.tokens.cacheWrite) },
		{ label: 'Uncached input', value: compactTokens(totals.tokens.input) },
		{ label: 'Output', value: compactTokens(totals.tokens.output) },
		{ label: 'Cache savings', value: dollars(totals.cacheSavings) }
	]);
</script>

<dl class="grid grid-cols-2 gap-x-6 gap-y-4 sm:grid-cols-3 lg:grid-cols-6">
	{#each tiles as t (t.label)}
		<div class="min-w-0">
			<dt class="text-[12px] leading-4 text-fg-subtle">{t.label}</dt>
			<dd class="mt-1 truncate font-mono text-[18px] leading-6 text-fg">{t.value}</dd>
		</div>
	{/each}
</dl>

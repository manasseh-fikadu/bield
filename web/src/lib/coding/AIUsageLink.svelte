<!--
	A link under a device's name that jumps down to its AI usage card, which is
	far down the page. It says what the card would: the cost over its range and
	how many sessions are live, or that watching is off, so a machine that has
	the card is never missed.
-->
<script lang="ts">
	import ArrowDown from '@lucide/svelte/icons/arrow-down';
	import Sparkles from '@lucide/svelte/icons/sparkles';
	import { dollars, type Glance } from './coding.js';
	import Live from './Live.svelte';

	interface Props {
		/** What the card says, or null before it has loaded. */
		glance: Glance | null;
	}

	let { glance }: Props = $props();
</script>

<a
	href="#ai-usage"
	class="mt-2 inline-flex h-7 w-fit max-w-full items-center gap-2 rounded-full border border-line-strong px-3 text-[12px] whitespace-nowrap text-fg-muted transition-colors hover:bg-hover/50 hover:text-fg"
>
	<Sparkles class="size-3.5 shrink-0" />
	<span class="text-fg">AI usage</span>
	{#if glance?.enabled && glance.hasData}
		<span class="font-mono text-fg">{dollars(glance.cost)}</span>
		<span class="text-fg-faint">{glance.range}</span>
		{#if glance.live}
			<Live label="{glance.live} live" />
		{/if}
	{:else if glance?.enabled}
		<span class="text-fg-faint">nothing yet</span>
	{:else if glance}
		<span class="text-fg-faint">not watched</span>
	{/if}
	<ArrowDown class="size-3 shrink-0 text-fg-faint" />
</a>

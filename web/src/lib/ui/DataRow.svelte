<!--
	One fact in a card's list, such as "Tailnet IP 100.64.0.2", with a button
	to copy it if `copy` is given. Put the rows in a <dl>.
-->
<script lang="ts">
	import type { Snippet } from 'svelte';
	import CopyButton from './CopyButton.svelte';

	interface Props {
		label: string;
		/** The text the copy button copies. */
		copy?: string;
		/** A tight row with no line under it, for a list on the sheet. */
		flat?: boolean;
		children: Snippet;
	}

	let { label, copy, flat = false, children }: Props = $props();
</script>

{#if flat}
	<div class="group flex items-center justify-between gap-3">
		<dt class="shrink-0 text-[12px] leading-4.25 text-fg-faint">{label}</dt>
		<dd class="flex min-w-0 items-center gap-1 text-right">
			{#if copy}
				<span
					class="-my-1.5 opacity-0 transition-opacity group-focus-within:opacity-100 group-hover:opacity-100 pointer-coarse:opacity-100"
				>
					<CopyButton value={copy} label="Copy {label}" />
				</span>
			{/if}
			<span class="truncate font-mono text-[12px] leading-4.25 text-fg">{@render children()}</span>
		</dd>
	</div>
{:else}
	<div
		class="flex min-h-9 items-center justify-between gap-4 border-b border-line px-4 py-1.5 last:border-0"
	>
		<dt class="shrink-0 text-[13px] text-fg-subtle">{label}</dt>
		<dd class="flex min-w-0 items-center gap-1 text-right">
			<span class="truncate num text-[12.5px] text-fg">{@render children()}</span>
			{#if copy}
				<span class="-mr-2"><CopyButton value={copy} label="Copy {label}" /></span>
			{/if}
		</dd>
	</div>
{/if}

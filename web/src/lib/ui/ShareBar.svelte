<!--
	A bar split into parts in proportion to their values, with a key under it
	that says what each part is worth. It's for parts of a whole that add up,
	such as what each kind of token cost.
-->
<script lang="ts">
	interface Segment {
		key: string;
		label: string;
		value: number;
		/** A CSS color. */
		color: string;
		/** The value as the key writes it, such as "$1,748.72". */
		text: string;
	}

	interface Props {
		segments: Segment[];
		/** What the bar shows, for screen readers, such as "Cost by type". */
		label: string;
		class?: string;
	}

	let { segments, label, class: className }: Props = $props();

	const total = $derived(segments.reduce((sum, s) => sum + Math.max(0, s.value), 0));
	const summary = $derived(segments.map((s) => `${s.label} ${s.text}`).join(', '));
</script>

<div class={className}>
	<div
		role="img"
		aria-label="{label}: {summary}"
		class="flex h-2 w-full gap-px overflow-hidden rounded-[3px] bg-line"
	>
		{#each segments as s (s.key)}
			{#if s.value > 0 && total > 0}
				<span
					class="block h-full min-w-0.5 first:rounded-l-[3px] last:rounded-r-[3px]"
					style:width="{(s.value / total) * 100}%"
					style:background={s.color}
				></span>
			{/if}
		{/each}
	</div>
	<ul class="mt-2.5 flex flex-wrap gap-x-4 gap-y-1">
		{#each segments as s (s.key)}
			<li class="flex items-center gap-1.5 text-[12px] leading-4 text-fg-subtle">
				<span class="size-2 shrink-0 rounded-[2px]" style:background={s.color}></span>
				{s.label}
				<span class="font-mono text-fg-muted">{s.text}</span>
			</li>
		{/each}
	</ul>
</div>

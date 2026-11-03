<!-- A labelled number over a thin bar, on a card on the network map. -->
<script lang="ts">
	import { percent } from '#lib/format.js';

	interface Props {
		label: string;
		/** A percentage, or null if it's not known. */
		value: number | null;
		/** From here it's amber. */
		warn?: number;
		/** From here it's red. */
		crit?: number;
		/** Brighter, for the hub. */
		bright?: boolean;
		/** Fainter, for a machine that's off. */
		dim?: boolean;
	}

	let {
		label,
		value,
		warn = Infinity,
		crit = Infinity,
		bright = false,
		dim = false
	}: Props = $props();

	const pct = $derived(value === null ? 0 : Math.max(0, Math.min(100, value)));
	const level = $derived(
		value === null || dim ? 'none' : pct >= crit ? 'crit' : pct >= warn ? 'warn' : 'fine'
	);
</script>

<div class="flex min-w-0 flex-1 flex-col gap-1.25">
	<div class="flex justify-between gap-1">
		<span class="text-[11px] leading-3.5 text-fg-faint">{label}</span>
		<span
			class={[
				'font-mono text-[11px] leading-3.5',
				level === 'crit'
					? 'text-red-fg'
					: level === 'warn'
						? 'text-amber'
						: dim || value === null
							? 'text-fg-faint'
							: bright
								? 'text-fg'
								: 'text-fg-muted'
			]}
		>
			{value === null ? '–' : percent(value)}
		</span>
	</div>
	<span
		class={['block h-0.5 rounded-[1px]', dim ? 'bg-line' : 'bg-line-strong']}
		aria-hidden="true"
	>
		{#if value !== null}
			<span
				class={[
					'block h-full rounded-[1px] transition-[width] duration-500',
					level === 'crit'
						? 'bg-red-fg'
						: level === 'warn'
							? 'bg-amber'
							: dim
								? 'bg-track'
								: bright
									? 'bg-fg-muted'
									: 'bg-fg-faint'
				]}
				style:width="{pct}%"
			></span>
		{/if}
	</span>
</div>

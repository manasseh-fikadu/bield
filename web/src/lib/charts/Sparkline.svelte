<!-- A small line of recent values with no axes, as on a metric tile. -->
<script lang="ts">
	import { areaPath, linePath, runs } from './path.js';

	interface Props {
		/** Evenly spaced values, oldest first. Null breaks the line. */
		values: (number | null)[];
		height?: number;
		/** A CSS color. */
		color?: string;
		/** A faint area under the line. */
		fill?: boolean;
		/** How thick the line is, in pixels. */
		stroke?: number;
		/** The value at the bottom. The smallest value if it's not given. */
		min?: number;
		/** The value at the top. The largest value if it's not given. */
		max?: number;
		class?: string;
	}

	let {
		values,
		height = 28,
		color = 'var(--color-fg)',
		fill = true,
		stroke = 1.25,
		min,
		max,
		class: className
	}: Props = $props();

	let width = $state(0);

	const lines = $derived.by(() => {
		const nums = values.filter((v) => v !== null);
		const lo = min ?? (nums.length ? Math.min(...nums) : 0);
		const hi = max ?? (nums.length ? Math.max(...nums) : 1);
		const span = hi - lo || 1;
		const x = (i: number) => (values.length > 1 ? (i / (values.length - 1)) * width : width / 2);
		const y = (v: number) => height - 2 - ((v - lo) / span) * (height - 4);
		return runs(
			values.map((v, i) => ({ ts: i, v })),
			x,
			y,
			1
		);
	});
</script>

<div
	bind:clientWidth={width}
	class={['w-full', className]}
	style:height="{height}px"
	aria-hidden="true"
>
	{#if width > 0}
		<svg {width} {height} class="block animate-wipe overflow-visible">
			{#each lines as run, i (i)}
				{#if run.length === 1}
					<!-- A value between two gaps has no line, so it gets a dot. -->
					<circle cx={run[0][0]} cy={run[0][1]} r="1.5" fill={color} />
				{:else}
					{#if fill}
						<path d={areaPath(run, height)} fill={color} fill-opacity="0.05" />
					{/if}
					<path
						d={linePath(run)}
						fill="none"
						stroke={color}
						stroke-width={stroke}
						stroke-linejoin="round"
						stroke-linecap="round"
					/>
				{/if}
			{/each}
		</svg>
	{/if}
</div>

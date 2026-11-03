<!--
	A small line of how something went lately, for a row of a table. It loads
	only while it's on screen, so a long table doesn't ask about every row, and
	again every minute.
-->
<script lang="ts">
	import { untrack } from 'svelte';
	import Sparkline from './Sparkline.svelte';

	interface Props {
		/** What it's a line of. Another one starts over. */
		id: string;
		/** Loads the values, oldest first. Null is a gap. */
		load: (id: string) => Promise<(number | null)[]>;
		/** The width and height classes. */
		class?: string;
	}

	let { id, load, class: className = 'h-4 w-16' }: Props = $props();

	let el = $state<HTMLElement>();
	let values = $state.raw<number[]>([]);

	$effect(() => {
		const current = id;
		const target = el;
		if (!target) {
			return;
		}
		let live = true;
		let timer: ReturnType<typeof setInterval> | undefined;
		async function get() {
			try {
				const got = await untrack(() => load(current));
				if (live) {
					// At this size a gap would only leave dots, so the line joins what there is.
					values = got.filter((v) => v !== null);
				}
			} catch {
				// The line stays as it was, and it tries again in a minute.
			}
		}
		const watch = new IntersectionObserver(([entry]) => {
			clearInterval(timer);
			if (entry.isIntersecting) {
				void get();
				timer = setInterval(get, 60_000);
			}
		});
		watch.observe(target);
		return () => {
			live = false;
			clearInterval(timer);
			watch.disconnect();
		};
	});

	// A little room above and below, so a quiet line doesn't look like a storm.
	const lo = $derived(values.length ? Math.min(...values) : 0);
	const hi = $derived(values.length ? Math.max(...values) : 1);
	const room = $derived(Math.max(hi - lo, 4));
</script>

<div bind:this={el} class={['shrink-0', className]}>
	{#if values.length > 1}
		<Sparkline
			{values}
			height={16}
			fill={false}
			min={Math.max(0, lo - room)}
			max={hi + room}
			color="var(--color-fg-faint)"
		/>
	{/if}
</div>

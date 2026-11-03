<!--
	Shortcuts to folders, the bookmarked ones and the machine's Docker volumes,
	with the disk's free space. The sidebar and the phone's menu both show it.
-->
<script lang="ts">
	import X from '@lucide/svelte/icons/x';
	import type { FileSpace } from '#lib/api/types.js';
	import { bytes } from '#lib/format.js';
	import Meter from '#lib/ui/Meter.svelte';
	import { basename, tildify } from './paths.js';
	import type { Place } from './places.js';

	interface Props {
		places: Place[];
		bookmarks: string[];
		/** The folder that's open. */
		current: string | null;
		home: string | null;
		/** The machine's Docker volumes, by where they're mounted. */
		volumes?: { name: string; path: string }[];
		space: FileSpace | null | undefined;
		ongo: (path: string) => void;
		onremovebookmark: (path: string) => void;
	}

	let {
		places,
		bookmarks,
		current,
		home,
		volumes = [],
		space,
		ongo,
		onremovebookmark
	}: Props = $props();

	const used = $derived(
		space && space.total > 0 ? ((space.total - space.free) / space.total) * 100 : null
	);

	const link =
		'flex h-7 w-full items-center gap-2 rounded-[5px] px-2 text-left text-[13px] leading-5 transition-colors pointer-coarse:h-9';
	const heading = 'px-2 pb-1.5 text-[12px] leading-4 text-fg-faint';
	const activeClass = 'bg-hover text-fg';
	const idleClass = 'text-fg-muted hover:bg-hover/60 hover:text-fg';
</script>

<div class="flex flex-col gap-3.5">
	<div class="flex flex-col gap-0.5">
		<div class={heading}>Places</div>
		{#each places as p (p.path)}
			<button
				type="button"
				title={p.path}
				onclick={() => ongo(p.path)}
				class={[link, current === p.path ? activeClass : idleClass]}
			>
				<span class="truncate">{p.label}</span>
			</button>
		{/each}
	</div>
	{#if bookmarks.length > 0}
		<div class="flex flex-col gap-0.5">
			<div class={heading}>Bookmarks</div>
			{#each bookmarks as b (b)}
				{@const label = b === '/' ? '/' : basename(b)}
				<div class="group relative">
					<button
						type="button"
						title={tildify(b, home)}
						onclick={() => ongo(b)}
						class={[link, 'pr-7', current === b ? activeClass : idleClass]}
					>
						<span class="truncate">{label}</span>
					</button>
					<button
						type="button"
						onclick={() => onremovebookmark(b)}
						aria-label="Remove bookmark {label}"
						title="Remove bookmark"
						class="absolute top-1/2 right-1 flex size-5 -translate-y-1/2 items-center justify-center rounded text-fg-faint opacity-0 group-hover:opacity-100 hover:text-fg focus-visible:opacity-100 pointer-coarse:opacity-100"
					>
						<X class="size-3" />
					</button>
				</div>
			{/each}
		</div>
	{/if}
	{#if volumes.length > 0}
		<div class="flex flex-col gap-0.5">
			<div class={heading}>Docker volumes</div>
			{#each volumes as v (v.path)}
				<button
					type="button"
					title={v.path}
					onclick={() => ongo(v.path)}
					class={[link, current === v.path ? activeClass : idleClass]}
				>
					<span class="truncate">{v.name}</span>
				</button>
			{/each}
		</div>
	{/if}
	{#if space && used !== null}
		<div class="px-2">
			<div class="mb-1.5 flex items-baseline justify-between gap-2 text-[12px] leading-4">
				<span class="text-fg-faint">Disk</span>
				<span class="num text-fg-muted">{bytes(space.free)} free</span>
			</div>
			<Meter value={used} warn={85} crit={95} />
			<div class="mt-1 num text-[11px] text-fg-faint">of {bytes(space.total)}</div>
		</div>
	{/if}
</div>

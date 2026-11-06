<!--
	What stops the agent from reading a coding tool's files, such as OpenCode
	on a machine that has neither the sqlite3 command nor Python 3, with what
	to do about it. It says nothing when every tool is read.
-->
<script lang="ts">
	import TriangleAlert from '@lucide/svelte/icons/triangle-alert';
	import type { CodingSource } from '#lib/api/types.js';
	import { sourceHint } from './coding.js';

	interface Props {
		sources: CodingSource[];
		/** The machine as the note names it, from its ID: its name, or "this machine". */
		where: (deviceId: string) => string;
	}

	let { sources, where }: Props = $props();

	const notes = $derived(
		sources.flatMap((s) => {
			const text = sourceHint(s, where(s.deviceId));
			return text ? [{ key: `${s.deviceId}:${s.tool}`, text }] : [];
		})
	);
</script>

{#if notes.length}
	<ul class="space-y-1.5 rounded-lg border border-amber/30 bg-amber/5 px-4 py-3" role="status">
		{#each notes as n (n.key)}
			<li class="flex items-start gap-2 text-[13px] leading-5 text-fg-muted">
				<TriangleAlert class="mt-0.5 size-3.5 shrink-0 text-amber" />
				{n.text}
			</li>
		{/each}
	</ul>
{/if}

<!--
	Recent sessions of the AI tools, the latest first: the tool, the machine and
	the models it used, its tokens and cost, and when it last did anything. One
	that is going now says Live. The folder it worked in is only there for
	admins.
-->
<script lang="ts">
	import type { CodingSession } from '#lib/api/types.js';
	import { ago } from '#lib/format.js';
	import { compactTokens, dollars, modelsText, toolName } from './coding.js';
	import Live from './Live.svelte';
	import ToolMark from './ToolMark.svelte';

	interface Props {
		sessions: CodingSession[];
		/** A device's name, from its ID. Without it the machine isn't named. */
		nameOf?: (deviceId: string) => string;
		/** How many to list, or all of them. */
		limit?: number;
		now?: number;
	}

	let { sessions, nameOf, limit, now = Date.now() }: Props = $props();

	const shown = $derived(limit ? sessions.slice(0, limit) : sessions);
</script>

<ul class="divide-y divide-line">
	{#each shown as s (`${s.deviceId}:${s.tool}:${s.id}`)}
		<li class="flex flex-wrap items-center gap-x-3 gap-y-1 px-4 py-2.5">
			<span class="flex min-w-0 flex-1 basis-56 flex-col">
				<span class="flex min-w-0 items-center gap-2">
					<ToolMark tool={s.tool} />
					<span class="truncate text-[13px] text-fg">{toolName(s.tool)}</span>
					{#if s.project}
						<span class="truncate font-mono text-[12px] text-fg-muted">{s.project}</span>
					{/if}
				</span>
				<span class="truncate pl-4 text-[12px] leading-5 text-fg-faint">
					{[nameOf?.(s.deviceId), modelsText(s.models)].filter(Boolean).join(' · ')}
				</span>
			</span>
			<span class="flex shrink-0 items-baseline gap-4 pl-4 sm:pl-0">
				<span class="w-14 text-right font-mono text-[12px] text-fg-muted"
					>{compactTokens(s.tokens.total)}</span
				>
				<span class="w-16 text-right font-mono text-[13px] text-fg">{dollars(s.cost)}</span>
				<span class="flex w-16 justify-end text-[12px] text-fg-faint">
					{#if s.active}
						<Live />
					{:else}
						{ago(s.last, now)}
					{/if}
				</span>
			</span>
		</li>
	{/each}
</ul>

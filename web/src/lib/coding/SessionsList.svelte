<!--
	Recent coding sessions, the latest first: the tool, the machine and the
	models it used, its tokens and cost, and when it last did anything. A
	lamp lights for one that is going now. The folder it worked in is only
	there for admins.
-->
<script lang="ts">
	import type { CodingSession } from '#lib/api/types.js';
	import { ago } from '#lib/format.js';
	import StatusDot from '#lib/ui/StatusDot.svelte';
	import { compactTokens, dollars, modelsText, toolColor, toolName } from './coding.js';

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
			<span class="flex w-3 shrink-0 justify-center">
				{#if s.active}
					<StatusDot status="online" breathe={false} />
				{/if}
			</span>
			<span class="flex min-w-0 flex-1 basis-56 flex-col">
				<span class="flex min-w-0 items-center gap-2">
					<span class="size-2 shrink-0 rounded-full" style:background={toolColor(s.tool)}></span>
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
				<span class="w-16 text-right text-[12px] text-fg-faint">
					{s.active ? 'now' : ago(s.last, now)}
				</span>
			</span>
		</li>
	{/each}
</ul>

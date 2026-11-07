<!--
	Where each tool's limits stand on each machine, as the tools last said.
	Codex says how much of a window it has used. Claude Code only writes a
	limit down when it refuses a request for being over one, so for it the
	usual answer is that none was hit.
-->
<script lang="ts">
	import Gauge from '@lucide/svelte/icons/gauge';
	import type { CodingSummary } from '#lib/api/types.js';
	import EmptyState from '#lib/ui/EmptyState.svelte';
	import Meter from '#lib/ui/Meter.svelte';
	import { limitGroups } from './coding.js';
	import ToolMark from './ToolMark.svelte';

	interface Props {
		summary: CodingSummary;
		nameOf: (deviceId: string) => string;
		/** The time the limits are read at, in Unix milliseconds. */
		now: number;
	}

	let { summary, nameOf, now }: Props = $props();

	const groups = $derived(limitGroups(summary, now, nameOf));

	const tone = {
		ok: 'text-fg-muted',
		rejected: 'text-red-fg',
		expired: 'text-fg-faint',
		unknown: 'text-fg-faint',
		none: 'text-fg-faint'
	} as const;
</script>

{#if groups.length === 0}
	<EmptyState title="No limits on record" icon={Gauge}>
		Codex says how much of its limit it has used while it works, and Claude Code says when it ran
		into one. Neither has said anything on a machine Bield watches yet.
	</EmptyState>
{:else}
	<div class="space-y-6">
		{#each groups as g (g.deviceId)}
			<section>
				<h3 class="mb-2 text-[14px] font-medium text-fg">{g.name}</h3>
				<ul class="divide-y divide-line rounded-lg border border-line">
					{#each g.rows as r (r.key)}
						<li class="grid gap-x-4 gap-y-2 px-4 py-3 sm:grid-cols-[14rem_minmax(0,1fr)]">
							<div class="flex min-w-0 items-center gap-2">
								<ToolMark tool={r.tool} />
								<span class="truncate text-[13px] text-fg">{r.title}</span>
								{#if r.plan}
									<span class="shrink-0 font-mono text-[11px] text-fg-faint">{r.plan}</span>
								{/if}
							</div>
							<div class="min-w-0 space-y-1.5">
								{#if r.used !== null}
									<Meter value={r.used} warn={70} crit={90} class="h-1" />
								{/if}
								<p class={['text-[13px] leading-5', tone[r.state]]}>{r.text}</p>
							</div>
						</li>
					{/each}
				</ul>
			</section>
		{/each}
		<p class="text-[12px] leading-5 text-fg-faint">
			A tool writes a limit down when it works, so each figure is as old as the last session. Claude
			Code writes one only when a request is refused for being over a limit, so "no limit hit" says
			nothing about how close it is.
		</p>
	</div>
{/if}

<!--
	A container in the list: its lamp, name and image, what's wrong with it if
	anything is, what it uses, its port and how long it has been up. Opening it
	goes to `onopen` on a screen with room for the panel, and otherwise to its
	page.
-->
<script lang="ts">
	import type { DockerContainerRow } from '#lib/api/types.js';
	import LazyTrend from '#lib/charts/LazyTrend.svelte';
	import { bytes, percent, since } from '#lib/format.js';
	import StatusDot from '#lib/ui/StatusDot.svelte';
	import { getDockerHistory } from './api.js';
	import { containerHref } from './containerLink.js';
	import { rowStatus } from './docker.js';
	import { firstPort, rowNote } from './list.js';

	interface Props {
		container: DockerContainerRow;
		deviceId: string;
		/** When "up for" is measured from, in Unix milliseconds. */
		now: number;
		/** It's the one open in the panel. */
		open: boolean;
		/** Opens it in the panel. */
		onopen: (name: string) => void;
	}

	let { container: c, deviceId, now, open, onopen }: Props = $props();

	const status = $derived(rowStatus(c));
	const running = $derived(c.state === 'running');
	const note = $derived(rowNote(c, now));
	const port = $derived(firstPort(c.ports));
	const noteTone = { amber: 'text-amber', red: 'text-red-fg', faint: 'text-fg-faint' };

	async function cpuTrend(name: string) {
		const h = await getDockerHistory(deviceId, name, '1h');
		return h.cpu.map((p) => p.v);
	}

	function onclick(e: MouseEvent) {
		// A click that asks for a new tab gets one. The panel needs a wide screen.
		if (e.metaKey || e.ctrlKey || e.shiftKey || !matchMedia('(min-width: 1280px)').matches) {
			return;
		}
		e.preventDefault();
		onopen(c.name);
	}
</script>

<tr
	class={[
		'relative h-12.5 border-t border-line transition-colors',
		open ? 'bg-raised' : 'hover:bg-hover/50'
	]}
>
	<td class="pr-3 pl-4 sm:pl-6">
		<span class="flex min-w-0 items-center">
			<span class="w-5 shrink-0"><StatusDot {status} class="block" /></span>
			<span class="flex min-w-0 flex-col">
				<span class="flex min-w-0 items-baseline gap-2">
					<a
						href={containerHref(deviceId, c.name)}
						{onclick}
						aria-current={open ? 'true' : undefined}
						class={[
							'shrink-0 text-[13px] leading-4.5 font-medium after:absolute after:inset-0 focus-visible:rounded-sm',
							running ? 'text-fg' : 'text-fg-muted'
						]}
					>
						{c.name}
					</a>
					{#if note}
						<span class={['truncate text-[12px] leading-4.5', noteTone[note.tone]]}>
							{note.text}
						</span>
					{/if}
				</span>
				<span class="truncate font-mono text-[12px] leading-4 text-fg-faint" title={c.image}>
					{c.image}
				</span>
			</span>
		</span>
	</td>
	<td class="w-16 text-right font-mono text-[12px] leading-4 sm:w-30">
		{#if running && c.stats}
			<div class="flex items-center justify-end gap-2.5">
				<LazyTrend id={c.name} load={cpuTrend} class="h-4 w-14 max-sm:hidden" />
				<span class="text-fg">{percent(c.stats.cpu)}</span>
			</div>
		{:else}
			<span class="text-fg-faint">—</span>
		{/if}
	</td>
	<td
		class={[
			'w-21 text-right font-mono text-[12px] leading-4 whitespace-nowrap max-sm:hidden',
			running && c.stats?.memUsed != null ? 'text-fg' : 'text-fg-faint'
		]}
	>
		{running && c.stats?.memUsed != null ? bytes(c.stats.memUsed) : '—'}
	</td>
	<td
		class={[
			'w-15 text-right font-mono text-[12px] leading-4 whitespace-nowrap max-sm:hidden',
			port ? 'text-fg-muted' : 'text-fg-faint'
		]}
	>
		{port ? `${port.port}${port.more ? ` +${port.more}` : ''}` : '—'}
	</td>
	<td
		class={[
			'w-16 pr-4 text-right font-mono text-[12px] leading-4 sm:pr-6',
			running && c.startedAt ? 'text-fg-muted' : 'text-fg-faint'
		]}
	>
		{running && c.startedAt ? since(c.startedAt, now) : '—'}
	</td>
</tr>

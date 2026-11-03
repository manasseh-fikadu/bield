<!--
	What's using a machine now, the busiest first: its containers, services and
	whatever else its agent groups together. It says nothing about a machine
	Bield has no sample of.
-->
<script lang="ts">
	import { unitStatus } from '#lib/alerts.js';
	import { getDeviceUsage, getUsageHistory } from '#lib/api/client.js';
	import type {
		Device,
		DockerContainerRow,
		Unit,
		UsageGroup,
		UsageHistory,
		UsageSample
	} from '#lib/api/types.js';
	import Sparkline from '#lib/charts/Sparkline.svelte';
	import { getDockerContainers } from '#lib/docker/api.js';
	import { rowStatus } from '#lib/docker/docker.js';
	import { bytes, since } from '#lib/format.js';
	import type { Status } from '#lib/status.js';
	import { cpuText, groupName, kindLabel } from '#lib/usage.js';
	import StatusDot from '#lib/ui/StatusDot.svelte';

	interface Props {
		deviceId: string;
		/** Whether Bield can read the machine's Docker, which says how its containers are. */
		docker: boolean;
		/** The units Bield watches on the machine, which say how its services are. */
		units: Unit[];
		/** When "up for" is measured from, in Unix milliseconds. */
		now: number;
		/** Shows everything that's using the machine. */
		onall: () => void;
	}

	let { deviceId, docker, units, now, onall }: Props = $props();

	const limit = 6;

	let sample = $state.raw<UsageSample | null>(null);
	let trends = $state.raw<UsageHistory | null>(null);
	let containers = $state.raw<DockerContainerRow[]>([]);

	$effect(() => {
		const id = deviceId;
		let current = true;
		async function loadSample() {
			try {
				const u = await getDeviceUsage(id);
				if (current) {
					sample = u.sample ?? null;
				}
			} catch {
				// It keeps what it had, and tries again.
			}
		}
		async function loadTrends() {
			try {
				const h = await getUsageHistory(id, '1h');
				if (current) {
					trends = h;
				}
			} catch {
				// The lines stay as they were.
			}
		}
		loadSample();
		loadTrends();
		const samples = setInterval(loadSample, 30_000);
		const lines = setInterval(loadTrends, 60_000);
		return () => {
			current = false;
			clearInterval(samples);
			clearInterval(lines);
		};
	});

	$effect(() => {
		const id = deviceId;
		if (!docker) {
			return;
		}
		let current = true;
		async function load() {
			try {
				const list = await getDockerContainers(id);
				if (current) {
					containers = list.containers;
				}
			} catch {
				// It keeps what it had, and tries again.
			}
		}
		load();
		const timer = setInterval(load, 30_000);
		return () => {
			current = false;
			clearInterval(timer);
		};
	});

	const groups = $derived((sample?.groups ?? []).toSorted((a, b) => b.cpu - a.cpu).slice(0, limit));
	const byName = $derived(new Map(containers.map((c) => [c.name, c])));
	const unitsByName = $derived(new Map(units.map((u) => [u.name, u])));

	function containerOf(g: UsageGroup): DockerContainerRow | undefined {
		return g.kind === 'container' ? byName.get(groupName(g.key)) : undefined;
	}

	function statusOf(g: UsageGroup): Status {
		const c = containerOf(g);
		if (c) {
			return rowStatus(c);
		}
		const u = g.kind === 'service' ? unitsByName.get(groupName(g.key)) : undefined;
		return u ? unitStatus(u) : 'online';
	}

	function values(key: string): number[] {
		const points = trends?.cpu.find((s) => s.key === key)?.points ?? [];
		return points.flatMap((p) => p.v ?? []);
	}

	const hasUp = $derived(groups.some((g) => containerOf(g)?.startedAt));
</script>

{#if groups.length > 0}
	<section class="border-t border-line" aria-label="Running now">
		<header class="flex h-12 items-center justify-between px-4 sm:px-6">
			<h2 class="text-[13px] leading-5 font-medium text-fg">Running now</h2>
			<button
				type="button"
				class="text-[12px] leading-4 text-fg-faint transition-colors hover:text-fg-muted pointer-coarse:-my-2.5 pointer-coarse:py-2.5"
				onclick={onall}
			>
				Busiest first · All of it →
			</button>
		</header>
		<div class="overflow-x-auto">
			<table class="w-full table-fixed border-collapse text-left">
				<thead>
					<tr class="border-t border-line text-[12px] leading-4 text-fg-faint">
						<th class="h-7.5 pl-4 font-normal sm:pl-11">Name</th>
						<th class="w-30 font-normal max-sm:hidden">Kind</th>
						<th class="w-39 text-right font-normal">CPU</th>
						<th class="w-24 text-right font-normal sm:w-30">Memory</th>
						{#if hasUp}
							<th class="w-22.5 pr-4 text-right font-normal max-sm:hidden sm:pr-6">Up</th>
						{:else}
							<th class="w-4 sm:w-6" aria-hidden="true"></th>
						{/if}
					</tr>
				</thead>
				<tbody>
					{#each groups as g (g.key)}
						{@const c = containerOf(g)}
						{@const status = statusOf(g)}
						{@const line = values(g.key)}
						{@const problem = c?.health === 'unhealthy' ? 'Unhealthy' : ''}
						<tr class="h-10 border-t border-line">
							<td class="pl-4 sm:pl-6">
								<span class="flex min-w-0 items-center">
									<span class="w-5 shrink-0"><StatusDot {status} class="block" /></span>
									<span class="flex min-w-0 items-baseline gap-2.5">
										<span class="truncate text-[13px] leading-5 font-medium text-fg">{g.name}</span>
										{#if problem}
											<span class="text-[12px] leading-5 text-amber">{problem}</span>
										{/if}
									</span>
								</span>
							</td>
							<td class="text-[13px] leading-5 text-fg-muted capitalize max-sm:hidden">
								{kindLabel(g.kind)}
							</td>
							<td class="text-right font-mono text-[12px] leading-5 text-fg">
								<span class="flex items-center justify-end gap-2.5">
									{#if line.length > 1}
										<span class="w-16 shrink-0 max-sm:hidden">
											<Sparkline
												values={line}
												height={16}
												fill={false}
												color="var(--color-fg-faint)"
											/>
										</span>
									{/if}
									<span class="w-11 shrink-0">{cpuText(g.cpu)}</span>
								</span>
							</td>
							<td class="text-right font-mono text-[12px] leading-5 whitespace-nowrap text-fg">
								{bytes(g.mem)}
							</td>
							{#if hasUp}
								<td
									class="pr-4 text-right font-mono text-[12px] leading-5 text-fg-muted max-sm:hidden sm:pr-6"
								>
									{c?.startedAt ? since(c.startedAt, now) : '—'}
								</td>
							{:else}
								<td></td>
							{/if}
						</tr>
					{/each}
				</tbody>
			</table>
		</div>
	</section>
{/if}

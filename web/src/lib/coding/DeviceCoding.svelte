<!--
	The AI tools on one device (Claude Code, Codex, OpenCode, Pi): what they
	used, over a day or a week, and where their limits stand. An admin turns
	watching them on here. Until then the device's agent doesn't read their
	files at all. It loads again every minute. The device page links to it from
	its top, with what `glance` says.
-->
<script lang="ts">
	import { resolve } from '$app/paths';
	import AdminOnly from '#lib/AdminOnly.svelte';
	import type { Device } from '#lib/api/types.js';
	import AddAgent from '#lib/devices/AddAgent.svelte';
	import { Poll } from '#lib/docker/poll.svelte.js';
	import { plural } from '#lib/format.js';
	import Badge from '#lib/ui/Badge.svelte';
	import Card from '#lib/ui/Card.svelte';
	import Meter from '#lib/ui/Meter.svelte';
	import Segmented from '#lib/ui/Segmented.svelte';
	import Skeleton from '#lib/ui/Skeleton.svelte';
	import Switch from '#lib/ui/Switch.svelte';
	import { getDeviceCoding, setDeviceCoding } from './api.js';
	import {
		apiEstimate,
		compactTokens,
		dollars,
		limitGroups,
		minAgentVersion,
		sourceHint,
		versionBefore,
		type CodingRange,
		type Glance
	} from './coding.js';
	import Live from './Live.svelte';
	import SessionsList from './SessionsList.svelte';
	import SourceNotes from './SourceNotes.svelte';
	import ToolList from './ToolList.svelte';

	interface Props {
		device: Device;
		/** The time to read limits and ages at, in Unix milliseconds. */
		now: number;
		/** What the page's top says about this card, kept up to date from here. */
		glance?: Glance | null;
	}

	let { device, now, glance = $bindable(null) }: Props = $props();

	const ranges: { value: CodingRange; label: string }[] = [
		{ value: '24h', label: '24h' },
		{ value: '7d', label: '7 days' }
	];
	let range: CodingRange = $state('24h');

	// Every snapshot passes in a new `device`, so only its ID and name are
	// read, and the poll starts again only for another device or range.
	const deviceId = $derived(device.id);
	const name = $derived(device.name);
	const poll = $derived.by(() => {
		const id = deviceId;
		const r = range;
		return new Poll(() => getDeviceCoding(id, r), 60_000);
	});
	$effect(() => poll.start());

	const summary = $derived(poll.data);
	const rangeLabel = $derived(ranges.find((r) => r.value === range)?.label ?? range);

	// What an admin just chose, until the hub's answer to the next load says
	// so too, so the switch doesn't jump back while it's on its way.
	let chosen: boolean | null = $state(null);
	const enabled = $derived(chosen ?? summary?.enabled ?? false);

	async function toggle(next: boolean) {
		chosen = next;
		try {
			await setDeviceCoding(
				deviceId,
				next,
				next ? `Watching the AI tools on ${name}` : `Stopped watching the AI tools on ${name}`
			);
			await poll.reload();
		} catch {
			// setDeviceCoding showed why.
		} finally {
			chosen = null;
		}
	}

	// The machine Bield runs on is measured by Bield itself, so its agent is
	// there only to read the AI tools' files. Bield doesn't list that agent
	// (it keeps none of its heartbeats), so it is known by what it has sent.
	const builtIn = $derived(device.measuredBy === 'hub');
	const agentVersion = $derived(device.agent?.version);
	const tooOld = $derived(!!agentVersion && versionBefore(agentVersion, minAgentVersion));
	const hasData = $derived(
		!!summary && (summary.totals.tokens.total > 0 || summary.sessions.length > 0)
	);
	const limits = $derived(summary ? (limitGroups(summary, now, () => name)[0]?.rows ?? []) : []);
	const active = $derived(summary?.sessions.filter((s) => s.active).length ?? 0);
	const reported = $derived(hasData || !!summary?.sources.length);
	const hasNotes = $derived(!!summary?.sources.some((s) => sourceHint(s, 'this machine')));
	const builtInNote =
		"Bield measures this machine itself, so an agent here is only for reading the AI tools' sessions.";

	$effect(() => {
		glance = summary
			? {
					enabled: summary.enabled,
					hasData,
					cost: summary.totals.cost.total,
					live: active,
					range: rangeLabel
				}
			: null;
	});
	const claudeNoLimit = $derived(limits.some((r) => r.tool === 'claude' && r.state === 'none'));
</script>

<Card id="ai-usage" title="AI usage" class="scroll-mt-16 lg:col-span-2">
	{#snippet actions()}
		{#if enabled}
			<Segmented bind:value={range} options={ranges} label="Time range" />
		{/if}
		<AdminOnly>
			<label class="ml-2 flex items-center gap-2 text-[13px] text-fg-muted">
				Watch
				<Switch
					label="Watch AI tools on {name}"
					bind:checked={() => enabled, (v) => void toggle(v)}
					disabled={!summary && !poll.error}
				/>
			</label>
			{#snippet fallback()}
				<Badge>{enabled ? 'watched' : 'not watched'}</Badge>
			{/snippet}
		</AdminOnly>
	{/snippet}

	{#if !summary && !poll.error}
		<div class="space-y-3 p-4">
			<Skeleton class="h-6 w-48" />
			<Skeleton class="h-16" />
		</div>
	{:else if poll.error && !summary}
		<p class="flex flex-wrap items-center gap-x-3 px-4 py-4 text-[13px] text-fg-muted">
			Couldn't load the AI usage: {poll.error}
			<button
				type="button"
				class="text-fg underline underline-offset-4"
				onclick={() => poll.reload()}>Try again</button
			>
		</p>
	{:else if !enabled}
		<div class="space-y-2 px-4 py-4 text-[13px] leading-5 text-fg-muted">
			<p>
				{#if hasData}
					Watching is off, so Bield isn't counting anything new. What it counted stays.
				{:else}
					Bield can count the tokens that Claude Code, Codex, OpenCode and Pi use on {name}, for
					anything they're used for, and what they'd cost, from the session files they write.
					Prompts and replies never leave the machine.
				{/if}
			</p>
			<AdminOnly>
				<p class="text-fg-subtle">
					Turn on <span class="text-fg">Watch</span> to start. It needs the agent, version
					{minAgentVersion} or newer.
					{#if builtIn}
						{builtInNote}
					{:else if !device.agent}
						Install it from the <a
							href={resolve('/devices/agents')}
							class="text-fg underline decoration-line-heavy underline-offset-4">Agents</a
						> page first.
					{/if}
				</p>
				{#snippet fallback()}
					<p class="text-fg-subtle">An admin can turn watching on.</p>
				{/snippet}
			</AdminOnly>
		</div>
	{:else}
		{#if builtIn && !reported}
			<!-- Bield measures this machine, so it has no agent unless an admin adds one for this. -->
			<div
				class="flex flex-wrap items-center justify-between gap-x-4 gap-y-2 border-b border-line px-4 py-2.5"
			>
				<p class="min-w-0 flex-1 basis-72 text-[13px] leading-5 text-fg-muted">
					{builtInNote} Nothing has come from it yet.
				</p>
				<AdminOnly>
					<AddAgent {device} label="Install the agent" variant="primary" size="xs" />
				</AdminOnly>
			</div>
		{:else if !builtIn && (!device.agent || tooOld)}
			<p class="border-b border-line px-4 py-2.5 text-[13px] leading-5 text-fg-muted">
				{#if tooOld}
					The agent on {name} is version {agentVersion}, which doesn't report AI tools. Update it to {minAgentVersion}
					or newer from the
				{:else}
					{name} has no agent to read the AI tools. Install one from the
				{/if}
				<a
					href={resolve('/devices/agents')}
					class="text-fg underline decoration-line-heavy underline-offset-4">Agents</a
				> page.
			</p>
		{/if}
		{#if summary && hasNotes}
			<div class="border-b border-line p-3">
				<SourceNotes sources={summary.sources} where={() => 'this machine'} />
			</div>
		{/if}
		{#if summary && hasData}
			<div class="grid gap-x-8 gap-y-5 p-4 lg:grid-cols-[minmax(0,16rem)_minmax(0,1fr)]">
				<dl class="grid grid-cols-3 gap-3 lg:col-span-2">
					<div class="min-w-0">
						<dt class="text-[12px] leading-4 text-fg-subtle">Tokens</dt>
						<dd class="mt-1 truncate font-mono text-[20px] leading-6.5 text-fg">
							{compactTokens(summary.totals.tokens.total)}
						</dd>
					</div>
					<div class="min-w-0">
						<dt class="text-[12px] leading-4 text-fg-subtle">Cost · {apiEstimate}</dt>
						<dd class="mt-1 truncate font-mono text-[20px] leading-6.5 text-fg">
							{dollars(summary.totals.cost.total)}
						</dd>
					</div>
					<div class="min-w-0">
						<dt class="text-[12px] leading-4 text-fg-subtle">Sessions</dt>
						<dd class="mt-1 truncate font-mono text-[20px] leading-6.5 text-fg">
							{summary.totals.sessions}
							{#if active}
								<Live label="{active} live" class="ml-1 font-sans text-[12px] text-fg-muted" />
							{/if}
						</dd>
					</div>
				</dl>
				<ToolList tools={summary.tools} totals={summary.totals} metric="cost" compact />
				<div class="min-w-0 space-y-3">
					{#each limits as r (r.key)}
						<div class="space-y-1">
							<p class="flex items-baseline justify-between gap-3 text-[13px] text-fg">
								{r.title}
								{#if r.plan}<span class="font-mono text-[11px] text-fg-faint">{r.plan}</span>{/if}
							</p>
							{#if r.used !== null}
								<Meter value={r.used} warn={70} crit={90} class="h-1" />
							{/if}
							<p class="text-[12px] leading-5 text-fg-faint">{r.text}</p>
						</div>
					{/each}
					{#if claudeNoLimit}
						<p class="text-[12px] leading-5 text-fg-faint">
							Claude Code only records a limit when it refuses a request for being over one.
						</p>
					{/if}
				</div>
			</div>
			{#if summary.sessions.length}
				<div class="border-t border-line">
					<SessionsList sessions={summary.sessions} limit={5} {now} />
				</div>
			{/if}
			<p class="border-t border-line px-4 py-2.5 text-[12px] text-fg-faint">
				{plural(summary.totals.requests, 'request')}.
				<a
					href={resolve('/devices/coding')}
					class="text-fg-muted underline decoration-line-heavy underline-offset-4 hover:text-fg"
					>AI usage on every device</a
				>
			</p>
		{:else if !builtIn || reported}
			<p class="px-4 py-4 text-[13px] leading-5 text-fg-muted">
				Nothing yet. The agent reads the AI tools' files once a minute, after its next heartbeat, so
				the first numbers come soon after a session runs here.
			</p>
		{/if}
	{/if}
</Card>

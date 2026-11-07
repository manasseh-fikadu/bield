<script lang="ts">
	import ArrowLeft from '@lucide/svelte/icons/arrow-left';
	import Sparkles from '@lucide/svelte/icons/sparkles';
	import Info from '@lucide/svelte/icons/info';
	import { resolve } from '$app/paths';
	import { Poll } from '#lib/docker/poll.svelte.js';
	import { getCoding } from '#lib/coding/api.js';
	import Breakdown from '#lib/coding/Breakdown.svelte';
	import CodingChart from '#lib/coding/CodingChart.svelte';
	import {
		apiEstimate,
		apiEstimateHelp,
		codingRangeName,
		codingRanges,
		compactTokens,
		costParts,
		dollars,
		speedParts,
		tokenParts,
		type CodingRange,
		type Metric
	} from '#lib/coding/coding.js';
	import LimitsPanel from '#lib/coding/LimitsPanel.svelte';
	import SessionsList from '#lib/coding/SessionsList.svelte';
	import SourceNotes from '#lib/coding/SourceNotes.svelte';
	import ToolList from '#lib/coding/ToolList.svelte';
	import TotalsGrid from '#lib/coding/TotalsGrid.svelte';
	import { plural } from '#lib/format.js';
	import { live } from '#lib/live.svelte.js';
	import Card from '#lib/ui/Card.svelte';
	import EmptyState from '#lib/ui/EmptyState.svelte';
	import ErrorState from '#lib/ui/ErrorState.svelte';
	import PageHeader from '#lib/ui/PageHeader.svelte';
	import Segmented from '#lib/ui/Segmented.svelte';
	import ShareBar from '#lib/ui/ShareBar.svelte';
	import Skeleton from '#lib/ui/Skeleton.svelte';
	import TabPanel from '#lib/ui/TabPanel.svelte';
	import Tabs from '#lib/ui/Tabs.svelte';
	import Tooltip from '#lib/ui/Tooltip.svelte';

	let range: CodingRange = $state('7d');
	let tab = $state('cost');

	// A new poll for another range, which the effect starts and the old one's stop.
	const poll = $derived.by(() => {
		const r = range;
		return new Poll(() => getCoding(r), 60_000);
	});
	$effect(() => poll.start());

	const summary = $derived(poll.data);
	const devices = $derived(live.snapshot?.devices ?? []);
	const now = $derived(live.snapshot ? Date.parse(live.snapshot.generatedAt) : Date.now());

	function nameOf(id: string): string {
		return devices.find((d) => d.id === id)?.name ?? id;
	}

	const watched = $derived(summary?.devices.filter((d) => d.enabled) ?? []);
	const empty = $derived(
		!!summary &&
			summary.totals.tokens.total === 0 &&
			summary.totals.sessions === 0 &&
			summary.limits.length === 0
	);
	const tabs = $derived([
		{ value: 'cost', label: 'Cost' },
		{ value: 'tokens', label: 'Tokens' },
		{ value: 'limits', label: 'Limits' }
	]);
</script>

{#snippet overview(metric: Metric)}
	{#if summary}
		{@const t = summary.totals}
		<div class="grid gap-x-12 gap-y-8 lg:grid-cols-[minmax(0,19rem)_minmax(0,1fr)]">
			<div class="min-w-0">
				<p class="font-mono text-[36px] leading-11 font-medium tracking-[-0.02em] text-fg">
					{metric === 'cost' ? dollars(t.cost.total) : compactTokens(t.tokens.total)}
				</p>
				<p class="mt-1 flex flex-wrap items-center gap-x-1.5 text-[13px] text-fg-subtle">
					{plural(t.sessions, 'session')} ·
					{#if metric === 'cost'}
						{apiEstimate}
						<Tooltip content={apiEstimateHelp} side="bottom">
							{#snippet trigger({ props })}
								<button
									type="button"
									{...props}
									aria-label="What an API estimate is"
									class="inline-flex size-5 items-center justify-center rounded-full text-fg-faint hover:text-fg pointer-coarse:size-8"
								>
									<Info class="size-3.5" />
								</button>
							{/snippet}
						</Tooltip>
					{:else}
						{plural(t.requests, 'request')}
					{/if}
				</p>
				<div class="mt-6">
					<ToolList tools={summary.tools} totals={t} {metric} />
				</div>
			</div>
			<div class="min-w-0">
				<h3 class="mb-3 text-[14px] font-medium text-fg">
					{summary.step === 'day' ? 'Daily' : 'Hourly'}
					{metric === 'cost' ? 'cost' : 'tokens'}
				</h3>
				<CodingChart {summary} {metric} />
			</div>
		</div>

		<section class="mt-10">
			<h3 class="mb-3 text-[14px] font-medium text-fg">Totals</h3>
			<TotalsGrid totals={t} />
		</section>

		<div class="mt-10 grid gap-x-12 gap-y-8 md:grid-cols-2">
			<section>
				<h3 class="mb-3 text-[14px] font-medium text-fg">
					{metric === 'cost' ? 'Cost by type' : 'Tokens by type'}
				</h3>
				<ShareBar
					label={metric === 'cost' ? 'Cost by type' : 'Tokens by type'}
					segments={metric === 'cost'
						? costParts(t.cost).map((p) => ({ ...p, text: dollars(p.value) }))
						: tokenParts(t.tokens).map((p) => ({ ...p, text: compactTokens(p.value) }))}
				/>
			</section>
			{#if metric === 'cost'}
				<section>
					<div class="mb-3 flex items-baseline justify-between gap-3">
						<h3 class="text-[14px] font-medium text-fg">Cost by speed</h3>
						{#if t.speedPremium > 0}
							<span class="text-[12px] text-fg-faint">Premium {dollars(t.speedPremium)}</span>
						{/if}
					</div>
					<ShareBar
						label="Cost by speed"
						segments={speedParts(t.cost.total, t.fastCost).map((p) => ({
							...p,
							text: dollars(p.value)
						}))}
					/>
				</section>
			{/if}
		</div>

		<section class="mt-10">
			<Breakdown {summary} {metric} {nameOf} fleet />
		</section>

		{#if summary.sessions.length}
			<Card title="Recent sessions" class="mt-10">
				<SessionsList sessions={summary.sessions} {nameOf} {now} limit={12} />
			</Card>
		{/if}

		{#if t.unpricedTokens > 0}
			<p class="mt-6 text-[12px] leading-5 text-fg-faint">
				{compactTokens(t.unpricedTokens)} tokens are of models without a price, so they count as nothing
				in the cost. Prices are from {summary.pricesAsOf}.
			</p>
		{:else}
			<p class="mt-6 text-[12px] leading-5 text-fg-faint">Prices are from {summary.pricesAsOf}.</p>
		{/if}
	{/if}
{/snippet}

<a
	href={resolve('/devices')}
	class="mb-4 inline-flex items-center gap-1.5 text-[13px] text-fg-subtle transition-colors hover:text-fg pointer-coarse:-mt-2 pointer-coarse:mb-2 pointer-coarse:py-2"
>
	<ArrowLeft class="size-3.5" /> Devices
</a>
<PageHeader
	title="AI usage"
	description="What the AI tools on your machines used, for coding or anything else: tokens, what that would cost on the model makers' APIs, and where their limits stand."
>
	{#snippet actions()}
		<Segmented bind:value={range} options={codingRanges} label="Time range" />
	{/snippet}
</PageHeader>

{#if poll.error && !summary}
	<Card>
		<ErrorState
			title="Couldn't load AI usage"
			message={poll.error}
			icon={Sparkles}
			onretry={() => poll.reload()}
		/>
	</Card>
{:else if !summary}
	<div class="space-y-6">
		<Skeleton class="h-10 w-64" />
		<Skeleton class="h-64 rounded-lg" />
		<Skeleton class="h-24 rounded-lg" />
	</div>
{:else if empty}
	<Card>
		{#if watched.length === 0}
			<EmptyState title="No AI tools are watched yet" icon={Sparkles}>
				Open a machine on the <a
					href={resolve('/devices')}
					class="text-fg underline decoration-line-heavy underline-offset-4">Devices</a
				>
				page and turn on <span class="text-fg">Watch AI tools</span>. Its agent then reads the
				session files of Claude Code, Codex, OpenCode and Pi once a minute and reports counts of
				tokens. Prompts and replies never leave the machine. It needs agent 1.4.0 or newer, which
				<a
					href={resolve('/devices/agents')}
					class="text-fg underline decoration-line-heavy underline-offset-4">Agents</a
				> installs and updates.
			</EmptyState>
		{:else}
			<EmptyState title="No AI sessions in the last {codingRangeName(range)}" icon={Sparkles}>
				Bield watches the AI tools on {plural(watched.length, 'machine')}: {watched
					.map((d) => nameOf(d.deviceId))
					.join(', ')}. Their agents report once a minute, so the first numbers come soon after a
				session runs. They need agent 1.4.0 or newer. Try a longer range if you were away.
			</EmptyState>
		{/if}
	</Card>
	{#if summary.sources.length}
		<div class="mt-4">
			<SourceNotes sources={summary.sources} where={nameOf} />
		</div>
	{/if}
{:else}
	<div class="space-y-6">
		<SourceNotes sources={summary.sources} where={nameOf} />
		<Tabs bind:value={tab} {tabs}>
			<!-- Only the tab on show is drawn, since each is a chart and a table. -->
			<TabPanel value="cost" class="pt-6">
				{#if tab === 'cost'}{@render overview('cost')}{/if}
			</TabPanel>
			<TabPanel value="tokens" class="pt-6">
				{#if tab === 'tokens'}{@render overview('tokens')}{/if}
			</TabPanel>
			<TabPanel value="limits" class="pt-6">
				{#if tab === 'limits'}<LimitsPanel {summary} {nameOf} {now} />{/if}
			</TabPanel>
		</Tabs>
		{#if watched.length < devices.filter((d) => d.agent).length}
			<p class="text-[12px] leading-5 text-fg-faint">
				Only {plural(watched.length, 'machine')} with an agent {watched.length === 1 ? 'is' : 'are'} watched.
				An admin turns it on for the others on their device pages.
			</p>
		{/if}
	</div>
{/if}

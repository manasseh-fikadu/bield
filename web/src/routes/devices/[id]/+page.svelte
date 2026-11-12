<script lang="ts">
	import BatteryCharging from '@lucide/svelte/icons/battery-charging';
	import BatteryMedium from '@lucide/svelte/icons/battery-medium';
	import Bot from '@lucide/svelte/icons/bot';
	import Monitor from '@lucide/svelte/icons/monitor';
	import { resolve } from '$app/paths';
	import { page } from '$app/state';
	import AdminOnly from '#lib/AdminOnly.svelte';
	import { agentRuns } from '#lib/agent.js';
	import { activeAlerts, onDevice } from '#lib/alerts.js';
	import AlertActions from '#lib/alerts/AlertActions.svelte';
	import type { Alert, Device } from '#lib/api/types.js';
	import AIUsageLink from '#lib/coding/AIUsageLink.svelte';
	import type { Glance } from '#lib/coding/coding.js';
	import DeviceCoding from '#lib/coding/DeviceCoding.svelte';
	import { levelText, limitsOf } from '#lib/device.js';
	import AddAgent from '#lib/devices/AddAgent.svelte';
	import DeviceActions from '#lib/devices/DeviceActions.svelte';
	import DeviceHistory from '#lib/devices/DeviceHistory.svelte';
	import DeviceSettingsDialog from '#lib/devices/DeviceSettingsDialog.svelte';
	import EditorCard from '#lib/devices/EditorCard.svelte';
	import RunningNow from '#lib/devices/RunningNow.svelte';
	import ServicesCard from '#lib/devices/ServicesCard.svelte';
	import SSHCard from '#lib/devices/SSHCard.svelte';
	import TagChips from '#lib/devices/TagChips.svelte';
	import UsagePanel from '#lib/devices/UsagePanel.svelte';
	import {
		ago,
		bytes,
		dateTime,
		latency,
		osLabel,
		percent,
		rate,
		ratio,
		shortDate,
		shortDuration
	} from '#lib/format.js';
	import type { HistoryRange } from '#lib/history.js';
	import { live } from '#lib/live.svelte.js';
	import type { Status } from '#lib/status.js';
	import { routeName, tailnetIP } from '#lib/tailnet.js';
	import Button from '#lib/ui/Button.svelte';
	import Card from '#lib/ui/Card.svelte';
	import DataRow from '#lib/ui/DataRow.svelte';
	import EmptyState from '#lib/ui/EmptyState.svelte';
	import Skeleton from '#lib/ui/Skeleton.svelte';
	import StatusDot from '#lib/ui/StatusDot.svelte';
	import type { UsageMetric } from '#lib/usage.js';

	const device = $derived(live.snapshot?.devices.find((d) => d.id === page.params.id));
	const now = $derived(live.snapshot ? Date.parse(live.snapshot.generatedAt) : Date.now());
	const devices = $derived(live.snapshot?.devices ?? []);
	// On the machine Bield runs on, which can be two nodes, either's alerts
	// and services count.
	const alerts = $derived(
		device ? activeAlerts(onDevice(live.snapshot?.alerts ?? [], device, devices)) : []
	);
	const units = $derived(device ? onDevice(live.snapshot?.units ?? [], device, devices) : []);
	const containers = $derived(
		device ? onDevice(live.snapshot?.containers ?? [], device, devices) : []
	);

	let editing = $state(false);
	// What the AI usage card says, for the link at the top.
	let glance: Glance | null = $state(null);
	let range: HistoryRange = $state('7d');
	let usageOpen = $state(false);
	let usageMetric: UsageMetric = $state('cpu');
	let usageAt = $state<number | null>(null);

	/** Opens what's using the device's CPU or memory, now or at a time. */
	function showUsage(metric: UsageMetric, at: number | null = null) {
		usageMetric = metric;
		usageAt = at;
		usageOpen = true;
	}

	/** Whether the device's metrics are out of date, from an agent that stopped or a device that's off. */
	function stale(d: Device): boolean {
		return d.measuredBy === 'agent' && (!d.online || !!d.agent?.stale);
	}

	const alertLamps: Record<Alert['severity'], Status> = {
		critical: 'offline',
		warning: 'attention',
		info: 'online'
	};

	/** What an alert's line says after its title. */
	function alertLine(a: Alert): string {
		const seen = a.ackedAt
			? ` Acknowledged${a.ackedBy ? ` by ${a.ackedBy}` : ''}, ${ago(a.ackedAt, now)}.`
			: '';
		return `${a.detail.replace(/\.?$/, '.')}${seen}`;
	}

	function ipv6(d: Device): string | undefined {
		return d.ips.find((ip) => ip.includes(':'));
	}

	/** When a key expires, and how long until then or since. */
	function expiry(t: string): string {
		const when = ago(t, now);
		return `${shortDate(t)} (${when.startsWith('in ') ? when.slice(3) : when})`;
	}
</script>

{#snippet agentsLink(text = 'Agents')}
	<a
		href={resolve('/devices/agents')}
		class="text-fg-muted underline decoration-line-heavy underline-offset-4 hover:text-fg">{text}</a
	>
{/snippet}

{#snippet stat(label: string, value: string, sub?: string, tone = 'text-fg', onclick?: () => void)}
	{#snippet body()}
		<span class="text-[12px] leading-4 text-fg-faint">{label}</span>
		<span class={['truncate font-mono text-[20px] leading-6.5', tone]}>{value}</span>
		{#if sub}
			<span class="truncate text-[12px] leading-4 text-fg-faint">{sub}</span>
		{/if}
	{/snippet}
	{#if onclick}
		<button
			type="button"
			{onclick}
			aria-label="{label}: {value}. See what's using it"
			class="-mt-px -ml-px flex min-w-0 flex-col gap-1.5 border-t border-l border-line px-4 py-3.5 text-left transition-colors hover:bg-hover/50 focus-visible:rounded-none sm:px-5 xl:first:pl-6"
		>
			{@render body()}
		</button>
	{:else}
		<div
			class="-mt-px -ml-px flex min-w-0 flex-col gap-1.5 border-t border-l border-line px-4 py-3.5 sm:px-5 xl:first:pl-6"
		>
			{@render body()}
		</div>
	{/if}
{/snippet}

{#if device}
	{@const m = device.metrics}
	{@const info = device.info}
	{@const limits = limitsOf(device, live.snapshot!.hub.thresholds)}
	{@const memory = ratio(m?.memory?.used, m?.memory?.total)}
	{@const disk = ratio(m?.disk?.used, m?.disk?.total)}
	<div class="flex flex-wrap items-end justify-between gap-x-6 gap-y-3 px-4 pt-5.5 pb-4.5 sm:px-6">
		<div class="flex min-w-0 flex-col gap-1">
			<h1 class="truncate text-[24px] leading-7.5 font-medium tracking-[-0.02em] text-fg">
				{device.name}
			</h1>
			<p class="font-mono text-[12px] leading-4 text-fg-faint">
				{[device.dnsName, info?.osVersion || osLabel(device.os), info?.model]
					.filter(Boolean)
					.join(' · ')}{device.self ? ' · this hub' : ''}
			</p>
			<AIUsageLink {glance} />
		</div>
		<div class="flex flex-wrap items-center gap-1.5">
			<DeviceActions {device} />
			<AdminOnly>
				<Button variant="ghost" onclick={() => (editing = true)}>Edit</Button>
			</AdminOnly>
		</div>
	</div>

	{#each alerts as a (a.id)}
		<div
			class="flex flex-wrap items-center gap-x-2.5 gap-y-1 border-t border-line px-4 py-1.5 sm:px-6"
		>
			<StatusDot
				status={alertLamps[a.severity]}
				breathe={a.state === 'open' && a.severity !== 'info'}
				filled
			/>
			<span class="text-[13px] leading-5 font-medium text-fg">{a.title.replace(/\.?$/, '.')}</span>
			<span class="min-w-0 flex-1 basis-60 text-[13px] leading-5 text-fg-muted">{alertLine(a)}</span
			>
			<AlertActions alert={a} plain />
		</div>
	{:else}
		{#if device.status !== 'online'}
			<div class="flex flex-wrap items-center gap-x-2.5 border-t border-line px-4 py-2.5 sm:px-6">
				<StatusDot status={device.status} quiet={!device.online} />
				<span class="text-[13px] leading-5 font-medium text-fg">{device.statusReason}</span>
				{#if !device.online && device.lastSeen}
					<span class="text-[13px] leading-5 text-fg-muted"
						>Last seen {ago(device.lastSeen, now)}.</span
					>
				{/if}
			</div>
		{/if}
	{/each}
	{#if stale(device) && device.agent}
		<p class="border-t border-line px-4 py-2.5 text-[13px] leading-5 text-fg-muted sm:px-6">
			These numbers are from {ago(device.agent.lastSeen, now)}, the agent's last report.
		</p>
	{/if}

	{#if m}
		<div class={['overflow-hidden border-t border-line', stale(device) && 'opacity-60']}>
			<div class="grid grid-cols-2 md:grid-cols-3 xl:grid-cols-6">
				{@render stat(
					'CPU',
					percent(m.cpu, 1),
					m.load ? `Load ${m.load[0].toFixed(2)}` : undefined,
					levelText(m.cpu, limits.cpu),
					() => showUsage('cpu')
				)}
				{@render stat(
					'Memory',
					percent(memory),
					`${bytes(m.memory?.used)} of ${bytes(m.memory?.total)}`,
					levelText(memory, limits.memory),
					() => showUsage('mem')
				)}
				{@render stat(
					'Disk',
					percent(disk),
					m.disk ? `${bytes(m.disk.total - m.disk.used)} free` : undefined,
					levelText(disk, limits.disk, limits.diskCritical)
				)}
				{@render stat('Network', `↓${rate(m.netRx)}`, `↑${rate(m.netTx)}`)}
				{@render stat(
					'Uptime',
					shortDuration(m.uptime),
					m.procs ? `${m.procs} processes` : undefined
				)}
				{#if m.battery}
					{@const b = m.battery}
					<div
						class="-mt-px -ml-px flex min-w-0 flex-col gap-1.5 border-t border-l border-line px-4 py-3.5 sm:px-5"
					>
						<span class="text-[12px] leading-4 text-fg-faint">Battery</span>
						<span class="flex items-center gap-2 font-mono text-[20px] leading-6.5 text-fg">
							{#if b.charging}
								<BatteryCharging class="size-5 text-fg-muted" />
							{:else}
								<BatteryMedium class="size-5 text-fg-muted" />
							{/if}
							{percent(b.percent)}
						</span>
						<span class="truncate text-[12px] leading-4 text-fg-faint">
							{b.charging ? 'Charging' : b.onAc ? 'On power' : 'On battery'}
						</span>
					</div>
				{:else if m.temp !== undefined}
					{@render stat('Temperature', `${m.temp.toFixed(0)}°C`)}
				{:else if m.swap}
					{@render stat(
						'Swap',
						percent(ratio(m.swap.used, m.swap.total)),
						`${bytes(m.swap.used)} of ${bytes(m.swap.total)}`
					)}
				{/if}
			</div>
		</div>
	{/if}

	{#if device.agent && !device.measuredBy}
		<div class="border-t border-line px-4 py-4 sm:px-6">
			<Card
				title="Waiting for the agent"
				description="The agent enrolled {ago(
					device.agent.enrolledAt,
					now
				)}, and its first heartbeat hasn't come yet."
			>
				<p class="p-4 text-[13px] text-fg-muted">
					On {device.name}, its log says why: on macOS,
					<code class="font-mono text-[12px]">~/Library/Logs/bield-agent.log</code>, and on Linux,
					<code class="font-mono text-[12px]">journalctl --user -u bield-agent</code>, or
					<code class="font-mono text-[12px]">journalctl -u bield-agent</code> if root installed it.
				</p>
			</Card>
		</div>
	{:else if !device.measuredBy}
		<div class="border-t border-line px-4 py-4 sm:px-6">
			<Card
				title={agentRuns(device.os)
					? 'Install the agent for CPU, memory and disk'
					: 'No agent on this machine'}
				description="Tailscale shows whether it's connected and how the hub reaches it. The agent adds CPU, memory, disk, battery and what's running."
			>
				<div class="space-y-3 p-4 text-[13px] text-fg-muted">
					{#if agentRuns(device.os)}
						<AdminOnly>
							<p>
								Run one command on {device.name}, or let Bield run it over SSH once it can log in.
								It installs a small background service for your user, without root.
							</p>
							<AddAgent {device} label="Install the agent" variant="primary" />
							{#snippet fallback()}
								<p>An admin can install the agent from the {@render agentsLink()} page.</p>
							{/snippet}
						</AdminOnly>
					{:else}
						<p>
							The agent runs on macOS and Linux. On {osLabel(device.os)}, Bield shows what Tailscale
							knows.
						</p>
					{/if}
				</div>
			</Card>
		</div>
	{/if}

	<div class="flex flex-col border-t border-line xl:flex-row">
		<div class="min-w-0 flex-1 px-4 pt-3.5 pb-5 sm:px-6">
			<DeviceHistory
				{device}
				bind:range
				onSelect={device.measuredBy ? (ts) => showUsage('cpu', ts) : undefined}
			/>
		</div>
		<div class="flex shrink-0 flex-col border-t border-line xl:w-72 xl:border-t-0 xl:border-l">
			{#if device.settings.notes}
				<div class="flex flex-col gap-1 border-b border-line px-4 py-3.5">
					<h2 class="text-[13px] leading-5 font-medium text-fg">Your note</h2>
					<p class="text-[13px] leading-5 whitespace-pre-wrap text-fg-muted">
						{device.settings.notes}
					</p>
				</div>
			{/if}
			<div class="flex flex-col gap-2.25 px-4 py-3.5">
				<h2 class="pb-0.75 text-[13px] leading-5 font-medium text-fg">Tailscale</h2>
				<dl class="contents">
					<DataRow flat label="Tailnet IP" copy={tailnetIP(device)}
						>{tailnetIP(device) ?? '—'}</DataRow
					>
					{#if ipv6(device) && ipv6(device) !== tailnetIP(device)}
						<DataRow flat label="IPv6" copy={ipv6(device)}>{ipv6(device)}</DataRow>
					{/if}
					{#if device.tags.length}
						<div class="flex items-start justify-between gap-3">
							<dt class="shrink-0 text-[12px] leading-5 text-fg-faint">Tags</dt>
							<dd class="min-w-0"><TagChips tags={device.tags} class="justify-end" /></dd>
						</div>
					{/if}
					<DataRow flat label="Route">
						{routeName(device).toLowerCase()}
						{#if live.snapshot?.relay.supported && live.snapshot.relay.derp.includes(device.id)}
							·
							<a
								href={resolve('/devices/relay')}
								class="underline decoration-line-heavy underline-offset-4 hover:text-fg"
								>try a peer relay</a
							>
						{/if}
					</DataRow>
					{#if device.path.endpoint}
						<DataRow flat label="Endpoint">{device.path.endpoint}</DataRow>
					{/if}
					{#if device.path.peerRelay}
						<DataRow flat label="Peer relay">{device.path.peerRelay}</DataRow>
					{/if}
					{#if device.path.directAt && device.path.kind !== 'direct' && !device.self}
						<DataRow flat label="Last direct">{ago(device.path.directAt, now)}</DataRow>
					{/if}
					<DataRow flat label="Home relay">{device.path.homeDerp ?? '—'}</DataRow>
					{#if !device.self}
						<DataRow flat label="RTT">
							{device.path.unanswered ? 'no reply' : latency(device.path.rttMs)}
						</DataRow>
						<DataRow flat label="Traffic">↓{bytes(device.rxBytes)} ↑{bytes(device.txBytes)}</DataRow
						>
					{/if}
					{#if device.exitNode}
						<DataRow flat label="Exit node">Bield's traffic</DataRow>
					{/if}
					<DataRow flat label="Key expiry">
						{device.keyExpiry ? expiry(device.keyExpiry) : 'disabled'}
					</DataRow>
					<DataRow flat label="Added">{shortDate(device.created)}</DataRow>
				</dl>
			</div>
		</div>
	</div>

	{#if device.measuredBy}
		<RunningNow
			deviceId={device.id}
			docker={device.docker}
			{units}
			{now}
			onall={() => showUsage('cpu')}
		/>
	{/if}

	<div class="grid items-start gap-4 border-t border-line p-4 sm:p-6 lg:grid-cols-2">
		<Card title="System">
			{#snippet actions()}
				{#if device.agent?.version}
					<span class="flex items-center gap-1 font-mono text-[11px] text-fg-faint">
						<Bot class="size-3" /> agent v{device.agent.version}
					</span>
				{:else if device.measuredBy === 'hub'}
					<span class="font-mono text-[11px] text-fg-faint">measured by the hub</span>
				{/if}
			{/snippet}
			{#if info}
				<dl>
					<DataRow label="Hostname">{info.hostname || device.hostName}</DataRow>
					<DataRow label="OS">{info.osVersion || osLabel(device.os)}</DataRow>
					{#if info.kernel}
						<!-- Only admins get the kernel. -->
						<DataRow label="Kernel">{info.kernel}</DataRow>
					{/if}
					<DataRow label="Model">{info.model || '—'}</DataRow>
					<DataRow label="Arch">{info.arch || '—'}</DataRow>
					<DataRow label="Cores">{info.cores || '—'}</DataRow>
					{#if m}
						<DataRow label="Last report">{ago(m.at, now)} · {dateTime(m.at)}</DataRow>
					{/if}
					{#if device.agent && (device.agent.outdated || !device.agent.enrolledAt)}
						{@const enrolled = !!device.agent.enrolledAt}
						<DataRow label="Agent">
							<span class="flex items-center justify-end gap-2">
								<span class="text-amber">{enrolled ? 'Update available' : 'Not enrolled'}</span>
								<AdminOnly>
									<AddAgent
										{device}
										label={enrolled ? 'Update' : 'Re-enroll'}
										size="xs"
										variant="ghost"
									/>
								</AdminOnly>
							</span>
						</DataRow>
					{/if}
				</dl>
			{:else}
				<p class="px-4 py-4 text-[13px] text-fg-faint">No system details without the agent.</p>
			{/if}
		</Card>
		{#if device.ssh}
			<!-- Only admins get device.ssh. -->
			<SSHCard {device} {now} />
			<EditorCard {device} {now} />
		{/if}
		{#if device.measuredBy === 'hub' && (live.snapshot?.collectors ?? []).some((c) => c.name === 'systemd units')}
			<ServicesCard {units} {containers} {now} />
		{/if}
		<DeviceCoding {device} {now} bind:glance />
	</div>
	<AdminOnly>
		<DeviceSettingsDialog {device} bind:open={editing} />
	</AdminOnly>
	{#if device.measuredBy}
		<UsagePanel
			{device}
			bind:open={usageOpen}
			bind:metric={usageMetric}
			bind:at={usageAt}
			bind:range
		/>
	{/if}
{:else if live.snapshot}
	<EmptyState title="No device here" icon={Monitor}>
		It may have left the tailnet. <a
			href={resolve('/devices')}
			class="text-fg underline underline-offset-4">See every device</a
		>.
	</EmptyState>
{:else}
	<div class="space-y-6 p-4 sm:p-6">
		<div class="space-y-2">
			<Skeleton class="h-7 w-48" />
			<Skeleton class="h-4 w-72 max-w-full" />
		</div>
		<div class="grid gap-4 xl:grid-cols-[minmax(0,1fr)_288px]">
			<Skeleton class="h-64 rounded-lg" />
			<Skeleton class="h-64 rounded-lg" />
		</div>
	</div>
{/if}

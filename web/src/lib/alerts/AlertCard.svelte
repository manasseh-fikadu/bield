<!--
	An alert: what's wrong, since when, and on which device. Admins can
	acknowledge it, snooze it and write notes on it.
-->
<script lang="ts">
	import Check from '@lucide/svelte/icons/check';
	import CircleAlert from '@lucide/svelte/icons/circle-alert';
	import Info from '@lucide/svelte/icons/info';
	import MoonStar from '@lucide/svelte/icons/moon-star';
	import OctagonAlert from '@lucide/svelte/icons/octagon-alert';
	import { resolve } from '$app/paths';
	import { severityMeta } from '#lib/alerts.js';
	import type { Alert, AlertSeverity, Device } from '#lib/api/types.js';
	import { ago, dateTime } from '#lib/format.js';
	import type { Icon } from '#lib/icon.js';
	import Badge from '#lib/ui/Badge.svelte';
	import AlertActions from './AlertActions.svelte';

	interface Props {
		alert: Alert;
		/** The device it's on, if it's on one. */
		device?: Device;
		/** The hub's time, in Unix milliseconds, for "5m ago". */
		now: number;
	}

	let { alert, device, now }: Props = $props();

	const icons: Record<AlertSeverity, Icon> = {
		critical: OctagonAlert,
		warning: CircleAlert,
		info: Info
	};

	const meta = $derived(severityMeta[alert.severity]);
	const resolved = $derived(alert.state === 'resolved');
	const SeverityIcon = $derived(icons[alert.severity]);
</script>

<div class={['rounded-lg border p-3', resolved ? 'border-line bg-surface' : meta.box]}>
	<div class="flex items-start gap-2.5">
		<SeverityIcon
			class={['mt-0.5 size-4 shrink-0', resolved ? 'text-fg-faint' : meta.text]}
			strokeWidth={1.8}
		/>
		<div class="min-w-0 flex-1">
			<div class="flex items-start justify-between gap-2">
				<p class={['text-[13px] leading-5 font-medium', resolved ? 'text-fg-muted' : 'text-fg']}>
					<span class="sr-only">{meta.label}:</span>
					{alert.title}
				</p>
				<Badge tone={resolved ? 'gray' : meta.tone} class="mt-px">
					{resolved ? 'Resolved' : meta.label}
				</Badge>
			</div>
			{#if alert.detail}
				<p class="mt-0.5 text-[12.5px] leading-5 break-words text-fg-subtle">
					{alert.detail}
				</p>
			{/if}
			<div
				class="mt-1.5 flex flex-wrap items-center gap-x-2 gap-y-0.5 font-mono text-[11px] text-fg-faint"
			>
				<span title={dateTime(alert.openedAt)}>Opened {ago(alert.openedAt, now)}</span>
				{#if alert.resolvedAt}
					<span title={dateTime(alert.resolvedAt)}>· Resolved {ago(alert.resolvedAt, now)}</span>
				{/if}
				{#if device}
					<span>·</span>
					<a
						href={resolve('/devices/[id]', { id: device.id })}
						class="text-fg-subtle hover:text-fg hover:underline pointer-coarse:-my-2 pointer-coarse:py-2"
					>
						{device.name}
					</a>
				{/if}
				{#if alert.checkId}
					<span>·</span>
					<a
						href="{resolve('/checks')}?check={encodeURIComponent(alert.checkId)}"
						class="text-fg-subtle hover:text-fg hover:underline"
					>
						Check
					</a>
				{/if}
			</div>
			{#if alert.ackedAt && !resolved}
				<p class="mt-1.5 flex items-center gap-1 font-mono text-[11px] text-fg-subtle">
					<Check class="size-3" />
					Acknowledged{alert.ackedBy ? ` by ${alert.ackedBy}` : ''} · {ago(alert.ackedAt, now)}
				</p>
			{/if}
			{#if alert.state === 'snoozed' && alert.snoozedUntil}
				<p class="mt-1.5 flex items-center gap-1 font-mono text-[11px] text-fg-subtle">
					<MoonStar class="size-3" />
					Snoozed until {dateTime(alert.snoozedUntil)}{alert.snoozedBy
						? ` by ${alert.snoozedBy}`
						: ''}
				</p>
			{/if}
			<AlertActions {alert} class="mt-2.5" />
		</div>
	</div>
</div>

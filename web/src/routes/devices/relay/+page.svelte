<!--
	Bield's suggestion of a peer relay: the devices it reaches through DERP,
	the machine that could relay for them instead, and the two steps to set it
	up, which Bield can't take itself.
-->
<script lang="ts">
	import ArrowLeft from '@lucide/svelte/icons/arrow-left';
	import Waypoints from '@lucide/svelte/icons/waypoints';
	import { resolve } from '$app/paths';
	import type { Device } from '#lib/api/types.js';
	import { deviceIcon, kindLabel } from '#lib/device.js';
	import { latency, plural, shortDuration } from '#lib/format.js';
	import { live } from '#lib/live.svelte.js';
	import { defaultRelayPort, relayCommand, relayGrant, validPort } from '#lib/relay.js';
	import NumberInput from '#lib/settings/NumberInput.svelte';
	import { routeName, tailnetIP } from '#lib/tailnet.js';
	import Card from '#lib/ui/Card.svelte';
	import CodeLine from '#lib/ui/CodeLine.svelte';
	import CopyButton from '#lib/ui/CopyButton.svelte';
	import EmptyState from '#lib/ui/EmptyState.svelte';
	import Field from '#lib/ui/Field.svelte';
	import PageHeader from '#lib/ui/PageHeader.svelte';
	import Select from '#lib/ui/Select.svelte';
	import Skeleton from '#lib/ui/Skeleton.svelte';

	interface Candidate {
		device: Device;
		publicAddress: boolean;
	}

	const relay = $derived(live.snapshot?.relay);
	const byId = $derived(new Map((live.snapshot?.devices ?? []).map((d) => [d.id, d])));
	/** The devices Bield reaches through DERP now. */
	const derp = $derived((relay?.derp ?? []).flatMap((id) => byId.get(id) ?? []));
	/** The devices that could be the relay, best first. */
	const candidates = $derived(
		(relay?.candidates ?? []).flatMap((c): Candidate[] => {
			const device = byId.get(c.deviceId);
			return device ? [{ device, publicAddress: c.publicAddress }] : [];
		})
	);

	/** The candidate picked in the list, or '' for Bield's first choice. */
	let choice = $state('');
	const picked = $derived(candidates.find((c) => c.device.id === choice) ?? candidates[0]);
	let port = $state(defaultRelayPort);
	const relayPort = $derived(validPort(port) ? port : defaultRelayPort);
	/** The addresses of the devices the relay is for: not its own. */
	const src = $derived(
		derp.flatMap((d) => (d.id === picked?.device.id ? [] : (tailnetIP(d) ?? [])))
	);
	const dst = $derived(picked ? tailnetIP(picked.device) : undefined);
	/** What to run on the relay and add to the policy file, once there's a relay and devices for it. */
	const command = $derived(picked ? relayCommand(picked.device.os, relayPort) : '');
	const grant = $derived(dst && src.length ? relayGrant(src, dst) : '');

	/** Why a candidate makes a good relay, such as "Server · public address · direct 12 ms · up 41d". */
	function reasons(c: Candidate): string {
		const out: string[] = [];
		if (c.device.kind !== 'other') {
			out.push(kindLabel(c.device.kind));
		}
		if (c.publicAddress) {
			out.push('public address');
		}
		out.push(`direct ${latency(c.device.path.rttMs)} from Bield`);
		const up = c.device.metrics?.uptime;
		if (up) {
			out.push(`up ${shortDuration(up)}`);
		}
		return out.join(' · ');
	}
</script>

<a
	href={resolve('/devices')}
	class="mb-4 inline-flex items-center gap-1.5 text-[13px] text-fg-subtle transition-colors hover:text-fg pointer-coarse:-mt-2 pointer-coarse:mb-2 pointer-coarse:py-2"
>
	<ArrowLeft class="size-3.5" /> Devices
</a>
<PageHeader
	title="Peer relay"
	description="A machine of yours that carries traffic for devices that can't connect directly, in place of Tailscale's shared DERP servers. It's often faster."
/>

{#if !live.snapshot || !relay}
	<Card>
		<div class="space-y-2 p-4">
			<Skeleton class="h-4 w-48" />
			<Skeleton class="h-3 w-72" />
		</div>
	</Card>
{:else if !relay.supported}
	<Card>
		<EmptyState title="Peer relays need Tailscale" icon={Waypoints}>
			This tailnet uses another coordination server, such as Headscale, which has no released
			support for peer relays yet, so Bield doesn't suggest one.
		</EmptyState>
	</Card>
{:else}
	<div class="grid grid-cols-[minmax(0,1fr)] gap-4 xl:grid-cols-[minmax(0,1fr)_400px]">
		<div class="space-y-4">
			<Card
				title="Through DERP now"
				description={derp.length
					? `Bield reaches ${plural(derp.length, 'device')} through one of Tailscale's shared relays.`
					: undefined}
			>
				{#if derp.length === 0}
					<p class="p-4 text-[13px] text-fg-muted">
						Bield reaches every online device directly or through a peer relay, so none needs one
						now.
						{#if candidates.length}
							If one falls back to DERP, the best machine to relay for it is
							<a
								href={resolve('/devices/[id]', { id: candidates[0].device.id })}
								class="text-fg underline decoration-line-heavy underline-offset-4"
								>{candidates[0].device.name}</a
							>.
						{/if}
					</p>
				{:else}
					<ul class="divide-y divide-line">
						{#each derp as d (d.id)}
							{@const Icon = deviceIcon(d)}
							<li class="flex items-center gap-3 px-4 py-2.5">
								<Icon class="size-4 shrink-0 text-fg-faint" strokeWidth={1.6} />
								<a
									href={resolve('/devices/[id]', { id: d.id })}
									class="min-w-0 flex-1 truncate text-[13px] font-medium text-fg hover:underline"
								>
									{d.name}
								</a>
								<span class="shrink-0 text-[12px] text-fg-muted">{routeName(d)}</span>
								<span class="w-16 shrink-0 text-right font-mono text-[12px] text-fg-muted">
									{latency(d.path.rttMs)}
								</span>
							</li>
						{/each}
					</ul>
				{/if}
			</Card>

			{#if derp.length}
				<Card title="Set one up">
					{#if !picked}
						<p class="p-4 text-[13px] text-fg-muted">
							No device fits. A relay takes an online computer that Bield reaches directly, best a
							server at a public address. Phones and tablets can use a relay but can't be one.
						</p>
					{:else}
						<div class="space-y-5 p-4">
							<div class="grid gap-3 sm:grid-cols-[minmax(0,1fr)_10rem]">
								<Field label="Relay" hint={reasons(picked)}>
									<Select bind:value={() => picked.device.id, (v) => (choice = v)}>
										{#each candidates as c, i (c.device.id)}
											<option value={c.device.id}>
												{c.device.name}{i === 0 ? ' (suggested)' : ''}
											</option>
										{/each}
									</Select>
								</Field>
								<Field label="UDP port">
									<NumberInput
										bind:value={port}
										unit=""
										label="UDP port"
										min={1}
										max={65535}
										placeholder={String(defaultRelayPort)}
									/>
								</Field>
							</div>
							{#if !grant}
								<p class="text-[13px] text-fg-muted">
									{picked.device.name} is the only device on DERP, so it has nobody to relay for.
								</p>
							{:else}
								<ol class="space-y-5 text-[13px] text-fg-muted">
									<li class="space-y-2">
										<p>
											<span class="font-medium text-fg">1.</span> On {picked.device.name}, open UDP
											port {relayPort} to the internet in its firewall, and in any cloud firewall in front
											of it. Then run:
										</p>
										<CodeLine {command} />
									</li>
									<li class="space-y-2">
										<p>
											<span class="font-medium text-fg">2.</span> In the Tailscale admin console,
											open
											<a
												href="https://login.tailscale.com/admin/acls/file"
												target="_blank"
												rel="noreferrer"
												class="text-fg underline decoration-line-heavy underline-offset-4"
												>Access controls</a
											>
											and add this to <code class="font-mono text-[12px]">"grants"</code>. It lets
											the devices on DERP use the relay.
										</p>
										<div class="flex items-start gap-2 rounded-lg border border-line bg-canvas p-3">
											<pre
												class="min-w-0 flex-1 overflow-x-auto font-mono text-[12.5px] text-fg">{grant}</pre>
											<CopyButton value={grant} label="Copy grant" />
										</div>
									</li>
									<li>
										<span class="font-medium text-fg">3.</span> Within a minute or two, the
										<a
											href={resolve('/devices')}
											class="text-fg underline decoration-line-heavy underline-offset-4">Devices</a
										> page should show them on a peer relay.
									</li>
								</ol>
							{/if}
						</div>
					{/if}
				</Card>
			{/if}
		</div>

		<Card title="What Bield can't see">
			<div class="space-y-3 p-4 text-[13px] text-fg-muted">
				<p>
					Bield only sees its own connections. It knows how it reaches each device, not how they
					reach each other, so it can't tell whether a relay will help them, or whether they can
					reach the relay at all.
				</p>
				<p>
					It suggests a computer that it reaches directly, preferring a server at a public address
					that hasn't just restarted, close to Bield. It can't tell a machine's own Tailscale from
					an app's built-in node, such as Bield's, which has no command to run, so pick another in
					the list if that happens. Bield can't change your policy file or run the command for you.
				</p>
				<p>
					Both ends need Tailscale 1.86 or newer. Phones and tablets can use a relay but can't be
					one. Tailscale's
					<a
						href="https://tailscale.com/kb/1591/peer-relays"
						target="_blank"
						rel="noreferrer"
						class="text-fg underline decoration-line-heavy underline-offset-4"
						>guide to peer relays</a
					> has the rest.
				</p>
			</div>
		</Card>
	</div>
{/if}

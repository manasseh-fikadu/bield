<!--
	VS Code in the browser on a device: code-server, which Bield installs and
	runs over SSH as the user it logs in as, and serves on a port of its own.
	The editor opens in a tab of its own, since that port is another origin.
	Only admins see it.
-->
<script lang="ts">
	import CircleAlert from '@lucide/svelte/icons/circle-alert';
	import Ellipsis from '@lucide/svelte/icons/ellipsis';
	import SquareArrowOutUpRight from '@lucide/svelte/icons/square-arrow-out-up-right';
	import TriangleAlert from '@lucide/svelte/icons/triangle-alert';
	import { getEditor, installEditor, removeEditor, stopEditor } from '#lib/api/client.js';
	import type { Device, DeviceEditor, EditorInstall, EditorRun } from '#lib/api/types.js';
	import { canUpdate, lowOnSpace } from '#lib/editor.js';
	import { openEditor } from '#lib/editor/open.js';
	import { ago, bytes } from '#lib/format.js';
	import { live } from '#lib/live.svelte.js';
	import { toast } from '#lib/toast.svelte.js';
	import Button from '#lib/ui/Button.svelte';
	import Card from '#lib/ui/Card.svelte';
	import DataRow from '#lib/ui/DataRow.svelte';
	import Dialog from '#lib/ui/Dialog.svelte';
	import IconButton from '#lib/ui/IconButton.svelte';
	import Menu from '#lib/ui/Menu.svelte';
	import MenuItem from '#lib/ui/MenuItem.svelte';
	import Skeleton from '#lib/ui/Skeleton.svelte';
	import StatusDot from '#lib/ui/StatusDot.svelte';

	interface Props {
		/** A device Bield has SSH access to set up for. */
		device: Device;
		/** The hub's clock. */
		now: number;
	}

	let { device, now }: Props = $props();

	// A prop's device is a new object with each snapshot, so effects read its
	// ID, which isn't. Each answer is an SSH round trip to the device.
	const deviceId = $derived(device.id);

	let editor = $state.raw<DeviceEditor | null>(null);
	/** Why the first answer for this device didn't come. */
	let loadError = $state<string | null>(null);
	let busy = $state<'install' | 'open' | 'confirm' | null>(null);
	/** What the admin is asked to confirm, while confirmOpen. */
	let confirm = $state<'stop' | 'remove'>('stop');
	let confirmOpen = $state(false);
	let keepData = $state(true);
	/** What the remove printed, if it failed. */
	let removeFailed = $state<EditorRun | null>(null);

	const install = $derived(editor?.install);
	const installing = $derived(install?.running === true);
	const updatable = $derived(editor !== null && canUpdate(editor));
	/** How big the download and the install are, if the device said, as the end of a sentence. */
	const sizes = $derived(
		editor?.download !== undefined && editor.disk !== undefined
			? `: about ${bytes(editor.download)}, and it takes about ${bytes(editor.disk)} of space`
			: ''
	);
	const openBy = $derived(editor?.openBy.join(', ') ?? '');

	/** Counts the requests for an answer, so one that a newer request overtook is dropped. */
	let seq = 0;

	/**
	 * Asks device `id` what it has. The answer is dropped if the card shows
	 * another device by then, such as after an action that took a while.
	 */
	async function load(id: string) {
		const mine = ++seq;
		const wanted = () => mine === seq && id === deviceId;
		try {
			const next = await getEditor(id);
			if (!wanted()) {
				return;
			}
			const was = editor?.install;
			editor = next;
			loadError = null;
			if (was?.running && next.install && !next.install.running) {
				finished(next.install);
			}
		} catch (err) {
			if (wanted()) {
				loadError = err instanceof Error ? err.message : String(err);
			}
		}
	}

	/** Says how an install the card watched went. A failure's output stays in the card. */
	function finished(done: EditorInstall) {
		if (done.ok) {
			toast(`Installed code-server ${done.version} on ${device.name}`);
		} else {
			toast(`Installing code-server on ${device.name} failed`, 'error');
		}
	}

	$effect(() => {
		const id = deviceId;
		editor = null;
		loadError = null;
		void load(id);
		return () => {
			seq++;
		};
	});

	// While an install runs, ask again 2 s after each answer, until it's over
	// or the card goes.
	$effect(() => {
		if (!installing) {
			return;
		}
		const id = deviceId;
		let stopped = false;
		let timer: ReturnType<typeof setTimeout>;
		const tick = async () => {
			await load(id);
			if (!stopped) {
				timer = setTimeout(tick, 2000);
			}
		};
		timer = setTimeout(tick, 2000);
		return () => {
			stopped = true;
			clearTimeout(timer);
		};
	});

	async function startInstall() {
		const id = deviceId;
		busy = 'install';
		try {
			const started = await installEditor(id);
			if (id === deviceId && editor) {
				// Shows the install at once, and drops an older answer on its way.
				seq++;
				editor = { ...editor, install: started };
			}
		} catch {
			// The client showed the error as a toast. If an install runs
			// already, the device's answer shows it.
			void load(id);
		} finally {
			busy = null;
		}
	}

	async function open() {
		const id = deviceId;
		busy = 'open';
		try {
			await openEditor(id);
		} finally {
			busy = null;
		}
		void load(id);
	}

	function ask(what: 'stop' | 'remove') {
		confirm = what;
		keepData = true;
		removeFailed = null;
		confirmOpen = true;
	}

	async function confirmed() {
		const id = deviceId;
		busy = 'confirm';
		try {
			if (confirm === 'stop') {
				await stopEditor(id);
				confirmOpen = false;
			} else {
				const ran = await removeEditor(id, keepData);
				if (ran.ok) {
					confirmOpen = false;
					toast(`Removed VS Code from ${device.name}`);
				} else {
					removeFailed = ran;
				}
			}
		} catch {
			// The client showed the error as a toast.
		} finally {
			busy = null;
		}
		void load(id);
	}
</script>

{#snippet spinner()}
	<span
		class="size-3.5 shrink-0 animate-spin rounded-full border-2 border-current border-t-transparent text-fg-subtle"
		aria-hidden="true"
	></span>
{/snippet}

{#snippet installRunning(i: EditorInstall)}
	<div class="space-y-1">
		<p class="flex items-center gap-2 text-[13px] text-fg-muted" role="status">
			{@render spinner()}
			<span>
				{editor?.state === 'installed' ? 'Updating to' : 'Installing'} code-server {i.version},
				started
				{ago(i.startedAt, now)} by {i.by}…
			</span>
		</p>
		<p class="pl-5.5 text-[12px] text-fg-subtle">
			It goes on if you leave this page, and can take a few minutes.
		</p>
	</div>
{/snippet}

{#snippet installFailed(i: EditorInstall)}
	<div class="space-y-2">
		<p class="text-[13px] text-red-fg" role="alert">
			{editor?.state === 'installed' ? 'The update to' : 'Installing'} code-server {i.version} failed
			{ago(i.endedAt ?? i.startedAt, now)}.
		</p>
		{#if i.output}
			<pre
				class="max-h-48 overflow-auto rounded-md border border-line bg-canvas px-3 py-2 font-mono text-[11.5px] whitespace-pre-wrap text-fg-muted">{i.output}</pre>
		{/if}
	</div>
{/snippet}

{#snippet openVSX()}
	<p class="text-[12px] text-fg-subtle">
		Extensions come from Open VSX, not Microsoft's marketplace, so Copilot, Remote-SSH and Live
		Share aren't there.
	</p>
{/snippet}

{#snippet note(tone: 'amber' | 'red' | 'gray', text: string)}
	<p
		class={[
			'flex items-start gap-2 border-b px-4 py-2.5 text-[12.5px]',
			tone === 'amber' && 'border-amber/25 bg-amber/5 text-amber-fg',
			tone === 'red' && 'border-red/30 bg-red/5 text-red-fg',
			tone === 'gray' && 'border-line bg-hover/50 text-fg-muted'
		]}
	>
		{#if tone === 'amber'}
			<TriangleAlert class="mt-0.5 size-3.5 shrink-0" />
		{:else}
			<CircleAlert class="mt-0.5 size-3.5 shrink-0" />
		{/if}
		<span class="min-w-0 break-words">{text}</span>
	</p>
{/snippet}

<div id="editor" class="scroll-mt-24">
	<Card title="VS Code">
		{#snippet actions()}
			{#if editor?.state === 'installed' && !installing}
				<Menu>
					{#snippet trigger({ props })}
						<IconButton label="VS Code options" size="sm" {...props}>
							<Ellipsis class="size-4" />
						</IconButton>
					{/snippet}
					<MenuItem disabled={busy !== null} onSelect={startInstall}>
						{updatable ? `Update to ${editor.latest}` : 'Reinstall'}
					</MenuItem>
					{#if editor.running}
						<MenuItem disabled={busy !== null} onSelect={() => ask('stop')}>Stop</MenuItem>
					{/if}
					<MenuItem danger disabled={busy !== null} onSelect={() => ask('remove')}>Remove</MenuItem>
				</Menu>
			{/if}
		{/snippet}

		{#if !editor}
			{#if loadError}
				<p class="px-4 py-4 text-[13px] text-fg-subtle">{loadError}</p>
			{:else}
				<div class="space-y-3 p-4" aria-busy="true">
					<Skeleton class="h-4 w-3/4" />
					<Skeleton class="h-4 w-1/2" />
					<Skeleton class="h-7.5 w-32" />
				</div>
			{/if}
		{:else if editor.state === 'unavailable'}
			<p class="px-4 py-4 text-[13px] text-fg-subtle">
				{editor.reason ?? "Bield can't run VS Code on this machine."}
			</p>
		{:else if editor.state === 'installed'}
			{#if editor.error}
				{@render note('gray', editor.error)}
			{/if}
			{#if editor.listenError}
				{@render note('red', editor.listenError)}
			{/if}
			{#if !editor.secure && editor.url}
				{@render note(
					'amber',
					'Bield is served over plain HTTP here, so the browser turns off parts of VS Code, such as webviews and the clipboard. ' +
						(live.snapshot?.hub.controlUrl
							? "This tailnet's control server gives no HTTPS certificates, as Headscale doesn't."
							: 'Turn on HTTPS in your tailnet to get them.')
				)}
			{/if}
			<dl>
				<DataRow label="Status">
					<!-- Long values wrap, so a phone's narrow card stays inside the page. -->
					<span class="flex min-w-0 items-center justify-end gap-2 whitespace-normal">
						<StatusDot status={editor.running ? 'online' : 'unknown'} />
						<span>{editor.running ? 'Running' : 'Stopped'}</span>
					</span>
				</DataRow>
				<DataRow label="Version">
					<span class="flex items-center justify-end gap-2">
						<span>code-server {editor.version ?? 'unknown'}</span>
						{#if updatable && !installing}
							<Button
								size="xs"
								variant="ghost"
								disabled={busy !== null}
								loading={busy === 'install'}
								onclick={startInstall}
							>
								Update to {editor.latest}
							</Button>
						{/if}
					</span>
				</DataRow>
				{#if editor.url}
					<DataRow label="Address" copy={editor.url}>
						<span class="font-mono break-all whitespace-normal">{editor.url}</span>
					</DataRow>
				{/if}
				{#if openBy}
					<DataRow label="Open by">{openBy}</DataRow>
				{/if}
			</dl>
			<div class="space-y-3 border-t border-line p-3">
				{#if install?.running}
					{@render installRunning(install)}
				{:else}
					{#if install && !install.ok}
						{@render installFailed(install)}
					{/if}
					<Button
						variant="primary"
						disabled={busy !== null}
						loading={busy === 'open'}
						onclick={open}
					>
						{#snippet icon()}<SquareArrowOutUpRight class="size-3.5" />{/snippet}
						Open VS Code
					</Button>
				{/if}
				{@render openVSX()}
			</div>
		{:else}
			<!-- Not installed, or installed and gone from the device. -->
			<div class="space-y-3 p-4 text-[13px] text-fg-muted">
				{#if editor.error}
					<p class="flex items-start gap-2 text-fg-subtle">
						<CircleAlert class="mt-0.5 size-3.5 shrink-0" />
						<span class="min-w-0 break-words">{editor.error}</span>
					</p>
				{/if}
				{#if editor.state === 'missing'}
					<p>Bield installed VS Code here, and its files are gone from the device.</p>
				{:else}
					<p>
						Run VS Code on this device in your browser, as
						<span class="font-mono text-fg">{device.ssh?.user ?? 'the user Bield logs in as'}</span
						>.
					</p>
					<p class="text-[12px] text-fg-subtle">
						Downloads code-server {editor.latest} from GitHub on the device{sizes}.
					</p>
					{#if lowOnSpace(editor)}
						<p class="flex items-start gap-2 text-[12px] text-amber-fg">
							<TriangleAlert class="mt-0.5 size-3.5 shrink-0" />
							The home folder has only {bytes(editor.free)} free.
						</p>
					{/if}
				{/if}
				{#if install?.running}
					{@render installRunning(install)}
				{:else}
					{#if install && !install.ok}
						{@render installFailed(install)}
					{/if}
					<Button
						variant="primary"
						disabled={busy !== null}
						loading={busy === 'install'}
						onclick={startInstall}
					>
						{install && !install.ok
							? 'Try again'
							: editor.state === 'missing'
								? 'Install again'
								: 'Install VS Code'}
					</Button>
				{/if}
				{#if editor.state === 'not-installed'}
					{@render openVSX()}
				{/if}
			</div>
		{/if}
	</Card>
</div>

<Dialog
	bind:open={confirmOpen}
	title={confirm === 'stop'
		? `Stop VS Code on ${device.name}?`
		: `Remove VS Code from ${device.name}?`}
>
	{#if confirm === 'stop'}
		<p class="text-[13px] text-fg-muted">
			VS Code closes for everyone who has it open{openBy ? ` now: ${openBy}` : ''}. Save your work
			there first. It starts again when someone opens it.
		</p>
	{:else}
		<div class="space-y-3 text-[13px] text-fg-muted">
			<p>
				Bield stops VS Code on {device.name} and deletes code-server from
				<span class="font-mono text-[12px] text-fg">~/.local/share/bield/code-server</span>.
			</p>
			<label class="flex cursor-pointer items-center gap-2 text-fg">
				<input
					type="checkbox"
					class="size-4 shrink-0 cursor-pointer accent-blue"
					bind:checked={keepData}
				/>
				Keep its settings and extensions on the device
			</label>
			{#if removeFailed}
				<p class="text-red-fg" role="alert">Removing it failed.</p>
				{#if removeFailed.output}
					<pre
						class="max-h-48 overflow-auto rounded-md border border-line bg-canvas px-3 py-2 font-mono text-[11.5px] whitespace-pre-wrap text-fg-muted">{removeFailed.output}</pre>
				{/if}
			{/if}
		</div>
	{/if}
	{#snippet footer()}
		<Button variant="ghost" onclick={() => (confirmOpen = false)}>Cancel</Button>
		<Button variant="danger" loading={busy === 'confirm'} onclick={confirmed}>
			{confirm === 'stop' ? 'Stop' : removeFailed ? 'Try again' : 'Remove'}
		</Button>
	{/snippet}
</Dialog>

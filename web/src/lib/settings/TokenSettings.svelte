<!--
	The API tokens tab: the tokens there are, expired and revoked ones too, with what each
	may do and when it was last used. Admins make tokens here and revoke them.
	A token's secret is shown once, when it's made.
-->
<script lang="ts">
	import { onMount, tick } from 'svelte';
	import KeyRound from '@lucide/svelte/icons/key-round';
	import Plus from '@lucide/svelte/icons/plus';
	import Trash2 from '@lucide/svelte/icons/trash-2';
	import { resolve } from '$app/paths';
	import { ApiError, getTokens, revokeToken } from '#lib/api/client.js';
	import { RoleAdmin, type APIToken, type SettingsView } from '#lib/api/types.js';
	import { roleLabel } from '#lib/access.js';
	import { ago, dateTime, shortDate } from '#lib/format.js';
	import { live } from '#lib/live.svelte.js';
	import { actsAsNote, tokenDone, tokenHintLabel } from '#lib/tokens.js';
	import Badge from '#lib/ui/Badge.svelte';
	import Button from '#lib/ui/Button.svelte';
	import Card from '#lib/ui/Card.svelte';
	import Dialog from '#lib/ui/Dialog.svelte';
	import EmptyState from '#lib/ui/EmptyState.svelte';
	import ErrorState from '#lib/ui/ErrorState.svelte';
	import IconButton from '#lib/ui/IconButton.svelte';
	import Skeleton from '#lib/ui/Skeleton.svelte';
	import TokenDialog from './TokenDialog.svelte';

	interface Props {
		/** For the address the CLI reaches Bield at. */
		view: SettingsView;
	}

	let { view }: Props = $props();

	const id = $props.id();

	let tokens = $state.raw<APIToken[] | null>(null);
	let failure = $state('');
	let loading = $state(false);

	/** Counts the loads, so only the latest one counts. */
	let latest = 0;

	async function load() {
		const mine = ++latest;
		loading = true;
		try {
			const list = await getTokens();
			if (mine === latest) {
				tokens = list;
				failure = '';
			}
		} catch (err) {
			if (mine === latest) {
				failure = err instanceof Error ? err.message : String(err);
			}
		} finally {
			if (mine === latest) {
				loading = false;
			}
		}
	}

	onMount(() => {
		void load();
	});

	const byName = (a: APIToken, b: APIToken) =>
		a.name.toLowerCase().localeCompare(b.name.toLowerCase());

	/** The hub's clock, as of its latest snapshot. */
	const now = $derived(live.snapshot ? Date.parse(live.snapshot.generatedAt) : Date.now());
	const hubUrl = $derived(view.settings.general.publicUrl || location.origin);
	const names = $derived(tokens?.map((t) => t.name) ?? []);

	let makeOpen = $state(false);

	function made(t: APIToken) {
		tokens = [...(tokens ?? []).filter((x) => x.id !== t.id), t].sort(byName);
	}

	// The question before revoking. The token stays after the dialog closes,
	// so its text doesn't change while it fades out.
	let revoking = $state.raw<APIToken | null>(null);
	let revokeOpen = $state(false);
	let answering = $state(false);

	function ask(t: APIToken) {
		revoking = t;
		revokeOpen = true;
	}

	async function revoke() {
		if (!revoking) {
			return;
		}
		const t = revoking;
		answering = true;
		let done = false;
		try {
			await revokeToken(t.id);
			done = true;
		} catch (err) {
			// The client showed the error. A token that's not there any more, or
			// was revoked by someone else, is done all the same.
			done = err instanceof ApiError && (err.status === 404 || err.status === 409);
		} finally {
			answering = false;
			revokeOpen = false;
		}
		if (done) {
			// A revoked token stays in the list, with when and who, so ask again.
			try {
				tokens = await getTokens();
			} catch {
				tokens = (tokens ?? []).filter((x) => x.id !== t.id);
			}
			// The button the dialog would give focus back to is gone.
			await tick();
			if (matchMedia('(pointer: fine)').matches) {
				document.getElementById(`${id}-make`)?.focus();
			}
		}
	}
</script>

{#snippet plus()}
	<Plus class="size-3.5" />
{/snippet}

<Card
	title="API tokens"
	description="Tokens let scripts, CI and AI agents on tagged machines use Bield's API and the bield CLI. They can't open terminals, make tokens or change who has access."
>
	{#snippet actions()}
		{#if tokens && tokens.length > 0}
			<Button id="{id}-make" variant="primary" icon={plus} onclick={() => (makeOpen = true)}>
				Make token
			</Button>
		{/if}
	{/snippet}
	{#if tokens === null && failure === ''}
		<div class="space-y-2 p-4" aria-busy="true">
			<Skeleton class="h-14" />
			<Skeleton class="h-14" />
		</div>
	{:else if tokens === null}
		<ErrorState
			title="Couldn't load the tokens"
			message={failure}
			{loading}
			onretry={() => void load()}
		/>
	{:else if tokens.length === 0}
		<EmptyState title="No API tokens yet" icon={KeyRound}>
			People use Bield as themselves, through Tailscale. Make a token for a program that runs on a
			tagged machine, where Tailscale has no person to name.
			<div class="mt-3">
				<Button id="{id}-make" variant="primary" icon={plus} onclick={() => (makeOpen = true)}>
					Make token
				</Button>
			</div>
		</EmptyState>
	{:else}
		<ul class="divide-y divide-line">
			{#each tokens as t (t.id)}
				{@const note = actsAsNote(t)}
				{@const done = tokenDone(t)}
				<li class="flex items-start gap-3 px-4 py-3">
					<span
						class={[
							'mt-0.5 flex size-8 shrink-0 items-center justify-center rounded-lg border border-line bg-raised',
							done ? 'text-fg-faint' : 'text-fg-muted'
						]}
					>
						<KeyRound class="size-4" />
					</span>
					<div class="min-w-0 flex-1">
						<div class="flex flex-wrap items-center gap-x-2 gap-y-1">
							<span
								class={['truncate text-[14px] font-medium', done ? 'text-fg-subtle' : 'text-fg']}
							>
								{t.name}
							</span>
							<Badge tone={t.role === RoleAdmin ? 'amber' : 'gray'}>{roleLabel(t.role)}</Badge>
							{#if t.revokedAt}
								<Badge tone="gray">Revoked</Badge>
							{:else if t.expired}
								<Badge tone="red">Expired</Badge>
							{/if}
						</div>
						<p class="text-[12.5px] text-fg-subtle">
							<span class="font-mono text-[12px]">{tokenHintLabel(t.hint)}</span>
							· Made by {t.createdBy} on {shortDate(t.createdAt)}
							·
							<span title={dateTime(t.expiresAt)}
								>{t.expired ? 'Expired' : 'Expires'} {shortDate(t.expiresAt)}</span
							>
						</p>
						{#if t.revokedAt}
							<p class="text-[12.5px] text-fg-subtle">
								Revoked <span title={dateTime(t.revokedAt)}>{ago(t.revokedAt, now)}</span>
								by {t.revokedBy}
							</p>
						{/if}
						<p class="text-[12.5px] text-fg-subtle">
							{#if t.lastUsedAt}
								Last used <span title={dateTime(t.lastUsedAt)}>{ago(t.lastUsedAt, now)}</span>
								{#if t.lastUsedFrom}
									from
									{#if t.lastUsedDeviceId}
										<a
											href={resolve('/devices/[id]', { id: t.lastUsedDeviceId })}
											class="text-fg-muted hover:text-fg hover:underline">{t.lastUsedFrom}</a
										>
									{:else}
										{t.lastUsedFrom}
									{/if}
								{/if}
							{:else}
								Never used
							{/if}
						</p>
						{#if note}
							<p class="mt-0.5 text-[12px] text-amber-fg">{note}</p>
						{/if}
					</div>
					{#if !done}
						<IconButton label="Revoke {t.name}" onclick={() => ask(t)}>
							<Trash2 class="size-4" />
						</IconButton>
					{/if}
				</li>
			{/each}
		</ul>
		<p class="border-t border-line px-4 py-2.5 text-[12.5px] text-fg-subtle">
			Who made and revoked tokens is in the
			<a href="{resolve('/activity')}?kind=access" class="text-fg-muted underline hover:text-fg">
				activity log</a
			>, and so is each change a token made, under its maker's name.
		</p>
	{/if}
</Card>

<TokenDialog bind:open={makeOpen} taken={names} {hubUrl} onmade={made} />

<Dialog
	bind:open={revokeOpen}
	title="Revoke {revoking?.name ?? 'the token'}?"
	description="Scripts using it stop working at once."
>
	<p class="text-[13px] text-fg-muted">
		Bield refuses it from now on, and it can't be brought back. It stays in this list as revoked,
		and its name isn't used again, so the activity log always means one token. Make a new token if
		you need one.
	</p>
	{#snippet footer()}
		<Button variant="ghost" onclick={() => (revokeOpen = false)}>Cancel</Button>
		<Button variant="danger" loading={answering} onclick={() => void revoke()}>Revoke</Button>
	{/snippet}
</Dialog>
